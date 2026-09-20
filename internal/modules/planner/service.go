package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ynshvrh/E-Fridge-Api/internal/config"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/nutrition"
)

var (
	ErrPlanNotFound         = errors.New("meal plan not found")
	ErrInvalidDate          = errors.New("invalid date format, expected YYYY-MM-DD")
	ErrEmptyTitle           = errors.New("recipe title cannot be empty")
	ErrEmptyMealType        = errors.New("meal type cannot be empty")
	ErrDayAlreadyGenerated  = errors.New("раціон на цей день уже згенеровано. Ви можете перегенерувати окремий прийом їжі")
	ErrInvalidMealType      = errors.New("невірний тип прийому їжі (очікується: breakfast, lunch або dinner)")
)

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
			Timeout: 45 * time.Second,
		},
	}
}

type RecipeIngredient struct {
	Name     string  `json:"name"`
	Amount   float64 `json:"amount"`
	Unit     string  `json:"unit"`
	InFridge bool    `json:"in_fridge"`
}

type RecipeData struct {
	PrepTimeMinutes int                `json:"prep_time_minutes"`
	Description     string             `json:"description"`
	Ingredients     []RecipeIngredient `json:"ingredients"`
	Instructions    []string           `json:"instructions"`
}

type MealPlanDTO struct {
	ID          uuid.UUID   `json:"id"`
	FridgeID    uuid.UUID   `json:"fridge_id"`
	UserID      uuid.UUID   `json:"user_id"`
	Date        string      `json:"date"`
	MealType    string      `json:"meal_type"`
	RecipeTitle string      `json:"recipe_title"`
	RecipeID    *uuid.UUID  `json:"recipe_id,omitempty"`
	Calories    int32       `json:"calories"`
	Protein     float64     `json:"protein"`
	Fat         float64     `json:"fat"`
	Carbs       float64     `json:"carbs"`
	IsCompleted bool        `json:"is_completed"`
	Notes       string      `json:"notes"`
	RecipeData  *RecipeData `json:"recipe_data,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type CreateMealPlanInput struct {
	Date        string      `json:"date"`
	MealType    string      `json:"meal_type"`
	RecipeTitle string      `json:"recipe_title"`
	RecipeID    *uuid.UUID  `json:"recipe_id"`
	Calories    int32       `json:"calories"`
	Protein     float64     `json:"protein"`
	Fat         float64     `json:"fat"`
	Carbs       float64     `json:"carbs"`
	Notes       string      `json:"notes"`
	RecipeData  *RecipeData `json:"recipe_data,omitempty"`
}

type UpdateMealPlanInput struct {
	Date        string      `json:"date"`
	MealType    string      `json:"meal_type"`
	RecipeTitle string      `json:"recipe_title"`
	RecipeID    *uuid.UUID  `json:"recipe_id"`
	Calories    int32       `json:"calories"`
	Protein     float64     `json:"protein"`
	Fat         float64     `json:"fat"`
	Carbs       float64     `json:"carbs"`
	Notes       string      `json:"notes"`
	RecipeData  *RecipeData `json:"recipe_data,omitempty"`
}

type GeneratePlanInput struct {
	Days              int    `json:"days"` // 3 or 7
	StartDate         string `json:"start_date"`
	DietaryPreference string `json:"dietary_preference"`
}

type GenerateDayInput struct {
	Date              string `json:"date"`
	DietaryPreference string `json:"dietary_preference"`
}

type GenerateMealInput struct {
	Date              string `json:"date"`
	MealType          string `json:"meal_type"` // breakfast, lunch, dinner
	DietaryPreference string `json:"dietary_preference"`
}

func parseRecipeData(raw []byte) *RecipeData {
	if len(raw) == 0 {
		return nil
	}
	var rd RecipeData
	if err := json.Unmarshal(raw, &rd); err != nil {
		return nil
	}
	return &rd
}

func encodeRecipeData(rd *RecipeData) []byte {
	if rd == nil {
		return nil
	}
	b, _ := json.Marshal(rd)
	return b
}

func matchFridgeIngredients(ingredients []RecipeIngredient, prods []db.Product) []RecipeIngredient {
	if len(ingredients) == 0 {
		return ingredients
	}
	res := make([]RecipeIngredient, len(ingredients))
	for i, ing := range ingredients {
		res[i] = ing
		ingNameLower := strings.ToLower(strings.TrimSpace(ing.Name))
		found := false
		for _, p := range prods {
			pNameLower := strings.ToLower(strings.TrimSpace(p.Name))
			if p.Quantity > 0 && (strings.Contains(ingNameLower, pNameLower) || strings.Contains(pNameLower, ingNameLower)) {
				found = true
				break
			}
		}
		res[i].InFridge = found
	}
	return res
}

func (s *Service) ListMealPlans(ctx context.Context, fridgeID uuid.UUID, startDate, endDate string) ([]MealPlanDTO, error) {
	sDate, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		sDate = time.Now().AddDate(0, 0, -1)
	}
	eDate, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		eDate = sDate.AddDate(0, 0, 7)
	}

	plans, err := s.queries.ListMealPlansByFridgeAndDateRange(ctx, db.ListMealPlansByFridgeAndDateRangeParams{
		FridgeID: fridgeID,
		Date:     pgtype.Date{Time: sDate, Valid: true},
		Date_2:   pgtype.Date{Time: eDate, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list meal plans: %w", err)
	}

	prods, _ := s.queries.ListProductsByFridge(ctx, fridgeID)

	result := make([]MealPlanDTO, 0, len(plans))
	for _, p := range plans {
		dateStr := ""
		if p.Date.Valid {
			dateStr = p.Date.Time.Format("2006-01-02")
		}
		var recipeID *uuid.UUID
		if p.RecipeID.Valid {
			id := uuid.UUID(p.RecipeID.Bytes)
			recipeID = &id
		}

		rd := parseRecipeData(p.RecipeData)
		if rd != nil && len(prods) > 0 {
			rd.Ingredients = matchFridgeIngredients(rd.Ingredients, prods)
		}

		result = append(result, MealPlanDTO{
			ID:          p.ID,
			FridgeID:    p.FridgeID,
			UserID:      p.UserID,
			Date:        dateStr,
			MealType:    p.MealType,
			RecipeTitle: p.RecipeTitle,
			RecipeID:    recipeID,
			Calories:    p.Calories,
			Protein:     p.Protein,
			Fat:         p.Fat,
			Carbs:       p.Carbs,
			IsCompleted: p.IsCompleted,
			Notes:       p.Notes,
			RecipeData:  rd,
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		})
	}
	return result, nil
}

func (s *Service) GetMealPlan(ctx context.Context, fridgeID, id uuid.UUID) (*MealPlanDTO, error) {
	plan, err := s.queries.GetMealPlanByID(ctx, db.GetMealPlanByIDParams{
		ID:       id,
		FridgeID: fridgeID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlanNotFound
		}
		return nil, err
	}

	dateStr := ""
	if plan.Date.Valid {
		dateStr = plan.Date.Time.Format("2006-01-02")
	}
	var recipeID *uuid.UUID
	if plan.RecipeID.Valid {
		recID := uuid.UUID(plan.RecipeID.Bytes)
		recipeID = &recID
	}

	rd := parseRecipeData(plan.RecipeData)
	prods, _ := s.queries.ListProductsByFridge(ctx, fridgeID)
	if rd != nil && len(prods) > 0 {
		rd.Ingredients = matchFridgeIngredients(rd.Ingredients, prods)
	}

	return &MealPlanDTO{
		ID:          plan.ID,
		FridgeID:    plan.FridgeID,
		UserID:      plan.UserID,
		Date:        dateStr,
		MealType:    plan.MealType,
		RecipeTitle: plan.RecipeTitle,
		RecipeID:    recipeID,
		Calories:    plan.Calories,
		Protein:     plan.Protein,
		Fat:         plan.Fat,
		Carbs:       plan.Carbs,
		IsCompleted: plan.IsCompleted,
		Notes:       plan.Notes,
		RecipeData:  rd,
		CreatedAt:   plan.CreatedAt,
		UpdatedAt:   plan.UpdatedAt,
	}, nil
}

func (s *Service) CreateMealPlan(ctx context.Context, fridgeID, userID uuid.UUID, input CreateMealPlanInput) (*MealPlanDTO, error) {
	title := strings.TrimSpace(input.RecipeTitle)
	if title == "" {
		return nil, ErrEmptyTitle
	}
	mealType := strings.TrimSpace(input.MealType)
	if mealType == "" {
		mealType = "lunch"
	}

	t, err := time.Parse("2006-01-02", input.Date)
	if err != nil {
		return nil, ErrInvalidDate
	}

	var recID pgtype.UUID
	if input.RecipeID != nil {
		recID = pgtype.UUID{Bytes: *input.RecipeID, Valid: true}
	}

	cals := input.Calories
	p, f, c := input.Protein, input.Fat, input.Carbs
	if cals == 0 && p == 0 && f == 0 && c == 0 {
		calsEst, pEst, fEst, cEst := nutrition.CalculateEstimatedNutrition(title, 1, "порц")
		cals = int32(calsEst)
		p, f, c = pEst, fEst, cEst
	}

	recipeDataBytes := encodeRecipeData(input.RecipeData)

	plan, err := s.queries.CreateMealPlan(ctx, db.CreateMealPlanParams{
		FridgeID:    fridgeID,
		UserID:      userID,
		Date:        pgtype.Date{Time: t, Valid: true},
		MealType:    mealType,
		RecipeTitle: title,
		RecipeID:    recID,
		Calories:    cals,
		Protein:     p,
		Fat:         f,
		Carbs:       c,
		Notes:       input.Notes,
		RecipeData:  recipeDataBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create meal plan: %w", err)
	}

	dateStr := ""
	if plan.Date.Valid {
		dateStr = plan.Date.Time.Format("2006-01-02")
	}

	return &MealPlanDTO{
		ID:          plan.ID,
		FridgeID:    plan.FridgeID,
		UserID:      plan.UserID,
		Date:        dateStr,
		MealType:    plan.MealType,
		RecipeTitle: plan.RecipeTitle,
		Calories:    plan.Calories,
		Protein:     plan.Protein,
		Fat:         plan.Fat,
		Carbs:       plan.Carbs,
		IsCompleted: plan.IsCompleted,
		Notes:       plan.Notes,
		RecipeData:  input.RecipeData,
		CreatedAt:   plan.CreatedAt,
		UpdatedAt:   plan.UpdatedAt,
	}, nil
}

func (s *Service) UpdateMealPlan(ctx context.Context, fridgeID, id uuid.UUID, input UpdateMealPlanInput) (*MealPlanDTO, error) {
	title := strings.TrimSpace(input.RecipeTitle)
	if title == "" {
		return nil, ErrEmptyTitle
	}

	t, err := time.Parse("2006-01-02", input.Date)
	if err != nil {
		return nil, ErrInvalidDate
	}

	var recID pgtype.UUID
	if input.RecipeID != nil {
		recID = pgtype.UUID{Bytes: *input.RecipeID, Valid: true}
	}

	recipeDataBytes := encodeRecipeData(input.RecipeData)

	plan, err := s.queries.UpdateMealPlan(ctx, db.UpdateMealPlanParams{
		ID:          id,
		FridgeID:    fridgeID,
		Date:        pgtype.Date{Time: t, Valid: true},
		MealType:    input.MealType,
		RecipeTitle: title,
		RecipeID:    recID,
		Calories:    input.Calories,
		Protein:     input.Protein,
		Fat:         input.Fat,
		Carbs:       input.Carbs,
		Notes:       input.Notes,
		RecipeData:  recipeDataBytes,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlanNotFound
		}
		return nil, fmt.Errorf("failed to update meal plan: %w", err)
	}

	dateStr := ""
	if plan.Date.Valid {
		dateStr = plan.Date.Time.Format("2006-01-02")
	}

	return &MealPlanDTO{
		ID:          plan.ID,
		FridgeID:    plan.FridgeID,
		UserID:      plan.UserID,
		Date:        dateStr,
		MealType:    plan.MealType,
		RecipeTitle: plan.RecipeTitle,
		Calories:    plan.Calories,
		Protein:     plan.Protein,
		Fat:         plan.Fat,
		Carbs:       plan.Carbs,
		IsCompleted: plan.IsCompleted,
		Notes:       plan.Notes,
		RecipeData:  input.RecipeData,
		CreatedAt:   plan.CreatedAt,
		UpdatedAt:   plan.UpdatedAt,
	}, nil
}

func (s *Service) ToggleMealPlanCompleted(ctx context.Context, fridgeID, id uuid.UUID, isCompleted bool) (*MealPlanDTO, error) {
	plan, err := s.queries.ToggleMealPlanCompleted(ctx, db.ToggleMealPlanCompletedParams{
		ID:          id,
		FridgeID:    fridgeID,
		IsCompleted: isCompleted,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlanNotFound
		}
		return nil, fmt.Errorf("failed to toggle meal plan: %w", err)
	}

	dateStr := ""
	if plan.Date.Valid {
		dateStr = plan.Date.Time.Format("2006-01-02")
	}

	return &MealPlanDTO{
		ID:          plan.ID,
		FridgeID:    plan.FridgeID,
		UserID:      plan.UserID,
		Date:        dateStr,
		MealType:    plan.MealType,
		RecipeTitle: plan.RecipeTitle,
		Calories:    plan.Calories,
		Protein:     plan.Protein,
		Fat:         plan.Fat,
		Carbs:       plan.Carbs,
		IsCompleted: plan.IsCompleted,
		Notes:       plan.Notes,
		RecipeData:  parseRecipeData(plan.RecipeData),
		CreatedAt:   plan.CreatedAt,
		UpdatedAt:   plan.UpdatedAt,
	}, nil
}

func (s *Service) DeleteMealPlan(ctx context.Context, fridgeID, id uuid.UUID) error {
	return s.queries.DeleteMealPlan(ctx, db.DeleteMealPlanParams{
		ID:       id,
		FridgeID: fridgeID,
	})
}

func (s *Service) ClearMealPlans(ctx context.Context, fridgeID uuid.UUID, startDate, endDate string) error {
	sDate, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return ErrInvalidDate
	}
	eDate, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		return ErrInvalidDate
	}

	return s.queries.ClearMealPlansByDateRange(ctx, db.ClearMealPlansByDateRangeParams{
		FridgeID: fridgeID,
		Date:     pgtype.Date{Time: sDate, Valid: true},
		Date_2:   pgtype.Date{Time: eDate, Valid: true},
	})
}

// GenerateDayWithAI generates a full day of meals (Breakfast, Lunch, Dinner) with recipes for the specified date.
// Rule: Allowed only once per date per fridge.
func (s *Service) GenerateDayWithAI(ctx context.Context, fridgeID, userID uuid.UUID, input GenerateDayInput) ([]MealPlanDTO, error) {
	targetDate := time.Now()
	if input.Date != "" {
		if t, err := time.Parse("2006-01-02", input.Date); err == nil {
			targetDate = t
		}
	}
	dateStr := targetDate.Format("2006-01-02")

	// Rule: Generate whole day allowed only once for this date
	count, err := s.queries.CountMealPlansByFridgeAndDate(ctx, db.CountMealPlansByFridgeAndDateParams{
		FridgeID: fridgeID,
		Date:     pgtype.Date{Time: targetDate, Valid: true},
	})
	if err == nil && count >= 3 {
		return nil, ErrDayAlreadyGenerated
	}

	// Fetch fridge inventory
	prods, _ := s.queries.ListProductsByFridge(ctx, fridgeID)
	inventory := make([]string, 0, len(prods))
	for _, p := range prods {
		if p.Quantity > 0 {
			inventory = append(inventory, fmt.Sprintf("%s (%v %s)", p.Name, p.Quantity, p.Unit))
		}
	}

	var generatedItems []CreateMealPlanInput

	// Try OpenRouter AI first
	if s.cfg.OpenRouterAPIKey != "" {
		items, err := s.generateDayWithOpenRouter(ctx, inventory, targetDate, input.DietaryPreference)
		if err == nil && len(items) > 0 {
			generatedItems = items
		} else {
			slog.Warn("AI day planner generation failed, using fallback", "error", err)
		}
	}

	// Fallback if AI didn't succeed
	if len(generatedItems) == 0 {
		generatedItems = s.generateFallbackDay(prods, targetDate, input.DietaryPreference)
	}

	// Clear any partial existing meals for this day before saving fresh whole day
	_ = s.queries.ClearMealPlansByDateRange(ctx, db.ClearMealPlansByDateRangeParams{
		FridgeID: fridgeID,
		Date:     pgtype.Date{Time: targetDate, Valid: true},
		Date_2:   pgtype.Date{Time: targetDate, Valid: true},
	})

	// Save all 3 meals
	result := make([]MealPlanDTO, 0, len(generatedItems))
	for _, item := range generatedItems {
		item.Date = dateStr
		if item.RecipeData != nil && len(prods) > 0 {
			item.RecipeData.Ingredients = matchFridgeIngredients(item.RecipeData.Ingredients, prods)
		}
		dto, err := s.CreateMealPlan(ctx, fridgeID, userID, item)
		if err == nil && dto != nil {
			result = append(result, *dto)
		}
	}

	return result, nil
}

// GenerateMealWithAI generates or regenerates a single meal slot (breakfast, lunch, or dinner) for a date.
// Rule: Allowed anytime with rate limit cooldown.
func (s *Service) GenerateMealWithAI(ctx context.Context, fridgeID, userID uuid.UUID, input GenerateMealInput) (*MealPlanDTO, error) {
	mealType := strings.ToLower(strings.TrimSpace(input.MealType))
	if mealType != "breakfast" && mealType != "lunch" && mealType != "dinner" && mealType != "snack" {
		return nil, ErrInvalidMealType
	}

	targetDate := time.Now()
	if input.Date != "" {
		if t, err := time.Parse("2006-01-02", input.Date); err == nil {
			targetDate = t
		}
	}
	dateStr := targetDate.Format("2006-01-02")

	// Fetch fridge inventory
	prods, _ := s.queries.ListProductsByFridge(ctx, fridgeID)
	inventory := make([]string, 0, len(prods))
	for _, p := range prods {
		if p.Quantity > 0 {
			inventory = append(inventory, fmt.Sprintf("%s (%v %s)", p.Name, p.Quantity, p.Unit))
		}
	}

	var generatedMeal *CreateMealPlanInput

	// Try OpenRouter AI
	if s.cfg.OpenRouterAPIKey != "" {
		meal, err := s.generateSingleMealWithOpenRouter(ctx, inventory, targetDate, mealType, input.DietaryPreference)
		if err == nil && meal != nil {
			generatedMeal = meal
		} else {
			slog.Warn("AI single meal generation failed, using fallback", "error", err)
		}
	}

	// Fallback
	if generatedMeal == nil {
		generatedMeal = s.generateFallbackMeal(prods, targetDate, mealType, input.DietaryPreference)
	}

	generatedMeal.Date = dateStr
	generatedMeal.MealType = mealType
	if generatedMeal.RecipeData != nil && len(prods) > 0 {
		generatedMeal.RecipeData.Ingredients = matchFridgeIngredients(generatedMeal.RecipeData.Ingredients, prods)
	}

	// Remove previous meal of this type for this date
	_ = s.queries.DeleteMealPlanByFridgeDateAndType(ctx, db.DeleteMealPlanByFridgeDateAndTypeParams{
		FridgeID: fridgeID,
		Date:     pgtype.Date{Time: targetDate, Valid: true},
		MealType: mealType,
	})

	// Save fresh meal
	return s.CreateMealPlan(ctx, fridgeID, userID, *generatedMeal)
}

// GeneratePlanWithAI generates multi-day plan for backward-compatibility.
func (s *Service) GeneratePlanWithAI(ctx context.Context, fridgeID, userID uuid.UUID, input GeneratePlanInput) ([]MealPlanDTO, error) {
	days := input.Days
	if days <= 0 {
		days = 7
	}
	if days > 14 {
		days = 14
	}

	startDate := time.Now()
	if input.StartDate != "" {
		if t, err := time.Parse("2006-01-02", input.StartDate); err == nil {
			startDate = t
		}
	}

	prods, _ := s.queries.ListProductsByFridge(ctx, fridgeID)
	inventory := make([]string, 0, len(prods))
	for _, p := range prods {
		if p.Quantity > 0 {
			inventory = append(inventory, fmt.Sprintf("%s (%v %s)", p.Name, p.Quantity, p.Unit))
		}
	}

	var generatedItems []CreateMealPlanInput

	if s.cfg.OpenRouterAPIKey != "" {
		items, err := s.generateWithOpenRouter(ctx, inventory, days, startDate, input.DietaryPreference)
		if err == nil && len(items) > 0 {
			generatedItems = items
		} else {
			slog.Warn("AI multi-day planner generation failed, using fallback", "error", err)
		}
	}

	if len(generatedItems) == 0 {
		generatedItems = s.generateFallbackPlan(prods, days, startDate, input.DietaryPreference)
	}

	result := make([]MealPlanDTO, 0, len(generatedItems))
	for _, item := range generatedItems {
		if item.RecipeData != nil && len(prods) > 0 {
			item.RecipeData.Ingredients = matchFridgeIngredients(item.RecipeData.Ingredients, prods)
		}
		dto, err := s.CreateMealPlan(ctx, fridgeID, userID, item)
		if err == nil && dto != nil {
			result = append(result, *dto)
		}
	}

	return result, nil
}

func (s *Service) generateDayWithOpenRouter(ctx context.Context, inventory []string, date time.Time, diet string) ([]CreateMealPlanInput, error) {
	modelsToTry := []string{s.cfg.SmartModel, s.cfg.FastModel, s.cfg.OpenRouterModel}

	prompt := fmt.Sprintf(`Склади повноцінний денний раціон на дату %s (3 прийоми їжі: breakfast, lunch, dinner).
Продукти в холодильнику: %s.
Дієтичні побажання: %s.

Поверни ВИКЛЮЧНО валідний JSON-об'єкт наступного формату:
{
  "meals": [
    {
      "meal_type": "breakfast",
      "recipe_title": "Назва страви",
      "calories": 420,
      "protein": 22.0,
      "fat": 14.0,
      "carbs": 48.0,
      "notes": "Шеф-порада до страви",
      "recipe_data": {
        "prep_time_minutes": 20,
        "description": "Опис страви",
        "ingredients": [
          {"name": "Назва продукту", "amount": 100, "unit": "г"}
        ],
        "instructions": [
          "Крок 1: ...",
          "Крок 2: ..."
        ]
      }
    }
  ]
}
Обов'язково включи по 1 страві для кожного meal_type: breakfast, lunch, dinner. Усі інгредієнти та інструкції мають бути конкретними. Мова українська.`,
		date.Format("2006-01-02"), strings.Join(inventory, ", "), diet)

	var lastErr error
	for _, model := range modelsToTry {
		if model == "" {
			continue
		}

		body := map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "system", "content": "Ти професійний дієтолог і шеф-кухар. Відповідай виключно валідним JSON."},
				{"role": "user", "content": prompt},
			},
			"temperature": 0.6,
			"response_format": map[string]string{
				"type": "json_object",
			},
		}

		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(jsonBody))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Authorization", "Bearer "+s.cfg.OpenRouterAPIKey)
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := s.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("openrouter returned status %d: %s", resp.StatusCode, string(b))
			continue
		}

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		var orResp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}

		if err := json.Unmarshal(respBytes, &orResp); err != nil || len(orResp.Choices) == 0 {
			lastErr = errors.New("failed to parse choices")
			continue
		}

		content := strings.TrimSpace(orResp.Choices[0].Message.Content)
		if strings.HasPrefix(content, "```json") {
			content = strings.TrimPrefix(content, "```json")
			content = strings.TrimSuffix(content, "```")
		} else if strings.HasPrefix(content, "```") {
			content = strings.TrimPrefix(content, "```")
			content = strings.TrimSuffix(content, "```")
		}
		content = strings.TrimSpace(content)

		var parsed struct {
			Meals []CreateMealPlanInput `json:"meals"`
		}
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			lastErr = err
			continue
		}

		if len(parsed.Meals) > 0 {
			return parsed.Meals, nil
		}
	}

	return nil, lastErr
}

func (s *Service) generateSingleMealWithOpenRouter(ctx context.Context, inventory []string, date time.Time, mealType, diet string) (*CreateMealPlanInput, error) {
	modelsToTry := []string{s.cfg.SmartModel, s.cfg.FastModel, s.cfg.OpenRouterModel}

	mealNamesUk := map[string]string{
		"breakfast": "сніданок",
		"lunch":     "обід",
		"dinner":    "вечерю",
		"snack":     "перекус",
	}
	ukMeal := mealNamesUk[mealType]
	if ukMeal == "" {
		ukMeal = mealType
	}

	prompt := fmt.Sprintf(`Склади смачний рецепт на %s (%s).
Продукти в наявності в холодильнику: %s.
Дієтичні побажання: %s.

Поверни ВИКЛЮЧНО валідний JSON-об'єкт:
{
  "meal_type": "%s",
  "recipe_title": "Назва страви",
  "calories": 450,
  "protein": 25.0,
  "fat": 15.0,
  "carbs": 50.0,
  "notes": "Порада шеф-кухаря",
  "recipe_data": {
    "prep_time_minutes": 25,
    "description": "Короткий апетитний опис страви",
    "ingredients": [
      {"name": "Назва продукту", "amount": 100, "unit": "г"}
    ],
    "instructions": [
      "Крок 1: ...",
      "Крок 2: ..."
    ]
  }
}
Мова українська.`,
		ukMeal, date.Format("2006-01-02"), strings.Join(inventory, ", "), diet, mealType)

	var lastErr error
	for _, model := range modelsToTry {
		if model == "" {
			continue
		}

		body := map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "system", "content": "Ти шеф-кухар. Відповідай виключно валідним JSON об'єктом страви."},
				{"role": "user", "content": prompt},
			},
			"temperature": 0.7,
			"response_format": map[string]string{
				"type": "json_object",
			},
		}

		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(jsonBody))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Authorization", "Bearer "+s.cfg.OpenRouterAPIKey)
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := s.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("openrouter returned status %d: %s", resp.StatusCode, string(b))
			continue
		}

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		var orResp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}

		if err := json.Unmarshal(respBytes, &orResp); err != nil || len(orResp.Choices) == 0 {
			lastErr = errors.New("failed to parse choices")
			continue
		}

		content := strings.TrimSpace(orResp.Choices[0].Message.Content)
		if strings.HasPrefix(content, "```json") {
			content = strings.TrimPrefix(content, "```json")
			content = strings.TrimSuffix(content, "```")
		} else if strings.HasPrefix(content, "```") {
			content = strings.TrimPrefix(content, "```")
			content = strings.TrimSuffix(content, "```")
		}
		content = strings.TrimSpace(content)

		var meal CreateMealPlanInput
		if err := json.Unmarshal([]byte(content), &meal); err == nil && meal.RecipeTitle != "" {
			return &meal, nil
		}

		// Or wrapped in { "meal": ... }
		var wrapped struct {
			Meal CreateMealPlanInput `json:"meal"`
		}
		if err := json.Unmarshal([]byte(content), &wrapped); err == nil && wrapped.Meal.RecipeTitle != "" {
			return &wrapped.Meal, nil
		}
	}

	return nil, lastErr
}

func (s *Service) generateWithOpenRouter(ctx context.Context, inventory []string, days int, startDate time.Time, diet string) ([]CreateMealPlanInput, error) {
	modelsToTry := []string{s.cfg.SmartModel, s.cfg.FastModel, s.cfg.OpenRouterModel}

	prompt := fmt.Sprintf(`Склади збалансований план харчування на %d днів, починаючи з %s.
Доступні продукти в холодильнику: %s.
Дієтичні побажання: %s.

Поверни виключно валідний JSON-об'єкт наступного формату:
{
  "meals": [
    {
      "date": "YYYY-MM-DD",
      "meal_type": "breakfast",
      "recipe_title": "Назва страви",
      "calories": 450,
      "protein": 25.0,
      "fat": 15.0,
      "carbs": 50.0,
      "notes": "Короткі поради з приготування",
      "recipe_data": {
        "prep_time_minutes": 20,
        "description": "Опис",
        "ingredients": [{"name": "Продукт", "amount": 100, "unit": "г"}],
        "instructions": ["Крок 1"]
      }
    }
  ]
}
Для кожного дня згенеруй щонайменше breakfast, lunch та dinner.`,
		days, startDate.Format("2006-01-02"), strings.Join(inventory, ", "), diet)

	var lastErr error
	for _, model := range modelsToTry {
		if model == "" {
			continue
		}

		body := map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "system", "content": "Ти професійний дієтолог та шеф-кухар. Відповідай виключно валідним JSON."},
				{"role": "user", "content": prompt},
			},
			"temperature": 0.6,
			"response_format": map[string]string{
				"type": "json_object",
			},
		}

		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(jsonBody))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Authorization", "Bearer "+s.cfg.OpenRouterAPIKey)
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := s.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("openrouter returned status %d: %s", resp.StatusCode, string(b))
			continue
		}

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		var orResp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}

		if err := json.Unmarshal(respBytes, &orResp); err != nil || len(orResp.Choices) == 0 {
			lastErr = errors.New("failed to parse choices")
			continue
		}

		content := strings.TrimSpace(orResp.Choices[0].Message.Content)
		if strings.HasPrefix(content, "```json") {
			content = strings.TrimPrefix(content, "```json")
			content = strings.TrimSuffix(content, "```")
		} else if strings.HasPrefix(content, "```") {
			content = strings.TrimPrefix(content, "```")
			content = strings.TrimSuffix(content, "```")
		}
		content = strings.TrimSpace(content)

		var parsed struct {
			Meals []CreateMealPlanInput `json:"meals"`
		}
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			lastErr = err
			continue
		}

		if len(parsed.Meals) > 0 {
			return parsed.Meals, nil
		}
	}

	return nil, lastErr
}

func (s *Service) generateFallbackMeal(prods []db.Product, date time.Time, mealType, diet string) *CreateMealPlanInput {
	dishes := s.generateFallbackDay(prods, date, diet)
	for _, d := range dishes {
		if d.MealType == mealType {
			return &d
		}
	}
	if len(dishes) > 0 {
		d := dishes[0]
		d.MealType = mealType
		return &d
	}
	return &CreateMealPlanInput{
		Date:        date.Format("2006-01-02"),
		MealType:    mealType,
		RecipeTitle: "Корисний перекус",
		Calories:    300,
		Protein:     15,
		Fat:         10,
		Carbs:       35,
		Notes:       "Швидка та збалансована страва",
		RecipeData: &RecipeData{
			PrepTimeMinutes: 10,
			Description:     "Проста страва на кожен день",
			Ingredients: []RecipeIngredient{
				{Name: "Йогурт", Amount: 200, Unit: "г"},
				{Name: "Фрукти", Amount: 100, Unit: "г"},
			},
			Instructions: []string{"Змішайте інгредієнти та насолоджуйтесь."},
		},
	}
}

func (s *Service) generateFallbackDay(prods []db.Product, date time.Time, diet string) []CreateMealPlanInput {
	dateStr := date.Format("2006-01-02")
	return []CreateMealPlanInput{
		{
			Date:        dateStr,
			MealType:    "breakfast",
			RecipeTitle: "Вівсянка з ягодами, горіхами та медом",
			Calories:    380,
			Protein:     14,
			Fat:         8,
			Carbs:       64,
			Notes:       "Чудовий повільний вуглевод для заряду енергії на ранок",
			RecipeData: &RecipeData{
				PrepTimeMinutes: 15,
				Description:     "Класична поживна вівсянка на воді або молоці з соковитими ягодами.",
				Ingredients: []RecipeIngredient{
					{Name: "Вівсяні пластівці", Amount: 60, Unit: "г"},
					{Name: "Молоко або вода", Amount: 200, Unit: "мл"},
					{Name: "Ягоди", Amount: 50, Unit: "г"},
					{Name: "Мед", Amount: 15, Unit: "г"},
					{Name: "Горіхи", Amount: 15, Unit: "г"},
				},
				Instructions: []string{
					"Закип'ятіть молоко або воду в невеликому сотейнику.",
					"Всипте вівсяні пластівці та варіть на слабкому вогні 5-7 хвилин, помішуючи.",
					"Зніміть з вогню, викладіть у тарілку.",
					"Додайте мед, свіжі ягоди та подрібнені горіхи перед подачею.",
				},
			},
		},
		{
			Date:        dateStr,
			MealType:    "lunch",
			RecipeTitle: "Куряче філе на грилі з рисом басматі та свіжими овочами",
			Calories:    540,
			Protein:     45,
			Fat:         12,
			Carbs:       62,
			Notes:       "Високобілковий обід для відновлення сил та підтримки м'язів",
			RecipeData: &RecipeData{
				PrepTimeMinutes: 30,
				Description:     "Ніжне куряче філе з паприкою та ароматний розсипчастий рис басматі.",
				Ingredients: []RecipeIngredient{
					{Name: "Куряче філе", Amount: 180, Unit: "г"},
					{Name: "Рис басматі", Amount: 70, Unit: "г"},
					{Name: "Огірок", Amount: 1, Unit: "шт"},
					{Name: "Помідор", Amount: 1, Unit: "шт"},
					{Name: "Оливкова олія", Amount: 10, Unit: "мл"},
				},
				Instructions: []string{
					"Промийте рис і відваріть у підсоленій воді у співвідношенні 1:2 протягом 15 хвилин.",
					"Куряче філе наріжте стейками, приправте сіллю, перцем і паприкою.",
					"Розігрійте гриль-пательню з невеликою кількістю олії.",
					"Обсмажте філе по 4-5 хвилин з кожного боку до золотистої скоринки.",
					"Наріжте свіжі овочі та подавайте разом із гарячим рисом і курятиною.",
				},
			},
		},
		{
			Date:        dateStr,
			MealType:    "dinner",
			RecipeTitle: "Омлет зі шпинатом, сиром фета та чері",
			Calories:    390,
			Protein:     26,
			Fat:         24,
			Carbs:       9,
			Notes:       "Легка низьковуглеводна вечеря, яка не перевантажує травлення",
			RecipeData: &RecipeData{
				PrepTimeMinutes: 20,
				Description:     "Пишний яєчний омлет зі свіжим листям шпинату та солонуватою фетою.",
				Ingredients: []RecipeIngredient{
					{Name: "Яйця курячі", Amount: 3, Unit: "шт"},
					{Name: "Шпинат свіжий", Amount: 50, Unit: "г"},
					{Name: "Сир фета", Amount: 40, Unit: "г"},
					{Name: "Помідори чері", Amount: 4, Unit: "шт"},
					{Name: "Вершкове масло", Amount: 10, Unit: "г"},
				},
				Instructions: []string{
					"Яйця збийте вінчиком з дрібкою перцю та столовою ложкою води.",
					"На сковороді розтопіть масло, киньте шпинат і тушкуйте 1 хвилину до зів'янення.",
					"Залийте шпинат збитими яйцями.",
					"Зверху покладіть розрізані навпіл чері та розкришіть фету.",
					"Готуйте на слабкому вогні під кришкою 5-6 хвилин.",
				},
			},
		},
	}
}

func (s *Service) generateFallbackPlan(prods []db.Product, days int, startDate time.Time, diet string) []CreateMealPlanInput {
	var result []CreateMealPlanInput
	for d := 0; d < days; d++ {
		date := startDate.AddDate(0, 0, d)
		dayMeals := s.generateFallbackDay(prods, date, diet)
		result = append(result, dayMeals...)
	}
	return result
}
