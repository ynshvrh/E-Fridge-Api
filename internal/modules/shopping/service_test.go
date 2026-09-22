package shopping

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
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/products"
)

func getTestDBURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://postgres:postgrespassword@localhost:5432/e_fridge?sslmode=disable"
}

func TestToDTO(t *testing.T) {
	id := uuid.New()
	fridgeID := uuid.New()
	userID := uuid.New()
	now := time.Now()

	item := db.ShoppingItem{
		ID:        id,
		FridgeID:  fridgeID,
		Name:      "Яблука",
		Category:  "fruits",
		Quantity:  1.5,
		Unit:      "кг",
		IsBought:  false,
		CreatedBy: userID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	dto := toDTO(item)

	if dto.ID != id {
		t.Errorf("expected ID %v, got %v", id, dto.ID)
	}
	if dto.Name != "Яблука" {
		t.Errorf("expected name 'Яблука', got %s", dto.Name)
	}
	if dto.Quantity != 1.5 {
		t.Errorf("expected quantity 1.5, got %f", dto.Quantity)
	}
	if dto.IsBought {
		t.Errorf("expected isBought false, got true")
	}
}

func TestFindMatchingProduct(t *testing.T) {
	prods := []db.Product{
		{ID: uuid.New(), Name: "Молоко 2.5%", Unit: "л", Quantity: 1.0},
		{ID: uuid.New(), Name: "Яйця курячі С0", Unit: "шт", Quantity: 10.0},
		{ID: uuid.New(), Name: "Вівсянка", Unit: "г", Quantity: 500.0},
	}

	// 1. Exact match
	m1 := findMatchingProduct(prods, "вівсянка")
	if m1 == nil || m1.Name != "Вівсянка" {
		t.Errorf("expected to match 'Вівсянка', got %v", m1)
	}

	// 2. Prefix match (item is prefix of product)
	m2 := findMatchingProduct(prods, "Молоко")
	if m2 == nil || m2.Name != "Молоко 2.5%" {
		t.Errorf("expected to match 'Молоко 2.5%%', got %v", m2)
	}

	// 3. Product is prefix of item
	m3 := findMatchingProduct(prods, "Вівсянка плющена")
	if m3 == nil || m3.Name != "Вівсянка" {
		t.Errorf("expected to match 'Вівсянка', got %v", m3)
	}

	// 4. Non-matching
	m4 := findMatchingProduct(prods, "Авокадо")
	if m4 != nil {
		t.Errorf("expected nil for 'Авокадо', got %v", m4)
	}
}

func TestShoppingAutoDeduplicationWithDB(t *testing.T) {
	ctx := context.Background()
	dbURL := getTestDBURL()
	dbConn, err := database.Connect(ctx, dbURL)
	if err != nil {
		t.Skip("skipping DB test, cannot connect to PostgreSQL")
		return
	}
	defer dbConn.Close()

	queries := db.New(dbConn.Pool)
	prodService := products.NewService(queries, &config.Config{})
	shopService := NewService(queries, dbConn.Pool, prodService)

	// Setup user & fridge
	u, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        "shop_dedup_" + uuid.New().String()[:8] + "@example.com",
		Name:         "Shop Tester",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	defer func() {
		_, _ = dbConn.Pool.Exec(ctx, "DELETE FROM users WHERE id = $1", u.ID)
	}()

	f, err := queries.CreateFridge(ctx, db.CreateFridgeParams{
		Name:    "Deduplication Test Fridge",
		OwnerID: u.ID,
	})
	if err != nil {
		t.Fatalf("failed to create fridge: %v", err)
	}
	defer func() {
		_, _ = dbConn.Pool.Exec(ctx, "DELETE FROM fridges WHERE id = $1", f.ID)
	}()

	// 1. Add "Молоко" 1.0 л
	i1, err := shopService.CreateItem(ctx, f.ID, u.ID, CreateItemInput{
		Name:     "Молоко",
		Quantity: 1.0,
		Unit:     "л",
		Category: "dairy",
	})
	if err != nil {
		t.Fatalf("failed to create shopping item 1: %v", err)
	}

	// 2. Add "молоко" 500 мл (should merge into 1.5 л)
	i2, err := shopService.CreateItem(ctx, f.ID, u.ID, CreateItemInput{
		Name:     "молоко",
		Quantity: 500,
		Unit:     "мл",
		Category: "dairy",
	})
	if err != nil {
		t.Fatalf("failed to create shopping item 2: %v", err)
	}

	if i1.ID != i2.ID {
		t.Errorf("expected items to merge into same ID, got %v vs %v", i1.ID, i2.ID)
	}
	if i2.Quantity != 1.5 {
		t.Errorf("expected merged quantity 1.5 л, got %v", i2.Quantity)
	}

	// Verify only 1 item in shopping list
	summary, err := shopService.ListItems(ctx, f.ID)
	if err != nil {
		t.Fatalf("failed to list shopping items: %v", err)
	}
	if summary.TotalCount != 1 {
		t.Errorf("expected exactly 1 item in shopping list, got %d", summary.TotalCount)
	}

	// 3. Test PurchaseAndMoveToFridge with product deduplication in fridge:
	// Create an existing product in fridge: "Вівсянка" 500 г
	expStr := time.Now().AddDate(0, 0, 14).Format("2006-01-02")
	tExp, _ := time.Parse("2006-01-02", expStr)
	existingProd, err := queries.CreateProduct(ctx, db.CreateProductParams{
		FridgeID:   f.ID,
		Name:       "Вівсянка",
		Category:   "groceries",
		Quantity:   500.0,
		Unit:       "г",
		ExpiryDate: pgtype.Date{Time: tExp, Valid: true},
		CreatedBy:  pgtype.UUID{Bytes: u.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("failed to create existing product: %v", err)
	}

	// Create a shopping item "вівсянка" with 1.0 кг
	oatmealItem, err := shopService.CreateItem(ctx, f.ID, u.ID, CreateItemInput{
		Name:     "вівсянка",
		Quantity: 1.0,
		Unit:     "кг",
		Category: "groceries",
	})
	if err != nil {
		t.Fatalf("failed to create oatmeal shopping item: %v", err)
	}

	// Move oatmeal shopping item to fridge
	resProd, err := shopService.PurchaseAndMoveToFridge(ctx, f.ID, u.ID, oatmealItem.ID, PurchaseItemInput{})
	if err != nil {
		t.Fatalf("PurchaseAndMoveToFridge failed: %v", err)
	}

	// Should merge into existing product ID!
	if resProd.ID != existingProd.ID {
		t.Errorf("expected merged product ID %v, got %v", existingProd.ID, resProd.ID)
	}
	// 500g + 1kg (1000g) = 1500g
	if resProd.Quantity != 1500.0 {
		t.Errorf("expected merged quantity 1500.0 g, got %v", resProd.Quantity)
	}

	// Verify shopping item was deleted
	afterSummary, err := shopService.ListItems(ctx, f.ID)
	if err != nil {
		t.Fatalf("failed to list shopping items after purchase: %v", err)
	}
	for _, itm := range afterSummary.Items {
		if itm.ID == oatmealItem.ID {
			t.Errorf("expected shopping item %v to be deleted after purchase", oatmealItem.ID)
		}
	}
}
