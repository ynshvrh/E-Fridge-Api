package planner

import (
	"testing"
	"time"

	"github.com/ynshvrh/E-Fridge-Api/internal/config"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
)

func TestGenerateFallbackPlan(t *testing.T) {
	svc := NewService(nil, &config.Config{})
	start := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	prods := []db.Product{
		{Name: "Яйця", Quantity: 10, Unit: "шт"},
		{Name: "Сир", Quantity: 200, Unit: "г"},
	}

	meals := svc.generateFallbackPlan(prods, 3, start, "")
	if len(meals) != 9 { // 3 days * 3 meals = 9
		t.Fatalf("expected 9 meals for 3 days, got %d", len(meals))
	}

	if meals[0].Date != "2026-09-20" {
		t.Errorf("expected start date 2026-09-20, got %s", meals[0].Date)
	}
	if meals[0].MealType != "breakfast" {
		t.Errorf("expected first meal breakfast, got %s", meals[0].MealType)
	}
	if meals[1].MealType != "lunch" {
		t.Errorf("expected second meal lunch, got %s", meals[1].MealType)
	}
	if meals[2].MealType != "dinner" {
		t.Errorf("expected third meal dinner, got %s", meals[2].MealType)
	}
}
