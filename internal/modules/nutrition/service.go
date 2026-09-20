package nutrition

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
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
	Date      string     `json:"date"` // YYYY-MM-DD
	MealType  string     `json:"meal_type"` // breakfast, lunch, dinner, snack
	FoodName  string     `json:"food_name"`
	Quantity  float64    `json:"quantity"`
	Unit      string     `json:"unit"`
	Calories  int32      `json:"calories"`
	Protein   float64    `json:"protein"`
	Fat       float64    `json:"fat"`
	Carbs     float64    `json:"carbs"`
	ProductID *uuid.UUID `json:"product_id,omitempty"`
	FridgeID  *uuid.UUID `json:"fridge_id,omitempty"`
}

type UpdateLogInput struct {
	MealType string   `json:"meal_type"`
	FoodName string   `json:"food_name"`
	Quantity float64  `json:"quantity"`
	Unit     string   `json:"unit"`
	Calories *int32   `json:"calories,omitempty"`
	Protein  *float64 `json:"protein,omitempty"`
	Fat      *float64 `json:"fat,omitempty"`
	Carbs    *float64 `json:"carbs,omitempty"`
}

type NutritionLogDTO struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	Date      string     `json:"date"`
	MealType  string     `json:"meal_type"`
	FoodName  string     `json:"food_name"`
	Quantity  float64    `json:"quantity"`
	Unit      string     `json:"unit"`
	Calories  int32      `json:"calories"`
	Protein   float64    `json:"protein"`
	Fat       float64    `json:"fat"`
	Carbs     float64    `json:"carbs"`
	LoggedAt  time.Time  `json:"logged_at"`
	ProductID *uuid.UUID `json:"product_id,omitempty"`
	FridgeID  *uuid.UUID `json:"fridge_id,omitempty"`
}

type DeleteLogResultDTO struct {
	Message          string  `json:"message"`
	RestoredToFridge bool    `json:"restored_to_fridge"`
	RestoredFridgeID string  `json:"restored_fridge_id,omitempty"`
	FoodName         string  `json:"food_name,omitempty"`
	Quantity         float64 `json:"quantity,omitempty"`
	Unit             string  `json:"unit,omitempty"`
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

func toLogDTO(r db.NutritionLog) *NutritionLogDTO {
	dto := &NutritionLogDTO{
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
	}
	if r.ProductID.Valid {
		pid := uuid.UUID(r.ProductID.Bytes)
		dto.ProductID = &pid
	}
	if r.FridgeID.Valid {
		fid := uuid.UUID(r.FridgeID.Bytes)
		dto.FridgeID = &fid
	}
	return dto
}

func (s *Service) LogMeal(ctx context.Context, userID uuid.UUID, input LogMealInput) (*NutritionLogDTO, error) {
	if strings.TrimSpace(input.FoodName) == "" {
		return nil, errors.New("food name is required")
	}

	date, err := time.Parse("2006-01-02", input.Date)
	if err != nil {
		date = time.Now()
	}

	mealType := strings.TrimSpace(input.MealType)
	if mealType == "" {
		mealType = "snack"
	}

	unit := strings.TrimSpace(input.Unit)
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

	var prodID, fridgeID pgtype.UUID
	if input.ProductID != nil {
		prodID = pgtype.UUID{Bytes: *input.ProductID, Valid: true}
	}
	if input.FridgeID != nil {
		fridgeID = pgtype.UUID{Bytes: *input.FridgeID, Valid: true}
	}

	logEntry, err := s.queries.CreateNutritionLog(ctx, db.CreateNutritionLogParams{
		UserID:    userID,
		Date:      pgtype.Date{Time: date, Valid: true},
		MealType:  mealType,
		FoodName:  input.FoodName,
		Quantity:  qty,
		Unit:      unit,
		Calories:  calories,
		Protein:   p,
		Fat:       f,
		Carbs:     c,
		ProductID: prodID,
		FridgeID:  fridgeID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to save nutrition log: %w", err)
	}

	return toLogDTO(logEntry), nil
}

func (s *Service) UpdateLog(ctx context.Context, id, userID uuid.UUID, input UpdateLogInput) (*NutritionLogDTO, error) {
	log, err := s.queries.GetNutritionLogByID(ctx, db.GetNutritionLogByIDParams{
		ID:     id,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("nutrition log not found")
		}
		return nil, err
	}

	mealType := strings.TrimSpace(input.MealType)
	if mealType == "" {
		mealType = log.MealType
	}
	foodName := strings.TrimSpace(input.FoodName)
	if foodName == "" {
		foodName = log.FoodName
	}
	unit := strings.TrimSpace(input.Unit)
	if unit == "" {
		unit = log.Unit
	}
	quantity := input.Quantity
	if quantity <= 0 {
		quantity = log.Quantity
	}

	// Recalculate or keep calories/macros
	calories := log.Calories
	protein := log.Protein
	fat := log.Fat
	carbs := log.Carbs

	if input.Calories != nil {
		calories = *input.Calories
	} else if quantity != log.Quantity && log.Quantity > 0 {
		ratio := quantity / log.Quantity
		calories = int32(float64(log.Calories) * ratio)
	}

	if input.Protein != nil {
		protein = *input.Protein
	} else if quantity != log.Quantity && log.Quantity > 0 {
		ratio := quantity / log.Quantity
		protein = round2(log.Protein * ratio)
	}

	if input.Fat != nil {
		fat = *input.Fat
	} else if quantity != log.Quantity && log.Quantity > 0 {
		ratio := quantity / log.Quantity
		fat = round2(log.Fat * ratio)
	}

	if input.Carbs != nil {
		carbs = *input.Carbs
	} else if quantity != log.Quantity && log.Quantity > 0 {
		ratio := quantity / log.Quantity
		carbs = round2(log.Carbs * ratio)
	}

	updated, err := s.queries.UpdateNutritionLog(ctx, db.UpdateNutritionLogParams{
		ID:       id,
		UserID:   userID,
		MealType: mealType,
		FoodName: foodName,
		Quantity: quantity,
		Unit:     unit,
		Calories: calories,
		Protein:  protein,
		Fat:      fat,
		Carbs:    carbs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update nutrition log: %w", err)
	}

	return toLogDTO(updated), nil
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

		dto := toLogDTO(r)
		summary.Logs = append(summary.Logs, *dto)
	}

	summary.TotalProtein = round2(summary.TotalProtein)
	summary.TotalFat = round2(summary.TotalFat)
	summary.TotalCarbs = round2(summary.TotalCarbs)

	return summary, nil
}

func (s *Service) DeleteLog(ctx context.Context, id, userID uuid.UUID) (*DeleteLogResultDTO, error) {
	log, err := s.queries.GetNutritionLogByID(ctx, db.GetNutritionLogByIDParams{
		ID:     id,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("nutrition log not found")
		}
		return nil, err
	}

	var restoredToFridge bool
	var restoredFridgeID string

	// If logged from a fridge, return consumed stock back into the fridge!
	if log.FridgeID.Valid {
		fridgeID := log.FridgeID.Bytes
		restoredFridgeID = uuid.UUID(fridgeID).String()

		// 1. Try to restore to existing product if product still exists
		if log.ProductID.Valid {
			prodID := log.ProductID.Bytes
			prod, err := s.queries.GetProductByID(ctx, db.GetProductByIDParams{
				ID:       prodID,
				FridgeID: fridgeID,
			})
			if err == nil {
				// Product exists! Add back consumed quantity
				newQty := math.Round((prod.Quantity+log.Quantity)*1000) / 1000
				_, _ = s.queries.UpdateProductQuantity(ctx, db.UpdateProductQuantityParams{
					ID:       prodID,
					FridgeID: fridgeID,
					Quantity: newQty,
				})
				restoredToFridge = true
			}
		}

		// 2. If product was completely eaten (removed from fridge) or ID was null, recreate it!
		if !restoredToFridge {
			expiry := pgtype.Date{Time: time.Now().AddDate(0, 0, 4), Valid: true}
			_, err := s.queries.CreateProduct(ctx, db.CreateProductParams{
				FridgeID:   fridgeID,
				Name:       log.FoodName,
				Category:   "other",
				Quantity:   log.Quantity,
				Unit:       log.Unit,
				ExpiryDate: expiry,
				Calories:   log.Calories,
				Protein:    log.Protein,
				Fat:        log.Fat,
				Carbs:      log.Carbs,
				Notes:      "Повернено зі щоденника харчування",
				CreatedBy:  userID,
			})
			if err == nil {
				restoredToFridge = true
			}
		}
	}

	// Delete log entry
	err = s.queries.DeleteNutritionLog(ctx, db.DeleteNutritionLogParams{
		ID:     id,
		UserID: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to delete nutrition log: %w", err)
	}

	msg := "Запис видалено"
	if restoredToFridge {
		msg = fmt.Sprintf("Запис видалено. Продукт \"%s\" (%v %s) повернуто в холодильник!", log.FoodName, log.Quantity, log.Unit)
	}

	return &DeleteLogResultDTO{
		Message:          msg,
		RestoredToFridge: restoredToFridge,
		RestoredFridgeID: restoredFridgeID,
		FoodName:         log.FoodName,
		Quantity:         log.Quantity,
		Unit:             log.Unit,
	}, nil
}

func (s *Service) GetGoals(ctx context.Context, userID uuid.UUID) (*GoalsDTO, error) {
	g, err := s.queries.GetNutritionGoals(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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
