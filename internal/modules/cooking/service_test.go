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
}
