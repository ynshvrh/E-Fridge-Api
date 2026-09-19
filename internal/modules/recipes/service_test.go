package recipes

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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
