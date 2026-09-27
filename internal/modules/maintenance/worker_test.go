package maintenance_test

import (
	"context"
	"testing"
	"time"

	"github.com/ynshvrh/E-Fridge-Api/internal/modules/auth"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/maintenance"
)

type testMockMailer struct {
	sentDigests []struct {
		toEmail string
		name    string
		items   []auth.ExpiringProductDigestItem
	}
}

func (m *testMockMailer) SendVerificationEmail(ctx context.Context, toEmail, name, code string) error {
	return nil
}

func (m *testMockMailer) SendGoogleWelcomeEmail(ctx context.Context, toEmail, name, generatedPassword string) error {
	return nil
}

func (m *testMockMailer) SendExpiringProductsEmail(ctx context.Context, toEmail, name string, items []auth.ExpiringProductDigestItem) error {
	m.sentDigests = append(m.sentDigests, struct {
		toEmail string
		name    string
		items   []auth.ExpiringProductDigestItem
	}{toEmail, name, items})
	return nil
}

func TestResolveKyivTimezone(t *testing.T) {
	loc := maintenance.ResolveKyivTimezone()
	if loc == nil {
		t.Fatal("expected non-nil timezone location")
	}
}

func TestTimeUntilNext0900Kyiv(t *testing.T) {
	loc := maintenance.ResolveKyivTimezone()

	// Case 1: At 08:00 today, should be 1 hour until 09:00
	t1 := time.Date(2026, 9, 27, 8, 0, 0, 0, loc)
	delay1 := maintenance.TimeUntilNext0900Kyiv(t1, loc)
	if delay1 != 1*time.Hour {
		t.Errorf("expected 1 hour delay, got %v", delay1)
	}

	// Case 2: At 09:00 today, target should be 09:00 tomorrow (24 hours)
	t2 := time.Date(2026, 9, 27, 9, 0, 0, 0, loc)
	delay2 := maintenance.TimeUntilNext0900Kyiv(t2, loc)
	if delay2 != 24*time.Hour {
		t.Errorf("expected 24 hour delay, got %v", delay2)
	}

	// Case 3: At 10:00 today, should be 23 hours until 09:00 tomorrow
	t3 := time.Date(2026, 9, 27, 10, 0, 0, 0, loc)
	delay3 := maintenance.TimeUntilNext0900Kyiv(t3, loc)
	if delay3 != 23*time.Hour {
		t.Errorf("expected 23 hour delay, got %v", delay3)
	}
}

func TestWorker_StartStopsOnContextCancel(t *testing.T) {
	worker := maintenance.NewWorker(nil, nil, &testMockMailer{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		worker.Start(ctx)
		close(done)
	}()

	// Immediately cancel context
	cancel()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("worker did not stop cleanly on context cancel")
	}
}
