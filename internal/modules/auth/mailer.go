package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"
)

type Mailer interface {
	SendVerificationEmail(ctx context.Context, toEmail, name, code string) error
}

type ResendMailer struct {
	apiKey      string
	fromEmail   string
	environment string
	httpClient  *http.Client
}

func NewResendMailer(apiKey, fromEmail, environment string) *ResendMailer {
	if fromEmail == "" {
		fromEmail = "E-Fridge <onboarding@resend.dev>"
	}
	return &ResendMailer{
		apiKey:      apiKey,
		fromEmail:   fromEmail,
		environment: environment,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

type resendResponse struct {
	ID         string `json:"id,omitempty"`
	StatusCode int    `json:"statusCode,omitempty"`
	Message    string `json:"message,omitempty"`
	Name       string `json:"name,omitempty"`
}

func (m *ResendMailer) SendVerificationEmail(ctx context.Context, toEmail, name, code string) error {
	toEmail = strings.TrimSpace(toEmail)
	if toEmail == "" {
		return errors.New("recipient email is required")
	}

	if m.apiKey == "" {
		slog.Warn("RESEND_API_KEY is not configured, logging verification code for development",
			"recipient", toEmail, "code", code)
		if m.environment == "production" {
			return errors.New("email service is not configured")
		}
		return nil
	}

	displayName := name
	if displayName == "" {
		displayName = "користувачу"
	}

	subject := fmt.Sprintf("Код підтвердження E-Fridge: %s", code)
	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html lang="uk">
<head>
  <meta charset="utf-8">
  <title>Код підтвердження E-Fridge</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #fafaf9; margin: 0; padding: 24px; color: #1c1917; }
    .card { max-width: 480px; margin: 0 auto; background: #ffffff; border-radius: 24px; padding: 36px 32px; border: 1px solid #e7e5e4; box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05); }
    .badge { width: 56px; height: 56px; margin: 0 auto 20px; border-radius: 18px; background: #ecfdf5; border: 1px solid #a7f3d0; text-align: center; line-height: 56px; font-size: 28px; }
    h1 { font-size: 22px; font-weight: 700; text-align: center; color: #1c1917; margin: 0 0 8px 0; }
    p { font-size: 14px; line-height: 1.6; color: #57534e; text-align: center; margin: 0 0 24px 0; }
    .code-box { background: #f0fdf4; border: 2px dashed #86efac; border-radius: 16px; padding: 18px 24px; text-align: center; margin: 28px 0; }
    .code { font-size: 34px; font-weight: 800; letter-spacing: 8px; color: #15803d; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
    .note { font-size: 13px; color: #78716c; margin-top: 24px; }
    .footer { font-size: 12px; color: #a8a29e; text-align: center; margin-top: 32px; border-top: 1px solid #f5f5f4; padding-top: 16px; }
  </style>
</head>
<body>
  <div class="card">
    <div class="badge">🥦</div>
    <h1>Підтвердження пошти</h1>
    <p>Вітаємо, <strong>%s</strong>! Щоб завершити створення акаунту в <strong>E-Fridge</strong>, введіть цей код:</p>
    <div class="code-box">
      <div class="code">%s</div>
    </div>
    <p class="note">Код дійсний протягом <strong>15 хвилин</strong>.<br>Якщо ви не реєструвалися в E-Fridge, просто проігноруйте цей лист.</p>
    <div class="footer">&copy; E-Fridge Ecosystem</div>
  </div>
</body>
</html>`, displayName, code)

	reqPayload := resendRequest{
		From:    m.fromEmail,
		To:      []string{toEmail},
		Subject: subject,
		HTML:    htmlBody,
	}

	payloadBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal email payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create email request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		slog.Error("Failed to call Resend API", "error", err, "recipient", toEmail)
		if m.environment == "development" {
			slog.Warn("Development fallback: logging verification code due to network failure",
				"recipient", toEmail, "code", code)
			return nil
		}
		return fmt.Errorf("failed to send verification email: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var resendResp resendResponse
		_ = json.Unmarshal(respBody, &resendResp)
		slog.Info("Verification email sent via Resend", "recipient", toEmail, "resend_id", resendResp.ID)
		return nil
	}

	var resendErr resendResponse
	_ = json.Unmarshal(respBody, &resendErr)

	// Resend free tier unverified domain restriction handling:
	if resp.StatusCode == http.StatusForbidden && strings.Contains(strings.ToLower(resendErr.Message), "testing emails to your own email address") {
		if m.environment == "development" {
			slog.Warn("Resend test-domain restriction hit; logging verification code for development",
				"recipient", toEmail, "code", code, "resend_message", resendErr.Message)
			return nil
		}
	}

	slog.Error("Resend API rejected email",
		"status", resp.StatusCode, "message", resendErr.Message, "recipient", toEmail)
	return fmt.Errorf("email delivery failed: %s", resendErr.Message)
}

func GenerateVerificationCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()+100000), nil
}
