package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port             string
	DatabaseURL      string
	JWTSecret        string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	AllowedOrigins   []string
	Environment      string
	OpenRouterAPIKey string
	FreeModel        string
	FallbackModel    string
	FastModel        string
	CheapModel       string
	SmartModel       string
	OpenRouterModel  string
	OpenRouterModels []string
	ResendAPIKey     string
	ResendFromEmail  string
	EmailProvider    string
	SMTPHost         string
	SMTPPort         string
	SMTPUser         string
	SMTPPassword     string
	SMTPFrom         string
}

func Load() *Config {
	// Try loading .env if present
	_ = godotenv.Load()

	port := getEnv("PORT", "8080")
	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgrespassword@localhost:5432/e_fridge?sslmode=disable")
	jwtSecret := getEnv("JWT_SECRET", "super-secret-e-fridge-development-key-change-in-production-12345")
	env := getEnv("APP_ENV", "development")

	accessMinutes, _ := strconv.Atoi(getEnv("JWT_ACCESS_MINUTES", "15"))
	refreshDays, _ := strconv.Atoi(getEnv("JWT_REFRESH_DAYS", "30"))

	corsRaw := getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:3000,http://localhost:8080")
	origins := strings.Split(corsRaw, ",")
	for i := range origins {
		origins[i] = strings.TrimSpace(origins[i])
	}

	openRouterKey := getEnv("OPENROUTER_API_KEY", "")
	freeModel := getEnv("OPENROUTER_FREE_MODEL", getEnv("OPENROUTER_FAST_MODEL", "meta-llama/llama-3.3-70b-instruct:free"))
	fallbackModel := getEnv("OPENROUTER_FALLBACK_MODEL", getEnv("OPENROUTER_CHEAP_MODEL", "deepseek/deepseek-chat"))

	resendKey := getEnv("RESEND_API_KEY", "")
	resendFrom := getEnv("RESEND_FROM_EMAIL", "E-Fridge <onboarding@resend.dev>")

	emailProvider := getEnv("EMAIL_PROVIDER", "resend")
	smtpHost := getEnv("SMTP_HOST", "smtp.gmail.com")
	smtpPort := getEnv("SMTP_PORT", "587")
	smtpUser := getEnv("SMTP_USER", "efr1dg3@gmail.com")
	smtpPass := getEnv("SMTP_PASS", "")
	smtpFrom := getEnv("SMTP_FROM", "E-Fridge <efr1dg3@gmail.com>")

	if env == "production" && len(jwtSecret) < 32 {
		log.Fatal("JWT_SECRET must be at least 32 characters in production")
	}

	return &Config{
		Port:             port,
		DatabaseURL:      dbURL,
		JWTSecret:        jwtSecret,
		AccessTokenTTL:   time.Duration(accessMinutes) * time.Minute,
		RefreshTokenTTL:  time.Duration(refreshDays) * 24 * time.Hour,
		AllowedOrigins:   origins,
		Environment:      env,
		OpenRouterAPIKey: openRouterKey,
		FreeModel:        freeModel,
		FallbackModel:    fallbackModel,
		FastModel:        freeModel,
		CheapModel:       fallbackModel,
		SmartModel:       fallbackModel,
		OpenRouterModel:  freeModel,
		OpenRouterModels: []string{freeModel, fallbackModel},
		ResendAPIKey:     resendKey,
		ResendFromEmail:  resendFrom,
		EmailProvider:    emailProvider,
		SMTPHost:         smtpHost,
		SMTPPort:         smtpPort,
		SMTPUser:         smtpUser,
		SMTPPassword:     smtpPass,
		SMTPFrom:         smtpFrom,
	}
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}
