package chef

import (
	"testing"

	"github.com/ynshvrh/E-Fridge-Api/internal/config"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
)

func TestChefFallback(t *testing.T) {
	svc := NewService(nil, &config.Config{})

	products := []db.Product{
		{Name: "Яйця", Quantity: 6, Unit: "шт"},
		{Name: "Сир", Quantity: 200, Unit: "г"},
	}
	res := svc.chatFallback(products, ChatRequest{Message: "Що можна приготувати?"})

	if res == nil || res.Recipe == nil {
		t.Fatalf("expected recipe from fallback, got nil")
	}

	if res.Recipe.Title != "Омлет з ніжним сиром" {
		t.Errorf("expected 'Омлет з ніжним сиром', got %s", res.Recipe.Title)
	}
}

func TestBuildChatSystemPrompt(t *testing.T) {
	inventory := []string{"Молоко (1 л)", "Банани (3 шт)"}
	prompt := buildChatSystemPrompt(inventory, "vegetarian", "uk")

	if prompt == "" {
		t.Fatalf("expected non-empty prompt")
	}
}
