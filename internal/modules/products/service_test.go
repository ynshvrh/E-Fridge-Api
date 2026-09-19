package products

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
)

func TestToDTOStatusCalculation(t *testing.T) {
	now := time.Now()
	expiredDate := now.AddDate(0, 0, -2)
	warningDate := now.AddDate(0, 0, 2)
	goodDate := now.AddDate(0, 0, 10)

	prodExpired := db.Product{
		ID:         uuid.New(),
		Name:       "Milk",
		Category:   "dairy",
		ExpiryDate: pgtype.Date{Time: expiredDate, Valid: true},
	}
	dtoExpired := toDTO(prodExpired)
	if dtoExpired.Status != "expired" {
		t.Errorf("expected status 'expired', got %s", dtoExpired.Status)
	}

	prodWarning := db.Product{
		ID:         uuid.New(),
		Name:       "Cheese",
		Category:   "dairy",
		ExpiryDate: pgtype.Date{Time: warningDate, Valid: true},
	}
	dtoWarning := toDTO(prodWarning)
	if dtoWarning.Status != "warning" {
		t.Errorf("expected status 'warning', got %s", dtoWarning.Status)
	}

	prodGood := db.Product{
		ID:         uuid.New(),
		Name:       "Yogurt",
		Category:   "dairy",
		ExpiryDate: pgtype.Date{Time: goodDate, Valid: true},
	}
	dtoGood := toDTO(prodGood)
	if dtoGood.Status != "good" {
		t.Errorf("expected status 'good', got %s", dtoGood.Status)
	}
}
