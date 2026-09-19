package shopping

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/nutrition"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/products"
)

var (
	ErrItemNotFound   = errors.New("shopping item not found")
	ErrEmptyName      = errors.New("item name cannot be empty")
	ErrInvalidQuantity = errors.New("quantity must be greater than 0")
)

type ShoppingItemDTO struct {
	ID        uuid.UUID `json:"id"`
	FridgeID  uuid.UUID `json:"fridge_id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	Quantity  float64   `json:"quantity"`
	Unit      string    `json:"unit"`
	IsBought  bool      `json:"is_bought"`
	CreatedBy uuid.UUID `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ShoppingSummaryDTO struct {
	Items       []ShoppingItemDTO `json:"items"`
	TotalCount  int               `json:"total_count"`
	BoughtCount int               `json:"bought_count"`
	LeftCount   int               `json:"left_count"`
}

type CreateItemInput struct {
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	IsBought bool    `json:"is_bought"`
}

type UpdateItemInput struct {
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	IsBought bool    `json:"is_bought"`
}

type PurchaseItemInput struct {
	ExpiryDays int      `json:"expiry_days"`
	Quantity   *float64 `json:"quantity,omitempty"`
}

type Service struct {
	queries         *db.Queries
	productsService *products.Service
}

func NewService(queries *db.Queries, productsService *products.Service) *Service {
	return &Service{
		queries:         queries,
		productsService: productsService,
	}
}

func (s *Service) ListItems(ctx context.Context, fridgeID uuid.UUID) (*ShoppingSummaryDTO, error) {
	rows, err := s.queries.ListShoppingItemsByFridge(ctx, fridgeID)
	if err != nil {
		return nil, fmt.Errorf("failed to list shopping items: %w", err)
	}

	items := make([]ShoppingItemDTO, 0, len(rows))
	boughtCount := 0

	for _, r := range rows {
		dto := toDTO(r)
		if dto.IsBought {
			boughtCount++
		}
		items = append(items, dto)
	}

	return &ShoppingSummaryDTO{
		Items:       items,
		TotalCount:  len(items),
		BoughtCount: boughtCount,
		LeftCount:   len(items) - boughtCount,
	}, nil
}

func (s *Service) CreateItem(ctx context.Context, fridgeID, userID uuid.UUID, input CreateItemInput) (*ShoppingItemDTO, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrEmptyName
	}
	if input.Quantity <= 0 {
		input.Quantity = 1.0
	}
	category := strings.TrimSpace(input.Category)
	if category == "" {
		category = "other"
	}
	unit := strings.TrimSpace(input.Unit)
	if unit == "" {
		unit = "шт"
	}

	item, err := s.queries.CreateShoppingItem(ctx, db.CreateShoppingItemParams{
		FridgeID:  fridgeID,
		Name:      name,
		Category:  category,
		Quantity:  input.Quantity,
		Unit:      unit,
		IsBought:  input.IsBought,
		CreatedBy: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create shopping item: %w", err)
	}

	dto := toDTO(item)
	return &dto, nil
}

func (s *Service) BatchCreateItems(ctx context.Context, fridgeID, userID uuid.UUID, inputs []CreateItemInput) ([]ShoppingItemDTO, error) {
	result := make([]ShoppingItemDTO, 0, len(inputs))
	for _, input := range inputs {
		dto, err := s.CreateItem(ctx, fridgeID, userID, input)
		if err != nil {
			continue // skip empty or invalid items gracefully
		}
		result = append(result, *dto)
	}
	return result, nil
}

func (s *Service) UpdateItem(ctx context.Context, fridgeID, itemID uuid.UUID, input UpdateItemInput) (*ShoppingItemDTO, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrEmptyName
	}
	if input.Quantity <= 0 {
		return nil, ErrInvalidQuantity
	}
	category := strings.TrimSpace(input.Category)
	if category == "" {
		category = "other"
	}
	unit := strings.TrimSpace(input.Unit)
	if unit == "" {
		unit = "шт"
	}

	item, err := s.queries.UpdateShoppingItem(ctx, db.UpdateShoppingItemParams{
		ID:       itemID,
		FridgeID: fridgeID,
		Name:     name,
		Category: category,
		Quantity: input.Quantity,
		Unit:     unit,
		IsBought: input.IsBought,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrItemNotFound
		}
		return nil, fmt.Errorf("failed to update shopping item: %w", err)
	}

	dto := toDTO(item)
	return &dto, nil
}

func (s *Service) ToggleItemBought(ctx context.Context, fridgeID, itemID uuid.UUID, isBought bool) (*ShoppingItemDTO, error) {
	item, err := s.queries.ToggleShoppingItemBought(ctx, db.ToggleShoppingItemBoughtParams{
		ID:       itemID,
		FridgeID: fridgeID,
		IsBought: isBought,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrItemNotFound
		}
		return nil, fmt.Errorf("failed to toggle shopping item: %w", err)
	}

	dto := toDTO(item)
	return &dto, nil
}

func (s *Service) DeleteItem(ctx context.Context, fridgeID, itemID uuid.UUID) error {
	err := s.queries.DeleteShoppingItem(ctx, db.DeleteShoppingItemParams{
		ID:       itemID,
		FridgeID: fridgeID,
	})
	if err != nil {
		return fmt.Errorf("failed to delete shopping item: %w", err)
	}
	return nil
}

func (s *Service) ClearBoughtItems(ctx context.Context, fridgeID uuid.UUID) error {
	return s.queries.DeleteBoughtShoppingItems(ctx, fridgeID)
}

func (s *Service) ClearAllItems(ctx context.Context, fridgeID uuid.UUID) error {
	return s.queries.ClearShoppingList(ctx, fridgeID)
}

// PurchaseAndMoveToFridge takes a shopping item, creates a corresponding product in the fridge, and deletes the shopping item.
func (s *Service) PurchaseAndMoveToFridge(ctx context.Context, fridgeID, userID, itemID uuid.UUID, input PurchaseItemInput) (*products.ProductDTO, error) {
	item, err := s.queries.GetShoppingItemByID(ctx, db.GetShoppingItemByIDParams{
		ID:       itemID,
		FridgeID: fridgeID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrItemNotFound
		}
		return nil, fmt.Errorf("failed to get shopping item: %w", err)
	}

	quantity := item.Quantity
	if input.Quantity != nil && *input.Quantity > 0 {
		quantity = *input.Quantity
	}

	expiryDays := input.ExpiryDays
	if expiryDays <= 0 {
		expiryDays = 7 // default 1 week
	}
	expiryDateStr := time.Now().AddDate(0, 0, expiryDays).Format("2006-01-02")

	// Calculate default nutritional profile if available
	cals, p, f, c := nutrition.CalculateEstimatedNutrition(item.Name, quantity, item.Unit)

	prod, err := s.productsService.CreateProduct(ctx, fridgeID, userID, products.CreateProductInput{
		Name:       item.Name,
		Category:   item.Category,
		Quantity:   quantity,
		Unit:       item.Unit,
		ExpiryDate: &expiryDateStr,
		Calories:   int32(cals),
		Protein:    p,
		Fat:        f,
		Carbs:      c,
		Notes:      "Куплено зі списку покупок",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create product in fridge: %w", err)
	}

	// Delete from shopping list once transferred to fridge
	_ = s.queries.DeleteShoppingItem(ctx, db.DeleteShoppingItemParams{
		ID:       itemID,
		FridgeID: fridgeID,
	})

	return prod, nil
}

func toDTO(item db.ShoppingItem) ShoppingItemDTO {
	return ShoppingItemDTO{
		ID:        item.ID,
		FridgeID:  item.FridgeID,
		Name:      item.Name,
		Category:  item.Category,
		Quantity:  item.Quantity,
		Unit:      item.Unit,
		IsBought:  item.IsBought,
		CreatedBy: item.CreatedBy,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}
