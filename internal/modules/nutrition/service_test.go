package nutrition

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ynshvrh/E-Fridge-Api/internal/database"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
)

func getTestDBURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://postgres:postgrespassword@localhost:5432/e_fridge?sslmode=disable"
}

func TestUpdateNutritionLogWithDB(t *testing.T) {
	ctx := context.Background()
	dbURL := getTestDBURL()
	dbConn, err := database.Connect(ctx, dbURL)
	if err != nil {
		t.Skip("skipping DB test, cannot connect to PostgreSQL")
		return
	}
	defer dbConn.Close()

	queries := db.New(dbConn.Pool)
	service := NewService(queries)

	// Setup user
	u, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        "nutr_update_" + uuid.New().String()[:8] + "@example.com",
		Name:         "Nutr Tester",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	defer func() {
		_, _ = dbConn.Pool.Exec(ctx, "DELETE FROM users WHERE id = $1", u.ID)
	}()

	// 1. Log a meal
	logged, err := service.LogMeal(ctx, u.ID, LogMealInput{
		Date:     time.Now().Format("2006-01-02"),
		MealType: "lunch",
		FoodName: "Куряче філе",
		Quantity: 100,
		Unit:     "г",
		Calories: 165,
		Protein:  31,
		Fat:      3.6,
		Carbs:    0,
	})
	if err != nil {
		t.Fatalf("failed to log meal: %v", err)
	}

	// 2. Update log to 200g
	updated, err := service.UpdateLog(ctx, logged.ID, u.ID, UpdateLogInput{
		MealType: "lunch",
		FoodName: "Куряче філе на грилі",
		Quantity: 200,
		Unit:     "г",
	})
	if err != nil {
		t.Fatalf("failed to update log: %v", err)
	}

	if updated.Quantity != 200 {
		t.Errorf("expected quantity 200, got %v", updated.Quantity)
	}
	if updated.Calories != 330 {
		t.Errorf("expected calories 330 (scaled 2x), got %d", updated.Calories)
	}
	if updated.FoodName != "Куряче філе на грилі" {
		t.Errorf("expected food name updated, got %s", updated.FoodName)
	}
}

func TestDeleteLogRestoresToFridgeWithDB(t *testing.T) {
	ctx := context.Background()
	dbURL := getTestDBURL()
	dbConn, err := database.Connect(ctx, dbURL)
	if err != nil {
		t.Skip("skipping DB test, cannot connect to PostgreSQL")
		return
	}
	defer dbConn.Close()

	queries := db.New(dbConn.Pool)
	service := NewService(queries)

	// Setup user & fridge
	u, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        "nutr_restore_" + uuid.New().String()[:8] + "@example.com",
		Name:         "Fridge Restorer",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	defer func() {
		_, _ = dbConn.Pool.Exec(ctx, "DELETE FROM users WHERE id = $1", u.ID)
	}()

	fridge, err := queries.CreateFridge(ctx, db.CreateFridgeParams{
		Name:    "Тестовий холодильник",
		OwnerID: u.ID,
	})
	if err != nil {
		t.Fatalf("failed to create fridge: %v", err)
	}

	// Create product in fridge: 500g Сир кисломолочний
	prod, err := queries.CreateProduct(ctx, db.CreateProductParams{
		FridgeID:   fridge.ID,
		Name:       "Сир кисломолочний",
		Category:   "dairy",
		Quantity:   350, // 350g remaining after eating 150g
		Unit:       "г",
		ExpiryDate: pgtype.Date{Time: time.Now().AddDate(0, 0, 5), Valid: true},
		Calories:   100,
		Protein:    16,
		Fat:        5,
		Carbs:      3,
		Notes:      "",
		CreatedBy:  pgtype.UUID{Bytes: u.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("failed to create product: %v", err)
	}

	// Log meal of 150g that was consumed from this product & fridge
	logged, err := service.LogMeal(ctx, u.ID, LogMealInput{
		Date:      time.Now().Format("2006-01-02"),
		MealType:  "breakfast",
		FoodName:  prod.Name,
		Quantity:  150,
		Unit:      "г",
		Calories:  150,
		Protein:   24,
		Fat:       7.5,
		Carbs:     4.5,
		ProductID: &prod.ID,
		FridgeID:  &fridge.ID,
	})
	if err != nil {
		t.Fatalf("failed to log meal: %v", err)
	}

	// Now accidentally logged, user deletes it from Calorie Tracker:
	deleteRes, err := service.DeleteLog(ctx, logged.ID, u.ID)
	if err != nil {
		t.Fatalf("failed to delete log: %v", err)
	}

	if !deleteRes.RestoredToFridge {
		t.Errorf("expected product to be restored to fridge")
	}

	// Check fridge product quantity: 350 + 150 = 500g!
	updatedProd, err := queries.GetProductByID(ctx, db.GetProductByIDParams{
		ID:       prod.ID,
		FridgeID: fridge.ID,
	})
	if err != nil {
		t.Fatalf("failed to get updated product: %v", err)
	}

	if updatedProd.Quantity != 500 {
		t.Errorf("expected restored quantity 500, got %v", updatedProd.Quantity)
	}
}
