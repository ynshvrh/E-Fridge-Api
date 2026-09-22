package recipes

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

func TestToDTO(t *testing.T) {
	id := uuid.New()
	userID := uuid.New()
	fridgeID := uuid.New()
	now := time.Now()

	r := db.SavedRecipe{
		ID:           id,
		UserID:       userID,
		FridgeID:     pgtype.UUID{Bytes: fridgeID, Valid: true},
		Title:        "Сирники з медом",
		Description:  "Класичні ніжні сирники",
		Ingredients:  []byte(`[{"name":"Сир","amount":400,"unit":"г","in_fridge":true}]`),
		Steps:        []byte(`["Змішати інгредієнти","Обсмажити на пательні"]`),
		Calories:     320,
		Protein:      22,
		Fat:          12,
		Carbs:        28,
		PrepTimeMins: 10,
		CookTimeMins: 15,
		Servings:     2,
		CreatedAt:    now,
	}

	dto, err := toDTO(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dto.ID != id {
		t.Errorf("expected ID %v, got %v", id, dto.ID)
	}
	if dto.Title != "Сирники з медом" {
		t.Errorf("expected Title 'Сирники з медом', got %s", dto.Title)
	}
	if len(dto.Ingredients) != 1 || dto.Ingredients[0].Name != "Сир" {
		t.Errorf("expected 1 ingredient with name 'Сир', got %v", dto.Ingredients)
	}
	if len(dto.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(dto.Steps))
	}
}

func getTestDBURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://postgres:postgrespassword@localhost:5432/e_fridge?sslmode=disable"
}

func TestRecipesFridgeScopingWithDB(t *testing.T) {
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

	// Setup users
	u1, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        "recipe_user1_" + uuid.New().String()[:8] + "@example.com",
		Name:         "Recipe User 1",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create user 1: %v", err)
	}
	defer func() {
		_, _ = dbConn.Pool.Exec(ctx, "DELETE FROM users WHERE id = $1", u1.ID)
	}()

	u2, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        "recipe_user2_" + uuid.New().String()[:8] + "@example.com",
		Name:         "Recipe User 2",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create user 2: %v", err)
	}
	defer func() {
		_, _ = dbConn.Pool.Exec(ctx, "DELETE FROM users WHERE id = $1", u2.ID)
	}()

	// Setup shared fridge
	f, err := queries.CreateFridge(ctx, db.CreateFridgeParams{
		Name:    "Family Kitchen",
		OwnerID: u1.ID,
	})
	if err != nil {
		t.Fatalf("failed to create fridge: %v", err)
	}
	defer func() {
		_, _ = dbConn.Pool.Exec(ctx, "DELETE FROM fridges WHERE id = $1", f.ID)
	}()

	// Add u2 to the shared fridge
	_, err = queries.AddFridgeMember(ctx, db.AddFridgeMemberParams{
		FridgeID: f.ID,
		UserID:   u2.ID,
		Role:     "member",
	})
	if err != nil {
		t.Fatalf("failed to add member: %v", err)
	}

	// 1. u1 creates a saved recipe associated with fridge f.ID
	created, err := service.CreateRecipe(ctx, f.ID, u1.ID, CreateRecipeInput{
		Title:       "Борщ український",
		Description: "Традиційний борщ",
		Ingredients: []RecipeIngredient{
			{Name: "Буряк", Amount: 2, Unit: "шт"},
			{Name: "Капуста", Amount: 300, Unit: "г"},
		},
		Steps:        []string{"Нарізати овочі", "Зварити бульйон"},
		Calories:     250,
		Protein:      15,
		Fat:          8,
		Carbs:        30,
		PrepTimeMins: 20,
		CookTimeMins: 40,
		Servings:     4,
	})
	if err != nil {
		t.Fatalf("failed to create recipe: %v", err)
	}

	// 2. u2 (member of the same fridge) should see this recipe in the fridge list!
	u2Recipes, err := service.ListRecipes(ctx, f.ID, u2.ID)
	if err != nil {
		t.Fatalf("failed to list recipes for u2: %v", err)
	}
	if len(u2Recipes) != 1 || u2Recipes[0].ID != created.ID {
		t.Errorf("expected u2 to see the shared recipe in the fridge, got: %v", u2Recipes)
	}

	// 3. u2 can fetch the recipe details
	fetched, err := service.GetRecipe(ctx, f.ID, u2.ID, created.ID)
	if err != nil {
		t.Fatalf("failed to get recipe for u2: %v", err)
	}
	if fetched.Title != "Борщ український" {
		t.Errorf("expected title 'Борщ український', got %s", fetched.Title)
	}

	// 4. Another unrelated fridge should NOT see this recipe
	otherFridge, err := queries.CreateFridge(ctx, db.CreateFridgeParams{
		Name:    "Other Fridge",
		OwnerID: u2.ID,
	})
	if err != nil {
		t.Fatalf("failed to create other fridge: %v", err)
	}
	defer func() {
		_, _ = dbConn.Pool.Exec(ctx, "DELETE FROM fridges WHERE id = $1", otherFridge.ID)
	}()

	otherList, err := service.ListRecipes(ctx, otherFridge.ID, u2.ID)
	if err != nil {
		t.Fatalf("failed to list recipes for other fridge: %v", err)
	}
	if len(otherList) != 0 {
		t.Errorf("expected 0 recipes in unrelated fridge, got %d", len(otherList))
	}

	// 5. Delete recipe
	err = service.DeleteRecipe(ctx, f.ID, u1.ID, created.ID)
	if err != nil {
		t.Fatalf("failed to delete recipe: %v", err)
	}
}
