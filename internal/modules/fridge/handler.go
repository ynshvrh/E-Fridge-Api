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

	// Public invite details check
	r.Get("/invites/{token}", h.GetInvite)

	// Protected routes
	r.Group(func(protected chi.Router) {
		protected.Use(middleware.Auth(jwtSecret))

		protected.Get("/", h.GetFridges)
		protected.Post("/", h.CreateFridge)
		protected.Get("/{id}", h.GetFridge)
		protected.Post("/{id}/members", h.AddMember)
		protected.Delete("/{id}/members/{userID}", h.RemoveMember)

		// Module 2 endpoints
		protected.Post("/{id}/invites", h.CreateInvite)
		protected.Post("/join/{token}", h.JoinFridge)
		protected.Post("/{id}/leave", h.LeaveFridge)
		protected.Post("/{id}/transfer", h.TransferOwnership)
	})

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

func (h *Handler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	actorID, _ := middleware.GetUserID(r.Context())
	fridgeIDStr := chi.URLParam(r, "id")
	fridgeID, err := uuid.Parse(fridgeIDStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid fridge ID", "INVALID_ID")
		return
	}

	invite, err := h.service.CreateInvite(r.Context(), fridgeID, actorID)
	if err != nil {
		if errors.Is(err, ErrNotAuthorized) {
			response.Error(w, http.StatusForbidden, "Лише власник або адміністратор може створювати запрошення", "FORBIDDEN")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "CREATE_INVITE_FAILED")
		return
	}

	response.JSON(w, http.StatusCreated, invite)
}

func (h *Handler) GetInvite(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	details, err := h.service.GetInviteDetails(r.Context(), token)
	if err != nil {
		if errors.Is(err, ErrInviteNotFoundOrExpired) {
			response.Error(w, http.StatusNotFound, "Запрошення не знайдено або термін його дії вичерпано", "INVITE_EXPIRED")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, details)
}

func (h *Handler) JoinFridge(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}
	token := chi.URLParam(r, "token")

	fridge, err := h.service.JoinFridge(r.Context(), token, userID)
	if err != nil {
		if errors.Is(err, ErrInviteNotFoundOrExpired) {
			response.Error(w, http.StatusNotFound, "Запрошення не знайдено або термін його дії вичерпано", "INVITE_EXPIRED")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "JOIN_FRIDGE_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, fridge)
}

func (h *Handler) LeaveFridge(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}
	fridgeIDStr := chi.URLParam(r, "id")
	fridgeID, err := uuid.Parse(fridgeIDStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid fridge ID", "INVALID_ID")
		return
	}

	if err := h.service.LeaveFridge(r.Context(), fridgeID, userID); err != nil {
		if errors.Is(err, ErrNotAuthorized) {
			response.Error(w, http.StatusForbidden, "Ви не є учасником цього холодильника", "FORBIDDEN")
			return
		}
		if errors.Is(err, ErrOwnerMustTransferOrDelete) {
			response.Error(w, http.StatusConflict, "Власник не може покинути холодильник, поки є інші учасники. Передайте права або видаліть холодильник.", "OWNER_MUST_TRANSFER")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "LEAVE_FRIDGE_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Ви успішно покинули холодильник"})
}

func (h *Handler) TransferOwnership(w http.ResponseWriter, r *http.Request) {
	currentOwnerID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}
	fridgeIDStr := chi.URLParam(r, "id")
	fridgeID, err := uuid.Parse(fridgeIDStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid fridge ID", "INVALID_ID")
		return
	}

	var body struct {
		NewOwnerID uuid.UUID `json:"new_owner_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NewOwnerID == uuid.Nil {
		response.Error(w, http.StatusBadRequest, "new_owner_id is required", "INVALID_REQUEST")
		return
	}

	if err := h.service.TransferOwnership(r.Context(), fridgeID, currentOwnerID, body.NewOwnerID); err != nil {
		if errors.Is(err, ErrCannotTransferToSelf) {
			response.Error(w, http.StatusBadRequest, "Неможливо передати права самому собі", "CANNOT_TRANSFER_TO_SELF")
			return
		}
		if errors.Is(err, ErrNotAuthorized) {
			response.Error(w, http.StatusForbidden, "Лише власник холодильника може передавати права власності", "FORBIDDEN")
			return
		}
		if errors.Is(err, ErrMemberNotFound) {
			response.Error(w, http.StatusNotFound, "Цільовий користувач не є учасником цього холодильника", "USER_NOT_FOUND")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "TRANSFER_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Права власності успішно передано"})
}
