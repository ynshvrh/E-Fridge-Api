package planner

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/middleware"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/response"
)

type Handler struct {
	service *Service
	aiGuard *middleware.AIGuard
}

func NewHandler(service *Service, aiGuard ...*middleware.AIGuard) *Handler {
	var guard *middleware.AIGuard
	if len(aiGuard) > 0 && aiGuard[0] != nil {
		guard = aiGuard[0]
	} else {
		guard = middleware.NewAIGuard(15*time.Second, 10, 5*time.Minute)
	}
	return &Handler{service: service, aiGuard: guard}
}

func (h *Handler) Routes(jwtSecret string, queries *db.Queries) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Auth(jwtSecret))
	r.Use(middleware.RequireFridge(queries))

	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.With(h.aiGuard.Middleware()).Post("/generate", h.Generate)
	r.With(h.aiGuard.Middleware()).Post("/generate-day", h.GenerateDay)
	r.With(h.aiGuard.Middleware()).Post("/generate-meal", h.GenerateMeal)
	r.Delete("/clear", h.Clear)

	r.Get("/{id}", h.Get)
	r.Put("/{id}", h.Update)
	r.Patch("/{id}/toggle", h.Toggle)
	r.Delete("/{id}", h.Delete)

	return r
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")

	plans, err := h.service.ListMealPlans(r.Context(), fridgeID, startDate, endDate)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error(), "FETCH_PLANS_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, plans)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid plan ID", "INVALID_ID")
		return
	}

	plan, err := h.service.GetMealPlan(r.Context(), fridgeID, id)
	if err != nil {
		response.Error(w, http.StatusNotFound, "Meal plan not found", "NOT_FOUND")
		return
	}

	response.JSON(w, http.StatusOK, plan)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	userID, _ := middleware.GetUserID(r.Context())

	var input CreateMealPlanInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	plan, err := h.service.CreateMealPlan(r.Context(), fridgeID, userID, input)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error(), "CREATE_PLAN_FAILED")
		return
	}

	response.JSON(w, http.StatusCreated, plan)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid plan ID", "INVALID_ID")
		return
	}

	var input UpdateMealPlanInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	plan, err := h.service.UpdateMealPlan(r.Context(), fridgeID, id, input)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error(), "UPDATE_PLAN_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, plan)
}

func (h *Handler) Toggle(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid plan ID", "INVALID_ID")
		return
	}

	var body struct {
		IsCompleted bool `json:"is_completed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	plan, err := h.service.ToggleMealPlanCompleted(r.Context(), fridgeID, id, body.IsCompleted)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error(), "TOGGLE_PLAN_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, plan)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid plan ID", "INVALID_ID")
		return
	}

	if err := h.service.DeleteMealPlan(r.Context(), fridgeID, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error(), "DELETE_PLAN_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "meal plan deleted"})
}

func (h *Handler) Clear(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")

	if err := h.service.ClearMealPlans(r.Context(), fridgeID, startDate, endDate); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error(), "CLEAR_PLANS_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "meal plans cleared"})
}

func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	userID, _ := middleware.GetUserID(r.Context())

	var input GeneratePlanInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		input.Days = 7
	}

	plans, err := h.service.GeneratePlanWithAI(r.Context(), fridgeID, userID, input)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error(), "GENERATE_PLAN_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, plans)
}

func (h *Handler) GenerateDay(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	userID, _ := middleware.GetUserID(r.Context())

	var input GenerateDayInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		input.Date = time.Now().Format("2006-01-02")
	}

	meals, err := h.service.GenerateDayWithAI(r.Context(), fridgeID, userID, input)
	if err != nil {
		if errors.Is(err, ErrDayAlreadyGenerated) {
			response.Error(w, http.StatusConflict, err.Error(), "DAY_ALREADY_GENERATED")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "GENERATE_DAY_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, meals)
}

func (h *Handler) GenerateMeal(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	userID, _ := middleware.GetUserID(r.Context())

	var input GenerateMealInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	meal, err := h.service.GenerateMealWithAI(r.Context(), fridgeID, userID, input)
	if err != nil {
		if errors.Is(err, ErrInvalidMealType) {
			response.Error(w, http.StatusBadRequest, err.Error(), "INVALID_MEAL_TYPE")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "GENERATE_MEAL_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, meal)
}

