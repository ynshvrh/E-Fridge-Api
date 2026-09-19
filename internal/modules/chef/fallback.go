package chef

import (
	"fmt"
	"strings"
)

func (s *Service) chatFallback(inventory []string, req ChatRequest) *ChatResponse {
	// If fridge is empty or no recipe requested
	msg := strings.ToLower(req.Message)
	isRecipeRequest := strings.Contains(msg, "рецепт") ||
		strings.Contains(msg, "приготу") ||
		strings.Contains(msg, "звари") ||
		strings.Contains(msg, "поїсти") ||
		strings.Contains(msg, "кухн") ||
		strings.Contains(msg, "зроби")

	if !isRecipeRequest && len(inventory) == 0 {
		return &ChatResponse{
			Reply: "Привіт! Я ваш шеф-кухар E-Chef. Заповніть свій холодильник продуктами або запитайте мене про будь-яку страву чи рецепт!",
		}
	}

	recipe := s.generateFallbackRecipe(inventory)
	return &ChatResponse{
		Reply:  fmt.Sprintf("Я підібрав для вас чудовий рецепт на основі доступних продуктів: %s!", recipe.Title),
		Recipe: &recipe,
	}
}

func (s *Service) generateFallbackRecipe(inventory []string) Recipe {
	// Simple matching heuristic
	hasEggs := false
	hasCheese := false
	hasTomato := false
	hasChicken := false

	for _, item := range inventory {
		lower := strings.ToLower(item)
		if strings.Contains(lower, "яйц") {
			hasEggs = true
		}
		if strings.Contains(lower, "сир") {
			hasCheese = true
		}
		if strings.Contains(lower, "помідор") || strings.Contains(lower, "томат") {
			hasTomato = true
		}
		if strings.Contains(lower, "курк") || strings.Contains(lower, "філе") {
			hasChicken = true
		}
	}

	if hasEggs && hasCheese {
		return Recipe{
			Title:        "Омлет з ніжним сиром",
			Description:  "Швидкий і поживний сніданок з апетитною скоринкою та розплавленим сиром.",
			PrepTimeMins: 5,
			CookTimeMins: 10,
			Servings:     2,
			Calories:     320,
			ProteinGrams: 22,
			FatGrams:     24,
			CarbsGrams:   3,
			Ingredients: []RecipeIngredient{
				{Name: "Яйця", Quantity: 3, Unit: "шт", Category: "dairy", InFridge: true},
				{Name: "Сир", Quantity: 50, Unit: "г", Category: "dairy", InFridge: true},
				{Name: "Вершкове масло", Quantity: 10, Unit: "г", Category: "dairy", InFridge: false},
			},
			Steps: []string{
				"Збийте яйця виделкою з дрібкою солі та перцю.",
				"Розігрійте пательню з невеликим шматочком масла.",
				"Вилийте яєчну суміш і готуйте на помірному вогні 3-4 хвилини.",
				"Посипте натертим сиром одну половину омлету, складіть навпіл і подавайте.",
			},
		}
	}

	if hasChicken {
		return Recipe{
			Title:        "Соковите філе на грилі або сковороді",
			Description:  "Просте та багате на білок куряче філе з хрусткою скоринкою.",
			PrepTimeMins: 10,
			CookTimeMins: 15,
			Servings:     2,
			Calories:     280,
			ProteinGrams: 42,
			FatGrams:     9,
			CarbsGrams:   1,
			Ingredients: []RecipeIngredient{
				{Name: "Куряче філе", Quantity: 350, Unit: "г", Category: "meat-fish", InFridge: true},
				{Name: "Олія", Quantity: 1, Unit: "ст.л.", Category: "sauces", InFridge: true},
				{Name: "Спеції (сіль, перець, паприка)", Quantity: 1, Unit: "ч.л.", Category: "sauces", InFridge: false},
			},
			Steps: []string{
				"Промийте та обсушіть куряче філе паперовим рушником.",
				"Натріть сіллю, улюбленими спеціями та змастіть олією.",
				"Обсмажуйте на добре розігрітій пательні по 5-7 хвилин з кожного боку до золотистості.",
			},
		}
	}

	// General fresh salad
	return Recipe{
		Title:        "Свіжий домашній салат",
		Description:  "Легка та корисна вітамінна страва з овочів, які знайшлися на полицях.",
		PrepTimeMins: 10,
		CookTimeMins: 0,
		Servings:     2,
		Calories:     150,
		ProteinGrams: 4,
		FatGrams:     10,
		CarbsGrams:   12,
		Ingredients: []RecipeIngredient{
			{Name: "Овочі", Quantity: 200, Unit: "г", Category: "vegetables", InFridge: hasTomato},
			{Name: "Оливкова олія", Quantity: 1, Unit: "ст.л.", Category: "sauces", InFridge: true},
			{Name: "Сіль, перець", Quantity: 1, Unit: "дрібка", Category: "sauces", InFridge: true},
		},
		Steps: []string{
			"Ретельно помийте та наріжте свіжі овочі скибочками.",
			"Викладіть у глибоку тарілку, посоліть і поперчіть за смаком.",
			"Заправте олією та обережно перемішайте.",
		},
	}
}
