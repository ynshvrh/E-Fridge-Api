package cooking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/nutrition"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/products"
)

type CookIngredient struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
}

type CookRecipeInput struct {
	RecipeTitle    string           `json:"recipe_title"`
	Servings       float64          `json:"servings"`
	ExpiryDays     int              `json:"expiry_days"`
	Ingredients    []CookIngredient `json:"ingredients"`
	IgnoreMissing  bool             `json:"ignore_missing"`
	AutoLogAsMeal  bool             `json:"auto_log_as_meal"`
	MealType       string           `json:"meal_type"` // breakfast, lunch, dinner, snack
}

type DeductedItem struct {
	ProductName string  `json:"product_name"`
	DeductedQty float64 `json:"deducted_qty"`
	Unit        string  `json:"unit"`
	FullyUsed   bool    `json:"fully_used"`
}

type CookResultDTO struct {
	PreparedMeal *products.ProductDTO       `json:"prepared_meal"`
	Deductions   []DeductedItem             `json:"deductions"`
	Missing      []string                   `json:"missing,omitempty"`
	LoggedMeal   *nutrition.NutritionLogDTO `json:"logged_meal,omitempty"`
}

type ConsumeMealInput struct {
	ProductID uuid.UUID `json:"product_id"`
	Portions  float64   `json:"portions,omitempty"` // backward compatibility
	Amount    float64   `json:"amount,omitempty"`
	Unit      string    `json:"unit,omitempty"`
	MealType  string    `json:"meal_type"`
}

type Service struct {
	queries          *db.Queries
	productsService  *products.Service
	nutritionService *nutrition.Service
}

func NewService(queries *db.Queries, prodSvc *products.Service, nutrSvc *nutrition.Service) *Service {
	return &Service{
		queries:          queries,
		productsService:  prodSvc,
		nutritionService: nutrSvc,
	}
}

func (s *Service) CookRecipe(ctx context.Context, fridgeID, userID uuid.UUID, input CookRecipeInput) (*CookResultDTO, error) {
	if strings.TrimSpace(input.RecipeTitle) == "" {
		return nil, fmt.Errorf("recipe title is required")
	}
	if input.Servings <= 0 {
		input.Servings = 1
	}
	if input.ExpiryDays <= 0 {
		input.ExpiryDays = 4 // standard shelf life for cooked food
	}

	// 1. Fetch current fridge products
	fridgeProds, err := s.queries.ListProductsByFridge(ctx, fridgeID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch fridge products: %w", err)
	}

	var deductions []DeductedItem
	var missing []string

	totalCalories := 0
	var totalProtein, totalFat, totalCarbs float64

	// 2. Process ingredients
	for _, ing := range input.Ingredients {
		if strings.TrimSpace(ing.Name) == "" {
			continue
		}

		// Calculate nutrition for ingredient
		cals, p, f, c := nutrition.CalculateEstimatedNutrition(ing.Name, ing.Quantity, ing.Unit)
		totalCalories += cals
		totalProtein += p
		totalFat += f
		totalCarbs += c

		// Find match in fridge
		match := FindFridgeProductMatch(ing.Name, fridgeProds)

		if match == nil {
			missing = append(missing, fmt.Sprintf("%s (%v %s)", ing.Name, ing.Quantity, ing.Unit))
			continue
		}

		// Convert and deduct
		deductQty := convertUnits(ing.Quantity, ing.Unit, match.Unit)
		if match.Quantity < deductQty && !input.IgnoreMissing {
			missing = append(missing, fmt.Sprintf("%s (потрібно %v %s, є %v %s)", match.Name, deductQty, match.Unit, match.Quantity, match.Unit))
			continue
		}

		fullyUsed := false
		if match.Quantity <= deductQty {
			fullyUsed = true
			_ = s.queries.DeleteProduct(ctx, db.DeleteProductParams{
				ID:       match.ID,
				FridgeID: fridgeID,
			})
			match.Quantity = 0
		} else {
			match.Quantity -= deductQty
			_, _ = s.queries.UpdateProductQuantity(ctx, db.UpdateProductQuantityParams{
				ID:       match.ID,
				FridgeID: fridgeID,
				Quantity: match.Quantity,
			})
		}

		deductions = append(deductions, DeductedItem{
			ProductName: match.Name,
			DeductedQty: deductQty,
			Unit:        match.Unit,
			FullyUsed:   fullyUsed,
		})
	}

	// If missing items and user didn't allow ignoring missing
	if len(missing) > 0 && !input.IgnoreMissing {
		return &CookResultDTO{
			Deductions: deductions,
			Missing:    missing,
		}, fmt.Errorf("missing required ingredients: %s", strings.Join(missing, ", "))
	}

	// 3. Create prepared meal in fridge
	perServingCals := int32(float64(totalCalories) / input.Servings)
	perServingProtein := round2(totalProtein / input.Servings)
	perServingFat := round2(totalFat / input.Servings)
	perServingCarbs := round2(totalCarbs / input.Servings)

	expiry := time.Now().AddDate(0, 0, input.ExpiryDays)
	createdMeal, err := s.queries.CreateProduct(ctx, db.CreateProductParams{
		FridgeID:   fridgeID,
		Name:       input.RecipeTitle,
		Category:   "prepared-meals",
		Quantity:   input.Servings,
		Unit:       "порц",
		ExpiryDate: pgtype.Date{Time: expiry, Valid: true},
		Calories:   perServingCals,
		Protein:    perServingProtein,
		Fat:        perServingFat,
		Carbs:      perServingCarbs,
		Notes:      "Свіжоприготована страва",
		CreatedBy:  userID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create prepared meal: %w", err)
	}

	mealDTO := products.ProductDTO{
		ID:        createdMeal.ID,
		FridgeID:  createdMeal.FridgeID,
		Name:      createdMeal.Name,
		Category:  createdMeal.Category,
		Quantity:  createdMeal.Quantity,
		Unit:      createdMeal.Unit,
		Calories:  createdMeal.Calories,
		Protein:   createdMeal.Protein,
		Fat:       createdMeal.Fat,
		Carbs:     createdMeal.Carbs,
		Notes:     createdMeal.Notes,
		CreatedBy: createdMeal.CreatedBy,
		CreatedAt: createdMeal.CreatedAt,
		UpdatedAt: createdMeal.UpdatedAt,
		Status:    "good",
	}
	dateStr := expiry.Format("2006-01-02")
	mealDTO.ExpiryDate = &dateStr
	daysLeft := input.ExpiryDays
	mealDTO.DaysLeft = &daysLeft

	result := &CookResultDTO{
		PreparedMeal: &mealDTO,
		Deductions:   deductions,
		Missing:      missing,
	}

	// 4. Auto-log meal if requested
	if input.AutoLogAsMeal && input.Servings >= 1 {
		// Consume 1 serving
		_, _ = s.queries.UpdateProductQuantity(ctx, db.UpdateProductQuantityParams{
			ID:       createdMeal.ID,
			FridgeID: fridgeID,
			Quantity: createdMeal.Quantity - 1,
		})
		mealDTO.Quantity -= 1

		logged, err := s.nutritionService.LogMeal(ctx, userID, nutrition.LogMealInput{
			Date:     time.Now().Format("2006-01-02"),
			MealType: input.MealType,
			FoodName: input.RecipeTitle,
			Quantity:  1,
			Unit:      "порц",
			Calories:  perServingCals,
			Protein:   perServingProtein,
			Fat:       perServingFat,
			Carbs:     perServingCarbs,
			ProductID: &createdMeal.ID,
			FridgeID:  &fridgeID,
		})
		if err == nil {
			result.LoggedMeal = logged
		}
	}

	return result, nil
}

func (s *Service) ConsumeMeal(ctx context.Context, fridgeID, userID uuid.UUID, input ConsumeMealInput) (*CookResultDTO, error) {
	// Get product to inspect nutrition and storage unit
	prod, err := s.productsService.GetProduct(ctx, fridgeID, input.ProductID)
	if err != nil {
		return nil, err
	}

	amount := input.Amount
	unit := strings.TrimSpace(input.Unit)
	if amount <= 0 {
		amount = input.Portions
	}
	if amount <= 0 {
		amount = 1.0
	}
	if unit == "" {
		unit = prod.Unit
	}

	mealType := input.MealType
	if mealType == "" {
		mealType = "snack"
	}

	// 1. Consume from fridge with unit
	updatedProd, err := s.productsService.ConsumeProductWithUnit(ctx, fridgeID, input.ProductID, amount, unit)
	if err != nil {
		return nil, err
	}


	// 3. Calculate accurate nutrition for the logged meal
	var cals int32
	var p, f, c float64

	prodUnit := strings.ToLower(strings.TrimSpace(prod.Unit))

	if prod.Calories == 0 && prod.Protein == 0 && prod.Fat == 0 && prod.Carbs == 0 {
		// If product had no nutrition info, calculate estimated nutrition based on food name and consumed amount
		calcCal, calcP, calcF, calcC := nutrition.CalculateEstimatedNutrition(prod.Name, amount, unit)
		cals = int32(calcCal)
		p = calcP
		f = calcF
		c = calcC
	} else if prod.Category == "prepared-meals" || prodUnit == "порц" || prodUnit == "порція" {
		// For prepared meals, product nutrition is stored per 1 portion
		portionsConsumed := convertUnitsWithFood(amount, unit, "порц", prod.Name)
		if portionsConsumed <= 0 {
			portionsConsumed = 1.0
		}
		cals = int32(float64(prod.Calories) * portionsConsumed)
		p = round2(prod.Protein * portionsConsumed)
		f = round2(prod.Fat * portionsConsumed)
		c = round2(prod.Carbs * portionsConsumed)
	} else {
		// For standard groceries, nutrition is stored per 100g/100ml.
		// Convert consumed quantity to grams/ml:
		gramsConsumed := convertUnitsWithFood(amount, unit, "г", prod.Name)
		if gramsConsumed <= 0 {
			gramsConsumed = amount
		}
		factor := gramsConsumed / 100.0
		cals = int32(float64(prod.Calories) * factor)
		p = round2(prod.Protein * factor)
		f = round2(prod.Fat * factor)
		c = round2(prod.Carbs * factor)
	}

	// 4. Log to nutrition
	logged, err := s.nutritionService.LogMeal(ctx, userID, nutrition.LogMealInput{
		Date:     time.Now().Format("2006-01-02"),
		MealType: mealType,
		FoodName: prod.Name,
		Quantity:  amount,
		Unit:      unit,
		Calories:  cals,
		Protein:   p,
		Fat:       f,
		Carbs:     c,
		ProductID: &prod.ID,
		FridgeID:  &fridgeID,
	})
	if err != nil {
		return nil, fmt.Errorf("consumed product but failed to log nutrition: %w", err)
	}

	return &CookResultDTO{
		PreparedMeal: updatedProd,
		LoggedMeal:   logged,
	}, nil
}

func convertUnits(qty float64, fromUnit, toUnit string) float64 {
	return convertUnitsWithFood(qty, fromUnit, toUnit, "")
}

func convertUnitsWithFood(qty float64, fromUnit, toUnit, foodName string) float64 {
	from := strings.ToLower(strings.TrimSpace(fromUnit))
	to := strings.ToLower(strings.TrimSpace(toUnit))

	if from == to || from == "" || to == "" {
		return qty
	}

	// Direct weight conversions
	if (from == "kg" || from == "кг") && (to == "g" || to == "г") {
		return qty * 1000.0
	}
	if (from == "g" || from == "г") && (to == "kg" || to == "кг") {
		return qty / 1000.0
	}

	// Direct volume conversions
	if (from == "l" || from == "л") && (to == "ml" || to == "мл") {
		return qty * 1000.0
	}
	if (from == "ml" || from == "мл") && (to == "l" || to == "л") {
		return qty / 1000.0
	}

	// Serving conversions
	if (from == "порц" || from == "порція") && (to == "порц" || to == "порція") {
		return qty
	}
	if (from == "порц" || from == "порція") && (to == "g" || to == "г" || to == "ml" || to == "мл") {
		return qty * 300.0
	}
	if (from == "порц" || from == "порція") && (to == "kg" || to == "кг" || to == "l" || to == "л") {
		return (qty * 300.0) / 1000.0
	}
	if (from == "g" || from == "г" || from == "ml" || from == "мл") && (to == "порц" || to == "порція") {
		return qty / 300.0
	}
	if (from == "kg" || from == "кг" || from == "l" || from == "л") && (to == "порц" || to == "порція") {
		return (qty * 1000.0) / 300.0
	}

	// Piece and package conversions
	pieceGrams := nutrition.GetPackageOrPieceGrams(foodName)
	if pieceGrams <= 0 {
		pieceGrams = 100.0
	}

	if (from == "шт" || from == "pcs") && (to == "g" || to == "г" || to == "ml" || to == "мл") {
		return qty * pieceGrams
	}
	if (from == "шт" || from == "pcs") && (to == "kg" || to == "кг" || to == "l" || to == "л") {
		return (qty * pieceGrams) / 1000.0
	}
	if (from == "g" || from == "г" || from == "ml" || from == "мл") && (to == "шт" || to == "pcs") {
		return qty / pieceGrams
	}
	if (from == "kg" || from == "кг" || from == "l" || from == "л") && (to == "шт" || to == "pcs") {
		return (qty * 1000.0) / pieceGrams
	}

	return qty
}


func round2(val float64) float64 {
	return float64(int(val*100+0.5)) / 100
}

// FindFridgeProductMatch searches for a matching product in the fridge inventory using substring and synonyms.
func FindFridgeProductMatch(ingName string, prods []db.Product) *db.Product {
	iName := strings.ToLower(strings.TrimSpace(ingName))
	if iName == "" {
		return nil
	}

	// 1. Direct or Substring match
	for i := range prods {
		if prods[i].Quantity <= 0 {
			continue
		}
		pName := strings.ToLower(strings.TrimSpace(prods[i].Name))
		if pName == iName || strings.Contains(pName, iName) || strings.Contains(iName, pName) {
			return &prods[i]
		}
	}

	// 2. Word-by-word match
	iWords := strings.Fields(iName)
	for i := range prods {
		if prods[i].Quantity <= 0 {
			continue
		}
		pName := strings.ToLower(strings.TrimSpace(prods[i].Name))
		pWords := strings.Fields(pName)
		for _, iw := range iWords {
			if len(iw) < 3 {
				continue
			}
			for _, pw := range pWords {
				if len(pw) < 3 {
					continue
				}
				if strings.HasPrefix(pw, iw) || strings.HasPrefix(iw, pw) {
					return &prods[i]
				}
			}
		}
	}

	// 3. Common Ukrainian food synonyms
	synonyms := map[string][]string{
		"томат":    {"помідор", "помідори", "томати", "чері"},
		"помідор":  {"томат", "томати", "помідори", "чері"},
		"сир":      {"моцарела", "пармезан", "сулугуні", "фета", "творог", "кисломолочний"},
		"моцарела": {"сир", "моцарелла"},
		"овочі":    {"помідор", "помідори", "огірок", "огірки", "перець", "салат", "зелень", "капуста"},
		"курка":    {"філе", "куряче", "грудка", "курятина"},
		"філе":     {"курка", "куряче", "індичка"},
	}

	for key, synList := range synonyms {
		if strings.Contains(iName, key) {
			for _, syn := range synList {
				for i := range prods {
					if prods[i].Quantity > 0 && strings.Contains(strings.ToLower(prods[i].Name), syn) {
						return &prods[i]
					}
				}
			}
		}
	}

	return nil
}
