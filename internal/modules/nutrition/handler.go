package nutrition

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ynshvrh/E-Fridge-Api/internal/middleware"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type CalculateRequest struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
}

func (h *Handler) Routes(jwtSecret string) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Auth(jwtSecret))

	r.Get("/daily", h.GetDaily)
	r.Post("/log", h.LogMeal)
	r.Put("/log/{id}", h.UpdateLog)
	r.Delete("/log/{id}", h.DeleteLog)
	r.Get("/goals", h.GetGoals)
	r.Put("/goals", h.UpdateGoals)
	r.Post("/calculate", h.Calculate)

	return r
}

func (h *Handler) GetDaily(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	dateStr := r.URL.Query().Get("date")
	summary, err := h.service.GetDailySummary(r.Context(), userID, dateStr)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to get daily summary", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, summary)
}

func (h *Handler) LogMeal(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	var input LogMealInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	item, err := h.service.LogMeal(r.Context(), userID, input)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error(), "BAD_REQUEST")
		return
	}

	response.JSON(w, http.StatusCreated, item)
}

func (h *Handler) UpdateLog(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid log ID", "INVALID_ID")
		return
	}

	var input UpdateLogInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	updated, err := h.service.UpdateLog(r.Context(), id, userID, input)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error(), "UPDATE_LOG_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, updated)
}

func (h *Handler) DeleteLog(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid log ID", "INVALID_ID")
		return
	}

	result, err := h.service.DeleteLog(r.Context(), id, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to delete log: "+err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, result)
}

func (h *Handler) GetGoals(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	goals, err := h.service.GetGoals(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to fetch goals", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, goals)
}

func (h *Handler) UpdateGoals(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	var req GoalsDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	goals, err := h.service.UpdateGoals(r.Context(), userID, req)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to update goals", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, goals)
}

func (h *Handler) Calculate(w http.ResponseWriter, r *http.Request) {
	var req CalculateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	cals, p, f, c := CalculateEstimatedNutrition(req.Name, req.Quantity, req.Unit)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"calories": cals,
		"protein":  p,
		"fat":      f,
		"carbs":    c,
	})
}
