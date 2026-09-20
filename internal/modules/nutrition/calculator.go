package nutrition

import (
	"strings"
)

type FoodNutrients struct {
	Calories          int     // per 100g / 100ml
	Protein           float64 // per 100g / 100ml
	Fat               float64 // per 100g / 100ml
	Carbs             float64 // per 100g / 100ml
	DefaultPieceGrams float64 // default weight in grams for 1 'pcs' / 'шт'
}

// FoodDatabase contains reference nutrition values per 100g/100ml
var FoodDatabase = map[string]FoodNutrients{
	// Meat & Poultry
	"курка":             {165, 31.0, 3.6, 0.0, 150},
	"куряче філе":       {165, 31.0, 3.6, 0.0, 200},
	"куряча грудка":     {165, 31.0, 3.6, 0.0, 200},
	"куряче стегно":     {210, 24.0, 12.0, 0.0, 150},
	"індичка":           {144, 30.0, 2.0, 0.0, 200},
	"філе індички":      {144, 30.0, 2.0, 0.0, 200},
	"яловичина":         {250, 26.0, 17.0, 0.0, 200},
	"яловичий фарш":     {240, 24.0, 16.0, 0.0, 200},
	"свинина":           {242, 27.0, 14.0, 0.0, 200},
	"свинячий фарш":     {260, 22.0, 19.0, 0.0, 200},
	"фарш":              {250, 23.0, 17.0, 0.0, 200},
	"бекон":             {450, 14.0, 42.0, 1.0, 30},
	"сосиски":           {260, 11.0, 23.0, 2.0, 60},
	"шинка":             {145, 21.0, 6.0, 1.5, 50},

	// Fish & Seafood
	"лосось":     {208, 20.0, 13.0, 0.0, 150},
	"сьомга":     {208, 20.0, 13.0, 0.0, 150},
	"форель":     {148, 20.8, 6.6, 0.0, 150},
	"хек":        {86, 18.0, 1.3, 0.0, 150},
	"минтай":     {72, 16.0, 0.7, 0.0, 150},
	"тунець":     {130, 28.0, 1.0, 0.0, 150},
	"риба":       {120, 20.0, 4.0, 0.0, 150},
	"рибне філе": {110, 19.0, 3.0, 0.0, 150},
	"креветки":   {99, 24.0, 0.3, 0.2, 15},
	"мідії":      {86, 12.0, 2.2, 3.7, 10},

	// Eggs & Dairy
	"яйце":               {143, 12.6, 9.5, 0.7, 50},
	"яйця":               {143, 12.6, 9.5, 0.7, 50},
	"молоко":             {60, 3.2, 3.2, 4.8, 200},
	"кефір":              {53, 3.0, 2.5, 4.0, 200},
	"сметана":            {206, 2.8, 20.0, 3.2, 30},
	"вершки":             {200, 2.8, 20.0, 3.7, 50},
	"йогурт":             {65, 4.5, 3.0, 5.0, 150},
	"сир":                {350, 25.0, 28.0, 1.5, 30},
	"твердий сир":        {360, 25.0, 29.0, 1.5, 30},
	"пармезан":           {431, 38.0, 29.0, 4.1, 20},
	"моцарела":           {280, 22.0, 22.0, 2.2, 50},
	"фета":               {264, 14.0, 21.0, 4.1, 30},
	"сир кисломолочний":  {120, 18.0, 5.0, 3.0, 100},
	"творог":             {120, 18.0, 5.0, 3.0, 100},
	"масло":              {717, 0.8, 81.0, 0.7, 10},
	"вершкове масло":     {717, 0.8, 81.0, 0.7, 10},

	// Grains & Bread
	"рис":                {130, 2.7, 0.3, 28.0, 100},
	"гречка":             {132, 4.5, 1.0, 25.0, 100},
	"вівсянка":           {389, 17.0, 7.0, 66.0, 50},
	"макарони":           {158, 5.8, 0.9, 31.0, 100},
	"паста":              {158, 5.8, 0.9, 31.0, 100},
	"хліб":               {265, 9.0, 3.2, 49.0, 40},
	"лаваш":              {270, 8.0, 1.0, 56.0, 50},
	"картопля":           {77, 2.0, 0.1, 17.0, 120},

	// Vegetables & Fruits
	"морква":             {41, 0.9, 0.2, 9.6, 100},
	"цибуля":             {40, 1.1, 0.1, 9.3, 100},
	"часник":             {149, 6.4, 0.5, 33.0, 5},
	"помідор":            {18, 0.9, 0.2, 3.9, 120},
	"помідори":           {18, 0.9, 0.2, 3.9, 120},
	"огірок":             {15, 0.7, 0.1, 3.6, 100},
	"огірки":             {15, 0.7, 0.1, 3.6, 100},
	"яблуко":             {52, 0.3, 0.2, 14.0, 150},
	"яблука":             {52, 0.3, 0.2, 14.0, 150},
	"банан":              {89, 1.1, 0.3, 23.0, 120},
	"банани":             {89, 1.1, 0.3, 23.0, 120},

	// Oils & Sauces
	"олія":               {884, 0.0, 100.0, 0.0, 15},
	"соняшникова олія":   {884, 0.0, 100.0, 0.0, 15},
	"оливкова олія":      {884, 0.0, 100.0, 0.0, 15},
	"майонез":            {680, 1.0, 75.0, 2.6, 20},
}

// CalculateEstimatedNutrition calculates total calories and macros for given ingredient
func CalculateEstimatedNutrition(name string, quantity float64, unit string) (cals int, p, f, c float64) {
	cleanName := strings.ToLower(strings.TrimSpace(name))
	unit = strings.ToLower(strings.TrimSpace(unit))

	var nutrients *FoodNutrients

	// Direct match
	if n, ok := FoodDatabase[cleanName]; ok {
		nutrients = &n
	} else {
		// Substring search
		for k, v := range FoodDatabase {
			if strings.Contains(cleanName, k) || strings.Contains(k, cleanName) {
				nutrients = &v
				break
			}
		}
	}

	if nutrients == nil {
		// Generic default fallback: 100 kcal / 100g
		nutrients = &FoodNutrients{Calories: 100, Protein: 4, Fat: 3, Carbs: 15, DefaultPieceGrams: 100}
	}

	// Determine weight in grams/ml
	var grams float64
	switch unit {
	case "g", "г":
		grams = quantity
	case "kg", "кг":
		grams = quantity * 1000.0
	case "ml", "мл":
		grams = quantity
	case "l", "л":
		grams = quantity * 1000.0
	case "pcs", "шт", "уп", "порц":
		grams = quantity * nutrients.DefaultPieceGrams
	default:
		grams = quantity * 100.0
	}

	factor := grams / 100.0
	cals = int(float64(nutrients.Calories) * factor)
	p = round2(nutrients.Protein * factor)
	f = round2(nutrients.Fat * factor)
	c = round2(nutrients.Carbs * factor)

	return cals, p, f, c
}

func round2(val float64) float64 {
	return float64(int(val*100+0.5)) / 100
}

// GetDefaultPieceGrams returns default piece weight in grams for given food name.
func GetDefaultPieceGrams(name string) float64 {
	cleanName := strings.ToLower(strings.TrimSpace(name))
	if cleanName == "" {
		return 100.0
	}
	if n, ok := FoodDatabase[cleanName]; ok && n.DefaultPieceGrams > 0 {
		return n.DefaultPieceGrams
	}
	for k, v := range FoodDatabase {
		if strings.Contains(cleanName, k) || strings.Contains(k, cleanName) {
			if v.DefaultPieceGrams > 0 {
				return v.DefaultPieceGrams
			}
		}
	}
	return 100.0
}
