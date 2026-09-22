package shopping

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ynshvrh/E-Fridge-Api/internal/database"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/nutrition"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/products"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/units"
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
	pool            *pgxpool.Pool
	productsService *products.Service
}

func NewService(queries *db.Queries, pool *pgxpool.Pool, productsService *products.Service) *Service {
	return &Service{
		queries:         queries,
		pool:            pool,
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

	// Auto-deduplication: check if an unbought shopping item with matching name exists in the fridge
	existingItems, err := s.queries.ListShoppingItemsByFridge(ctx, fridgeID)
	if err == nil {
		for _, ex := range existingItems {
			if !ex.IsBought && strings.EqualFold(strings.TrimSpace(ex.Name), name) {
				// Same item name exists in shopping list! Merge quantities
				qtyToAdd := input.Quantity
				targetUnit := ex.Unit
				if units.AreCompatible(unit, targetUnit) {
					qtyToAdd = units.Convert(qtyToAdd, unit, targetUnit)
				}
				newQty := units.Round(ex.Quantity+qtyToAdd, 2)
				updatedCat := ex.Category
				if updatedCat == "other" && category != "other" {
					updatedCat = category
				}

				updated, upErr := s.queries.UpdateShoppingItem(ctx, db.UpdateShoppingItemParams{
					ID:       ex.ID,
					FridgeID: fridgeID,
					Name:     ex.Name,
					Category: updatedCat,
					Quantity: newQty,
					Unit:     targetUnit,
					IsBought: ex.IsBought,
				})
				if upErr == nil {
					dto := toDTO(updated)
					return &dto, nil
				}
			}
		}
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

// PurchaseAndMoveToFridge takes a shopping item, creates or merges into a corresponding product in the fridge, and deletes the shopping item.
func (s *Service) PurchaseAndMoveToFridge(ctx context.Context, fridgeID, userID, itemID uuid.UUID, input PurchaseItemInput) (*products.ProductDTO, error) {
	var resultProduct *products.ProductDTO

	execFunc := func(q *db.Queries) error {
		item, err := q.GetShoppingItemByID(ctx, db.GetShoppingItemByIDParams{
			ID:       itemID,
			FridgeID: fridgeID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrItemNotFound
			}
			return fmt.Errorf("failed to get shopping item: %w", err)
		}

		quantity := item.Quantity
		if input.Quantity != nil && *input.Quantity > 0 {
			quantity = *input.Quantity
		}

		unit := strings.TrimSpace(item.Unit)
		if unit == "" {
			unit = "шт"
		}

		// Auto-deduplication: check if product already exists in the fridge
		fridgeProducts, err := q.ListProductsByFridge(ctx, fridgeID)
		if err != nil {
			return fmt.Errorf("failed to list products in fridge: %w", err)
		}

		candidate := findMatchingProduct(fridgeProducts, item.Name)
		if candidate != nil && units.AreCompatible(unit, candidate.Unit) {
			// Convert quantity to candidate's storage unit
			convertedQty := units.Convert(quantity, unit, candidate.Unit)
			newTotalQty := units.Round(candidate.Quantity+convertedQty, 2)
			updated, updateErr := q.UpdateProductQuantity(ctx, db.UpdateProductQuantityParams{
				ID:       candidate.ID,
				FridgeID: fridgeID,
				Quantity: newTotalQty,
			})
			if updateErr != nil {
				return fmt.Errorf("failed to update product quantity: %w", updateErr)
			}

			// Delete from shopping list once transferred to fridge
			_ = q.DeleteShoppingItem(ctx, db.DeleteShoppingItemParams{
				ID:       itemID,
				FridgeID: fridgeID,
			})

			dto := products.ToDTO(updated)
			resultProduct = &dto
			return nil
		}

		// No matching candidate or incompatible unit: create new product in fridge
		expiryDays := input.ExpiryDays
		if expiryDays <= 0 {
			expiryDays = 7 // default 1 week
		}
		expiryDateStr := time.Now().AddDate(0, 0, expiryDays).Format("2006-01-02")
		t, err := time.Parse("2006-01-02", expiryDateStr)
		var expiry pgtype.Date
		if err == nil {
			expiry = pgtype.Date{Time: t, Valid: true}
		}

		cals, p, f, c := nutrition.CalculateEstimatedNutrition(item.Name, quantity, unit)

		newProd, err := q.CreateProduct(ctx, db.CreateProductParams{
			FridgeID:   fridgeID,
			Name:       item.Name,
			Category:   item.Category,
			Quantity:   quantity,
			Unit:       unit,
			ExpiryDate: expiry,
			Calories:   int32(cals),
			Protein:    p,
			Fat:        f,
			Carbs:      c,
			Notes:      "Куплено зі списку покупок",
			CreatedBy:  pgtype.UUID{Bytes: userID, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("failed to create product in fridge: %w", err)
		}

		// Delete from shopping list once transferred to fridge
		_ = q.DeleteShoppingItem(ctx, db.DeleteShoppingItemParams{
			ID:       itemID,
			FridgeID: fridgeID,
		})

		dto := products.ToDTO(newProd)
		resultProduct = &dto
		return nil
	}

	if s.pool != nil {
		if err := database.WithTransaction(ctx, s.pool, execFunc); err != nil {
			return nil, err
		}
	} else {
		if err := execFunc(s.queries); err != nil {
			return nil, err
		}
	}

	return resultProduct, nil
}

func findMatchingProduct(prods []db.Product, itemName string) *db.Product {
	target := strings.ToLower(strings.TrimSpace(itemName))
	if target == "" {
		return nil
	}

	// 1. Exact case-insensitive match
	for i := range prods {
		if strings.ToLower(strings.TrimSpace(prods[i].Name)) == target {
			return &prods[i]
		}
	}

	// 2. Prefix or substring match (e.g. "Молоко" matches "Молоко 2.5%" or vice-versa)
	for i := range prods {
		pName := strings.ToLower(strings.TrimSpace(prods[i].Name))
		if strings.HasPrefix(pName, target) || strings.HasPrefix(target, pName) {
			return &prods[i]
		}
	}

	return nil
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
