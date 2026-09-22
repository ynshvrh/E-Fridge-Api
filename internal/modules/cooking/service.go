package cooking

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ynshvrh/E-Fridge-Api/internal/database"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/nutrition"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/products"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/units"
)

type CookIngredient struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
}

type CookRecipeInput struct {
	RecipeTitle   string           `json:"recipe_title"`
	Servings      float64          `json:"servings"`
	ExpiryDays    int              `json:"expiry_days"`
	Ingredients   []CookIngredient `json:"ingredients"`
	IgnoreMissing bool             `json:"ignore_missing"`
	AutoLogAsMeal bool             `json:"auto_log_as_meal"`
	MealType      string           `json:"meal_type"` // breakfast, lunch, dinner, snack
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
	Date      string    `json:"date,omitempty"` // optional client date (YYYY-MM-DD)
}

type Service struct {
	queries          *db.Queries
	pool             *pgxpool.Pool
	productsService  *products.Service
	nutritionService *nutrition.Service
}

func NewService(queries *db.Queries, pool *pgxpool.Pool, prodSvc *products.Service, nutrSvc *nutrition.Service) *Service {
	return &Service{
		queries:          queries,
		pool:             pool,
		productsService:  prodSvc,
		nutritionService: nutrSvc,
	}
}

type plannedDeduction struct {
	productID    uuid.UUID
	productName  string
	deductQty    float64
	unit         string
	fullyUsed    bool
	remainingQty float64
}

func (s *Service) CookRecipe(ctx context.Context, fridgeID, userID uuid.UUID, input CookRecipeInput) (*CookResultDTO, error) {
	if strings.TrimSpace(input.RecipeTitle) == "" {
		return nil, errors.New("recipe title is required")
	}
	if len(input.Ingredients) == 0 {
		return nil, errors.New("не вказано інгредієнти для страви")
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
	if len(fridgeProds) == 0 {
		return nil, errors.New("у вашому холодильнику немає продуктів: спочатку додайте або купіть інгредієнти")
	}

	// In-memory simulation of available product quantities to prevent overdrafting
	simulatedQty := make(map[uuid.UUID]float64)
	for _, p := range fridgeProds {
		simulatedQty[p.ID] = p.Quantity
	}

	var planned []plannedDeduction
	var missing []string

	totalCalories := 0
	var totalProtein, totalFat, totalCarbs float64
	matchedCount := 0
	matchedMajorCount := 0
	totalMajorCount := 0

	// 2. Process and validate ingredients in memory (Dry Run)
	for _, ing := range input.Ingredients {
		if strings.TrimSpace(ing.Name) == "" {
			continue
		}

		cals, p, f, c := nutrition.CalculateEstimatedNutrition(ing.Name, ing.Quantity, ing.Unit)
		totalCalories += cals
		totalProtein += p
		totalFat += f
		totalCarbs += c

		isMinor := isMinorIngredient(ing.Name, ing.Unit, ing.Quantity)
		if !isMinor {
			totalMajorCount++
		}

		match := FindFridgeProductMatch(ing.Name, fridgeProds)
		if match == nil {
			missing = append(missing, fmt.Sprintf("%s (%v %s)", ing.Name, ing.Quantity, ing.Unit))
			continue
		}

		deductQty := convertUnitsWithFood(ing.Quantity, ing.Unit, match.Unit, match.Name)
		avail := simulatedQty[match.ID]

		if avail < deductQty {
			missing = append(missing, fmt.Sprintf("%s (потрібно %v %s, є %v %s)", match.Name, deductQty, match.Unit, avail, match.Unit))
			continue
		}

		matchedCount++
		if !isMinor {
			matchedMajorCount++
		}

		simulatedQty[match.ID] -= deductQty
		fullyUsed := simulatedQty[match.ID] <= 0
		planned = append(planned, plannedDeduction{
			productID:    match.ID,
			productName:  match.Name,
			deductQty:    deductQty,
			unit:         match.Unit,
			fullyUsed:    fullyUsed,
			remainingQty: math.Round(simulatedQty[match.ID]*1000) / 1000,
		})
	}

	// Anti-Thin-Air checks:
	if len(planned) == 0 {
		return nil, errors.New("неможливо приготувати страву: жоден із необхідних інгредієнтів не знайдено в холодильнику")
	}
	if len(missing) > 0 && !input.IgnoreMissing {
		var deductions []DeductedItem
		for _, pl := range planned {
			deductions = append(deductions, DeductedItem{
				ProductName: pl.productName,
				DeductedQty: pl.deductQty,
				Unit:        pl.unit,
				FullyUsed:   pl.fullyUsed,
			})
		}
		return &CookResultDTO{
			Deductions: deductions,
			Missing:    missing,
		}, fmt.Errorf("missing required ingredients: %s", strings.Join(missing, ", "))
	}
	if input.IgnoreMissing && totalMajorCount > 0 && matchedMajorCount == 0 {
		return nil, errors.New("неможливо приготувати страву: відсутні всі основні інгредієнти (знайдено лише спеції або приправи)")
	}

	// 3. Transactional execution (Database Writes)
	var createdMeal db.Product
	var deductions []DeductedItem

	perServingCals := int32(float64(totalCalories) / input.Servings)
	perServingProtein := round2(totalProtein / input.Servings)
	perServingFat := round2(totalFat / input.Servings)
	perServingCarbs := round2(totalCarbs / input.Servings)
	expiry := nutrition.GetCurrentKyivDate().AddDate(0, 0, input.ExpiryDays)

	executeTx := func(q *db.Queries) error {
		// Aggregate planned deductions by productID
		productDeductions := make(map[uuid.UUID]float64)
		for _, pl := range planned {
			productDeductions[pl.productID] += pl.deductQty
		}

		for _, pl := range planned {
			deductions = append(deductions, DeductedItem{
				ProductName: pl.productName,
				DeductedQty: pl.deductQty,
				Unit:        pl.unit,
				FullyUsed:   pl.fullyUsed,
			})
		}

		for prodID, totalDeduct := range productDeductions {
			current, err := q.GetProductByID(ctx, db.GetProductByIDParams{
				ID:       prodID,
				FridgeID: fridgeID,
			})
			if err != nil {
				return fmt.Errorf("failed to fetch product %s for deduction: %w", prodID, err)
			}
			newQty := math.Round((current.Quantity-totalDeduct)*1000) / 1000
			if newQty <= 0 {
				if err := q.DeleteProduct(ctx, db.DeleteProductParams{ID: prodID, FridgeID: fridgeID}); err != nil {
					return fmt.Errorf("failed to delete fully used product %s: %w", prodID, err)
				}
			} else {
				if _, err := q.UpdateProductQuantity(ctx, db.UpdateProductQuantityParams{
					ID:       prodID,
					FridgeID: fridgeID,
					Quantity: newQty,
				}); err != nil {
					return fmt.Errorf("failed to update product quantity for %s: %w", prodID, err)
				}
			}
		}

		// Create prepared meal in fridge
		meal, err := q.CreateProduct(ctx, db.CreateProductParams{
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
			CreatedBy:  pgtype.UUID{Bytes: userID, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("failed to create prepared meal: %w", err)
		}
		createdMeal = meal

		return nil
	}

	if s.pool != nil {
		if err := database.WithTransaction(ctx, s.pool, executeTx); err != nil {
			return nil, err
		}
	} else {
		if err := executeTx(s.queries); err != nil {
			return nil, err
		}
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
		CreatedAt: createdMeal.CreatedAt,
		UpdatedAt: createdMeal.UpdatedAt,
		Status:    "good",
	}
	if createdMeal.CreatedBy.Valid {
		uid := uuid.UUID(createdMeal.CreatedBy.Bytes)
		mealDTO.CreatedBy = &uid
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
			Date:     nutrition.GetCurrentKyivDate().Format("2006-01-02"),
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

	// Calculate accurate nutrition for the logged meal
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

	dateStr := strings.TrimSpace(input.Date)
	if dateStr == "" {
		dateStr = nutrition.GetCurrentKyivDate().Format("2006-01-02")
	}

	var updatedProd *products.ProductDTO
	var loggedMeal *nutrition.NutritionLogDTO

	// Transactional execution: consume from fridge AND log meal atomically
	executeTx := func(q *db.Queries) error {
		consumed, err := s.productsService.ConsumeProductWithUnitTx(ctx, q, fridgeID, input.ProductID, amount, unit)
		if err != nil {
			return err
		}
		updatedProd = consumed

		prodIDVal := pgtype.UUID{Bytes: prod.ID, Valid: true}
		fridgeIDVal := pgtype.UUID{Bytes: fridgeID, Valid: true}
		parsedDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			parsedDate = nutrition.GetCurrentKyivDate()
		}

		logEntry, err := q.CreateNutritionLog(ctx, db.CreateNutritionLogParams{
			UserID:    userID,
			Date:      pgtype.Date{Time: parsedDate, Valid: true},
			MealType:  mealType,
			FoodName:  prod.Name,
			Quantity:  amount,
			Unit:      unit,
			Calories:  cals,
			Protein:   p,
			Fat:       f,
			Carbs:     c,
			ProductID: prodIDVal,
			FridgeID:  fridgeIDVal,
		})
		if err != nil {
			return fmt.Errorf("consumed product but failed to log nutrition: %w", err)
		}

		dto := nutrition.NutritionLogDTO{
			ID:        logEntry.ID,
			UserID:    logEntry.UserID,
			Date:      logEntry.Date.Time.Format("2006-01-02"),
			MealType:  logEntry.MealType,
			FoodName:  logEntry.FoodName,
			Quantity:  logEntry.Quantity,
			Unit:      logEntry.Unit,
			Calories:  logEntry.Calories,
			Protein:   logEntry.Protein,
			Fat:       logEntry.Fat,
			Carbs:     logEntry.Carbs,
			LoggedAt:  logEntry.LoggedAt,
			ProductID: &prod.ID,
			FridgeID:  &fridgeID,
		}
		loggedMeal = &dto
		return nil
	}

	if s.pool != nil {
		if err := database.WithTransaction(ctx, s.pool, executeTx); err != nil {
			return nil, err
		}
	} else {
		if err := executeTx(s.queries); err != nil {
			return nil, err
		}
	}

	return &CookResultDTO{
		PreparedMeal: updatedProd,
		LoggedMeal:   loggedMeal,
	}, nil
}

func convertUnits(qty float64, fromUnit, toUnit string) float64 {
	return convertUnitsWithFood(qty, fromUnit, toUnit, "")
}

func convertUnitsWithFood(qty float64, fromUnit, toUnit, foodName string) float64 {
	fromNorm := units.Normalize(fromUnit)
	toNorm := units.Normalize(toUnit)

	if fromNorm == toNorm || fromNorm == "" || toNorm == "" {
		return qty
	}

	// Check standard conversions (g <-> kg, ml <-> l, servings, tbsp, tsp, etc.)
	converted := units.Convert(qty, fromNorm, toNorm)
	if converted != qty || units.AreCompatible(fromNorm, toNorm) {
		return converted
	}

	// Piece and package conversions using nutrition piece weight
	pieceGrams := nutrition.GetPackageOrPieceGrams(foodName)
	if pieceGrams <= 0 {
		pieceGrams = 100.0
	}

	if (fromNorm == units.Piece || fromNorm == units.Pack) && (toNorm == units.Gram || toNorm == units.Milliliter) {
		return qty * pieceGrams
	}
	if (fromNorm == units.Piece || fromNorm == units.Pack) && (toNorm == units.Kilogram || toNorm == units.Liter) {
		return (qty * pieceGrams) / 1000.0
	}
	if (fromNorm == units.Gram || fromNorm == units.Milliliter) && (toNorm == units.Piece || toNorm == units.Pack) {
		return qty / pieceGrams
	}
	if (fromNorm == units.Kilogram || fromNorm == units.Liter) && (toNorm == units.Piece || toNorm == units.Pack) {
		return (qty * 1000.0) / pieceGrams
	}

	return qty
}

func round2(val float64) float64 {
	return float64(int(val*100+0.5)) / 100
}

// Canonical Ukrainian and English synonyms dictionary
var canonicalAliases = map[string]string{
	"курка": "CANONICAL_CHICKEN", "куряче": "CANONICAL_CHICKEN", "куряча": "CANONICAL_CHICKEN",
	"курячий": "CANONICAL_CHICKEN", "курятина": "CANONICAL_CHICKEN", "курча": "CANONICAL_CHICKEN", "chicken": "CANONICAL_CHICKEN",
	"яйце": "CANONICAL_EGG", "яйця": "CANONICAL_EGG", "яєць": "CANONICAL_EGG", "яйцем": "CANONICAL_EGG", "egg": "CANONICAL_EGG", "eggs": "CANONICAL_EGG",
	"помідор": "CANONICAL_TOMATO", "помідори": "CANONICAL_TOMATO", "помідорів": "CANONICAL_TOMATO", "томат": "CANONICAL_TOMATO", "томати": "CANONICAL_TOMATO", "чері": "CANONICAL_TOMATO", "tomato": "CANONICAL_TOMATO",
	"творог": "CANONICAL_COTTAGE_CHEESE", "сир кисломолочний": "CANONICAL_COTTAGE_CHEESE", "кисломолочний": "CANONICAL_COTTAGE_CHEESE", "домашній сир": "CANONICAL_COTTAGE_CHEESE", "cottage cheese": "CANONICAL_COTTAGE_CHEESE",
	"масло": "CANONICAL_BUTTER", "масло вершкове": "CANONICAL_BUTTER", "вершкове масло": "CANONICAL_BUTTER", "butter": "CANONICAL_BUTTER",
	"олія": "CANONICAL_OIL", "рослинна олія": "CANONICAL_OIL", "соняшникова олія": "CANONICAL_OIL", "оливкова олія": "CANONICAL_OIL", "oil": "CANONICAL_OIL",
	"паста": "CANONICAL_PASTA", "макарони": "CANONICAL_PASTA", "спагеті": "CANONICAL_PASTA", "вермішель": "CANONICAL_PASTA", "pasta": "CANONICAL_PASTA", "spaghetti": "CANONICAL_PASTA",
	"сіль": "CANONICAL_SALT", "солі": "CANONICAL_SALT", "salt": "CANONICAL_SALT",
	"перець": "CANONICAL_PEPPER", "перцю": "CANONICAL_PEPPER", "pepper": "CANONICAL_PEPPER",
	"часник": "CANONICAL_GARLIC", "часнику": "CANONICAL_GARLIC", "garlic": "CANONICAL_GARLIC",
	"цибуля": "CANONICAL_ONION", "цибулі": "CANONICAL_ONION", "onion": "CANONICAL_ONION",
	"морква": "CANONICAL_CARROT", "моркви": "CANONICAL_CARROT", "carrot": "CANONICAL_CARROT",
	"картопля": "CANONICAL_POTATO", "картоплі": "CANONICAL_POTATO", "potato": "CANONICAL_POTATO",
	"молоко": "CANONICAL_MILK", "milk": "CANONICAL_MILK",
	"цукор": "CANONICAL_SUGAR", "цукру": "CANONICAL_SUGAR", "sugar": "CANONICAL_SUGAR",
	"борошно": "CANONICAL_FLOUR", "мука": "CANONICAL_FLOUR", "пшеничне борошно": "CANONICAL_FLOUR", "flour": "CANONICAL_FLOUR",
	"сметана": "CANONICAL_SOUR_CREAM", "сметани": "CANONICAL_SOUR_CREAM", "sour cream": "CANONICAL_SOUR_CREAM",
	"сир": "CANONICAL_CHEESE", "твердий сир": "CANONICAL_CHEESE", "cheese": "CANONICAL_CHEESE",
}

// Ukrainian inflection suffixes to strip when finding stem (minimum stem length: 4 characters)
var inflectionSuffixes = []string{
	"ами", "ями", "ного", "ному", "них", "ній", "ної", "ним", "ний",
	"ою", "ею", "єю", "ом", "ем", "єм", "ів", "ей",
	"на", "не", "ні", "та", "те", "ті",
	"а", "я", "и", "і", "у", "ю", "е", "є", "о",
}

var noiseWords = []string{
	"великої", "великий", "велика", "великі", "великих",
	"середньої", "середній", "середня", "середні", "середніх",
	"маленької", "маленький", "маленька", "маленькі", "маленьких",
	"свіжого", "свіжий", "свіжа", "свіжі", "свіжих",
	"стиглого", "стиглий", "стигла", "стиглі", "стиглих",
	"пастеризоване", "знежирене",
}

func cleanNoise(s string) string {
	str := strings.ToLower(strings.TrimSpace(s))
	for _, nw := range noiseWords {
		if strings.HasPrefix(str, nw+" ") {
			str = strings.TrimSpace(str[len(nw)+1:])
		}
		if strings.HasSuffix(str, " "+nw) {
			str = strings.TrimSpace(str[:len(str)-len(nw)-1])
		}
	}
	return str
}

func stripEnding(word string) string {
	w := strings.ToLower(strings.TrimSpace(word))
	runes := []rune(w)
	if len(runes) < 4 {
		return w
	}
	for _, suffix := range inflectionSuffixes {
		sRunes := []rune(suffix)
		if len(runes) > len(sRunes) && (len(runes)-len(sRunes)) >= 4 {
			if strings.HasSuffix(w, suffix) {
				return string(runes[:len(runes)-len(sRunes)])
			}
		}
	}
	return w
}

func scoreMatch(ingClean, prodClean string) int {
	if ingClean == prodClean {
		return 100
	}

	// Canonical match of full phrases
	canonI := canonicalAliases[ingClean]
	canonP := canonicalAliases[prodClean]
	if canonI != "" && canonP != "" && canonI == canonP {
		return 90
	}

	// Check if any canonical alias phrase is inside ingClean and prodClean
	for phrase, canon := range canonicalAliases {
		if canonI == "" && strings.Contains(ingClean, phrase) {
			canonI = canon
		}
		if canonP == "" && strings.Contains(prodClean, phrase) {
			canonP = canon
		}
	}
	if canonI != "" && canonP != "" && canonI == canonP {
		return 85
	}

	// Check if entire ingredient is contained in product or vice versa
	if strings.Contains(prodClean, ingClean) || strings.Contains(ingClean, prodClean) {
		return 80
	}

	iWords := strings.Fields(ingClean)
	pWords := strings.Fields(prodClean)

	maxWordScore := 0
	for _, iw := range iWords {
		canonI := canonicalAliases[iw]
		stemI := stripEnding(iw)

		for _, pw := range pWords {
			canonP := canonicalAliases[pw]
			stemP := stripEnding(pw)

			if iw == pw {
				if 75 > maxWordScore {
					maxWordScore = 75
				}
			} else if canonI != "" && canonP != "" && canonI == canonP {
				if 70 > maxWordScore {
					maxWordScore = 70
				}
			} else if len([]rune(stemI)) >= 4 && stemI == stemP {
				if 60 > maxWordScore {
					maxWordScore = 60
				}
			} else if (len(pw) >= 3 && strings.HasPrefix(iw, pw)) || (len(iw) >= 3 && strings.HasPrefix(pw, iw)) {
				if 50 > maxWordScore {
					maxWordScore = 50
				}
			}
		}
	}

	return maxWordScore
}

type candidateMatch struct {
	product db.Product
	score   int
}

// FindFridgeProductMatch searches for a matching product in the fridge inventory using deterministic scoring and stemming.
func FindFridgeProductMatch(ingName string, prods []db.Product) *db.Product {
	rawIng := strings.TrimSpace(ingName)
	if rawIng == "" {
		return nil
	}
	ingClean := cleanNoise(rawIng)

	var candidates []candidateMatch
	for _, p := range prods {
		if p.Quantity <= 0 {
			continue
		}
		pClean := cleanNoise(p.Name)
		score := scoreMatch(ingClean, pClean)
		if score >= 40 {
			candidates = append(candidates, candidateMatch{product: p, score: score})
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Deterministic sorting:
	// 1. Match score DESC
	// 2. Absolute difference in name length ASC
	// 3. Alphabetical product name ASC
	// 4. Product ID string ASC
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		diffI := math.Abs(float64(len(candidates[i].product.Name) - len(rawIng)))
		diffJ := math.Abs(float64(len(candidates[j].product.Name) - len(rawIng)))
		if diffI != diffJ {
			return diffI < diffJ
		}
		if candidates[i].product.Name != candidates[j].product.Name {
			return candidates[i].product.Name < candidates[j].product.Name
		}
		return candidates[i].product.ID.String() < candidates[j].product.ID.String()
	})

	matched := candidates[0].product
	return &matched
}

func isMinorIngredient(name, unit string, qty float64) bool {
	normName := strings.ToLower(strings.TrimSpace(name))
	normUnit := units.Normalize(unit)

	// Units that imply minor seasonings
	if normUnit == units.Pinch || normUnit == units.Clove || normUnit == units.Teaspoon {
		return true
	}

	minorKeywords := []string{
		"сіль", "солі", "salt",
		"перець", "перцю", "pepper",
		"спеції", "приправа", "лавровий", "орегано", "базилік", "паприка", "кріп", "петрушка", "зелень",
		"сода", "оцет", "ваниль", "ваніль", "кориця", "гвоздика", "мускатний", "вода", "кмин", "кунжут",
	}

	for _, kw := range minorKeywords {
		if strings.Contains(normName, kw) {
			return true
		}
	}

	if (strings.Contains(normName, "олія") || strings.Contains(normName, "цукор") || strings.Contains(normName, "соус")) &&
		((normUnit == units.Gram || normUnit == units.Milliliter) && qty <= 15) {
		return true
	}

	return false
}
