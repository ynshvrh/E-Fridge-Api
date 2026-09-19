package chef

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/middleware"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Routes(jwtSecret string, queries *db.Queries) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Auth(jwtSecret))
	r.Use(middleware.RequireFridge(queries))

	r.Post("/chat", h.Chat)
	r.Post("/generate", h.Generate)
	r.Get("/history", h.GetHistory)
	r.Delete("/history", h.ClearHistory)

	return r
}

func (h *Handler) GetHistory(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	history, err := h.service.GetHistory(r.Context(), fridgeID, 50)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error(), "FETCH_HISTORY_FAILED")
		return
	}
	response.JSON(w, http.StatusOK, history)
}

func (h *Handler) ClearHistory(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	if err := h.service.ClearHistory(r.Context(), fridgeID); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error(), "CLEAR_HISTORY_FAILED")
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "chat history cleared"})
}

func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	userID, _ := middleware.GetUserID(r.Context())

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	result, err := h.service.Chat(r.Context(), fridgeID, userID, req)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "AI Chef failed to process message", "CHEF_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, result)
}

func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	userID, _ := middleware.GetUserID(r.Context())

	var req GenerateRecipeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	result, err := h.service.GenerateRecipe(r.Context(), fridgeID, userID, req)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "AI Chef failed to generate recipe", "CHEF_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, result)
}
