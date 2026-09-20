package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/ynshvrh/E-Fridge-Api/internal/config"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/crypto"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/jwt"
)

var (
	ErrInvalidCredentials          = errors.New("invalid email or password")
	ErrEmailAlreadyExists          = errors.New("email is already registered")
	ErrInvalidToken                = errors.New("invalid or expired refresh token")
	ErrUserNotFound                = errors.New("user not found")
	ErrInvalidInput                = errors.New("invalid input data")
	ErrInvalidVerificationCode     = errors.New("invalid or expired verification code")
	ErrPendingRegistrationNotFound = errors.New("no pending registration found for this email")
	ErrRateLimited                 = errors.New("rate limit exceeded")
)

type Service struct {
	queries         *db.Queries
	cfg             *config.Config
	mailer          Mailer
	resendCooldowns map[string]time.Time
	resendMu        sync.Mutex
}

func NewService(queries *db.Queries, cfg *config.Config) *Service {
	return &Service{
		queries:         queries,
		cfg:             cfg,
		mailer:          NewMailer(cfg),
		resendCooldowns: make(map[string]time.Time),
	}
}

func (s *Service) SetMailer(mailer Mailer) {
	s.mailer = mailer
}

type UserDTO struct {
	ID                 uuid.UUID `json:"id"`
	Email              string    `json:"email"`
	Name               string    `json:"name"`
	DietaryPreferences string    `json:"dietary_preferences"`
	CuisinePreference  string    `json:"cuisine_preference"`
	PreferredLanguage  string    `json:"preferred_language"`
	PreferredModel     string    `json:"preferred_model"`
	CreatedAt          time.Time `json:"created_at"`
}

type UpdateProfileInput struct {
	Name               string `json:"name"`
	DietaryPreferences string `json:"dietary_preferences"`
	CuisinePreference  string `json:"cuisine_preference"`
	PreferredLanguage  string `json:"preferred_language"`
	PreferredModel     string `json:"preferred_model"`
}

type UpdatePasswordInput struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

type FridgeDTO struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Role string    `json:"role"`
}

type AuthResult struct {
	User         UserDTO     `json:"user"`
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	Fridges      []FridgeDTO `json:"fridges"`
}

type TokenResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type RegisterInitiateResult struct {
	Status  string `json:"status"`
	Email   string `json:"email"`
	Message string `json:"message"`
	DevCode string `json:"dev_code,omitempty"`
}

func (s *Service) Register(ctx context.Context, email, name, password string) (*RegisterInitiateResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	name = strings.TrimSpace(name)

	if email == "" || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("%w: valid email is required", ErrInvalidInput)
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("%w: password must be at least 8 characters", ErrInvalidInput)
	}
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}

	// Check if already registered in users
	_, err := s.queries.GetUserByEmail(ctx, email)
	if err == nil {
		return nil, ErrEmailAlreadyExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed to check existing user: %w", err)
	}

	// Rate limit / cooldown per email (60s)
	s.resendMu.Lock()
	if lastSent, exists := s.resendCooldowns[email]; exists {
		if elapsed := time.Since(lastSent); elapsed < 60*time.Second {
			remaining := int((60*time.Second - elapsed).Seconds()) + 1
			s.resendMu.Unlock()
			return nil, fmt.Errorf("%w: код вже надіслано. Будь ласка, зачекайте %d с. перед повторною спробою", ErrRateLimited, remaining)
		}
	}
	s.resendCooldowns[email] = time.Now()
	s.resendMu.Unlock()

	hashedPassword, err := crypto.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	code, err := GenerateVerificationCode()
	if err != nil {
		return nil, fmt.Errorf("failed to generate verification code: %w", err)
	}

	expiresAt := time.Now().Add(48 * time.Hour)

	_, err = s.queries.CreateOrUpdatePendingRegistration(ctx, db.CreateOrUpdatePendingRegistrationParams{
		Email:            email,
		Name:             name,
		PasswordHash:     hashedPassword,
		VerificationCode: code,
		ExpiresAt:        expiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to save pending registration: %w", err)
	}

	if s.mailer != nil {
		if err := s.mailer.SendVerificationEmail(ctx, email, name, code); err != nil {
			return nil, fmt.Errorf("failed to send verification email: %w", err)
		}
	}

	res := &RegisterInitiateResult{
		Status:  "verification_required",
		Email:   email,
		Message: "Код підтвердження надіслано на вашу електронну пошту",
	}
	if s.cfg.Environment == "development" {
		res.DevCode = code
	}

	return res, nil
}

func (s *Service) ConfirmRegistration(ctx context.Context, email, code string) (*AuthResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	code = strings.TrimSpace(code)

	if email == "" || code == "" {
		return nil, fmt.Errorf("%w: email and verification code are required", ErrInvalidInput)
	}

	pending, err := s.queries.GetPendingRegistrationByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPendingRegistrationNotFound
		}
		return nil, fmt.Errorf("failed to find pending registration: %w", err)
	}

	if time.Now().After(pending.ExpiresAt) {
		return nil, ErrInvalidVerificationCode
	}

	if pending.VerificationCode != code {
		return nil, ErrInvalidVerificationCode
	}

	// Verify user doesn't already exist
	_, err = s.queries.GetUserByEmail(ctx, email)
	if err == nil {
		_ = s.queries.DeletePendingRegistration(ctx, email)
		return nil, ErrEmailAlreadyExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed to verify existing user: %w", err)
	}

	// Create user
	createdUser, err := s.queries.CreateUser(ctx, db.CreateUserParams{
		Email:        pending.Email,
		Name:         pending.Name,
		PasswordHash: pending.PasswordHash,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Delete pending registration
	_ = s.queries.DeletePendingRegistration(ctx, email)
	s.resendMu.Lock()
	delete(s.resendCooldowns, email)
	s.resendMu.Unlock()

	// Create default fridge
	defaultFridge, err := s.queries.CreateFridge(ctx, db.CreateFridgeParams{
		Name:    "Мій холодильник",
		OwnerID: createdUser.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create default fridge: %w", err)
	}

	// Add member as owner
	_, err = s.queries.AddFridgeMember(ctx, db.AddFridgeMemberParams{
		FridgeID: defaultFridge.ID,
		UserID:   createdUser.ID,
		Role:     "owner",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add user to fridge: %w", err)
	}

	// Tokens
	accessToken, err := jwt.GenerateAccessToken(s.cfg.JWTSecret, s.cfg.AccessTokenTTL, createdUser.ID, createdUser.Email, createdUser.Name)
	if err != nil {
		return nil, err
	}

	rawRefreshToken, err := crypto.GenerateRandomToken(32)
	if err != nil {
		return nil, err
	}

	_, err = s.queries.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		UserID:     createdUser.ID,
		TokenHash:  crypto.HashToken(rawRefreshToken),
		ExpiresAt:  time.Now().Add(s.cfg.RefreshTokenTTL),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to store refresh token: %w", err)
	}

	return &AuthResult{
		User: UserDTO{
			ID:        createdUser.ID,
			Email:     createdUser.Email,
			Name:      createdUser.Name,
			CreatedAt: createdUser.CreatedAt,
		},
		AccessToken:  accessToken,
		RefreshToken: rawRefreshToken,
		Fridges: []FridgeDTO{
			{
				ID:   defaultFridge.ID,
				Name: defaultFridge.Name,
				Role: "owner",
			},
		},
	}, nil
}

func (s *Service) ResendVerificationCode(ctx context.Context, email string) (*RegisterInitiateResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("%w: valid email is required", ErrInvalidInput)
	}

	// Check if already registered
	_, err := s.queries.GetUserByEmail(ctx, email)
	if err == nil {
		return nil, fmt.Errorf("%w: користувач вже зареєстрований, будь ласка, увійдіть", ErrInvalidInput)
	}

	pending, err := s.queries.GetPendingRegistrationByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPendingRegistrationNotFound
		}
		return nil, fmt.Errorf("failed to lookup pending registration: %w", err)
	}

	// Rate limit / cooldown per email (60s)
	s.resendMu.Lock()
	if lastSent, exists := s.resendCooldowns[email]; exists {
		if elapsed := time.Since(lastSent); elapsed < 60*time.Second {
			remaining := int((60*time.Second - elapsed).Seconds()) + 1
			s.resendMu.Unlock()
			return nil, fmt.Errorf("%w: зачекайте %d с. перед повторною відправкою коду", ErrRateLimited, remaining)
		}
	}
	s.resendCooldowns[email] = time.Now()
	s.resendMu.Unlock()

	code, err := GenerateVerificationCode()
	if err != nil {
		return nil, fmt.Errorf("failed to generate verification code: %w", err)
	}

	expiresAt := time.Now().Add(48 * time.Hour)
	err = s.queries.UpdatePendingRegistrationCode(ctx, db.UpdatePendingRegistrationCodeParams{
		Email:            email,
		VerificationCode: code,
		ExpiresAt:        expiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update verification code: %w", err)
	}

	if s.mailer != nil {
		if err := s.mailer.SendVerificationEmail(ctx, email, pending.Name, code); err != nil {
			return nil, fmt.Errorf("failed to send verification email: %w", err)
		}
	}

	res := &RegisterInitiateResult{
		Status:  "verification_required",
		Email:   email,
		Message: "Новий код підтвердження надіслано на вашу пошту",
	}
	if s.cfg.Environment == "development" {
		res.DevCode = code
	}

	return res, nil
}

func (s *Service) CleanExpiredPendingRegistrations(ctx context.Context) error {
	return s.queries.CleanExpiredPendingRegistrations(ctx)
}

func (s *Service) ResetResendCooldown(email string) {
	s.resendMu.Lock()
	delete(s.resendCooldowns, email)
	s.resendMu.Unlock()
}

func (s *Service) Login(ctx context.Context, email, password string) (*AuthResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))

	user, err := s.queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if !crypto.CheckPassword(password, user.PasswordHash) {
		return nil, ErrInvalidCredentials
	}

	accessToken, err := jwt.GenerateAccessToken(s.cfg.JWTSecret, s.cfg.AccessTokenTTL, user.ID, user.Email, user.Name)
	if err != nil {
		return nil, err
	}

	rawRefreshToken, err := crypto.GenerateRandomToken(32)
	if err != nil {
		return nil, err
	}

	_, err = s.queries.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		UserID:     user.ID,
		TokenHash:  crypto.HashToken(rawRefreshToken),
		ExpiresAt:  time.Now().Add(s.cfg.RefreshTokenTTL),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to store refresh token: %w", err)
	}

	// Fetch fridges
	fridges, err := s.queries.GetFridgesByUserID(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user fridges: %w", err)
	}

	var fridgeDTOs []FridgeDTO
	for _, f := range fridges {
		fridgeDTOs = append(fridgeDTOs, FridgeDTO{
			ID:   f.ID,
			Name: f.Name,
			Role: f.Role,
		})
	}

	return &AuthResult{
		User: UserDTO{
			ID:        user.ID,
			Email:     user.Email,
			Name:      user.Name,
			CreatedAt: user.CreatedAt,
		},
		AccessToken:  accessToken,
		RefreshToken: rawRefreshToken,
		Fridges:      fridgeDTOs,
	}, nil
}

func (s *Service) RefreshToken(ctx context.Context, rawRefreshToken string) (*TokenResult, error) {
	tokenHash := crypto.HashToken(rawRefreshToken)

	storedToken, err := s.queries.GetValidRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}

	// Rotate: revoke current token
	_ = s.queries.RevokeRefreshToken(ctx, tokenHash)

	user, err := s.queries.GetUserByID(ctx, storedToken.UserID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	newAccessToken, err := jwt.GenerateAccessToken(s.cfg.JWTSecret, s.cfg.AccessTokenTTL, user.ID, user.Email, user.Name)
	if err != nil {
		return nil, err
	}

	newRawRefreshToken, err := crypto.GenerateRandomToken(32)
	if err != nil {
		return nil, err
	}

	_, err = s.queries.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		UserID:     user.ID,
		TokenHash:  crypto.HashToken(newRawRefreshToken),
		ExpiresAt:  time.Now().Add(s.cfg.RefreshTokenTTL),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to store new refresh token: %w", err)
	}

	return &TokenResult{
		AccessToken:  newAccessToken,
		RefreshToken: newRawRefreshToken,
	}, nil
}

func (s *Service) Logout(ctx context.Context, rawRefreshToken string) error {
	if rawRefreshToken == "" {
		return nil
	}
	tokenHash := crypto.HashToken(rawRefreshToken)
	return s.queries.RevokeRefreshToken(ctx, tokenHash)
}

func (s *Service) GetMe(ctx context.Context, userID uuid.UUID) (*UserDTO, []FridgeDTO, error) {
	user, err := s.queries.GetUserFullByID(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrUserNotFound
		}
		return nil, nil, err
	}

	fridges, err := s.queries.GetFridgesByUserID(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}

	var fridgeDTOs []FridgeDTO
	for _, f := range fridges {
		fridgeDTOs = append(fridgeDTOs, FridgeDTO{
			ID:   f.ID,
			Name: f.Name,
			Role: f.Role,
		})
	}

	return &UserDTO{
		ID:                 user.ID,
		Email:              user.Email,
		Name:               user.Name,
		DietaryPreferences: user.DietaryPreferences,
		CuisinePreference:  user.CuisinePreference,
		PreferredLanguage:  user.PreferredLanguage,
		PreferredModel:     user.PreferredModel,
		CreatedAt:          user.CreatedAt,
	}, fridgeDTOs, nil
}

func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (*UserDTO, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, errors.New("name cannot be empty")
	}

	u, err := s.queries.UpdateUserProfile(ctx, db.UpdateUserProfileParams{
		ID:                 userID,
		Name:               name,
		DietaryPreferences: input.DietaryPreferences,
		CuisinePreference:  input.CuisinePreference,
		PreferredLanguage:  input.PreferredLanguage,
		PreferredModel:     input.PreferredModel,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update profile: %w", err)
	}

	return &UserDTO{
		ID:                 u.ID,
		Email:              u.Email,
		Name:               u.Name,
		DietaryPreferences: u.DietaryPreferences,
		CuisinePreference:  u.CuisinePreference,
		PreferredLanguage:  u.PreferredLanguage,
		PreferredModel:     u.PreferredModel,
		CreatedAt:          u.CreatedAt,
	}, nil
}

func (s *Service) DeleteAccount(ctx context.Context, userID uuid.UUID) error {
	return s.queries.DeleteUser(ctx, userID)
}


func (s *Service) UpdatePassword(ctx context.Context, userID uuid.UUID, input UpdatePasswordInput) error {
	if len(input.NewPassword) < 8 {
		return errors.New("new password must be at least 8 characters")
	}

	stored, err := s.queries.GetUserPasswordByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}

	if !crypto.CheckPassword(input.OldPassword, stored.PasswordHash) {
		return errors.New("incorrect old password")
	}

	newHash, err := crypto.HashPassword(input.NewPassword)
	if err != nil {
		return err
	}

	return s.queries.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
		ID:           userID,
		PasswordHash: newHash,
	})
}

type GoogleTokenInfo struct {
	Audience      string `json:"aud"`
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	Error         string `json:"error_description"`
}

func (s *Service) SignInWithGoogle(ctx context.Context, idToken string) (*AuthResult, error) {
	idToken = strings.TrimSpace(idToken)
	if idToken == "" {
		return nil, fmt.Errorf("%w: google id_token is required", ErrInvalidInput)
	}

	var email, name string

	// Support mock token in development / test environments
	if (s.cfg.Environment == "development" || s.cfg.Environment == "test") && strings.HasPrefix(idToken, "mock_google_") {
		email = strings.TrimPrefix(idToken, "mock_google_")
		name = "Google User"
	} else {
		// Call Google tokeninfo endpoint
		url := "https://oauth2.googleapis.com/tokeninfo?id_token=" + idToken
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create google verification request: %w", err)
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to verify google token: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, errors.New("invalid google token")
		}

		var info GoogleTokenInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			return nil, fmt.Errorf("failed to decode google token info: %w", err)
		}

		if info.Email == "" {
			return nil, errors.New("google token does not contain email")
		}

		if info.EmailVerified != "true" && info.EmailVerified != "1" {
			return nil, errors.New("google email is not verified")
		}

		if s.cfg.GoogleClientID != "" && info.Audience != s.cfg.GoogleClientID {
			return nil, errors.New("google token audience mismatch")
		}

		email = strings.TrimSpace(strings.ToLower(info.Email))
		name = strings.TrimSpace(info.Name)
		if name == "" {
			name = strings.Split(email, "@")[0]
		}
	}

	// Check if user exists in DB
	user, err := s.queries.GetUserByEmail(ctx, email)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed to lookup user: %w", err)
	}

	var userID uuid.UUID
	var userName = name

	if errors.Is(err, pgx.ErrNoRows) {
		// First-time Google registration: generate secure >= 8 chars password and send to email
		genPassword, err := GenerateSecurePassword(12)
		if err != nil {
			return nil, fmt.Errorf("failed to generate secure password: %w", err)
		}

		hash, err := crypto.HashPassword(genPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password: %w", err)
		}

		createdUser, err := s.queries.CreateUser(ctx, db.CreateUserParams{
			Email:        email,
			Name:         name,
			PasswordHash: hash,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
		userID = createdUser.ID
		userName = createdUser.Name

		// Create default fridge
		defaultFridge, err := s.queries.CreateFridge(ctx, db.CreateFridgeParams{
			Name:    "Мій холодильник",
			OwnerID: createdUser.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create default fridge: %w", err)
		}

		_, err = s.queries.AddFridgeMember(ctx, db.AddFridgeMemberParams{
			FridgeID: defaultFridge.ID,
			UserID:   createdUser.ID,
			Role:     "owner",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add member to fridge: %w", err)
		}

		// Send email with generated password
		if s.mailer != nil {
			_ = s.mailer.SendGoogleWelcomeEmail(ctx, email, name, genPassword)
		}
	} else {
		userID = user.ID
		userName = user.Name
	}

	// Generate tokens
	accessToken, err := jwt.GenerateAccessToken(s.cfg.JWTSecret, s.cfg.AccessTokenTTL, userID, email, userName)
	if err != nil {
		return nil, err
	}

	rawRefreshToken, err := crypto.GenerateRandomToken(32)
	if err != nil {
		return nil, err
	}

	_, err = s.queries.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		UserID:     userID,
		TokenHash:  crypto.HashToken(rawRefreshToken),
		ExpiresAt:  time.Now().Add(s.cfg.RefreshTokenTTL),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to store refresh token: %w", err)
	}

	fridges, err := s.queries.GetFridgesByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user fridges: %w", err)
	}

	var fridgeDTOs []FridgeDTO
	for _, f := range fridges {
		fridgeDTOs = append(fridgeDTOs, FridgeDTO{
			ID:   f.ID,
			Name: f.Name,
			Role: f.Role,
		})
	}

	return &AuthResult{
		User: UserDTO{
			ID:        userID,
			Email:     email,
			Name:      userName,
			CreatedAt: time.Now(),
		},
		AccessToken:  accessToken,
		RefreshToken: rawRefreshToken,
		Fridges:      fridgeDTOs,
	}, nil
}
