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
	ErrPlanNotFound  = errors.New("meal plan not found")
	ErrInvalidDate   = errors.New("invalid date format, expected YYYY-MM-DD")
	ErrEmptyTitle    = errors.New("recipe title cannot be empty")
	ErrEmptyMealType = errors.New("meal type cannot be empty")
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

type MealPlanDTO struct {
	ID          uuid.UUID  `json:"id"`
	FridgeID    uuid.UUID  `json:"fridge_id"`
	UserID      uuid.UUID  `json:"user_id"`
	Date        string     `json:"date"`
	MealType    string     `json:"meal_type"`
	RecipeTitle string     `json:"recipe_title"`
	RecipeID    *uuid.UUID `json:"recipe_id,omitempty"`
	Calories    int32      `json:"calories"`
	Protein     float64    `json:"protein"`
	Fat         float64    `json:"fat"`
	Carbs       float64    `json:"carbs"`
	IsCompleted bool       `json:"is_completed"`
	Notes       string     `json:"notes"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateMealPlanInput struct {
	Date        string     `json:"date"`
	MealType    string     `json:"meal_type"`
	RecipeTitle string     `json:"recipe_title"`
	RecipeID    *uuid.UUID `json:"recipe_id"`
	Calories    int32      `json:"calories"`
	Protein     float64    `json:"protein"`
	Fat         float64    `json:"fat"`
	Carbs       float64    `json:"carbs"`
	Notes       string     `json:"notes"`
}

type UpdateMealPlanInput struct {
	Date        string     `json:"date"`
	MealType    string     `json:"meal_type"`
	RecipeTitle string     `json:"recipe_title"`
	RecipeID    *uuid.UUID `json:"recipe_id"`
	Calories    int32      `json:"calories"`
	Protein     float64    `json:"protein"`
	Fat         float64    `json:"fat"`
	Carbs       float64    `json:"carbs"`
	Notes       string     `json:"notes"`
}

type GeneratePlanInput struct {
	Days              int    `json:"days"` // 3 or 7
	StartDate         string `json:"start_date"`
	DietaryPreference string `json:"dietary_preference"`
}

func toDTO(p db.MealPlan) MealPlanDTO {
	dateStr := ""
	if p.Date.Valid {
		dateStr = p.Date.Time.Format("2006-01-02")
	}

	var recipeID *uuid.UUID
	if p.RecipeID.Valid {
		id := uuid.UUID(p.RecipeID.Bytes)
		recipeID = &id
	}

	return MealPlanDTO{
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
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
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

	result := make([]MealPlanDTO, 0, len(plans))
	for _, p := range plans {
		result = append(result, toDTO(p))
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
	dto := toDTO(plan)
	return &dto, nil
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

	// Auto-estimate nutrition if missing
	cals := input.Calories
	p, f, c := input.Protein, input.Fat, input.Carbs
	if cals == 0 && p == 0 && f == 0 && c == 0 {
		calsEst, pEst, fEst, cEst := nutrition.CalculateEstimatedNutrition(title, 1, "порц")
		cals = int32(calsEst)
		p, f, c = pEst, fEst, cEst
	}

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
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create meal plan: %w", err)
	}

	dto := toDTO(plan)
	return &dto, nil
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
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlanNotFound
		}
		return nil, fmt.Errorf("failed to update meal plan: %w", err)
	}

	dto := toDTO(plan)
	return &dto, nil
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
	dto := toDTO(plan)
	return &dto, nil
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

// GeneratePlanWithAI generates a 3 to 7-day meal plan using the SmartModel (Claude/OpenRouter) or deterministic fallback.
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

	// Fetch fridge inventory
	prods, _ := s.queries.ListProductsByFridge(ctx, fridgeID)
	inventory := make([]string, 0, len(prods))
	for _, p := range prods {
		if p.Quantity > 0 {
			inventory = append(inventory, fmt.Sprintf("%s (%v %s)", p.Name, p.Quantity, p.Unit))
		}
	}

	var generatedItems []CreateMealPlanInput

	// If OpenRouter key configured, try SmartModel first (Claude 3.5 Sonnet / etc)
	if s.cfg.OpenRouterAPIKey != "" {
		items, err := s.generateWithOpenRouter(ctx, inventory, days, startDate, input.DietaryPreference)
		if err == nil && len(items) > 0 {
			generatedItems = items
		} else {
			slog.Warn("AI planner generation failed, using fallback planner", "error", err)
		}
	}

	// Fallback deterministic plan generator based on user's fridge and dietary preference
	if len(generatedItems) == 0 {
		generatedItems = s.generateFallbackPlan(prods, days, startDate, input.DietaryPreference)
	}

	// Save all generated meal plans into DB
	result := make([]MealPlanDTO, 0, len(generatedItems))
	for _, item := range generatedItems {
		dto, err := s.CreateMealPlan(ctx, fridgeID, userID, item)
		if err == nil && dto != nil {
			result = append(result, *dto)
		}
	}

	return result, nil
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
      "notes": "Короткі поради з приготування"
    }
  ]
}
Для кожного дня згенеруй щонайменше breakfast (сніданок), lunch (обід) та dinner (вечеря).`,
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
			return nil, err
		}

		httpReq.Header.Set("Authorization", "Bearer "+s.cfg.OpenRouterAPIKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("HTTP-Referer", "https://e-fridge.app")
		httpReq.Header.Set("X-Title", "E-Fridge Planner")

		resp, err := s.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}

		respBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, string(respBytes))
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

func (s *Service) generateFallbackPlan(prods []db.Product, days int, startDate time.Time, diet string) []CreateMealPlanInput {
	baseDishes := []struct {
		mealType string
		title    string
		cals     int32
		p, f, c  float64
		notes    string
	}{
		{"breakfast", "Вівсянка з ягодами та медом", 350, 12, 6, 62, "Швидкий і поживний сніданок"},
		{"lunch", "Куряче філе з рисом та свіжими овочами", 520, 42, 10, 65, "Багатий на білок обід"},
		{"dinner", "Омлет із сиром, зеленню та томатами", 380, 26, 24, 8, "Легка збалансована вечеря"},
		{"snack", "Грецький йогурт з горіхами", 210, 15, 11, 14, "Корисний перекус"},
		{"breakfast", "Тости з авокадо та яйцем пашот", 390, 16, 21, 34, "Свіжий енергійний початок дня"},
		{"lunch", "Свіжий салат з моцарелою та овочами", 440, 18, 28, 22, "Легкий вітамінний обід"},
		{"dinner", "Запечена риба з броколі та лимоном", 410, 38, 14, 12, "Корисні жири Омега-3"},
	}

	var result []CreateMealPlanInput
	idx := 0
	for d := 0; d < days; d++ {
		dateStr := startDate.AddDate(0, 0, d).Format("2006-01-02")
		// Breakfast
		b := baseDishes[idx%len(baseDishes)]
		idx++
		result = append(result, CreateMealPlanInput{
			Date:        dateStr,
			MealType:    "breakfast",
			RecipeTitle: b.title,
			Calories:    b.cals,
			Protein:     b.p,
			Fat:         b.f,
			Carbs:       b.c,
			Notes:       b.notes,
		})

		// Lunch
		l := baseDishes[idx%len(baseDishes)]
		idx++
		result = append(result, CreateMealPlanInput{
			Date:        dateStr,
			MealType:    "lunch",
			RecipeTitle: l.title,
			Calories:    l.cals,
			Protein:     l.p,
			Fat:         l.f,
			Carbs:       l.c,
			Notes:       l.notes,
		})

		// Dinner
		din := baseDishes[idx%len(baseDishes)]
		idx++
		result = append(result, CreateMealPlanInput{
			Date:        dateStr,
			MealType:    "dinner",
			RecipeTitle: din.title,
			Calories:    din.cals,
			Protein:     din.p,
			Fat:         din.f,
			Carbs:       din.c,
			Notes:       din.notes,
		})
	}

	return result
}
