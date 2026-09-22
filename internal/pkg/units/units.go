package units

import (
	"math"
	"strings"
)

// Canonical unit constants
const (
	Gram       = "g"
	Kilogram   = "kg"
	Milliliter = "ml"
	Liter      = "l"
	Piece      = "pcs"
	Servings   = "servings"
	Pinch      = "pinch"
	Clove      = "clove"
	Tablespoon = "tbsp"
	Teaspoon   = "tsp"
	Pack       = "pack"
)

// Normalize converts localized or slang unit string into its canonical key.
func Normalize(unit string) string {
	u := strings.ToLower(strings.TrimSpace(unit))
	u = strings.TrimRight(u, ".")

	switch u {
	case "кг", "kg", "кілограм", "кілограмів", "килограмм", "килограм":
		return Kilogram
	case "г", "g", "грам", "грамів", "грамм", "гр":
		return Gram
	case "л", "l", "літр", "літрів", "литр":
		return Liter
	case "мл", "ml", "мілілітр", "мілілітрів", "миллилитр":
		return Milliliter
	case "шт", "pcs", "штук", "штуки", "штука", "pc", "piece", "pieces":
		return Piece
	case "порц", "порція", "порції", "порцій", "порция", "порций", "serving", "servings":
		return Servings
	case "дрібка", "щепотка", "pinch":
		return Pinch
	case "зубчик", "зубчики", "зубчиків", "clove", "cloves":
		return Clove
	case "ст.л", "ст. л", "ст л", "ст.л.", "столова ложка", "столові ложки", "tbsp", "tablespoon":
		return Tablespoon
	case "ч.л", "ч. л", "ч л", "ч.л.", "чайна ложка", "чайні ложки", "tsp", "teaspoon":
		return Teaspoon
	case "уп", "упак", "упаковка", "упаковки", "pack", "packs", "pkg":
		return Pack
	default:
		return u
	}
}

// Convert converts a quantity from fromUnit to toUnit for standard units.
func Convert(qty float64, fromUnit, toUnit string) float64 {
	fromNorm := Normalize(fromUnit)
	toNorm := Normalize(toUnit)

	if fromNorm == toNorm || fromNorm == "" || toNorm == "" {
		return qty
	}

	// Direct weight conversions
	if fromNorm == Gram && toNorm == Kilogram {
		return qty / 1000.0
	}
	if fromNorm == Kilogram && toNorm == Gram {
		return qty * 1000.0
	}

	// Direct volume conversions
	if fromNorm == Milliliter && toNorm == Liter {
		return qty / 1000.0
	}
	if fromNorm == Liter && toNorm == Milliliter {
		return qty * 1000.0
	}

	// Servings conversions (~300g / 300ml standard)
	if fromNorm == Servings && (toNorm == Gram || toNorm == Milliliter) {
		return qty * 300.0
	}
	if fromNorm == Servings && (toNorm == Kilogram || toNorm == Liter) {
		return (qty * 300.0) / 1000.0
	}
	if (fromNorm == Gram || fromNorm == Milliliter) && toNorm == Servings {
		return qty / 300.0
	}
	if (fromNorm == Kilogram || fromNorm == Liter) && toNorm == Servings {
		return (qty * 1000.0) / 300.0
	}

	// Spoons & pinch (~15g tbsp, ~5g tsp, ~0.5g pinch, ~5g clove)
	if fromNorm == Tablespoon && (toNorm == Gram || toNorm == Milliliter) {
		return qty * 15.0
	}
	if fromNorm == Tablespoon && (toNorm == Kilogram || toNorm == Liter) {
		return (qty * 15.0) / 1000.0
	}
	if fromNorm == Teaspoon && (toNorm == Gram || toNorm == Milliliter) {
		return qty * 5.0
	}
	if fromNorm == Teaspoon && (toNorm == Kilogram || toNorm == Liter) {
		return (qty * 5.0) / 1000.0
	}
	if fromNorm == Pinch && (toNorm == Gram || toNorm == Milliliter) {
		return qty * 0.5
	}
	if fromNorm == Pinch && (toNorm == Kilogram || toNorm == Liter) {
		return (qty * 0.5) / 1000.0
	}
	if fromNorm == Clove && (toNorm == Gram || toNorm == Milliliter) {
		return qty * 5.0
	}
	if fromNorm == Clove && (toNorm == Kilogram || toNorm == Liter) {
		return (qty * 5.0) / 1000.0
	}

	return qty
}

// AreCompatible checks if two units can be converted into each other.
func AreCompatible(unitA, unitB string) bool {
	a := Normalize(unitA)
	b := Normalize(unitB)

	if a == b {
		return true
	}

	isWeightA := a == Gram || a == Kilogram
	isWeightB := b == Gram || b == Kilogram
	if isWeightA && isWeightB {
		return true
	}

	isVolA := a == Milliliter || a == Liter
	isVolB := b == Milliliter || b == Liter
	if isVolA && isVolB {
		return true
	}

	return false
}

// Round rounds a float to specified decimal places.
func Round(val float64, decimals int) float64 {
	pow := math.Pow10(decimals)
	return math.Round(val*pow) / pow
}
