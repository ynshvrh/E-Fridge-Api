package auth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/crypto"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/jwt"
)

func TestPasswordHashing(t *testing.T) {
	password := "SecretPass123!"
	hash, err := crypto.HashPassword(password)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !crypto.CheckPassword(password, hash) {
		t.Errorf("expected password to match hash")
	}

	if crypto.CheckPassword("WrongPassword", hash) {
		t.Errorf("expected wrong password to fail")
	}
}

func TestTokenGenerationAndValidation(t *testing.T) {
	secret := "test-secret-key-12345678901234567890"
	userID := uuid.New()
	email := "test@example.com"
	name := "Test User"

	token, err := jwt.GenerateAccessToken(secret, 15*time.Minute, userID, email, name)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims, err := jwt.ValidateAccessToken(secret, token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("expected userID %s, got %s", userID, claims.UserID)
	}
	if claims.Email != email {
		t.Errorf("expected email %s, got %s", email, claims.Email)
	}
	if claims.Name != name {
		t.Errorf("expected name %s, got %s", name, claims.Name)
	}
}

func TestRegisterValidation(t *testing.T) {
	s := &Service{}

	// Invalid email
	_, err := s.Register(context.Background(), "invalid-email", "Name", "password123")
	if err == nil {
		t.Errorf("expected error for invalid email")
	}

	// Short password
	_, err = s.Register(context.Background(), "test@example.com", "Name", "123")
	if err == nil {
		t.Errorf("expected error for short password")
	}

	// Empty name
	_, err = s.Register(context.Background(), "test@example.com", "", "password123")
	if err == nil {
		t.Errorf("expected error for empty name")
	}
}
