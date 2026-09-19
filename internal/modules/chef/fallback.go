package chef

import (
	"fmt"
	"strings"

	"github.com/ynshvrh/E-Fridge-Api/internal/db"
)

func (s *Service) chatFallback(prods []db.Product, req ChatRequest) *ChatResponse {
	msg := strings.ToLower(req.Message)
	isRecipeRequest := strings.Contains(msg, "рецепт") ||
		strings.Contains(msg, "приготу") ||
		strings.Contains(msg, "звари") ||
		strings.Contains(msg, "поїсти") ||
		strings.Contains(msg, "кухн") ||
		strings.Contains(msg, "салат") ||
		strings.Contains(msg, "сніданок") ||
		strings.Contains(msg, "обід") ||
		strings.Contains(msg, "вечер") ||
		strings.Contains(msg, "зроби") ||
		strings.Contains(msg, "що")

	if !isRecipeRequest && len(prods) == 0 {
		return &ChatResponse{
			Reply: "Привіт! Я ваш шеф-кухар E-Chef. Заповніть свій холодильник продуктами або запитайте мене про будь-яку страву чи рецепт!",
		}
	}

	recipe := s.generateFallbackRecipe(prods)
	return &ChatResponse{
		Reply:  fmt.Sprintf("Я підібрав для вас чудовий рецепт на основі доступних продуктів: %s!", recipe.Title),
		Recipe: &recipe,
	}
}

func (s *Service) generateFallbackRecipe(prods []db.Product) Recipe {
	var eggProd, cheeseProd, chickenProd, vegProd *db.Product

	for i := range prods {
		if prods[i].Quantity <= 0 {
			continue
		}
		lower := strings.ToLower(prods[i].Name)
		if strings.Contains(lower, "яйц") && eggProd == nil {
			eggProd = &prods[i]
		}
		if (strings.Contains(lower, "сир") || strings.Contains(lower, "моцарел")) && cheeseProd == nil {
			cheeseProd = &prods[i]
		}
		if (strings.Contains(lower, "курк") || strings.Contains(lower, "філе")) && chickenProd == nil {
			chickenProd = &prods[i]
		}
		if (strings.Contains(lower, "овоч") || strings.Contains(lower, "помідор") || strings.Contains(lower, "огірок") || strings.Contains(lower, "томат") || strings.Contains(lower, "перець") || strings.Contains(lower, "салат")) && vegProd == nil {
			vegProd = &prods[i]
		}
	}

	// 1. Eggs + Cheese -> Omelette
	if eggProd != nil && cheeseProd != nil {
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
				{Name: eggProd.Name, Quantity: minFloat(eggProd.Quantity, 3), Unit: eggProd.Unit, Category: "dairy", InFridge: true},
				{Name: cheeseProd.Name, Quantity: minFloat(cheeseProd.Quantity, 50), Unit: cheeseProd.Unit, Category: "dairy", InFridge: true},
				{Name: "Вершкове масло", Quantity: 10, Unit: "г", Category: "dairy", InFridge: false},
				{Name: "Сіль та перець", Quantity: 1, Unit: "дрібка", Category: "sauces", InFridge: false},
			},
			Steps: []string{
				"Збийте яйця виделкою з дрібкою солі та перцю.",
				"Розігрійте пательню з невеликим шматочком масла.",
				"Вилийте яєчну суміш і готуйте на помірному вогні 3-4 хвилини.",
				"Посипте натертим сиром одну половину омлету, складіть навпіл і подавайте.",
			},
		}
	}

	// 2. Cheese + Vegetables -> Salad with Cheese (e.g. Caprese / Greek)
	if cheeseProd != nil && vegProd != nil {
		return Recipe{
			Title:        fmt.Sprintf("Свіжий салат з %s та %s", strings.ToLower(vegProd.Name), strings.ToLower(cheeseProd.Name)),
			Description:  "Легка та корисна вітамінна страва з хрусткими овочами та ніжним сиром.",
			PrepTimeMins: 10,
			CookTimeMins: 0,
			Servings:     2,
			Calories:     240,
			ProteinGrams: 14,
			FatGrams:     16,
			CarbsGrams:   8,
			Ingredients: []RecipeIngredient{
				{Name: vegProd.Name, Quantity: minFloat(vegProd.Quantity, 200), Unit: vegProd.Unit, Category: "vegetables", InFridge: true},
				{Name: cheeseProd.Name, Quantity: minFloat(cheeseProd.Quantity, 100), Unit: cheeseProd.Unit, Category: "dairy", InFridge: true},
				{Name: "Оливкова олія", Quantity: 1, Unit: "ст.л.", Category: "sauces", InFridge: false},
				{Name: "Сіль та перець", Quantity: 1, Unit: "дрібка", Category: "sauces", InFridge: false},
			},
			Steps: []string{
				fmt.Sprintf("Ретельно помийте та наріжте %s шматочками.", strings.ToLower(vegProd.Name)),
				fmt.Sprintf("Наріжте кубиками або скибочками %s.", strings.ToLower(cheeseProd.Name)),
				"Викладіть у салатник, додайте олію, посоліть і поперчіть за смаком.",
				"Обережно перемішайте та подавайте до столу свіжим.",
			},
		}
	}

	// 3. Chicken
	if chickenProd != nil {
		return Recipe{
			Title:        "Соковите куряче філе",
			Description:  "Просте та багате на білок філе з апетитною скоринкою.",
			PrepTimeMins: 10,
			CookTimeMins: 15,
			Servings:     2,
			Calories:     280,
			ProteinGrams: 42,
			FatGrams:     9,
			CarbsGrams:   1,
			Ingredients: []RecipeIngredient{
				{Name: chickenProd.Name, Quantity: minFloat(chickenProd.Quantity, 300), Unit: chickenProd.Unit, Category: "meat-fish", InFridge: true},
				{Name: "Олія для смаження", Quantity: 1, Unit: "ст.л.", Category: "sauces", InFridge: false},
				{Name: "Спеції (сіль, перець, паприка)", Quantity: 1, Unit: "ч.л.", Category: "sauces", InFridge: false},
			},
			Steps: []string{
				"Промийте та обсушіть куряче філе паперовим рушником.",
				"Натріть сіллю, улюбленими спеціями та змастіть олією.",
				"Обсмажуйте на добре розігрітій пательні по 5-7 хвилин з кожного боку до готовності.",
			},
		}
	}

	// 4. Any products available in fridge
	if len(prods) > 0 {
		first := prods[0]
		ings := []RecipeIngredient{
			{Name: first.Name, Quantity: first.Quantity, Unit: first.Unit, Category: first.Category, InFridge: true},
			{Name: "Сіль та приправи", Quantity: 1, Unit: "дрібка", Category: "sauces", InFridge: false},
		}
		if len(prods) > 1 {
			second := prods[1]
			ings = append([]RecipeIngredient{ings[0], {Name: second.Name, Quantity: second.Quantity, Unit: second.Unit, Category: second.Category, InFridge: true}}, ings[1])
		}

		return Recipe{
			Title:        fmt.Sprintf("Домашня страва з %s", strings.ToLower(first.Name)),
			Description:  "Швидка та ситна страва, приготована з наявних у холодильнику інгредієнтів.",
			PrepTimeMins: 10,
			CookTimeMins: 15,
			Servings:     2,
			Calories:     260,
			ProteinGrams: 15,
			FatGrams:     12,
			CarbsGrams:   18,
			Ingredients:  ings,
			Steps: []string{
				"Підготуйте інгредієнти, промийте та наріжте зручними шматочками.",
				"Розігрійте пательню або сотейник на помірному вогні.",
				"Приготуйте основні продукти до готовності, додавши спеції за смаком.",
			},
		}
	}

	// Empty fridge default
	return Recipe{
		Title:        "Швидкий легкий салат",
		Description:  "Вітамінний овочевий салат для гарного настрою.",
		PrepTimeMins: 10,
		CookTimeMins: 0,
		Servings:     2,
		Calories:     120,
		ProteinGrams: 3,
		FatGrams:     8,
		CarbsGrams:   10,
		Ingredients: []RecipeIngredient{
			{Name: "Огірки", Quantity: 200, Unit: "г", Category: "vegetables", InFridge: false},
			{Name: "Помідори", Quantity: 200, Unit: "г", Category: "vegetables", InFridge: false},
			{Name: "Оливкова олія", Quantity: 1, Unit: "ст.л.", Category: "sauces", InFridge: false},
		},
		Steps: []string{
			"Наріжте овочі скибочками.",
			"Заправте олією та перемішайте.",
		},
	}
}

func minFloat(a, b float64) float64 {
	if a < b && a > 0 {
		return a
	}
	return b
}
