package config

import (
	"axiom/code-audit-server/internal/auth"
	"bytes"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Explicit production mode fails before opening or migrating the database.
// Development keeps its existing local defaults.
func ValidateEnvironment(getenv func(string) string) error {
	mode := getenv("AXIOM_ENV")
	if mode != "production" {
		if mode != "" && mode != "development" && mode != "test" {
			return fmt.Errorf("AXIOM_ENV must be development, test or production")
		}
		return nil
	}
	for _, name := range []string{"AXIOM_JWT_SECRET", "AXIOM_AGENT_TOKEN"} {
		v := getenv(name)
		if len(v) < 32 || strings.HasPrefix(v, "dev-") || strings.HasPrefix(v, "replace-") {
			return fmt.Errorf("%s must be an independently generated secret of at least 32 characters in production", name)
		}
	}
	password := getenv("AXIOM_ADMIN_PASSWORD")
	if !auth.ValidNewPassword(password) || strings.HasPrefix(password, "replace-") {
		return fmt.Errorf("AXIOM_ADMIN_PASSWORD must be set to at least 12 characters in production")
	}
	key, err := base64.RawURLEncoding.DecodeString(getenv("AXIOM_MASTER_KEY"))
	if err != nil || len(key) != 32 || bytes.Equal(key, make([]byte, 32)) || string(key) == "0123456789abcdef0123456789abcdef" {
		return fmt.Errorf("AXIOM_MASTER_KEY must be a generated base64url key of 32 bytes in production")
	}
	u, err := url.Parse(getenv("AXIOM_DATABASE_URL"))
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || strings.Trim(u.Path, "/") == "" {
		return fmt.Errorf("AXIOM_DATABASE_URL must point to PostgreSQL in production")
	}
	for _, name := range []string{"AXIOM_MAX_ACTIVE_SCANS", "AXIOM_MAX_MODEL_SCANS", "AXIOM_MAX_PROJECT_SCANS"} {
		if v := getenv(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 64 {
				return fmt.Errorf("%s must be between 1 and 64", name)
			}
		}
	}
	if v := getenv("AXIOM_SCAN_MOCK"); v != "" && v != "0" && v != "1" {
		return fmt.Errorf("AXIOM_SCAN_MOCK must be 0 or 1")
	}
	if v := getenv("AXIOM_MAX_RECOVERIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 10 {
			return fmt.Errorf("AXIOM_MAX_RECOVERIES must be between 0 and 10")
		}
	}
	return nil
}

func MaxRecoveries(getenv func(string) string) int {
	if n, err := strconv.Atoi(getenv("AXIOM_MAX_RECOVERIES")); err == nil && n >= 0 && n <= 10 {
		return n
	}
	if getenv("AXIOM_ENV") == "production" {
		return 0
	}
	return 2
}
