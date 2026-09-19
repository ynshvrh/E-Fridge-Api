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
	inventory := s.getFridgeInventoryStrings(ctx, fridgeID)

	if s.cfg.OpenRouterAPIKey != "" {
		res, err := s.chatWithOpenRouter(ctx, inventory, req)
		if err == nil && res != nil {
			return res, nil
		}
		slog.Warn("OpenRouter chat failed, using fallback", "error", err)
	}

	// Fallback
	return s.chatFallback(inventory, req), nil
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

	fallback := s.generateFallbackRecipe(s.getFridgeInventoryStrings(ctx, fridgeID))
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

	modelsToTry := s.cfg.OpenRouterModels
	if len(modelsToTry) == 0 {
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
