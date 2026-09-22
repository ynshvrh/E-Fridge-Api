package units

import (
	"math"
	"testing"
)

func TestUnitsNormalize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"кг", Kilogram},
		{"kg", Kilogram},
		{"кілограм", Kilogram},
		{"г", Gram},
		{"g", Gram},
		{"грам", Gram},
		{"гр.", Gram},
		{"л", Liter},
		{"l", Liter},
		{"мл", Milliliter},
		{"ml", Milliliter},
		{"шт", Piece},
		{"pcs", Piece},
		{"порц", Servings},
		{"порція", Servings},
		{"дрібка", Pinch},
		{"зубчик", Clove},
		{"ст. л.", Tablespoon},
		{"ч. л.", Teaspoon},
		{"уп", Pack},
	}

	for _, tc := range tests {
		got := Normalize(tc.input)
		if got != tc.expected {
			t.Errorf("Normalize(%q) = %q; expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestUnitsConvert(t *testing.T) {
	// Gram to kg
	if val := Convert(150, "г", "кг"); math.Abs(val-0.15) > 1e-6 {
		t.Errorf("expected 0.15 kg, got %v", val)
	}

	// Kg to gram
	if val := Convert(1.5, "кг", "г"); val != 1500.0 {
		t.Errorf("expected 1500 g, got %v", val)
	}

	// Milliliter to liter
	if val := Convert(250, "мл", "л"); math.Abs(val-0.25) > 1e-6 {
		t.Errorf("expected 0.25 l, got %v", val)
	}

	// Liter to milliliter
	if val := Convert(2, "л", "мл"); val != 2000.0 {
		t.Errorf("expected 2000 ml, got %v", val)
	}

	// Tablespoon to grams
	if val := Convert(2, "ст. л.", "г"); val != 30.0 {
		t.Errorf("expected 30 g, got %v", val)
	}

	// Teaspoon to grams
	if val := Convert(3, "ч.л", "г"); val != 15.0 {
		t.Errorf("expected 15 g, got %v", val)
	}
}

func TestAreCompatible(t *testing.T) {
	if !AreCompatible("г", "кг") {
		t.Errorf("expected g and kg to be compatible")
	}
	if !AreCompatible("мл", "л") {
		t.Errorf("expected ml and l to be compatible")
	}
	if AreCompatible("г", "мл") {
		t.Errorf("expected g and ml NOT to be strictly compatible without density")
	}
	if AreCompatible("шт", "кг") {
		t.Errorf("expected pcs and kg NOT to be compatible")
	}
}
