package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/ynshvrh/E-Fridge-Api/internal/config"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/crypto"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/jwt"
)

var (
	ErrInvalidCredentials  = errors.New("invalid email or password")
	ErrEmailAlreadyExists  = errors.New("email is already registered")
	ErrInvalidToken        = errors.New("invalid or expired refresh token")
	ErrUserNotFound        = errors.New("user not found")
	ErrInvalidInput        = errors.New("invalid input data")
)

type Service struct {
	queries *db.Queries
	cfg     *config.Config
}

func NewService(queries *db.Queries, cfg *config.Config) *Service {
	return &Service{
		queries: queries,
		cfg:     cfg,
	}
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

func (s *Service) Register(ctx context.Context, email, name, password string) (*AuthResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	name = strings.TrimSpace(name)

	if email == "" || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("%w: valid email is required", ErrInvalidInput)
	}
	if len(password) < 6 {
		return nil, fmt.Errorf("%w: password must be at least 6 characters", ErrInvalidInput)
	}
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}

	// Check if already exists
	_, err := s.queries.GetUserByEmail(ctx, email)
	if err == nil {
		return nil, ErrEmailAlreadyExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed to check existing user: %w", err)
	}

	hashedPassword, err := crypto.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Create user
	createdUser, err := s.queries.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		Name:         name,
		PasswordHash: hashedPassword,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

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

func (s *Service) UpdatePassword(ctx context.Context, userID uuid.UUID, input UpdatePasswordInput) error {
	if len(input.NewPassword) < 6 {
		return errors.New("new password must be at least 6 characters")
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
