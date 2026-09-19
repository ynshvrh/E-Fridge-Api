package nutrition

import (
	"testing"
)

func TestCalculateEstimatedNutrition(t *testing.T) {
	// 200g of chicken breast ("куряче філе", 165 kcal / 100g, 31g P, 3.6g F)
	cals, p, f, c := CalculateEstimatedNutrition("куряче філе", 200, "г")
	if cals != 330 {
		t.Errorf("expected 330 kcal, got %d", cals)
	}
	if p != 62.0 {
		t.Errorf("expected 62.0g protein, got %v", p)
	}
	if f != 7.2 {
		t.Errorf("expected 7.2g fat, got %v", f)
	}
	if c != 0.0 {
		t.Errorf("expected 0.0g carbs, got %v", c)
	}

	// 2 eggs (each ~50g -> 100g, 143 kcal)
	eggCals, eggP, _, _ := CalculateEstimatedNutrition("яйця", 2, "шт")
	if eggCals != 143 {
		t.Errorf("expected 143 kcal for 2 eggs, got %d", eggCals)
	}
	if eggP != 12.6 {
		t.Errorf("expected 12.6g protein for 2 eggs, got %v", eggP)
	}
}
