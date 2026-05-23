package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerAddr  string
	DatabaseURL string
	RedisURL    string
	JWTSecret   string
	OTPSecret   string

	SMSBaseURL string
	SMSAPIKey  string
	SMSSender  string

	LSBaseURL   string
	LSUsername  string
	LSPassword  string
	LSCompanyID string

	OTPExpiryMinutes     int
	OTPMaxRequests       int
	CounterTokenHours    int
	AdminTokenHours      int
	VarianceTolerancePct float64
	ExportDir            string
}

func Load() *Config {
	_ = godotenv.Load()

	return &Config{
		ServerAddr:  getEnv("SERVER_ADDR", ":8080"),
		DatabaseURL: mustEnv("DATABASE_URL"),
		RedisURL:    mustEnv("REDIS_URL"),
		JWTSecret:   mustEnv("JWT_SECRET"),
		OTPSecret:   mustEnv("OTP_SECRET"),

		// SMS and LS are required in production but warn-only so dev can start without them
		SMSBaseURL: getEnv("SMS_BASE_URL", "https://sms.localhost.co.zw/api/v1/sms"),
		SMSAPIKey:  warnEnv("SMS_API_KEY", "placeholder-set-in-production"),
		SMSSender:  getEnv("SMS_SENDER", "StockCount"),

		LSBaseURL:   warnEnv("LS_BASE_URL", "http://placeholder"),
		LSUsername:  warnEnv("LS_USERNAME", "placeholder"),
		LSPassword:  warnEnv("LS_PASSWORD", "placeholder"),
		LSCompanyID: warnEnv("LS_COMPANY_ID", "placeholder"),

		OTPExpiryMinutes:     getEnvInt("OTP_EXPIRY_MINUTES", 10),
		OTPMaxRequests:       getEnvInt("OTP_MAX_REQUESTS", 3),
		CounterTokenHours:    getEnvInt("COUNTER_TOKEN_HOURS", 12),
		AdminTokenHours:      getEnvInt("ADMIN_TOKEN_HOURS", 8),
		VarianceTolerancePct: getEnvFloat("VARIANCE_TOLERANCE_PCT", 2.0),
		ExportDir:            getEnv("EXPORT_DIR", "./exports"),
	}
}

// mustEnv panics if the variable is missing — used for secrets the app cannot start without.
func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic("required environment variable not set: " + key)
	}
	return v
}

// warnEnv logs a warning if missing but returns the fallback — used for integration keys.
func warnEnv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Printf("WARNING: %s not set, using placeholder — set this before using SMS/LS features", key)
		return fallback
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return fallback
	}
	return n
}

func getEnvFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var f float64
	if _, err := fmt.Sscanf(v, "%f", &f); err != nil {
		return fallback
	}
	return f
}
