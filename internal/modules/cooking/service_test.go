package cooking

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ynshvrh/E-Fridge-Api/internal/config"
	"github.com/ynshvrh/E-Fridge-Api/internal/database"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/nutrition"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/products"
)

func getTestDBURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://postgres:postgrespassword@localhost:5432/e_fridge?sslmode=disable"
}

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

func TestFindFridgeProductMatch_DeterministicAndStemming(t *testing.T) {
	prods := []db.Product{
		{ID: uuid.New(), Name: "Молоко 2.5%", Quantity: 1.0, Unit: "л"},
		{ID: uuid.New(), Name: "Куряче філе", Quantity: 500.0, Unit: "г"},
		{ID: uuid.New(), Name: "Томати чері", Quantity: 300.0, Unit: "г"},
		{ID: uuid.New(), Name: "Сир кисломолочний 5%", Quantity: 400.0, Unit: "г"},
		{ID: uuid.New(), Name: "Сіль кухонна", Quantity: 1000.0, Unit: "г"},
	}

	// 1. Ukrainian stemming & noise words
	m1 := FindFridgeProductMatch("свіже молоко", prods)
	if m1 == nil || m1.Name != "Молоко 2.5%" {
		t.Errorf("expected match 'Молоко 2.5%%%%', got %v", m1)
	}

	// 2. Canonical alias: курка -> куряче філе
	m2 := FindFridgeProductMatch("курка", prods)
	if m2 == nil || m2.Name != "Куряче філе" {
		t.Errorf("expected match 'Куряче філе', got %v", m2)
	}

	// 3. Canonical alias: помідори -> томати чері
	m3 := FindFridgeProductMatch("помідори", prods)
	if m3 == nil || m3.Name != "Томати чері" {
		t.Errorf("expected match 'Томати чері', got %v", m3)
	}

	// 4. Canonical alias: творог -> сир кисломолочний 5%
	m4 := FindFridgeProductMatch("творог", prods)
	if m4 == nil || m4.Name != "Сир кисломолочний 5%" {
		t.Errorf("expected match 'Сир кисломолочний 5%%%%', got %v", m4)
	}

	// 5. Unrelated ingredient
	m5 := FindFridgeProductMatch("ананас", prods)
	if m5 != nil {
		t.Errorf("expected nil for 'ананас', got %v", m5)
	}
}

func TestCookRecipe_AntiThinAir(t *testing.T) {
	ctx := context.Background()
	dbURL := getTestDBURL()
	dbConn, err := database.Connect(ctx, dbURL)
	if err != nil {
		t.Skip("skipping DB test, cannot connect to PostgreSQL")
		return
	}
	defer dbConn.Close()

	queries := db.New(dbConn.Pool)
	cfg := &config.Config{}
	prodSvc := products.NewService(queries, cfg)
	nutrSvc := nutrition.NewService(queries, dbConn.Pool)
	service := NewService(queries, dbConn.Pool, prodSvc, nutrSvc)

	// Setup user & fridge
	u, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        "cook_thinair_" + uuid.New().String()[:8] + "@example.com",
		Name:         "Thin Air Tester",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	defer func() {
		_, _ = dbConn.Pool.Exec(ctx, "DELETE FROM users WHERE id = $1", u.ID)
	}()

	fridge, err := queries.CreateFridge(ctx, db.CreateFridgeParams{
		Name:    "Порожній холодильник",
		OwnerID: u.ID,
	})
	if err != nil {
		t.Fatalf("failed to create fridge: %v", err)
	}

	// Case 1: Empty Fridge -> Must reject cooking immediately!
	_, err = service.CookRecipe(ctx, fridge.ID, u.ID, CookRecipeInput{
		RecipeTitle: "Омлет",
		Servings:    2,
		ExpiryDays:  3,
		Ingredients: []CookIngredient{
			{Name: "Яйця", Quantity: 3, Unit: "шт"},
			{Name: "Молоко", Quantity: 100, Unit: "мл"},
		},
		IgnoreMissing: true, // Even with ignore missing!
	})
	if err == nil {
		t.Fatalf("expected error when cooking from empty fridge, but got nil")
	}

	// Case 2: Fridge has ONLY salt, recipe needs beef and potatoes -> Must reject!
	_, err = queries.CreateProduct(ctx, db.CreateProductParams{
		FridgeID:   fridge.ID,
		Name:       "Сіль кухонна",
		Category:   "spices",
		Quantity:   500,
		Unit:       "г",
		ExpiryDate: pgtype.Date{Time: time.Now().AddDate(0, 1, 0), Valid: true},
		CreatedBy:  pgtype.UUID{Bytes: u.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("failed to add salt: %v", err)
	}

	_, err = service.CookRecipe(ctx, fridge.ID, u.ID, CookRecipeInput{
		RecipeTitle: "Стейк з картоплею",
		Servings:    1,
		ExpiryDays:  3,
		Ingredients: []CookIngredient{
			{Name: "Яловичина", Quantity: 300, Unit: "г"},
			{Name: "Картопля", Quantity: 200, Unit: "г"},
			{Name: "Сіль", Quantity: 5, Unit: "г"},
		},
		IgnoreMissing: true, // Only condiment matched!
	})
	if err == nil {
		t.Fatalf("expected error when only condiments are available and major ingredients missing, but got nil")
	}
}

func TestCookRecipe_SuccessAndDeductionWithDB(t *testing.T) {
	ctx := context.Background()
	dbURL := getTestDBURL()
	dbConn, err := database.Connect(ctx, dbURL)
	if err != nil {
		t.Skip("skipping DB test, cannot connect to PostgreSQL")
		return
	}
	defer dbConn.Close()

	queries := db.New(dbConn.Pool)
	cfg := &config.Config{}
	prodSvc := products.NewService(queries, cfg)
	nutrSvc := nutrition.NewService(queries, dbConn.Pool)
	service := NewService(queries, dbConn.Pool, prodSvc, nutrSvc)

	u, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        "cook_success_" + uuid.New().String()[:8] + "@example.com",
		Name:         "Chef Success",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	defer func() {
		_, _ = dbConn.Pool.Exec(ctx, "DELETE FROM users WHERE id = $1", u.ID)
	}()

	fridge, err := queries.CreateFridge(ctx, db.CreateFridgeParams{
		Name:    "Холодильник Шефа",
		OwnerID: u.ID,
	})
	if err != nil {
		t.Fatalf("failed to create fridge: %v", err)
	}

	// Add products: 500g куряче філе, 4 шт яйця
	pChicken, err := queries.CreateProduct(ctx, db.CreateProductParams{
		FridgeID:   fridge.ID,
		Name:       "Куряче філе",
		Category:   "meat",
		Quantity:   500,
		Unit:       "г",
		ExpiryDate: pgtype.Date{Time: time.Now().AddDate(0, 0, 5), Valid: true},
		Calories:   165,
		Protein:    31,
		Fat:        3.6,
		Carbs:      0,
		CreatedBy:  pgtype.UUID{Bytes: u.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("failed to add chicken: %v", err)
	}

	pEggs, err := queries.CreateProduct(ctx, db.CreateProductParams{
		FridgeID:   fridge.ID,
		Name:       "Яйця курячі",
		Category:   "dairy",
		Quantity:   4,
		Unit:       "шт",
		ExpiryDate: pgtype.Date{Time: time.Now().AddDate(0, 0, 10), Valid: true},
		Calories:   70,
		Protein:    6,
		Fat:        5,
		Carbs:      0.5,
		CreatedBy:  pgtype.UUID{Bytes: u.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("failed to add eggs: %v", err)
	}

	// Cook: uses 200g chicken and 2 eggs
	res, err := service.CookRecipe(ctx, fridge.ID, u.ID, CookRecipeInput{
		RecipeTitle: "Курячий омлет",
		Servings:    2,
		ExpiryDays:  4,
		Ingredients: []CookIngredient{
			{Name: "курка", Quantity: 200, Unit: "г"},
			{Name: "яйця", Quantity: 2, Unit: "шт"},
		},
		IgnoreMissing: false,
		AutoLogAsMeal: true,
		MealType:      "breakfast",
	})
	if err != nil {
		t.Fatalf("failed to cook recipe: %v", err)
	}

	if res.PreparedMeal == nil {
		t.Fatalf("expected prepared meal to be created")
	}
	if len(res.Deductions) != 2 {
		t.Errorf("expected 2 deductions, got %d", len(res.Deductions))
	}

	// Verify stock deduction: Chicken was 500g, now 300g
	updatedChicken, err := queries.GetProductByID(ctx, db.GetProductByIDParams{
		ID:       pChicken.ID,
		FridgeID: fridge.ID,
	})
	if err != nil {
		t.Fatalf("failed to fetch updated chicken: %v", err)
	}
	if updatedChicken.Quantity != 300 {
		t.Errorf("expected chicken quantity 300g, got %v", updatedChicken.Quantity)
	}

	// Eggs were 4 шт, now 2 шт
	updatedEggs, err := queries.GetProductByID(ctx, db.GetProductByIDParams{
		ID:       pEggs.ID,
		FridgeID: fridge.ID,
	})
	if err != nil {
		t.Fatalf("failed to fetch updated eggs: %v", err)
	}
	if updatedEggs.Quantity != 2 {
		t.Errorf("expected eggs quantity 2, got %v", updatedEggs.Quantity)
	}

	// Verify auto-logged meal in nutrition
	if res.LoggedMeal == nil {
		t.Errorf("expected meal to be auto-logged")
	}
}
