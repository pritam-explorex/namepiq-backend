package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Env              string
	HTTPAddr         string
	DatabaseURL      string
	CORSOrigins      []string
	JWTSecret        string
	AccessTokenTTL   time.Duration
	AppBaseURL       string
	APIBaseURL       string
	OpenSRSUsername       string
	OpenSRSAPIKey         string
	OpenSRSEndpoint       string
	OpenSRSMaxConcurrent  int
	RazorpayKeyID         string
	RazorpayKeySecret     string
	RazorpayWebhookSecret string
	ResendAPIKey          string
	ResendFromEmail       string
	TelegramBotToken      string
	TelegramChatID        string
	USDINRRate               float64
	RetailMarkupPercent      float64
	DisplayDiscountPercent   float64
	AdminEmails              []string
	// E2EMailCapture enables GET /v1/test/last-email for Playwright (never in production).
	E2EMailCapture bool
}

func Load() (Config, error) {
	_ = godotenv.Load()
	_ = godotenv.Load("../../.env")
	_ = godotenv.Load(".env")

	cfg := Config{
		Env:             getEnv("APP_ENV", "development"),
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://namepiq:namepiq@localhost:5432/namepiq?sslmode=disable"),
		CORSOrigins:     splitCSV(getEnv("CORS_ORIGINS", "http://localhost:3000")),
		JWTSecret:       getEnv("JWT_SECRET", "dev-only-change-me"),
		AccessTokenTTL:  getDuration("ACCESS_TOKEN_TTL", 24*time.Hour),
		AppBaseURL:      getEnv("APP_BASE_URL", "http://localhost:3000"),
		APIBaseURL:      getEnv("API_BASE_URL", "http://localhost:8080"),
		OpenSRSUsername:      getEnv("OPENSRS_USERNAME", ""),
		OpenSRSAPIKey:        getEnv("OPENSRS_API_KEY", ""),
		OpenSRSEndpoint:      getEnv("OPENSRS_ENDPOINT", "https://horizon.opensrs.net:55443"),
		OpenSRSMaxConcurrent: getInt("OPENSRS_MAX_CONCURRENT", 2),
		RazorpayKeyID:         getEnv("RAZORPAY_KEY_ID", ""),
		RazorpayKeySecret:     getEnv("RAZORPAY_KEY_SECRET", ""),
		RazorpayWebhookSecret: getEnv("RAZORPAY_WEBHOOK_SECRET", ""),
		ResendAPIKey:          getEnv("RESEND_API_KEY", ""),
		ResendFromEmail:       getEnv("RESEND_FROM_EMAIL", "Namepiq <noreply@namepiq.com>"),
		TelegramBotToken:      getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:        getEnv("TELEGRAM_CHAT_ID", ""),
		USDINRRate:             getFloat("USD_INR_RATE", 84),
		RetailMarkupPercent:    getFloat("RETAIL_MARKUP_PERCENT", 0),
		DisplayDiscountPercent: getFloat("DISPLAY_DISCOUNT_PERCENT", 0),
		AdminEmails:            splitCSV(strings.ToLower(getEnv("ADMIN_EMAILS", ""))),
		E2EMailCapture:         getEnv("E2E_MAIL_CAPTURE", "") == "1" || getEnv("E2E_MAIL_CAPTURE", "") == "true",
	}

	if cfg.Env == "production" && cfg.JWTSecret == "dev-only-change-me" {
		return Config{}, fmt.Errorf("JWT_SECRET must be set in production")
	}
	if cfg.Env == "production" && cfg.E2EMailCapture {
		return Config{}, fmt.Errorf("E2E_MAIL_CAPTURE must not be enabled in production")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return strings.Trim(v, `"'`)
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		if secs, err2 := strconv.Atoi(v); err2 == nil {
			return time.Duration(secs) * time.Second
		}
		return fallback
	}
	return d
}

func getFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func getInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
