package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ynshvrh/E-Fridge-Api/internal/middleware"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type RegisterRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) Routes(jwtSecret string) chi.Router {
	r := chi.NewRouter()

	// Public routes
	r.Post("/register", h.Register)
	r.Post("/login", h.Login)
	r.Post("/refresh", h.Refresh)
	r.Post("/logout", h.Logout)

	// Protected routes
	r.Group(func(protected chi.Router) {
		protected.Use(middleware.Auth(jwtSecret))
		protected.Get("/me", h.Me)
		protected.Put("/profile", h.UpdateProfile)
		protected.Put("/password", h.UpdatePassword)
	})

	return r
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	var input UpdateProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	updated, err := h.service.UpdateProfile(r.Context(), userID, input)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error(), "UPDATE_PROFILE_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, updated)
}

func (h *Handler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	var input UpdatePasswordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if err := h.service.UpdatePassword(r.Context(), userID, input); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error(), "PASSWORD_CHANGE_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "password updated successfully"})
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	result, err := h.service.Register(r.Context(), req.Email, req.Name, req.Password)
	if err != nil {
		if errors.Is(err, ErrEmailAlreadyExists) {
			response.Error(w, http.StatusConflict, "Email is already registered", "EMAIL_EXISTS")
			return
		}
		if errors.Is(err, ErrInvalidInput) {
			response.Error(w, http.StatusBadRequest, err.Error(), "INVALID_INPUT")
			return
		}
		response.Error(w, http.StatusInternalServerError, "Failed to register user", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusCreated, result)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	result, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			response.Error(w, http.StatusUnauthorized, "Invalid email or password", "INVALID_CREDENTIALS")
			return
		}
		response.Error(w, http.StatusInternalServerError, "Failed to authenticate", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, result)
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	result, err := h.service.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrInvalidToken) {
			response.Error(w, http.StatusUnauthorized, "Refresh token is invalid or expired", "TOKEN_EXPIRED")
			return
		}
		response.Error(w, http.StatusInternalServerError, "Failed to refresh token", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, result)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	_ = h.service.Logout(r.Context(), req.RefreshToken)
	response.JSON(w, http.StatusOK, map[string]string{"message": "Logged out successfully"})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	user, fridges, err := h.service.GetMe(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to retrieve user profile", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"user":    user,
		"fridges": fridges,
	})
}
