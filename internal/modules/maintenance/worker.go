package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/auth"
)

type Worker struct {
	queries     *db.Queries
	pool        *pgxpool.Pool
	mailer      auth.Mailer
	authService *auth.Service
	location    *time.Location
}

func NewWorker(queries *db.Queries, pool *pgxpool.Pool, mailer auth.Mailer, authService *auth.Service) *Worker {
	return &Worker{
		queries:     queries,
		pool:        pool,
		mailer:      mailer,
		authService: authService,
		location:    ResolveKyivTimezone(),
	}
}

// ResolveKyivTimezone finds the Europe/Kyiv IANA timezone or falls back to fixed UTC+2.
func ResolveKyivTimezone() *time.Location {
	for _, name := range []string{"Europe/Kyiv", "Europe/Kiev"} {
		loc, err := time.LoadLocation(name)
		if err == nil {
			return loc
		}
	}
	return time.FixedZone("EET", 2*60*60)
}

// TimeUntilNext0900Kyiv computes the duration until the next 09:00 in Kyiv timezone.
func TimeUntilNext0900Kyiv(now time.Time, loc *time.Location) time.Duration {
	nowKyiv := now.In(loc)
	target := time.Date(nowKyiv.Year(), nowKyiv.Month(), nowKyiv.Day(), 9, 0, 0, 0, loc)
	if !nowKyiv.Before(target) {
		target = target.Add(24 * time.Hour)
	}
	return target.Sub(nowKyiv)
}

// RunOnce runs both passes (expiry digest and stale registration cleanup) once.
func (w *Worker) RunOnce(ctx context.Context) error {
	slog.Info("Running daily maintenance pass...")

	if err := w.SendExpiryDigests(ctx); err != nil {
		slog.Error("Failed to send expiry digests", "error", err)
	}

	if w.authService != nil {
		cleanCtx, cleanCancel := context.WithTimeout(ctx, 10*time.Second)
		defer cleanCancel()
		if err := w.authService.CleanExpiredPendingRegistrations(cleanCtx); err != nil {
			slog.Warn("Failed to clean expired pending registrations", "error", err)
		} else {
			slog.Info("Successfully cleaned expired pending registrations")
		}
	}

	slog.Info("Daily maintenance pass completed")
	return nil
}

// SendExpiryDigests queries products expiring in <= 3 days and sends an email digest to each affected user.
func (w *Worker) SendExpiryDigests(ctx context.Context) error {
	now := time.Now().In(w.location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, w.location)
	threshold := today.AddDate(0, 0, 3)

	var thresholdDate pgtype.Date
	thresholdDate.Time = threshold
	thresholdDate.Valid = true

	rows, err := w.queries.ListExpiringProductsForDigest(ctx, thresholdDate)
	if err != nil {
		return fmt.Errorf("failed to query expiring products: %w", err)
	}

	if len(rows) == 0 {
		slog.Info("No expiring products found for daily digest")
		return nil
	}

	// Group expiring items by user email
	type userDigest struct {
		name  string
		items []auth.ExpiringProductDigestItem
	}
	digestsByUser := make(map[string]*userDigest)

	for _, row := range rows {
		email := strings.TrimSpace(row.UserEmail)
		if email == "" {
			continue
		}

		expiryStr := "невідомо"
		isExpired := false
		if row.ExpiryDate.Valid {
			expTime := row.ExpiryDate.Time
			expiryStr = expTime.Format("2006-01-02")
			if expTime.Before(today) {
				isExpired = true
			}
		}

		item := auth.ExpiringProductDigestItem{
			ProductName: row.ProductName,
			FridgeName:  row.FridgeName,
			Quantity:    row.Quantity,
			Unit:        row.Unit,
			ExpiryDate:  expiryStr,
			IsExpired:   isExpired,
		}

		d, exists := digestsByUser[email]
		if !exists {
			d = &userDigest{name: row.UserName}
			digestsByUser[email] = d
		}
		d.items = append(d.items, item)
	}

	sentCount := 0
	for email, d := range digestsByUser {
		if err := w.mailer.SendExpiringProductsEmail(ctx, email, d.name, d.items); err != nil {
			slog.Error("Failed to send expiring products digest to user", "email", email, "error", err)
		} else {
			sentCount++
		}
	}

	slog.Info("Daily expiry digest sent", "total_users", len(digestsByUser), "successful_sent", sentCount)
	return nil
}

// Start begins the background maintenance worker loop.
func (w *Worker) Start(ctx context.Context) {
	slog.Info("Starting DailyMaintenanceWorker (fires daily at 09:00 Europe/Kyiv)")

	// Optional initial registration cleanup on startup
	if w.authService != nil {
		cleanCtx, cleanCancel := context.WithTimeout(ctx, 10*time.Second)
		_ = w.authService.CleanExpiredPendingRegistrations(cleanCtx)
		cleanCancel()
	}

	// Secondary hourly ticker for expired pending registrations
	hourlyTicker := time.NewTicker(1 * time.Hour)
	defer hourlyTicker.Stop()

	for {
		delay := TimeUntilNext0900Kyiv(time.Now(), w.location)
		slog.Info("DailyMaintenanceWorker sleeping until next 09:00 Europe/Kyiv tick", "duration", delay.String())

		dailyTimer := time.NewTimer(delay)

		select {
		case <-ctx.Done():
			dailyTimer.Stop()
			slog.Info("DailyMaintenanceWorker stopped")
			return

		case <-hourlyTicker.C:
			dailyTimer.Stop()
			if w.authService != nil {
				cCtx, cCancel := context.WithTimeout(ctx, 10*time.Second)
				_ = w.authService.CleanExpiredPendingRegistrations(cCtx)
				cCancel()
			}

		case <-dailyTimer.C:
			tickCtx, tickCancel := context.WithTimeout(ctx, 2*time.Minute)
			if err := w.RunOnce(tickCtx); err != nil {
				slog.Error("DailyMaintenanceWorker 09:00 tick failed", "error", err)
			}
			tickCancel()
		}
	}
}
