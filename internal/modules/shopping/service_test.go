package shopping

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
)

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
