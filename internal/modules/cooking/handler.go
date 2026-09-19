package cooking

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

	r.Post("/cook", h.Cook)
	r.Post("/consume", h.Consume)

	return r
}

func (h *Handler) Cook(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	userID, _ := middleware.GetUserID(r.Context())

	var input CookRecipeInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	result, err := h.service.CookRecipe(r.Context(), fridgeID, userID, input)
	if err != nil {
		if result != nil && len(result.Missing) > 0 {
			// Return missing items with 422 Unprocessable Entity
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(response.APIResponse{
				Success: false,
				Data:    result,
				Error: &response.APIError{
					Code:    "MISSING_INGREDIENTS",
					Message: err.Error(),
				},
			})
			return
		}
		response.Error(w, http.StatusBadRequest, err.Error(), "COOKING_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, result)
}

func (h *Handler) Consume(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	userID, _ := middleware.GetUserID(r.Context())

	var input ConsumeMealInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	result, err := h.service.ConsumeMeal(r.Context(), fridgeID, userID, input)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error(), "CONSUMPTION_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, result)
}
