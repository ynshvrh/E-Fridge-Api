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

func TestGenerateFallbackDay(t *testing.T) {
	svc := NewService(nil, &config.Config{})
	date := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	prods := []db.Product{
		{Name: "Яйця курячі", Quantity: 6, Unit: "шт"},
		{Name: "Шпинат", Quantity: 100, Unit: "г"},
	}

	dayMeals := svc.generateFallbackDay(prods, date, "")
	if len(dayMeals) != 3 {
		t.Fatalf("expected exactly 3 meals for day, got %d", len(dayMeals))
	}

	expectedTypes := []string{"breakfast", "lunch", "dinner"}
	for i, m := range dayMeals {
		if m.MealType != expectedTypes[i] {
			t.Errorf("meal %d: expected type %s, got %s", i, expectedTypes[i], m.MealType)
		}
		if m.RecipeTitle == "" {
			t.Errorf("meal %d: expected non-empty title", i)
		}
		if m.Calories <= 0 {
			t.Errorf("meal %d: expected calories > 0, got %d", i, m.Calories)
		}
		if m.RecipeData == nil {
			t.Fatalf("meal %d: expected RecipeData not nil", i)
		}
		if m.RecipeData.PrepTimeMinutes <= 0 {
			t.Errorf("meal %d: expected prep time > 0, got %d", i, m.RecipeData.PrepTimeMinutes)
		}
		if len(m.RecipeData.Ingredients) == 0 {
			t.Errorf("meal %d: expected ingredients", i)
		}
		if len(m.RecipeData.Instructions) == 0 {
			t.Errorf("meal %d: expected instructions", i)
		}
	}
}

func TestGenerateFallbackMeal(t *testing.T) {
	svc := NewService(nil, &config.Config{})
	date := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	prods := []db.Product{}

	meal := svc.generateFallbackMeal(prods, date, "dinner", "")
	if meal == nil {
		t.Fatalf("expected non-nil meal")
	}
	if meal.MealType != "dinner" {
		t.Errorf("expected meal type dinner, got %s", meal.MealType)
	}
	if meal.RecipeData == nil {
		t.Fatalf("expected RecipeData not nil")
	}
	if len(meal.RecipeData.Ingredients) == 0 {
		t.Errorf("expected ingredients in RecipeData")
	}
}

func TestMatchFridgeIngredients(t *testing.T) {
	ingredients := []RecipeIngredient{
		{Name: "Яйця курячі", Amount: 2, Unit: "шт"},
		{Name: "Авокадо", Amount: 1, Unit: "шт"},
		{Name: "Сир пармезан", Amount: 50, Unit: "г"},
	}

	prods := []db.Product{
		{Name: "яйця", Quantity: 10, Unit: "шт"},
		{Name: "молоко", Quantity: 1, Unit: "л"},
		{Name: "сир", Quantity: 0, Unit: "г"}, // quantity 0 -> not in fridge
	}

	matched := matchFridgeIngredients(ingredients, prods)
	if !matched[0].InFridge {
		t.Errorf("expected eggs to be in fridge")
	}
	if matched[1].InFridge {
		t.Errorf("expected avocado NOT to be in fridge")
	}
	if matched[2].InFridge {
		t.Errorf("expected cheese with quantity 0 NOT to be in fridge")
	}
}
