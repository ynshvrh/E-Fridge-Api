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

func TestParseQuantityAndUnit(t *testing.T) {
	tests := []struct {
		raw      string
		expected float64
		unit     string
	}{
		{"900 g", 900, "г"},
		{"1 l", 1, "л"},
		{"500 ml", 500, "мл"},
		{"33 cl", 330, "мл"},
		{"2.5 kg", 2.5, "кг"},
		{"10 шт", 10, "шт"},
		{"", 1, "шт"},
	}

	for _, tt := range tests {
		qty, unit := parseQuantityAndUnit(tt.raw)
		if qty != tt.expected || unit != tt.unit {
			t.Errorf("parseQuantityAndUnit(%q) = (%v, %s), expected (%v, %s)", tt.raw, qty, unit, tt.expected, tt.unit)
		}
	}
}

func TestMapCategoriesToCategory(t *testing.T) {
	tests := []struct {
		tags []string
		name string
		want string
	}{
		{[]string{"dairy"}, "Молоко 2.5%", "dairy"},
		{nil, "Куряче філе охолоджене", "meat-fish"},
		{[]string{"beverages"}, "Сік яблучний", "drinks"},
		{nil, "Хліб житній", "bakery"},
		{nil, "Свіжі огірки", "vegetables"},
	}

	for _, tt := range tests {
		got := mapCategoriesToCategory(tt.tags, tt.name)
		if got != tt.want {
			t.Errorf("mapCategoriesToCategory(%v, %q) = %q, want %q", tt.tags, tt.name, got, tt.want)
		}
	}
}

func TestHeuristicNutritionEstimate(t *testing.T) {
	svc := &Service{}
	est := svc.heuristicNutritionEstimate("Куряче філе", "г", 100)
	if est.Calories <= 0 || est.Protein <= 0 || est.Category != "meat-fish" {
		t.Errorf("unexpected chicken estimate: %+v", est)
	}

	eggEst := svc.heuristicNutritionEstimate("Яйце куряче", "шт", 1)
	if eggEst.StandardUnit != "шт" || eggEst.Calories <= 0 {
		t.Errorf("unexpected egg estimate: %+v", eggEst)
	}
}

