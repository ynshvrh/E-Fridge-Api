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
	r.Post("/{id}/members", h.AddMember)
	r.Delete("/{id}/members/{userID}", h.RemoveMember)

	return r
}

func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	actorID, _ := middleware.GetUserID(r.Context())
	fridgeIDStr := chi.URLParam(r, "id")
	fridgeID, err := uuid.Parse(fridgeIDStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid fridge ID", "INVALID_ID")
		return
	}

	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	member, err := h.service.AddMemberByEmail(r.Context(), fridgeID, actorID, body.Email, body.Role)
	if err != nil {
		if errors.Is(err, ErrMemberNotFound) {
			response.Error(w, http.StatusNotFound, "Користувача з таким email не знайдено", "USER_NOT_FOUND")
			return
		}
		if errors.Is(err, ErrNotAuthorized) {
			response.Error(w, http.StatusForbidden, "Лише власник або адміністратор може додавати учасників", "FORBIDDEN")
			return
		}
		if errors.Is(err, ErrInvalidRole) {
			response.Error(w, http.StatusBadRequest, "Неприпустима роль учасника (дозволені: member, admin)", "INVALID_ROLE")
			return
		}
		response.Error(w, http.StatusBadRequest, err.Error(), "ADD_MEMBER_FAILED")
		return
	}

	response.JSON(w, http.StatusCreated, member)
}

func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	actorID, _ := middleware.GetUserID(r.Context())
	fridgeIDStr := chi.URLParam(r, "id")
	fridgeID, err := uuid.Parse(fridgeIDStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid fridge ID", "INVALID_ID")
		return
	}

	targetUserIDStr := chi.URLParam(r, "userID")
	targetUserID, err := uuid.Parse(targetUserIDStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid member user ID", "INVALID_ID")
		return
	}

	if err := h.service.RemoveMember(r.Context(), fridgeID, actorID, targetUserID); err != nil {
		if errors.Is(err, ErrCannotRemoveOwner) {
			response.Error(w, http.StatusForbidden, "Неможливо видалити власника холодильника", "CANNOT_REMOVE_OWNER")
			return
		}
		if errors.Is(err, ErrNotAuthorized) {
			response.Error(w, http.StatusForbidden, "Недостатньо прав для видалення цього учасника", "FORBIDDEN")
			return
		}
		if errors.Is(err, ErrMemberNotFound) {
			response.Error(w, http.StatusNotFound, "Учасника не знайдено", "USER_NOT_FOUND")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "REMOVE_MEMBER_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "member removed successfully"})
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
