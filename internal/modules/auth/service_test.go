package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/ynshvrh/E-Fridge-Api/internal/config"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
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

	// Short password (< 8 chars)
	_, err = s.Register(context.Background(), "test@example.com", "Name", "1234567")
	if err == nil {
		t.Errorf("expected error for short password (< 8 characters)")
	}

	// Empty name
	_, err = s.Register(context.Background(), "test@example.com", "", "password123")
	if err == nil {
		t.Errorf("expected error for empty name")
	}
}

func TestGenerateVerificationCode(t *testing.T) {
	codes := make(map[string]bool)
	for i := 0; i < 20; i++ {
		code, err := GenerateVerificationCode()
		if err != nil {
			t.Fatalf("failed to generate verification code: %v", err)
		}
		if len(code) != 6 {
			t.Errorf("expected code length 6, got %d (%s)", len(code), code)
		}
		for _, c := range code {
			if c < '0' || c > '9' {
				t.Errorf("expected only digits in code, got %c in %s", c, code)
			}
		}
		codes[code] = true
	}
	// At least several unique codes should be generated in 20 iterations
	if len(codes) < 15 {
		t.Errorf("expected high randomness, got %d unique codes out of 20", len(codes))
	}
}

func TestConfirmRegistrationValidation(t *testing.T) {
	s := &Service{}

	// Empty email
	_, err := s.ConfirmRegistration(context.Background(), "", "123456")
	if err == nil {
		t.Errorf("expected error for empty email")
	}

	// Empty code
	_, err = s.ConfirmRegistration(context.Background(), "user@example.com", "")
	if err == nil {
		t.Errorf("expected error for empty code")
	}
}

type mockMailer struct {
	sentEmails []struct {
		toEmail string
		name    string
		code    string
	}
	sentWelcomeEmails []struct {
		toEmail  string
		name     string
		password string
	}
}

func (m *mockMailer) SendVerificationEmail(ctx context.Context, toEmail, name, code string) error {
	m.sentEmails = append(m.sentEmails, struct {
		toEmail string
		name    string
		code    string
	}{toEmail, name, code})
	return nil
}

func (m *mockMailer) SendGoogleWelcomeEmail(ctx context.Context, toEmail, name, generatedPassword string) error {
	m.sentWelcomeEmails = append(m.sentWelcomeEmails, struct {
		toEmail  string
		name     string
		password string
	}{toEmail, name, generatedPassword})
	return nil
}

func TestRegistrationFullFlowWithDB(t *testing.T) {
	ctx := context.Background()
	dbURL := "postgres://postgres:postgrespassword@localhost:5432/e_fridge?sslmode=disable"

	pool, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Skip("skipping DB integration test, cannot connect to PostgreSQL")
		return
	}
	defer pool.Close(ctx)

	queries := db.New(pool)
	cfg := &config.Config{
		JWTSecret:       "test-secret-key-12345678901234567890",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,
		Environment:     "test",
	}

	service := NewService(queries, cfg)
	mailer := &mockMailer{}
	service.SetMailer(mailer)

	testEmail := "test_flow_" + uuid.New().String()[:8] + "@example.com"
	testName := "Flow Tester"
	testPassword := "SecretPassword123"

	// 1. Initiate Registration
	regRes, err := service.Register(ctx, testEmail, testName, testPassword)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if regRes.Status != "verification_required" {
		t.Errorf("expected status 'verification_required', got %s", regRes.Status)
	}
	if len(mailer.sentEmails) != 1 {
		t.Fatalf("expected 1 email sent, got %d", len(mailer.sentEmails))
	}

	sentCode := mailer.sentEmails[0].code
	if len(sentCode) != 6 {
		t.Errorf("expected 6-digit code, got %s", sentCode)
	}

	// Verify user does NOT exist in users table yet!
	_, err = queries.GetUserByEmail(ctx, testEmail)
	if err == nil {
		t.Errorf("user should NOT exist in users table before confirmation!")
	}

	// 2. Try confirming with invalid code
	_, err = service.ConfirmRegistration(ctx, testEmail, "000000")
	if err == nil {
		t.Errorf("expected error when confirming with wrong code")
	}

	// 3. Resend code - immediate call should fail due to rate limit cooldown
	_, err = service.ResendVerificationCode(ctx, testEmail)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited on immediate resend, got %v", err)
	}

	// Reset cooldown to simulate elapsed time
	service.ResetResendCooldown(testEmail)

	// Now resend should succeed
	resendRes, err := service.ResendVerificationCode(ctx, testEmail)
	if err != nil {
		t.Fatalf("ResendVerificationCode failed after cooldown reset: %v", err)
	}
	if resendRes.Status != "verification_required" {
		t.Errorf("expected status 'verification_required', got %s", resendRes.Status)
	}
	if len(mailer.sentEmails) != 2 {
		t.Fatalf("expected 2 emails sent after resend, got %d", len(mailer.sentEmails))
	}

	newSentCode := mailer.sentEmails[1].code

	// 4. Confirm with old code (should fail)
	if sentCode != newSentCode {
		_, err = service.ConfirmRegistration(ctx, testEmail, sentCode)
		if err == nil {
			t.Errorf("expected old code to be rejected after resend")
		}
	}

	// 5. Confirm with valid new code
	authRes, err := service.ConfirmRegistration(ctx, testEmail, newSentCode)
	if err != nil {
		t.Fatalf("ConfirmRegistration failed: %v", err)
	}
	if authRes.User.Email != testEmail {
		t.Errorf("expected user email %s, got %s", testEmail, authRes.User.Email)
	}
	if authRes.AccessToken == "" || authRes.RefreshToken == "" {
		t.Errorf("expected tokens to be generated")
	}
	if len(authRes.Fridges) == 0 {
		t.Errorf("expected default fridge to be created")
	}

	// 6. Verify user now exists in users table and pending is cleaned up
	createdUser, err := queries.GetUserByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("user should now exist in users table: %v", err)
	}
	if createdUser.Email != testEmail {
		t.Errorf("expected user email %s, got %s", testEmail, createdUser.Email)
	}

	_, err = queries.GetPendingRegistrationByEmail(ctx, testEmail)
	if err == nil {
		t.Errorf("pending registration should be deleted after successful confirmation")
	}

	// 7. Cleanup test user and fridges
	if len(authRes.Fridges) > 0 {
		_ = queries.DeleteFridge(ctx, authRes.Fridges[0].ID)
	}
	_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", testEmail)
}

func TestSignInWithGoogleNewUser(t *testing.T) {
	ctx := context.Background()
	dbURL := "postgres://postgres:postgrespassword@localhost:5432/e_fridge?sslmode=disable"

	pool, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Skip("skipping DB test, cannot connect to PostgreSQL")
		return
	}
	defer pool.Close(ctx)

	queries := db.New(pool)
	cfg := &config.Config{
		JWTSecret:       "test-secret-key-12345678901234567890",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,
		Environment:     "test",
	}

	service := NewService(queries, cfg)
	mailer := &mockMailer{}
	service.SetMailer(mailer)

	googleEmail := "google_user_" + uuid.New().String()[:8] + "@gmail.com"
	mockIDToken := "mock_google_" + googleEmail

	// 1. Google sign-in for new user
	authRes, err := service.SignInWithGoogle(ctx, mockIDToken)
	if err != nil {
		t.Fatalf("SignInWithGoogle failed: %v", err)
	}

	if authRes.User.Email != googleEmail {
		t.Errorf("expected email %s, got %s", googleEmail, authRes.User.Email)
	}
	if authRes.AccessToken == "" || authRes.RefreshToken == "" {
		t.Errorf("expected tokens to be returned")
	}
	if len(authRes.Fridges) == 0 {
		t.Errorf("expected default fridge to be created")
	}

	// 2. Check welcome email with generated password
	if len(mailer.sentWelcomeEmails) != 1 {
		t.Fatalf("expected 1 welcome email sent, got %d", len(mailer.sentWelcomeEmails))
	}

	welcome := mailer.sentWelcomeEmails[0]
	if welcome.toEmail != googleEmail {
		t.Errorf("expected welcome email to %s, got %s", googleEmail, welcome.toEmail)
	}
	if len(welcome.password) < 8 {
		t.Errorf("expected generated password >= 8 characters, got '%s'", welcome.password)
	}

	// 3. Verify user can also log in using the generated password!
	loginRes, err := service.Login(ctx, googleEmail, welcome.password)
	if err != nil {
		t.Fatalf("failed to login with generated password from Google signup: %v", err)
	}
	if loginRes.User.Email != googleEmail {
		t.Errorf("expected login email %s, got %s", googleEmail, loginRes.User.Email)
	}

	// 4. Cleanup
	if len(authRes.Fridges) > 0 {
		_ = queries.DeleteFridge(ctx, authRes.Fridges[0].ID)
	}
	_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", googleEmail)
}

func TestCleanExpiredPendingRegistrationsWithDB(t *testing.T) {
	ctx := context.Background()
	dbURL := "postgres://postgres:postgrespassword@localhost:5432/e_fridge?sslmode=disable"
	pool, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Skip("skipping DB integration test, cannot connect to PostgreSQL")
		return
	}
	defer pool.Close(ctx)

	queries := db.New(pool)
	cfg := &config.Config{
		JWTSecret:       "test-secret",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Environment:     "test",
	}
	service := NewService(queries, cfg)

	expiredEmail := "expired_" + uuid.New().String()[:8] + "@example.com"
	activeEmail := "active_" + uuid.New().String()[:8] + "@example.com"

	// Insert an expired registration (expired 1 hour ago)
	_, err = pool.Exec(ctx, `
		INSERT INTO pending_registrations (email, name, password_hash, verification_code, expires_at)
		VALUES ($1, 'Expired User', 'dummyhash', '111111', NOW() - INTERVAL '1 hour')
	`, expiredEmail)
	if err != nil {
		t.Fatalf("failed to insert expired pending registration: %v", err)
	}

	// Insert an active registration (expires in 48 hours)
	_, err = pool.Exec(ctx, `
		INSERT INTO pending_registrations (email, name, password_hash, verification_code, expires_at)
		VALUES ($1, 'Active User', 'dummyhash', '222222', NOW() + INTERVAL '48 hours')
	`, activeEmail)
	if err != nil {
		t.Fatalf("failed to insert active pending registration: %v", err)
	}

	// Run cleanup
	err = service.CleanExpiredPendingRegistrations(ctx)
	if err != nil {
		t.Fatalf("CleanExpiredPendingRegistrations failed: %v", err)
	}

	// Verify expired was deleted
	_, err = queries.GetPendingRegistrationByEmail(ctx, expiredEmail)
	if err == nil {
		t.Errorf("expected expired pending registration to be deleted")
	}

	// Verify active still exists
	activePending, err := queries.GetPendingRegistrationByEmail(ctx, activeEmail)
	if err != nil {
		t.Errorf("expected active pending registration to remain, got err: %v", err)
	} else if activePending.Email != activeEmail {
		t.Errorf("expected email %s, got %s", activeEmail, activePending.Email)
	}

	// Cleanup
	_ = queries.DeletePendingRegistration(ctx, activeEmail)
	_ = queries.DeletePendingRegistration(ctx, expiredEmail)
}

func TestDeleteAccountWithDB(t *testing.T) {
	ctx := context.Background()
	dbURL := "postgres://postgres:postgrespassword@localhost:5432/e_fridge?sslmode=disable"
	pool, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Skip("skipping DB integration test, cannot connect to PostgreSQL")
		return
	}
	defer pool.Close(ctx)

	queries := db.New(pool)
	cfg := &config.Config{
		JWTSecret:       "test-secret",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Environment:     "test",
	}
	service := NewService(queries, cfg)

	testEmail := "delete_test_" + uuid.New().String()[:8] + "@example.com"
	u, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        testEmail,
		Name:         "User To Delete",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Verify user exists
	_, err = queries.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("user should exist: %v", err)
	}

	// Delete user
	err = service.DeleteAccount(ctx, u.ID)
	if err != nil {
		t.Fatalf("DeleteAccount failed: %v", err)
	}

	// Verify user no longer exists
	_, err = queries.GetUserByID(ctx, u.ID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

