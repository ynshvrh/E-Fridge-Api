package cooking

import "testing"

func TestConvertUnits(t *testing.T) {
	if val := convertUnits(1.5, "kg", "g"); val != 1500.0 {
		t.Errorf("expected 1500g, got %v", val)
	}
	if val := convertUnits(500, "г", "кг"); val != 0.5 {
		t.Errorf("expected 0.5kg, got %v", val)
	}
	if val := convertUnits(2, "л", "мл"); val != 2000.0 {
		t.Errorf("expected 2000ml, got %v", val)
	}
	if val := convertUnits(3, "шт", "шт"); val != 3.0 {
		t.Errorf("expected 3, got %v", val)
	}

	// Cross unit conversions with food names
	// Eggs (~50g per egg in FoodDatabase)
	if val := convertUnitsWithFood(100, "г", "шт", "яйця"); val != 2.0 {
		t.Errorf("expected 2 eggs for 100g, got %v", val)
	}
	if val := convertUnitsWithFood(2, "шт", "г", "яйця"); val != 100.0 {
		t.Errorf("expected 100g for 2 eggs, got %v", val)
	}

	// 200g to kg
	if val := convertUnitsWithFood(200, "г", "кг", "куряче філе"); val != 0.2 {
		t.Errorf("expected 0.2kg for 200g, got %v", val)
	}

	// 1 portion to grams
	if val := convertUnitsWithFood(1, "порц", "г", "борщ"); val != 300.0 {
		t.Errorf("expected 300g for 1 portion, got %v", val)
	}
}
