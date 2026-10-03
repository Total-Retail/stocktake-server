package config

import (
	"fmt"
	"log"
	"os"
	"strings"

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

	// ERPBackend selects the ERP the server talks to: ERPBackendLS or ERPBackendVantage.
	ERPBackend string

	// Vantage Retail (Business Central online) — required when ERPBackend is "vantage".
	VantageTenantID     string
	VantageEnvironment  string
	VantageCompanyID    string
	VantageClientID     string
	VantageClientSecret string

	OTPExpiryMinutes     int
	OTPMaxRequests       int
	CounterTokenHours    int
	AdminTokenHours      int
	VarianceTolerancePct float64
	ExportDir            string
}

const (
	ERPBackendLS      = "ls"
	ERPBackendVantage = "vantage"
)

func Load() *Config {
	_ = godotenv.Load()

	erpBackend := getEnv("ERP_BACKEND", ERPBackendLS)

	// LS variables only warn when LS is the backend in use.
	lsEnv := warnEnv
	if erpBackend != ERPBackendLS {
		lsEnv = getEnv
	}

	cfg := &Config{
		ServerAddr:  getEnv("SERVER_ADDR", ":8080"),
		DatabaseURL: mustEnv("DATABASE_URL"),
		RedisURL:    mustEnv("REDIS_URL"),
		JWTSecret:   mustEnv("JWT_SECRET"),
		OTPSecret:   mustEnv("OTP_SECRET"),

		// SMS and LS are required in production but warn-only so dev can start without them
		SMSBaseURL: getEnv("SMS_BASE_URL", "https://sms.localhost.co.zw/api/v1/sms"),
		SMSAPIKey:  warnEnv("SMS_API_KEY", "placeholder-set-in-production"),
		SMSSender:  getEnv("SMS_SENDER", "StockCount"),

		LSBaseURL:   lsEnv("LS_BASE_URL", "http://placeholder"),
		LSUsername:  lsEnv("LS_USERNAME", "placeholder"),
		LSPassword:  lsEnv("LS_PASSWORD", "placeholder"),
		LSCompanyID: lsEnv("LS_COMPANY_ID", "placeholder"),

		ERPBackend:          erpBackend,
		VantageTenantID:     os.Getenv("VANTAGE_TENANT_ID"),
		VantageEnvironment:  os.Getenv("VANTAGE_ENVIRONMENT"),
		VantageCompanyID:    os.Getenv("VANTAGE_COMPANY_ID"),
		VantageClientID:     os.Getenv("VANTAGE_CLIENT_ID"),
		VantageClientSecret: os.Getenv("VANTAGE_CLIENT_SECRET"),

		OTPExpiryMinutes:     getEnvInt("OTP_EXPIRY_MINUTES", 10),
		OTPMaxRequests:       getEnvInt("OTP_MAX_REQUESTS", 3),
		CounterTokenHours:    getEnvInt("COUNTER_TOKEN_HOURS", 12),
		AdminTokenHours:      getEnvInt("ADMIN_TOKEN_HOURS", 8),
		VarianceTolerancePct: getEnvFloat("VARIANCE_TOLERANCE_PCT", 2.0),
		ExportDir:            getEnv("EXPORT_DIR", "./exports"),
	}

	if err := cfg.ValidateERP(); err != nil {
		panic(err.Error())
	}
	return cfg
}

// ValidateERP checks ERPBackend and, for Vantage, that every Vantage variable
// is set. It reads only the Config, so it is testable without a database.
func (c *Config) ValidateERP() error {
	switch c.ERPBackend {
	case ERPBackendLS:
		return nil
	case ERPBackendVantage:
		required := []struct{ env, val string }{
			{"VANTAGE_TENANT_ID", c.VantageTenantID},
			{"VANTAGE_ENVIRONMENT", c.VantageEnvironment},
			{"VANTAGE_COMPANY_ID", c.VantageCompanyID},
			{"VANTAGE_CLIENT_ID", c.VantageClientID},
			{"VANTAGE_CLIENT_SECRET", c.VantageClientSecret},
		}
		var missing []string
		for _, r := range required {
			if strings.TrimSpace(r.val) == "" {
				missing = append(missing, r.env)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("ERP_BACKEND=vantage but required environment variable(s) not set: %s",
				strings.Join(missing, ", "))
		}
		return nil
	default:
		return fmt.Errorf("invalid ERP_BACKEND %q: must be %q or %q", c.ERPBackend, ERPBackendLS, ERPBackendVantage)
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
