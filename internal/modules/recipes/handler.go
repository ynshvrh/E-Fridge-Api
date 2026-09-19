package recipes

import (
	"encoding/json"
	"errors"
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

func (h *Handler) Routes(jwtSecret string) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Auth(jwtSecret))

	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/{id}", h.Get)
	r.Delete("/{id}", h.Delete)

	return r
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	recipes, err := h.service.ListRecipes(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, recipes)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	idStr := chi.URLParam(r, "id")
	recipeID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid recipe ID", "BAD_REQUEST")
		return
	}

	recipe, err := h.service.GetRecipe(r.Context(), userID, recipeID)
	if err != nil {
		if errors.Is(err, ErrRecipeNotFound) {
			response.Error(w, http.StatusNotFound, "Recipe not found", "NOT_FOUND")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, recipe)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	var fridgeIDPtr *uuid.UUID
	if fid, ok := middleware.GetFridgeID(r.Context()); ok {
		fridgeIDPtr = &fid
	}

	var input CreateRecipeInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	recipe, err := h.service.CreateRecipe(r.Context(), userID, fridgeIDPtr, input)
	if err != nil {
		if errors.Is(err, ErrEmptyTitle) {
			response.Error(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusCreated, recipe)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	idStr := chi.URLParam(r, "id")
	recipeID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid recipe ID", "BAD_REQUEST")
		return
	}

	if err := h.service.DeleteRecipe(r.Context(), userID, recipeID); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Recipe deleted successfully"})
}
