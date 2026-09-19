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

	return r
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
