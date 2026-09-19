package chef

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ynshvrh/E-Fridge-Api/internal/config"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/cooking"
)

type Service struct {
	queries    *db.Queries
	cfg        *config.Config
	httpClient *http.Client
}

func NewService(queries *db.Queries, cfg *config.Config) *Service {
	return &Service{
		queries: queries,
		cfg:     cfg,
		httpClient: &http.Client{
			Timeout: 40 * time.Second,
		},
	}
}

func (s *Service) getFridgeInventoryStrings(ctx context.Context, fridgeID uuid.UUID) []string {
	prods, err := s.queries.ListProductsByFridge(ctx, fridgeID)
	if err != nil {
		return nil
	}
	res := make([]string, 0, len(prods))
	for _, p := range prods {
		if p.Quantity > 0 {
			res = append(res, fmt.Sprintf("%s (%v %s)", p.Name, p.Quantity, p.Unit))
		}
	}
	return res
}

func (s *Service) Chat(ctx context.Context, fridgeID, userID uuid.UUID, req ChatRequest) (*ChatResponse, error) {
	prods, err := s.queries.ListProductsByFridge(ctx, fridgeID)
	if err != nil {
		prods = []db.Product{}
	}

	inventory := make([]string, 0, len(prods))
	for _, p := range prods {
		if p.Quantity > 0 {
			inventory = append(inventory, fmt.Sprintf("%s (%v %s)", p.Name, p.Quantity, p.Unit))
		}
	}

	// Save user message to DB
	_ = s.saveMessage(ctx, fridgeID, userID, "user", req.Message, nil, nil)

	var chatRes *ChatResponse
	if s.cfg.OpenRouterAPIKey != "" {
		res, err := s.chatWithOpenRouter(ctx, inventory, req)
		if err == nil && res != nil {
			chatRes = res
		} else {
			slog.Warn("OpenRouter chat failed, using fallback", "error", err)
		}
	}

	if chatRes == nil {
		chatRes = s.chatFallback(prods, req)
	}

	// Verify and enforce InFridge against real DB products
	if chatRes != nil && chatRes.Recipe != nil {
		s.verifyAndEnrichRecipe(prods, chatRes)
	}

	// Save assistant message to DB
	if chatRes != nil {
		var recBytes, sugBytes []byte
		if chatRes.Recipe != nil {
			recBytes, _ = json.Marshal(chatRes.Recipe)
		}
		if len(chatRes.ShoppingSuggestions) > 0 {
			sugBytes, _ = json.Marshal(chatRes.ShoppingSuggestions)
		}
		_ = s.saveMessage(ctx, fridgeID, userID, "assistant", chatRes.Reply, recBytes, sugBytes)
	}

	return chatRes, nil
}

func (s *Service) verifyAndEnrichRecipe(prods []db.Product, chatRes *ChatResponse) {
	recipe := chatRes.Recipe
	if recipe == nil {
		return
	}

	existingSuggestions := make(map[string]bool)
	for _, sug := range chatRes.ShoppingSuggestions {
		existingSuggestions[strings.ToLower(strings.TrimSpace(sug.Name))] = true
	}

	for i := range recipe.Ingredients {
		ing := &recipe.Ingredients[i]
		match := cooking.FindFridgeProductMatch(ing.Name, prods)
		if match != nil && match.Quantity > 0 {
			ing.InFridge = true
		} else {
			ing.InFridge = false
			cleanName := strings.ToLower(strings.TrimSpace(ing.Name))
			if !existingSuggestions[cleanName] {
				chatRes.ShoppingSuggestions = append(chatRes.ShoppingSuggestions, ShoppingSuggestion{
					Name:     ing.Name,
					Quantity: ing.Quantity,
					Unit:     ing.Unit,
					Category: ing.Category,
				})
				existingSuggestions[cleanName] = true
			}
		}
	}
}

func (s *Service) GenerateRecipe(ctx context.Context, fridgeID, userID uuid.UUID, req GenerateRecipeRequest) (*Recipe, error) {
	prompt := "Запропонуй найкращий смачний рецепт з продуктів у моєму холодильнику."
	if req.MealType != "" {
		prompt += fmt.Sprintf(" Тип прийому їжі: %s.", req.MealType)
	}
	if req.TargetCalories > 0 {
		prompt += fmt.Sprintf(" Орієнтовна калорійність: %d ккал.", req.TargetCalories)
	}

	chatRes, err := s.Chat(ctx, fridgeID, userID, ChatRequest{
		Message:           prompt,
		DietaryPreference: req.DietaryPreference,
	})
	if err != nil {
		return nil, err
	}

	if chatRes.Recipe != nil {
		return chatRes.Recipe, nil
	}

	prods, _ := s.queries.ListProductsByFridge(ctx, fridgeID)
	fallback := s.generateFallbackRecipe(prods)
	dummyRes := &ChatResponse{Recipe: &fallback}
	s.verifyAndEnrichRecipe(prods, dummyRes)
	return &fallback, nil
}

func (s *Service) chatWithOpenRouter(ctx context.Context, inventory []string, req ChatRequest) (*ChatResponse, error) {
	systemPrompt := buildChatSystemPrompt(inventory, req.DietaryPreference, req.Language)

	messages := make([]map[string]string, 0, len(req.History)+2)
	messages = append(messages, map[string]string{
		"role":    "system",
		"content": systemPrompt,
	})

	for _, msg := range req.History {
		role := msg.Role
		if role != "user" && role != "assistant" {
			role = "user"
		}
		messages = append(messages, map[string]string{
			"role":    role,
			"content": msg.Content,
		})
	}

	messages = append(messages, map[string]string{
		"role":    "user",
		"content": req.Message,
	})

	var modelsToTry []string
	lowerMsg := strings.ToLower(req.Message)
	isComplex := strings.Contains(lowerMsg, "рецепт") ||
		strings.Contains(lowerMsg, "приготу") ||
		strings.Contains(lowerMsg, "план") ||
		strings.Contains(lowerMsg, "вечер") ||
		strings.Contains(lowerMsg, "обід")

	if isComplex {
		if s.cfg.SmartModel != "" {
			modelsToTry = append(modelsToTry, s.cfg.SmartModel)
		}
		if s.cfg.FastModel != "" {
			modelsToTry = append(modelsToTry, s.cfg.FastModel)
		}
	} else {
		if s.cfg.FastModel != "" {
			modelsToTry = append(modelsToTry, s.cfg.FastModel)
		}
		if s.cfg.SmartModel != "" {
			modelsToTry = append(modelsToTry, s.cfg.SmartModel)
		}
	}
	for _, m := range s.cfg.OpenRouterModels {
		found := false
		for _, added := range modelsToTry {
			if added == m {
				found = true
				break
			}
		}
		if !found {
			modelsToTry = append(modelsToTry, m)
		}
	}
	if len(modelsToTry) == 0 && s.cfg.OpenRouterModel != "" {
		modelsToTry = []string{s.cfg.OpenRouterModel}
	}

	var lastErr error
	for _, model := range modelsToTry {
		body := map[string]any{
			"model":       model,
			"messages":    messages,
			"temperature": 0.7,
			"response_format": map[string]string{
				"type": "json_object",
			},
		}

		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(jsonBody))
		if err != nil {
			return nil, err
		}

		httpReq.Header.Set("Authorization", "Bearer "+s.cfg.OpenRouterAPIKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("HTTP-Referer", "https://e-fridge.app")
		httpReq.Header.Set("X-Title", "E-Fridge")

		resp, err := s.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}

		respBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("openrouter returned status %d: %s", resp.StatusCode, string(respBytes))
			continue
		}

		var openRouterResp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}

		if err := json.Unmarshal(respBytes, &openRouterResp); err != nil {
			lastErr = err
			continue
		}

		if len(openRouterResp.Choices) == 0 {
			lastErr = errors.New("empty choices from openrouter")
			continue
		}

		rawContent := openRouterResp.Choices[0].Message.Content
		rawContent = cleanJSON(rawContent)

		var chatRes ChatResponse
		if err := json.Unmarshal([]byte(rawContent), &chatRes); err != nil {
			lastErr = fmt.Errorf("failed to decode JSON response: %w, content: %s", err, rawContent)
			continue
		}

		return &chatRes, nil
	}

	return nil, lastErr
}

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimSuffix(s, "```")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

type ChatMessageDTO struct {
	ID                  uuid.UUID            `json:"id"`
	FridgeID            uuid.UUID            `json:"fridge_id"`
	UserID              uuid.UUID            `json:"user_id"`
	Role                string               `json:"role"`
	Content             string               `json:"content"`
	Recipe              *Recipe              `json:"recipe,omitempty"`
	ShoppingSuggestions []ShoppingSuggestion `json:"shopping_suggestions,omitempty"`
	CreatedAt           time.Time            `json:"created_at"`
}

func (s *Service) saveMessage(ctx context.Context, fridgeID, userID uuid.UUID, role, content string, recData, sugData []byte) error {
	_, err := s.queries.CreateChefMessage(ctx, db.CreateChefMessageParams{
		FridgeID:            fridgeID,
		UserID:              userID,
		Role:                role,
		Content:             content,
		RecipeData:          recData,
		ShoppingSuggestions: sugData,
	})
	return err
}

func (s *Service) GetHistory(ctx context.Context, fridgeID uuid.UUID, limit int32) ([]ChatMessageDTO, error) {
	if limit <= 0 {
		limit = 50
	}
	msgs, err := s.queries.ListChefMessages(ctx, db.ListChefMessagesParams{
		FridgeID: fridgeID,
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}

	result := make([]ChatMessageDTO, 0, len(msgs))
	for _, m := range msgs {
		dto := ChatMessageDTO{
			ID:        m.ID,
			FridgeID:  m.FridgeID,
			UserID:    m.UserID,
			Role:      m.Role,
			Content:   m.Content,
			CreatedAt: m.CreatedAt,
		}
		if len(m.RecipeData) > 0 {
			var rec Recipe
			if err := json.Unmarshal(m.RecipeData, &rec); err == nil {
				dto.Recipe = &rec
			}
		}
		if len(m.ShoppingSuggestions) > 0 {
			var sugs []ShoppingSuggestion
			if err := json.Unmarshal(m.ShoppingSuggestions, &sugs); err == nil {
				dto.ShoppingSuggestions = sugs
			}
		}
		result = append(result, dto)
	}
	return result, nil
}

func (s *Service) ClearHistory(ctx context.Context, fridgeID uuid.UUID) error {
	return s.queries.ClearChefMessages(ctx, fridgeID)
}
