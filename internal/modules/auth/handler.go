package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ynshvrh/E-Fridge-Api/internal/middleware"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/response"
)

type Handler struct {
	service         *Service
	loginLimiter    *middleware.RateLimiter
	registerLimiter *middleware.RateLimiter
	resendLimiter   *middleware.RateLimiter
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service:         service,
		loginLimiter:    middleware.NewRateLimiter(5, 1*time.Minute, "Занадто багато спроб входу. Зачекайте 1 хвилину перед наступною спробою.", "TOO_MANY_REQUESTS"),
		registerLimiter: middleware.NewRateLimiter(5, 5*time.Minute, "Занадто багато спроб реєстрації. Зачекайте кілька хвилин перед наступною спробою.", "TOO_MANY_REQUESTS"),
		resendLimiter:   middleware.NewRateLimiter(3, 5*time.Minute, "Занадто багато запитів коду. Зачекайте кілька хвилин перед наступною спробою.", "TOO_MANY_REQUESTS"),
	}
}

type RegisterRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type ConfirmRegistrationRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type ResendCodeRequest struct {
	Email string `json:"email"`
}

type GoogleAuthRequest struct {
	IDToken string `json:"id_token"`
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

	// Public routes with rate limiters
	r.With(h.registerLimiter.Middleware()).Post("/register", h.Register)
	r.Post("/register/confirm", h.ConfirmRegistration)
	r.With(h.resendLimiter.Middleware()).Post("/register/resend", h.ResendCode)
	r.Post("/google", h.GoogleAuth)
	r.With(h.loginLimiter.Middleware()).Post("/login", h.Login)
	r.Post("/refresh", h.Refresh)
	r.Post("/logout", h.Logout)

	// Protected routes
	r.Group(func(protected chi.Router) {
		protected.Use(middleware.Auth(jwtSecret))
		protected.Get("/me", h.Me)
		protected.Put("/profile", h.UpdateProfile)
		protected.Delete("/profile", h.DeleteAccount)
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

func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	if err := h.service.DeleteAccount(r.Context(), userID); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to delete account", "DELETE_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Акаунт успішно видалено"})
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
		if errors.Is(err, ErrRateLimited) {
			response.Error(w, http.StatusTooManyRequests, err.Error(), "RATE_LIMITED")
			return
		}
		if errors.Is(err, ErrInvalidInput) {
			response.Error(w, http.StatusBadRequest, err.Error(), "INVALID_INPUT")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, result)
}

func (h *Handler) ConfirmRegistration(w http.ResponseWriter, r *http.Request) {
	var req ConfirmRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	result, err := h.service.ConfirmRegistration(r.Context(), req.Email, req.Code)
	if err != nil {
		if errors.Is(err, ErrPendingRegistrationNotFound) {
			response.Error(w, http.StatusNotFound, "Не знайдено активної заявки на реєстрацію. Будь ласка, почніть реєстрацію спочатку.", "NOT_FOUND")
			return
		}
		if errors.Is(err, ErrInvalidVerificationCode) {
			response.Error(w, http.StatusBadRequest, "Невірний або прострочений код підтвердження", "INVALID_CODE")
			return
		}
		if errors.Is(err, ErrEmailAlreadyExists) {
			response.Error(w, http.StatusConflict, "Користувач вже зареєстрований", "EMAIL_EXISTS")
			return
		}
		if errors.Is(err, ErrInvalidInput) {
			response.Error(w, http.StatusBadRequest, err.Error(), "INVALID_INPUT")
			return
		}
		response.Error(w, http.StatusInternalServerError, "Помилка підтвердження реєстрації", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusCreated, result)
}

func (h *Handler) ResendCode(w http.ResponseWriter, r *http.Request) {
	var req ResendCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	result, err := h.service.ResendVerificationCode(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, ErrRateLimited) {
			response.Error(w, http.StatusTooManyRequests, err.Error(), "RATE_LIMITED")
			return
		}
		if errors.Is(err, ErrPendingRegistrationNotFound) {
			response.Error(w, http.StatusNotFound, "Немає активної реєстрації для цієї пошти. Будь ласка, заповніть форму реєстрації.", "NOT_FOUND")
			return
		}
		if errors.Is(err, ErrInvalidInput) {
			response.Error(w, http.StatusBadRequest, err.Error(), "INVALID_INPUT")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, result)
}

func (h *Handler) GoogleAuth(w http.ResponseWriter, r *http.Request) {
	var req GoogleAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	result, err := h.service.SignInWithGoogle(r.Context(), req.IDToken)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error(), "GOOGLE_AUTH_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, result)
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
