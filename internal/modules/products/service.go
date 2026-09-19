package products

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ynshvrh/E-Fridge-Api/internal/config"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
)

var (
	ErrProductNotFound = errors.New("product not found")
	ErrInvalidQuantity = errors.New("quantity must be greater than 0")
	ErrEmptyName       = errors.New("product name cannot be empty")
)

type CategoryInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon"`
}

var DefaultCategories = []CategoryInfo{
	{ID: "dairy", Label: "Молочні продукти", Icon: "Milk"},
	{ID: "meat-fish", Label: "М'ясо та риба", Icon: "Beef"},
	{ID: "vegetables", Label: "Овочі та зелень", Icon: "Carrot"},
	{ID: "fruits", Label: "Фрукти та ягоди", Icon: "Apple"},
	{ID: "bakery", Label: "Хліб та випічка", Icon: "Croissant"},
	{ID: "pantry", Label: "Бакалія", Icon: "Wheat"},
	{ID: "snacks", Label: "Снеки та солодощі", Icon: "Cookie"},
	{ID: "drinks", Label: "Напої", Icon: "CupSoda"},
	{ID: "alcohol", Label: "Алкоголь", Icon: "Wine"},
	{ID: "sauces", Label: "Соуси та приправи", Icon: "Salad"},
	{ID: "frozen", Label: "Заморожені продукти", Icon: "Snowflake"},
	{ID: "canned-prepared", Label: "Консервація", Icon: "Box"},
	{ID: "prepared-meals", Label: "Готові страви", Icon: "CookingPot"},
	{ID: "other", Label: "Інше", Icon: "Package"},
}

type ProductDTO struct {
	ID         uuid.UUID  `json:"id"`
	FridgeID   uuid.UUID  `json:"fridge_id"`
	Name       string     `json:"name"`
	Category   string     `json:"category"`
	Quantity   float64    `json:"quantity"`
	Unit       string     `json:"unit"`
	ExpiryDate *string    `json:"expiry_date,omitempty"`
	DaysLeft   *int       `json:"days_left,omitempty"`
	Status     string     `json:"status"` // "good", "warning", "expired"
	Calories   int32      `json:"calories"`
	Protein    float64    `json:"protein"`
	Fat        float64    `json:"fat"`
	Carbs      float64    `json:"carbs"`
	Notes      string     `json:"notes"`
	CreatedBy  uuid.UUID  `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type CreateProductInput struct {
	Name       string  `json:"name"`
	Category   string  `json:"category"`
	Quantity   float64 `json:"quantity"`
	Unit       string  `json:"unit"`
	ExpiryDate *string `json:"expiry_date"`
	Calories   int32   `json:"calories"`
	Protein    float64 `json:"protein"`
	Fat        float64 `json:"fat"`
	Carbs      float64 `json:"carbs"`
	Notes      string  `json:"notes"`
}

type UpdateProductInput struct {
	Name       string  `json:"name"`
	Category   string  `json:"category"`
	Quantity   float64 `json:"quantity"`
	Unit       string  `json:"unit"`
	ExpiryDate *string `json:"expiry_date"`
	Calories   int32   `json:"calories"`
	Protein    float64 `json:"protein"`
	Fat        float64 `json:"fat"`
	Carbs      float64 `json:"carbs"`
	Notes      string  `json:"notes"`
}

type Service struct {
	queries    *db.Queries
	cfg        *config.Config
	httpClient *http.Client
}

func NewService(queries *db.Queries, cfg *config.Config) *Service {
	return &Service{
		queries: queries,
		cfg:     cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *Service) GetCategories() []CategoryInfo {
	return DefaultCategories
}

func (s *Service) ListProducts(ctx context.Context, fridgeID uuid.UUID, category, search string) ([]ProductDTO, error) {
	rows, err := s.queries.ListProductsByFridge(ctx, fridgeID)
	if err != nil {
		return nil, fmt.Errorf("failed to list products: %w", err)
	}

	searchLower := strings.ToLower(strings.TrimSpace(search))
	categoryLower := strings.ToLower(strings.TrimSpace(category))

	var result []ProductDTO
	for _, p := range rows {
		if categoryLower != "" && categoryLower != "all" && strings.ToLower(p.Category) != categoryLower {
			continue
		}
		if searchLower != "" {
			if !strings.Contains(strings.ToLower(p.Name), searchLower) &&
				!strings.Contains(strings.ToLower(p.Notes), searchLower) {
				continue
			}
		}
		result = append(result, toDTO(p))
	}

	if result == nil {
		result = []ProductDTO{}
	}
	return result, nil
}

func (s *Service) CreateProduct(ctx context.Context, fridgeID, userID uuid.UUID, input CreateProductInput) (*ProductDTO, error) {
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
		unit = "pcs"
	}

	var expiry pgtype.Date
	if input.ExpiryDate != nil && *input.ExpiryDate != "" {
		t, err := time.Parse("2006-01-02", *input.ExpiryDate)
		if err == nil {
			expiry = pgtype.Date{Time: t, Valid: true}
		}
	}

	p, err := s.queries.CreateProduct(ctx, db.CreateProductParams{
		FridgeID:   fridgeID,
		Name:       name,
		Category:   category,
		Quantity:   input.Quantity,
		Unit:       unit,
		ExpiryDate: expiry,
		Calories:   input.Calories,
		Protein:    input.Protein,
		Fat:        input.Fat,
		Carbs:      input.Carbs,
		Notes:      input.Notes,
		CreatedBy:  userID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create product: %w", err)
	}

	dto := toDTO(p)
	return &dto, nil
}

func (s *Service) GetProduct(ctx context.Context, fridgeID, id uuid.UUID) (*ProductDTO, error) {
	p, err := s.queries.GetProductByID(ctx, db.GetProductByIDParams{
		ID:       id,
		FridgeID: fridgeID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProductNotFound
		}
		return nil, err
	}

	dto := toDTO(p)
	return &dto, nil
}

func (s *Service) UpdateProduct(ctx context.Context, fridgeID, id uuid.UUID, input UpdateProductInput) (*ProductDTO, error) {
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
		unit = "pcs"
	}

	var expiry pgtype.Date
	if input.ExpiryDate != nil && *input.ExpiryDate != "" {
		t, err := time.Parse("2006-01-02", *input.ExpiryDate)
		if err == nil {
			expiry = pgtype.Date{Time: t, Valid: true}
		}
	}

	p, err := s.queries.UpdateProduct(ctx, db.UpdateProductParams{
		ID:         id,
		FridgeID:   fridgeID,
		Name:       name,
		Category:   category,
		Quantity:   input.Quantity,
		Unit:       unit,
		ExpiryDate: expiry,
		Calories:   input.Calories,
		Protein:    input.Protein,
		Fat:        input.Fat,
		Carbs:      input.Carbs,
		Notes:      input.Notes,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProductNotFound
		}
		return nil, err
	}

	dto := toDTO(p)
	return &dto, nil
}

func (s *Service) ConsumeProduct(ctx context.Context, fridgeID, id uuid.UUID, amount float64) (*ProductDTO, error) {
	if amount <= 0 {
		amount = 1.0
	}

	p, err := s.queries.GetProductByID(ctx, db.GetProductByIDParams{
		ID:       id,
		FridgeID: fridgeID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProductNotFound
		}
		return nil, err
	}

	newQty := p.Quantity - amount
	if newQty <= 0 {
		// Delete product if fully consumed
		if err := s.queries.DeleteProduct(ctx, db.DeleteProductParams{
			ID:       id,
			FridgeID: fridgeID,
		}); err != nil {
			return nil, err
		}
		p.Quantity = 0
		dto := toDTO(p)
		return &dto, nil
	}

	updated, err := s.queries.UpdateProductQuantity(ctx, db.UpdateProductQuantityParams{
		ID:       id,
		FridgeID: fridgeID,
		Quantity: newQty,
	})
	if err != nil {
		return nil, err
	}

	dto := toDTO(updated)
	return &dto, nil
}

func (s *Service) DeleteProduct(ctx context.Context, fridgeID, id uuid.UUID) error {
	return s.queries.DeleteProduct(ctx, db.DeleteProductParams{
		ID:       id,
		FridgeID: fridgeID,
	})
}

func (s *Service) ClearFridge(ctx context.Context, fridgeID uuid.UUID) error {
	return s.queries.DeleteAllProductsByFridge(ctx, fridgeID)
}

func toDTO(p db.Product) ProductDTO {
	dto := ProductDTO{
		ID:        p.ID,
		FridgeID:  p.FridgeID,
		Name:      p.Name,
		Category:  p.Category,
		Quantity:  p.Quantity,
		Unit:      p.Unit,
		Calories:  p.Calories,
		Protein:   p.Protein,
		Fat:       p.Fat,
		Carbs:     p.Carbs,
		Notes:     p.Notes,
		CreatedBy: p.CreatedBy,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
		Status:    "good",
	}

	if p.ExpiryDate.Valid {
		dateStr := p.ExpiryDate.Time.Format("2006-01-02")
		dto.ExpiryDate = &dateStr

		// Calculate days left relative to start of today
		now := time.Now()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		expDate := time.Date(p.ExpiryDate.Time.Year(), p.ExpiryDate.Time.Month(), p.ExpiryDate.Time.Day(), 0, 0, 0, 0, now.Location())
		days := int(expDate.Sub(today).Hours() / 24)
		dto.DaysLeft = &days

		if days < 0 {
			dto.Status = "expired"
		} else if days <= 3 {
			dto.Status = "warning"
		} else {
			dto.Status = "good"
		}
	}

	return dto
}
