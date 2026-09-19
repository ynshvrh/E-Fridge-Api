package nutrition

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
)

type Service struct {
	queries *db.Queries
}

func NewService(queries *db.Queries) *Service {
	return &Service{queries: queries}
}

type LogMealInput struct {
	Date     string  `json:"date"` // YYYY-MM-DD
	MealType string  `json:"meal_type"` // breakfast, lunch, dinner, snack
	FoodName string  `json:"food_name"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	Calories int32   `json:"calories"`
	Protein  float64 `json:"protein"`
	Fat      float64 `json:"fat"`
	Carbs    float64 `json:"carbs"`
}

type NutritionLogDTO struct {
	ID       uuid.UUID `json:"id"`
	UserID   uuid.UUID `json:"user_id"`
	Date     string    `json:"date"`
	MealType string    `json:"meal_type"`
	FoodName string    `json:"food_name"`
	Quantity float64   `json:"quantity"`
	Unit     string    `json:"unit"`
	Calories int32     `json:"calories"`
	Protein  float64   `json:"protein"`
	Fat      float64   `json:"fat"`
	Carbs    float64   `json:"carbs"`
	LoggedAt time.Time `json:"logged_at"`
}

type DailySummaryDTO struct {
	Date          string            `json:"date"`
	TotalCalories int32             `json:"total_calories"`
	TotalProtein  float64           `json:"total_protein"`
	TotalFat      float64           `json:"total_fat"`
	TotalCarbs    float64           `json:"total_carbs"`
	Goals         GoalsDTO          `json:"goals"`
	Logs          []NutritionLogDTO `json:"logs"`
}

type GoalsDTO struct {
	CalorieTarget int32   `json:"calorie_target"`
	ProteinTarget float64 `json:"protein_target"`
	FatTarget     float64 `json:"fat_target"`
	CarbsTarget   float64 `json:"carbs_target"`
}

func (s *Service) LogMeal(ctx context.Context, userID uuid.UUID, input LogMealInput) (*NutritionLogDTO, error) {
	if input.FoodName == "" {
		return nil, errors.New("food name is required")
	}

	date, err := time.Parse("2006-01-02", input.Date)
	if err != nil {
		date = time.Now()
	}

	mealType := input.MealType
	if mealType == "" {
		mealType = "snack"
	}

	unit := input.Unit
	if unit == "" {
		unit = "порц"
	}

	qty := input.Quantity
	if qty <= 0 {
		qty = 1.0
	}

	// Auto-estimate if not provided
	calories := input.Calories
	p, f, c := input.Protein, input.Fat, input.Carbs
	if calories == 0 && p == 0 && f == 0 && c == 0 {
		calcCal, calcP, calcF, calcC := CalculateEstimatedNutrition(input.FoodName, qty, unit)
		calories = int32(calcCal)
		p, f, c = calcP, calcF, calcC
	}

	logEntry, err := s.queries.CreateNutritionLog(ctx, db.CreateNutritionLogParams{
		UserID:   userID,
		Date:     pgtype.Date{Time: date, Valid: true},
		MealType: mealType,
		FoodName: input.FoodName,
		Quantity: qty,
		Unit:     unit,
		Calories: calories,
		Protein:  p,
		Fat:      f,
		Carbs:    c,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to save nutrition log: %w", err)
	}

	return &NutritionLogDTO{
		ID:       logEntry.ID,
		UserID:   logEntry.UserID,
		Date:     logEntry.Date.Time.Format("2006-01-02"),
		MealType: logEntry.MealType,
		FoodName: logEntry.FoodName,
		Quantity: logEntry.Quantity,
		Unit:     logEntry.Unit,
		Calories: logEntry.Calories,
		Protein:  logEntry.Protein,
		Fat:      logEntry.Fat,
		Carbs:    logEntry.Carbs,
		LoggedAt: logEntry.LoggedAt,
	}, nil
}

func (s *Service) GetDailySummary(ctx context.Context, userID uuid.UUID, dateStr string) (*DailySummaryDTO, error) {
	parsedDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		parsedDate = time.Now()
		dateStr = parsedDate.Format("2006-01-02")
	}

	rows, err := s.queries.ListNutritionLogsByDate(ctx, db.ListNutritionLogsByDateParams{
		UserID: userID,
		Date:   pgtype.Date{Time: parsedDate, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get nutrition logs: %w", err)
	}

	goals, err := s.GetGoals(ctx, userID)
	if err != nil {
		return nil, err
	}

	summary := &DailySummaryDTO{
		Date:  dateStr,
		Goals: *goals,
		Logs:  make([]NutritionLogDTO, 0, len(rows)),
	}

	for _, r := range rows {
		summary.TotalCalories += r.Calories
		summary.TotalProtein += r.Protein
		summary.TotalFat += r.Fat
		summary.TotalCarbs += r.Carbs

		summary.Logs = append(summary.Logs, NutritionLogDTO{
			ID:       r.ID,
			UserID:   r.UserID,
			Date:     r.Date.Time.Format("2006-01-02"),
			MealType: r.MealType,
			FoodName: r.FoodName,
			Quantity: r.Quantity,
			Unit:     r.Unit,
			Calories: r.Calories,
			Protein:  r.Protein,
			Fat:      r.Fat,
			Carbs:    r.Carbs,
			LoggedAt: r.LoggedAt,
		})
	}

	summary.TotalProtein = round2(summary.TotalProtein)
	summary.TotalFat = round2(summary.TotalFat)
	summary.TotalCarbs = round2(summary.TotalCarbs)

	return summary, nil
}

func (s *Service) DeleteLog(ctx context.Context, id, userID uuid.UUID) error {
	return s.queries.DeleteNutritionLog(ctx, db.DeleteNutritionLogParams{
		ID:     id,
		UserID: userID,
	})
}

func (s *Service) GetGoals(ctx context.Context, userID uuid.UUID) (*GoalsDTO, error) {
	g, err := s.queries.GetNutritionGoals(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// default goals
			return &GoalsDTO{
				CalorieTarget: 2000,
				ProteinTarget: 100,
				FatTarget:     70,
				CarbsTarget:   250,
			}, nil
		}
		return nil, err
	}

	return &GoalsDTO{
		CalorieTarget: g.CalorieTarget,
		ProteinTarget: g.ProteinTarget,
		FatTarget:     g.FatTarget,
		CarbsTarget:   g.CarbsTarget,
	}, nil
}

func (s *Service) UpdateGoals(ctx context.Context, userID uuid.UUID, goals GoalsDTO) (*GoalsDTO, error) {
	g, err := s.queries.UpsertNutritionGoals(ctx, db.UpsertNutritionGoalsParams{
		UserID:        userID,
		CalorieTarget: goals.CalorieTarget,
		ProteinTarget: goals.ProteinTarget,
		FatTarget:     goals.FatTarget,
		CarbsTarget:   goals.CarbsTarget,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update goals: %w", err)
	}

	return &GoalsDTO{
		CalorieTarget: g.CalorieTarget,
		ProteinTarget: g.ProteinTarget,
		FatTarget:     g.FatTarget,
		CarbsTarget:   g.CarbsTarget,
	}, nil
}
