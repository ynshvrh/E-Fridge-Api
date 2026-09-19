package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
)

var (
	ErrRecipeNotFound = errors.New("recipe not found")
	ErrEmptyTitle     = errors.New("recipe title cannot be empty")
)

type RecipeIngredient struct {
	Name     string  `json:"name"`
	Amount   float64 `json:"amount"`
	Unit     string  `json:"unit"`
	InFridge bool    `json:"in_fridge,omitempty"`
}

type SavedRecipeDTO struct {
	ID           uuid.UUID          `json:"id"`
	UserID       uuid.UUID          `json:"user_id"`
	FridgeID     *uuid.UUID         `json:"fridge_id,omitempty"`
	Title        string             `json:"title"`
	Description  string             `json:"description"`
	Ingredients  []RecipeIngredient `json:"ingredients"`
	Steps        []string           `json:"steps"`
	Calories     int32              `json:"calories"`
	Protein      float64            `json:"protein"`
	Fat          float64            `json:"fat"`
	Carbs        float64            `json:"carbs"`
	PrepTimeMins int32              `json:"prep_time_mins"`
	CookTimeMins int32              `json:"cook_time_mins"`
	Servings     int32              `json:"servings"`
	CreatedAt    time.Time          `json:"created_at"`
}

type CreateRecipeInput struct {
	Title        string             `json:"title"`
	Description  string             `json:"description"`
	Ingredients  []RecipeIngredient `json:"ingredients"`
	Steps        []string           `json:"steps"`
	Calories     int32              `json:"calories"`
	Protein      float64            `json:"protein"`
	Fat          float64            `json:"fat"`
	Carbs        float64            `json:"carbs"`
	PrepTimeMins int32              `json:"prep_time_mins"`
	CookTimeMins int32              `json:"cook_time_mins"`
	Servings     int32              `json:"servings"`
}

type Service struct {
	queries *db.Queries
}

func NewService(queries *db.Queries) *Service {
	return &Service{queries: queries}
}

func (s *Service) ListRecipes(ctx context.Context, userID uuid.UUID) ([]SavedRecipeDTO, error) {
	rows, err := s.queries.ListSavedRecipesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list saved recipes: %w", err)
	}

	result := make([]SavedRecipeDTO, 0, len(rows))
	for _, r := range rows {
		dto, err := toDTO(r)
		if err != nil {
			continue
		}
		result = append(result, dto)
	}

	return result, nil
}

func (s *Service) GetRecipe(ctx context.Context, userID, recipeID uuid.UUID) (*SavedRecipeDTO, error) {
	row, err := s.queries.GetSavedRecipeByID(ctx, db.GetSavedRecipeByIDParams{
		ID:     recipeID,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRecipeNotFound
		}
		return nil, fmt.Errorf("failed to get saved recipe: %w", err)
	}

	dto, err := toDTO(row)
	if err != nil {
		return nil, fmt.Errorf("failed to parse saved recipe: %w", err)
	}

	return &dto, nil
}

func (s *Service) CreateRecipe(ctx context.Context, userID uuid.UUID, fridgeID *uuid.UUID, input CreateRecipeInput) (*SavedRecipeDTO, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, ErrEmptyTitle
	}

	if input.Servings <= 0 {
		input.Servings = 2
	}

	ingBytes, err := json.Marshal(input.Ingredients)
	if err != nil {
		return nil, fmt.Errorf("failed to encode ingredients: %w", err)
	}

	stepsBytes, err := json.Marshal(input.Steps)
	if err != nil {
		return nil, fmt.Errorf("failed to encode steps: %w", err)
	}

	var fid pgtype.UUID
	if fridgeID != nil {
		fid = pgtype.UUID{Bytes: *fridgeID, Valid: true}
	}

	row, err := s.queries.CreateSavedRecipe(ctx, db.CreateSavedRecipeParams{
		UserID:       userID,
		FridgeID:     fid,
		Title:        title,
		Description:  strings.TrimSpace(input.Description),
		Ingredients:  ingBytes,
		Steps:        stepsBytes,
		Calories:     input.Calories,
		Protein:      input.Protein,
		Fat:          input.Fat,
		Carbs:        input.Carbs,
		PrepTimeMins: input.PrepTimeMins,
		CookTimeMins: input.CookTimeMins,
		Servings:     input.Servings,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to save recipe: %w", err)
	}

	dto, err := toDTO(row)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to dto: %w", err)
	}

	return &dto, nil
}

func (s *Service) DeleteRecipe(ctx context.Context, userID, recipeID uuid.UUID) error {
	err := s.queries.DeleteSavedRecipe(ctx, db.DeleteSavedRecipeParams{
		ID:     recipeID,
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("failed to delete recipe: %w", err)
	}
	return nil
}

func toDTO(r db.SavedRecipe) (SavedRecipeDTO, error) {
	var ingredients []RecipeIngredient
	if len(r.Ingredients) > 0 {
		_ = json.Unmarshal(r.Ingredients, &ingredients)
	}
	if ingredients == nil {
		ingredients = []RecipeIngredient{}
	}

	var steps []string
	if len(r.Steps) > 0 {
		_ = json.Unmarshal(r.Steps, &steps)
	}
	if steps == nil {
		steps = []string{}
	}

	var fid *uuid.UUID
	if r.FridgeID.Valid {
		id := uuid.UUID(r.FridgeID.Bytes)
		fid = &id
	}

	return SavedRecipeDTO{
		ID:           r.ID,
		UserID:       r.UserID,
		FridgeID:     fid,
		Title:        r.Title,
		Description:  r.Description,
		Ingredients:  ingredients,
		Steps:        steps,
		Calories:     r.Calories,
		Protein:      r.Protein,
		Fat:          r.Fat,
		Carbs:        r.Carbs,
		PrepTimeMins: r.PrepTimeMins,
		CookTimeMins: r.CookTimeMins,
		Servings:     r.Servings,
		CreatedAt:    r.CreatedAt,
	}, nil
}
