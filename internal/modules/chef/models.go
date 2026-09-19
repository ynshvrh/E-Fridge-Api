package chef

type RecipeIngredient struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	Category string  `json:"category"`
	InFridge bool    `json:"in_fridge"`
}

type Recipe struct {
	Title         string             `json:"title"`
	Description   string             `json:"description"`
	PrepTimeMins  int                `json:"prep_time_mins"`
	CookTimeMins  int                `json:"cook_time_mins"`
	Servings      int                `json:"servings"`
	Calories      int32              `json:"calories"`
	ProteinGrams  float64            `json:"protein_grams"`
	FatGrams      float64            `json:"fat_grams"`
	CarbsGrams    float64            `json:"carbs_grams"`
	Ingredients   []RecipeIngredient `json:"ingredients"`
	Steps         []string           `json:"steps"`
}

type ShoppingSuggestion struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	Category string  `json:"category"`
}

type ChatMessage struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

type ChatRequest struct {
	Message           string        `json:"message"`
	History           []ChatMessage `json:"history,omitempty"`
	DietaryPreference string        `json:"dietary_preference,omitempty"`
	Language          string        `json:"language,omitempty"`
}

type ChatResponse struct {
	Reply               string               `json:"reply"`
	Recipe              *Recipe              `json:"recipe,omitempty"`
	ShoppingSuggestions []ShoppingSuggestion `json:"shopping_suggestions,omitempty"`
}

type GenerateRecipeRequest struct {
	MealType          string  `json:"meal_type,omitempty"`
	DietaryPreference string  `json:"dietary_preference,omitempty"`
	MaxPrepTimeMins   int     `json:"max_prep_time_mins,omitempty"`
	TargetCalories    int     `json:"target_calories,omitempty"`
}
