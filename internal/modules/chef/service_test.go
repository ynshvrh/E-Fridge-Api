package chef

import (
	"strings"
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

func TestChefFallback_WithImage(t *testing.T) {
	svc := NewService(nil, &config.Config{})

	products := []db.Product{
		{Name: "Яйця", Quantity: 4, Unit: "шт"},
		{Name: "Сир", Quantity: 150, Unit: "г"},
	}
	res := svc.chatFallback(products, ChatRequest{
		Message:     "Що на цьому фото?",
		ImageBase64: "data:image/jpeg;base64,/9j/4AAQSkZJRg==",
	})

	if res == nil {
		t.Fatalf("expected fallback response for image request, got nil")
	}

	if !strings.Contains(res.Reply, "фото") {
		t.Errorf("expected mention of photo in fallback reply, got: %s", res.Reply)
	}

	if res.Recipe == nil {
		t.Errorf("expected fallback recipe when image provided with products")
	}
}

func TestBuildChatSystemPrompt_DietaryAndFlavorPairing(t *testing.T) {
	inventory := []string{"Молоко (1 л)", "Банани (3 шт)"}
	prompt := buildChatSystemPrompt(inventory, "без лактози, алергія на горіхи", "ukrainian", "uk")

	if prompt == "" {
		t.Fatalf("expected non-empty prompt")
	}

	// Verify Flavor Pairing rules presence
	if !strings.Contains(prompt, "FLAVOR PAIRING") {
		t.Errorf("expected FLAVOR PAIRING rules in prompt")
	}

	// Verify strict prohibition of allergens
	if !strings.Contains(prompt, "КАТЕГОРИЧНА ЗАБОРОНА") {
		t.Errorf("expected КАТЕГОРИЧНА ЗАБОРОНА in prompt")
	}

	// Verify dietary profile injection
	if !strings.Contains(prompt, "без лактози, алергія на горіхи") {
		t.Errorf("expected user dietary preferences in prompt")
	}

	// Verify cuisine preference injection
	if !strings.Contains(prompt, "ukrainian") {
		t.Errorf("expected cuisine preference in prompt")
	}

	// Verify Multimodal Vision instructions
	if !strings.Contains(prompt, "VISION AI") {
		t.Errorf("expected VISION AI instructions in prompt")
	}
}
