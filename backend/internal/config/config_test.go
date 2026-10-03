package config

import (
	"fmt"
	"strings"
	"testing"
)

func vantageCfg() Config {
	return Config{
		ERPBackend:          ERPBackendVantage,
		VantageTenantID:     "tenant",
		VantageEnvironment:  "Production",
		VantageCompanyID:    "company",
		VantageClientID:     "client",
		VantageClientSecret: "secret",
	}
}

func TestValidateERP(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr []string // substrings; nil = valid
	}{
		{"ls needs no vantage vars", func(c *Config) { *c = Config{ERPBackend: ERPBackendLS} }, nil},
		{"vantage complete", func(c *Config) {}, nil},
		{"unknown backend named", func(c *Config) { c.ERPBackend = "navision" }, []string{`"navision"`}},
		{"case matters", func(c *Config) { c.ERPBackend = "Vantage" }, []string{`"Vantage"`}},
		{"empty backend invalid", func(c *Config) { c.ERPBackend = "" }, []string{`ERP_BACKEND ""`}},
		{"missing tenant", func(c *Config) { c.VantageTenantID = "" }, []string{"VANTAGE_TENANT_ID"}},
		{"missing environment", func(c *Config) { c.VantageEnvironment = "" }, []string{"VANTAGE_ENVIRONMENT"}},
		{"missing company", func(c *Config) { c.VantageCompanyID = "" }, []string{"VANTAGE_COMPANY_ID"}},
		{"missing client id", func(c *Config) { c.VantageClientID = "" }, []string{"VANTAGE_CLIENT_ID"}},
		{"blank secret", func(c *Config) { c.VantageClientSecret = "  " }, []string{"VANTAGE_CLIENT_SECRET"}},
		{"several missing all named", func(c *Config) { c.VantageTenantID, c.VantageClientSecret = "", "" },
			[]string{"VANTAGE_TENANT_ID", "VANTAGE_CLIENT_SECRET"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := vantageCfg()
			tt.mutate(&c)
			err := c.ValidateERP()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateERP() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateERP() = nil, want error naming %v", tt.wantErr)
			}
			for _, w := range tt.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not name %q", err, w)
				}
			}
		})
	}
}

// TestLoadERPBackend checks the env wiring through Load (which reads env only;
// it does not connect to the database).
func TestLoadERPBackend(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantPanic string
		want      string
	}{
		{name: "default is ls", env: map[string]string{}, want: ERPBackendLS},
		{name: "explicit vantage", env: map[string]string{
			"ERP_BACKEND": "vantage", "VANTAGE_TENANT_ID": "t", "VANTAGE_ENVIRONMENT": "e",
			"VANTAGE_COMPANY_ID": "c", "VANTAGE_CLIENT_ID": "i", "VANTAGE_CLIENT_SECRET": "s",
		}, want: ERPBackendVantage},
		{name: "bad value fails startup", env: map[string]string{"ERP_BACKEND": "sap"}, wantPanic: `"sap"`},
		{name: "vantage missing secret fails startup", env: map[string]string{
			"ERP_BACKEND": "vantage", "VANTAGE_TENANT_ID": "t", "VANTAGE_ENVIRONMENT": "e",
			"VANTAGE_COMPANY_ID": "c", "VANTAGE_CLIENT_ID": "i",
		}, wantPanic: "VANTAGE_CLIENT_SECRET"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range []string{"ERP_BACKEND", "VANTAGE_TENANT_ID", "VANTAGE_ENVIRONMENT",
				"VANTAGE_COMPANY_ID", "VANTAGE_CLIENT_ID", "VANTAGE_CLIENT_SECRET"} {
				t.Setenv(k, "")
			}
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv("REDIS_URL", "redis://x")
			t.Setenv("JWT_SECRET", "j")
			t.Setenv("OTP_SECRET", "o")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			var got *Config
			panicked := func() (msg string) {
				defer func() {
					if r := recover(); r != nil {
						msg = fmt.Sprint(r)
					}
				}()
				got = Load()
				return ""
			}()
			if tt.wantPanic != "" {
				if !strings.Contains(panicked, tt.wantPanic) {
					t.Fatalf("Load() panic = %q, want one naming %s", panicked, tt.wantPanic)
				}
				return
			}
			if panicked != "" {
				t.Fatalf("Load() panicked: %s", panicked)
			}
			if got.ERPBackend != tt.want {
				t.Errorf("ERPBackend = %q, want %q", got.ERPBackend, tt.want)
			}
		})
	}
}
