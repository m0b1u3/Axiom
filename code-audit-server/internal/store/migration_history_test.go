package store

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

// Historical SQL is immutable. New migrations may be added without rewriting
// what an existing installation has already applied.
func TestMigrationHistorySurvivesOwnershipMove(t *testing.T) {
	expected := map[string]string{
		"schema.sql":                                "04d253faafbb051ffc50bdeef6518305af981a40143605c6d4bf65c252027fe7",
		"migrations/001_business.sql":               "508975f218415db8c8778ec3b8bb131a199fb3f9db0d5ae135fc6bd026bd196e",
		"migrations/002_execution.sql":              "e42e1d41e1c7c897ffc4e2777514292218ce23ac5c3947bd9f98cbd7bad54495",
		"migrations/003_legacy_attempt_counter.sql": "6dd0c1e46b364529877ca13a60cadca5e8f5b77261273cb6583e7da9f9ae6a3b",
		"migrations/004_task_management.sql":        "e5bd1286899f0c89c3ee996d264644e67493f4d4d211aa79a406e9fc97079ed1",
		"migrations/005_scan_observability.sql":     "b4cc6f4f7d54e30e3ce073cb827d78cd895f9305b1a1bc9446c69b072375dd2e",
		"migrations/006_exploitability.sql":         "cabd1de68d65c391684d5de242965d4746fb7e1a6d58bdad2f5c9cd381982ebe",
		"migrations/007_input_contract.sql":         "15a677bec1ee3a5058311423658c50fea1e4e03a6a1ab9145c954ca3fe669f0e",
		"migrations/008_result_contract.sql":        "3f3979b3c2d1281f3c3e7d103d32a18fccd59d2e0f7c211c9e004d03b5bd3113",
		"migrations/009_observation_contract.sql":   "0f217096c461c4dc682973f18ec9b80806983589e1d97a7e8351bdf24ec31da2",
	}
	for name, want := range expected {
		var raw []byte
		var err error
		if name == "schema.sql" {
			raw = []byte(schemaSQL)
		} else {
			raw, err = migrations.ReadFile(name)
		}
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != want {
			t.Fatalf("historical SQL changed: %s (%v)", name, err)
		}
	}
}
