package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestProductionRejectsUnsafeConfiguration(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	good := map[string]string{"AXIOM_ENV": "production", "AXIOM_JWT_SECRET": strings.Repeat("aB9_", 10), "AXIOM_AGENT_TOKEN": strings.Repeat("kD3_", 10), "AXIOM_MASTER_KEY": base64.RawURLEncoding.EncodeToString(key), "AXIOM_ADMIN_PASSWORD": "fixture-admin-password", "AXIOM_DATABASE_URL": "postgres://fixture:secret@postgres:5432/axiom"}
	if e := ValidateEnvironment(func(k string) string { return good[k] }); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []struct{ name, value string }{
		{"AXIOM_JWT_SECRET", ""}, {"AXIOM_JWT_SECRET", "replace-with-long-random-string-fixture"},
		{"AXIOM_AGENT_TOKEN", "dev-agent-token"}, {"AXIOM_ADMIN_PASSWORD", "admin"},
		{"AXIOM_ADMIN_PASSWORD", "密码不足十二字符"}, {"AXIOM_ADMIN_PASSWORD", strings.Repeat("a", 1025)},
		{"AXIOM_MASTER_KEY", ""}, {"AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32))},
		{"AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))},
		{"AXIOM_DATABASE_URL", "/tmp/agent.db"}, {"AXIOM_MAX_ACTIVE_SCANS", "0"}, {"AXIOM_MAX_MODEL_SCANS", "65"}, {"AXIOM_SCAN_MOCK", "yes"},
	} {
		t.Run(bad.name+"-"+bad.value[:min(8, len(bad.value))], func(t *testing.T) {
			e := ValidateEnvironment(func(k string) string {
				if k == bad.name {
					return bad.value
				}
				return good[k]
			})
			if e == nil || !strings.Contains(e.Error(), bad.name) {
				t.Fatalf("unsafe configuration accepted: %v", e)
			}
			if strings.Contains(e.Error(), "fixture:secret") {
				t.Fatal("configuration error exposed a credential")
			}
		})
	}
}

func TestDevelopmentDefaultsRemainAvailable(t *testing.T) {
	if e := ValidateEnvironment(func(string) string { return "" }); e != nil {
		t.Fatal(e)
	}
}

func TestProductionRequiresExplicitAutomaticRestartPolicy(t *testing.T) {
	get := func(k string) string {
		if k == "AXIOM_ENV" {
			return "production"
		}
		return ""
	}
	if MaxRecoveries(get) != 0 {
		t.Fatal("production silently restarts model work")
	}
	if MaxRecoveries(func(string) string { return "" }) != 2 {
		t.Fatal("development recovery policy changed")
	}
	if MaxRecoveries(func(k string) string {
		if k == "AXIOM_MAX_RECOVERIES" {
			return "2"
		}
		return get(k)
	}) != 2 {
		t.Fatal("explicit recovery policy ignored")
	}
}
