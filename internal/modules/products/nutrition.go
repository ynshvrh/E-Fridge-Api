package products

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrBarcodeNotFound  = errors.New("barcode not found in OpenFoodFacts database")
	ErrInvalidBarcode   = errors.New("invalid barcode format")
	ErrEstimationFailed = errors.New("failed to estimate product nutrition")
)

type EstimateNutritionInput struct {
	Name     string  `json:"name"`
	Unit     string  `json:"unit,omitempty"`
	Quantity float64 `json:"quantity,omitempty"`
}

type NutritionEstimate struct {
	Name         string  `json:"name"`
	Calories     int32   `json:"calories"`
	Protein      float64 `json:"protein"`
	Fat          float64 `json:"fat"`
	Carbs        float64 `json:"carbs"`
	Category     string  `json:"category"`
	StandardUnit string  `json:"standard_unit"`
}

type BarcodeProductResult struct {
	Barcode  string  `json:"barcode"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	Calories int32   `json:"calories"`
	Protein  float64 `json:"protein"`
	Fat      float64 `json:"fat"`
	Carbs    float64 `json:"carbs"`
	Brands   string  `json:"brands,omitempty"`
	ImageURL string  `json:"image_url,omitempty"`
}

// EstimateNutrition estimates KBZhV and category for a given product name using AI
func (s *Service) EstimateNutrition(ctx context.Context, input EstimateNutritionInput) (*NutritionEstimate, error) {
	trimmedName := strings.TrimSpace(input.Name)
	if trimmedName == "" {
		return nil, ErrEmptyName
	}

	if s.cfg == nil || s.cfg.OpenRouterAPIKey == "" {
		return s.heuristicNutritionEstimate(trimmedName, input.Unit, input.Quantity), nil
	}

	modelsToTry := []string{}
	if s.cfg.FastModel != "" {
		modelsToTry = append(modelsToTry, s.cfg.FastModel)
	}
	if s.cfg.CheapModel != "" && s.cfg.CheapModel != s.cfg.FastModel {
		modelsToTry = append(modelsToTry, s.cfg.CheapModel)
	}
	if len(modelsToTry) == 0 {
		modelsToTry = []string{"meta-llama/llama-3.3-70b-instruct:free", "deepseek/deepseek-chat"}
	}

	systemPrompt := `Ти експерт-нутріціолог. За назвою продукту визнач його приблизну поживну цінність (КБЖВ) на 100г (або на 1 шт для яєць/фруктів) та найбільш відповідну категорію.
Категорія ОБОВ'ЯЗКОВО має бути однією з:
"dairy", "meat-fish", "vegetables", "fruits", "bakery", "pantry", "snacks", "drinks", "alcohol", "sauces", "frozen", "canned-prepared", "prepared-meals", "other".
Відповідай ВИКЛЮЧНО валідним JSON форматом без зайвих слів:
{
  "name": "очищена або уточнена назва продукту",
  "calories": 250,
  "protein": 12.5,
  "fat": 8.0,
  "carbs": 30.2,
  "category": "dairy",
  "standard_unit": "г"
}`

	userPrompt := fmt.Sprintf("Продукт: %s. Одиниця: %s, Кількість: %v.", trimmedName, input.Unit, input.Quantity)

	for _, model := range modelsToTry {
		estimate, err := s.callOpenRouterForNutrition(ctx, model, systemPrompt, userPrompt)
		if err == nil && estimate != nil {
			estimate.Name = trimmedName
			return estimate, nil
		}
		slog.Warn("Nutrition AI estimation model failed, trying next", "model", model, "error", err)
	}

	// Fallback to mechanical heuristic estimation if AI unavailable
	return s.heuristicNutritionEstimate(trimmedName, input.Unit, input.Quantity), nil
}

func (s *Service) callOpenRouterForNutrition(ctx context.Context, model, systemPrompt, userPrompt string) (*NutritionEstimate, error) {
	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.2,
		"response_format": map[string]string{
			"type": "json_object",
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+s.cfg.OpenRouterAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://e-fridge.app")
	req.Header.Set("X-Title", "E-Fridge")

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openrouter status %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	if len(parsed.Choices) == 0 {
		return nil, errors.New("empty response choices from openrouter")
	}

	rawContent := parsed.Choices[0].Message.Content
	rawContent = strings.TrimSpace(rawContent)
	rawContent = strings.TrimPrefix(rawContent, "```json")
	rawContent = strings.TrimPrefix(rawContent, "```")
	rawContent = strings.TrimSuffix(rawContent, "```")
	rawContent = strings.TrimSpace(rawContent)

	var estimate NutritionEstimate
	if err := json.Unmarshal([]byte(rawContent), &estimate); err != nil {
		return nil, fmt.Errorf("failed to parse AI nutrition response: %w", err)
	}

	if estimate.Calories < 0 {
		estimate.Calories = 0
	}
	if estimate.Protein < 0 {
		estimate.Protein = 0
	}
	if estimate.Fat < 0 {
		estimate.Fat = 0
	}
	if estimate.Carbs < 0 {
		estimate.Carbs = 0
	}

	return &estimate, nil
}

// LookupBarcode queries Open Food Facts API directly (without LLM)
func (s *Service) LookupBarcode(ctx context.Context, barcode string) (*BarcodeProductResult, error) {
	code := strings.TrimSpace(barcode)
	if code == "" || len(code) < 6 || len(code) > 20 {
		return nil, ErrInvalidBarcode
	}

	apiURL := fmt.Sprintf("https://world.openfoodfacts.org/api/v0/product/%s.json", url.PathEscape(code))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "E-Fridge - Go - Version 1.0 (https://github.com/ynshvrh/E-Fridge)")

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openfoodfacts request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrBarcodeNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openfoodfacts returned HTTP %d", resp.StatusCode)
	}

	var offResp struct {
		Status  int `json:"status"`
		Product *struct {
			ProductNameUk  string   `json:"product_name_uk"`
			ProductName    string   `json:"product_name"`
			ProductNameEn  string   `json:"product_name_en"`
			Brands         string   `json:"brands"`
			CategoriesTags []string `json:"categories_tags"`
			QuantityStr    string   `json:"quantity"`
			ImageFrontURL  string   `json:"image_front_url"`
			Nutriments     struct {
				EnergyKcal100g *float64 `json:"energy-kcal_100g"`
				EnergyKcal     *float64 `json:"energy-kcal"`
				Proteins100g   *float64 `json:"proteins_100g"`
				Fat100g        *float64 `json:"fat_100g"`
				Carbs100g      *float64 `json:"carbohydrates_100g"`
			} `json:"nutriments"`
		} `json:"product"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&offResp); err != nil {
		return nil, fmt.Errorf("failed to decode openfoodfacts response: %w", err)
	}

	if offResp.Status != 1 || offResp.Product == nil {
		return nil, ErrBarcodeNotFound
	}

	p := offResp.Product
	name := strings.TrimSpace(p.ProductNameUk)
	if name == "" {
		name = strings.TrimSpace(p.ProductName)
	}
	if name == "" {
		name = strings.TrimSpace(p.ProductNameEn)
	}
	if name == "" {
		name = strings.TrimSpace(p.Brands)
	}
	if name == "" {
		name = "Продукт #" + code
	}

	category := mapCategoriesToCategory(p.CategoriesTags, name)
	qty, unit := parseQuantityAndUnit(p.QuantityStr)

	var calories int32
	if p.Nutriments.EnergyKcal100g != nil {
		calories = int32(*p.Nutriments.EnergyKcal100g)
	} else if p.Nutriments.EnergyKcal != nil {
		calories = int32(*p.Nutriments.EnergyKcal)
	}

	var protein, fat, carbs float64
	if p.Nutriments.Proteins100g != nil {
		protein = *p.Nutriments.Proteins100g
	}
	if p.Nutriments.Fat100g != nil {
		fat = *p.Nutriments.Fat100g
	}
	if p.Nutriments.Carbs100g != nil {
		carbs = *p.Nutriments.Carbs100g
	}

	return &BarcodeProductResult{
		Barcode:  code,
		Name:     name,
		Category: category,
		Quantity: qty,
		Unit:     unit,
		Calories: calories,
		Protein:  protein,
		Fat:      fat,
		Carbs:    carbs,
		Brands:   p.Brands,
		ImageURL: p.ImageFrontURL,
	}, nil
}

func mapCategoriesToCategory(tags []string, name string) string {
	flat := strings.ToLower(strings.Join(tags, " ") + " " + name)

	switch {
	case containsAny(flat, "milk", "cheese", "yogurt", "butter", "cream", "dairy", "молок", "сир", "йогурт", "масло", "кефір", "сметан"):
		return "dairy"
	case containsAny(flat, "meat", "fish", "seafood", "beef", "pork", "chicken", "sausage", "м'яс", "риб", "кур", "яловичин", "свинин", "ковбас", "сосиск"):
		return "meat-fish"
	case containsAny(flat, "vegetable", "salad", "tomato", "cucumber", "potato", "onion", "carrot", "pepper", "garlic", "herb", "овоч", "помідор", "томат", "огір", "картоп", "цибул", "моркв", "салат", "капуст", "перец", "зелен", "часник", "брокол", "буряк"):
		return "vegetables"
	case containsAny(flat, "fruit", "apple", "banana", "orange", "berry", "strawberry", "фрукт", "яблук", "банан", "апельсин", "ягід", "полуниц", "лимон"):
		return "fruits"
	case containsAny(flat, "bread", "bakery", "croissant", "pastry", "хліб", "батон", "булочк", "круасан", "лаваш", "випічк"):
		return "bakery"
	case containsAny(flat, "pasta", "rice", "flour", "grain", "cereal", "oat", "гречк", "рис", "макарон", "борошн", "вівсян", "круп"):
		return "pantry"
	case containsAny(flat, "chip", "snack", "sweet", "chocolate", "candy", "cookie", "чипс", "снек", "шоколад", "цукерк", "печив", "горіх"):
		return "snacks"
	case containsAny(flat, "water", "juice", "tea", "coffee", "soda", "drink", "вод", "сік", "чай", "кава", "напій", "лимонад", "компот"):
		return "drinks"
	case containsAny(flat, "beer", "wine", "vodka", "whiskey", "cider", "пиво", "вино", "горілк", "сидр", "алкогол"):
		return "alcohol"
	case containsAny(flat, "oil", "sauce", "ketchup", "mayo", "vinegar", "spice", "олія", "соус", "кетчуп", "майонез", "оцет", "спеці"):
		return "sauces"
	case containsAny(flat, "frozen", "ice-cream", "заморож", "морозив"):
		return "frozen"
	case containsAny(flat, "canned", "preserve", "консерв", "паштет", "тушкованк"):
		return "canned-prepared"
	case containsAny(flat, "soup", "meal", "ready", "страва", "борщ", "суп", "плов", "вареник"):
		return "prepared-meals"
	default:
		return "other"
	}
}

func containsAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

var qtyRegex = regexp.MustCompile(`(?i)^([\d.,]+)\s*(kg|g|l|ml|cl|шт|pcs|уп)?$`)

func parseQuantityAndUnit(raw string) (float64, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 1, "шт"
	}

	matches := qtyRegex.FindStringSubmatch(raw)
	if len(matches) < 2 {
		return 1, "шт"
	}

	valStr := strings.Replace(matches[1], ",", ".", 1)
	val, err := strconv.ParseFloat(valStr, 64)
	if err != nil || val <= 0 {
		val = 1
	}

	unit := "шт"
	if len(matches) >= 3 && matches[2] != "" {
		u := strings.ToLower(matches[2])
		switch u {
		case "kg":
			unit = "кг"
		case "g":
			unit = "г"
		case "l":
			unit = "л"
		case "ml":
			unit = "мл"
		case "cl":
			val = val * 10
			unit = "мл"
		default:
			unit = "шт"
		}
	}

	return val, unit
}

// heuristicNutritionEstimate returns deterministic baseline nutrition for common food items
func (s *Service) heuristicNutritionEstimate(name, unit string, qty float64) *NutritionEstimate {
	lower := strings.ToLower(name)

	cat := mapCategoriesToCategory(nil, lower)
	var calories int32 = 120
	var protein float64 = 5.0
	var fat float64 = 4.0
	var carbs float64 = 15.0
	stdUnit := "г"

	switch {
	case strings.Contains(lower, "яйц"):
		calories = 143
		protein = 12.6
		fat = 9.5
		carbs = 0.7
		stdUnit = "шт"
	case strings.Contains(lower, "кур") || strings.Contains(lower, "філе"):
		calories = 165
		protein = 31.0
		fat = 3.6
		carbs = 0.0
	case strings.Contains(lower, "яловичин") || strings.Contains(lower, "свинин"):
		calories = 250
		protein = 26.0
		fat = 17.0
		carbs = 0.0
	case strings.Contains(lower, "молок"):
		calories = 54
		protein = 2.9
		fat = 2.5
		carbs = 4.7
		stdUnit = "мл"
	case strings.Contains(lower, "сир") || strings.Contains(lower, "cheese"):
		calories = 340
		protein = 25.0
		fat = 27.0
		carbs = 1.3
	case strings.Contains(lower, "гречк"):
		calories = 132
		protein = 4.5
		fat = 1.0
		carbs = 26.0
	case strings.Contains(lower, "рис"):
		calories = 130
		protein = 2.7
		fat = 0.3
		carbs = 28.0
	case strings.Contains(lower, "яблук"):
		calories = 52
		protein = 0.3
		fat = 0.2
		carbs = 13.8
	case strings.Contains(lower, "банан"):
		calories = 89
		protein = 1.1
		fat = 0.3
		carbs = 22.8
	case strings.Contains(lower, "хліб"):
		calories = 265
		protein = 9.0
		fat = 3.2
		carbs = 49.0
	case strings.Contains(lower, "огірок") || strings.Contains(lower, "помідор") || strings.Contains(lower, "томат"):
		calories = 18
		protein = 0.9
		fat = 0.2
		carbs = 3.9
	}

	return &NutritionEstimate{
		Name:         name,
		Calories:     calories,
		Protein:      protein,
		Fat:          fat,
		Carbs:        carbs,
		Category:     cat,
		StandardUnit: stdUnit,
	}
}
