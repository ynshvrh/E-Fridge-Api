package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/ynshvrh/E-Fridge-Api/internal/config"
)

type Mailer interface {
	SendVerificationEmail(ctx context.Context, toEmail, name, code string) error
}

func NewMailer(cfg *config.Config) Mailer {
	if strings.ToLower(cfg.EmailProvider) == "smtp" || (cfg.SMTPPassword != "" && cfg.EmailProvider != "resend") {
		return NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPFrom, cfg.Environment)
	}
	return NewResendMailer(cfg.ResendAPIKey, cfg.ResendFromEmail, cfg.Environment)
}

// ---------------------------------------------------------------------
// SMTP Mailer (e.g. for efr1dg3@gmail.com with App Password)
// ---------------------------------------------------------------------

type SMTPMailer struct {
	host        string
	port        string
	user        string
	password    string
	fromEmail   string
	environment string
}

func NewSMTPMailer(host, port, user, password, fromEmail, environment string) *SMTPMailer {
	if host == "" {
		host = "smtp.gmail.com"
	}
	if port == "" {
		port = "587"
	}
	if fromEmail == "" {
		fromEmail = fmt.Sprintf("E-Fridge <%s>", user)
	}
	return &SMTPMailer{
		host:        host,
		port:        port,
		user:        user,
		password:    password,
		fromEmail:   fromEmail,
		environment: environment,
	}
}

func (m *SMTPMailer) SendVerificationEmail(ctx context.Context, toEmail, name, code string) error {
	toEmail = strings.TrimSpace(toEmail)
	if toEmail == "" {
		return errors.New("recipient email is required")
	}

	if m.user == "" || m.password == "" {
		slog.Warn("SMTP credentials (user/password) not configured, logging verification code for development",
			"recipient", toEmail, "code", code)
		if m.environment == "production" {
			return errors.New("SMTP credentials are not configured")
		}
		return nil
	}

	displayName := name
	if displayName == "" {
		displayName = "користувачу"
	}

	subject := fmt.Sprintf("Код підтвердження E-Fridge: %s", code)
	htmlBody := buildVerificationHTML(displayName, code)

	msg := bytes.NewBuffer(nil)
	msg.WriteString(fmt.Sprintf("From: %s\r\n", m.fromEmail))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", toEmail))
	encodedSubject := "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(subject)) + "?="
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", encodedSubject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)

	addr := fmt.Sprintf("%s:%s", m.host, m.port)
	auth := smtp.PlainAuth("", m.user, m.password, m.host)

	err := smtp.SendMail(addr, auth, m.user, []string{toEmail}, msg.Bytes())
	if err != nil {
		slog.Error("Failed to send email via SMTP", "error", err, "recipient", toEmail)
		if m.environment == "development" {
			slog.Warn("Development fallback: logging code due to SMTP delivery failure",
				"recipient", toEmail, "code", code, "error", err)
			return nil
		}
		return fmt.Errorf("failed to send email via SMTP: %w", err)
	}

	slog.Info("Verification email sent successfully via SMTP", "recipient", toEmail, "sender", m.user)
	return nil
}

// ---------------------------------------------------------------------
// Resend HTTPS API Mailer
// ---------------------------------------------------------------------

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
	htmlBody := buildVerificationHTML(displayName, code)

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

func buildVerificationHTML(name, code string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
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
</html>`, name, code)
}

func GenerateVerificationCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()+100000), nil
}
