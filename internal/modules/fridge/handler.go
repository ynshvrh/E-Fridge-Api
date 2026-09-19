package fridge

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

type CreateFridgeRequest struct {
	Name string `json:"name"`
}

func (h *Handler) Routes(jwtSecret string) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Auth(jwtSecret))

	r.Get("/", h.GetFridges)
	r.Post("/", h.CreateFridge)
	r.Get("/{id}", h.GetFridge)

	return r
}

func (h *Handler) GetFridges(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	fridges, err := h.service.GetMyFridges(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to get fridges", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, fridges)
}

func (h *Handler) CreateFridge(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	var req CreateFridgeRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	fridge, err := h.service.CreateFridge(r.Context(), userID, req.Name)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to create fridge", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusCreated, fridge)
}

func (h *Handler) GetFridge(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	idStr := chi.URLParam(r, "id")
	fridgeID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid fridge ID format", "INVALID_ID")
		return
	}

	fridge, err := h.service.GetFridgeDetails(r.Context(), fridgeID, userID)
	if err != nil {
		if errors.Is(err, ErrNotAuthorized) {
			response.Error(w, http.StatusForbidden, "Access denied to this fridge", "FORBIDDEN")
			return
		}
		if errors.Is(err, ErrFridgeNotFound) {
			response.Error(w, http.StatusNotFound, "Fridge not found", "NOT_FOUND")
			return
		}
		response.Error(w, http.StatusInternalServerError, "Failed to get fridge details", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, fridge)
}
