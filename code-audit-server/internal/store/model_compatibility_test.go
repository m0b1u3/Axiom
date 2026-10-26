package store

import (
	"path/filepath"
	"testing"
)

func TestLegacyConnectivityCompatibilityResetIsOnceAndPreservesModel(t *testing.T) {
	file := filepath.Join(t.TempDir(), "legacy.db")
	s, e := Open(file)
	if e != nil {
		t.Fatal(e)
	}
	row, e := s.CreateModelConfig(t.Context(), ModelConfigRow{Name: "Fixture legacy", Provider: "custom", Model: "fixture", BaseURL: "https://fixture.invalid", WireAPI: "responses", ApiKeyEncrypted: "encrypted-fixture-bytes", CompatibilityStatus: "supported"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetModelConfigEnabled(t.Context(), row.ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`DELETE FROM schema_migrations WHERE version='027_model_compatibility.sql'`); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(file)
	if e != nil {
		t.Fatal(e)
	}
	after, e := s.GetModelConfig(t.Context(), row.ID)
	if e != nil {
		t.Fatal(e)
	}
	if after.CompatibilityStatus != "experimental" || after.ApiKeyEncrypted != row.ApiKeyEncrypted || after.Model != row.Model || !after.Enabled {
		t.Fatalf("migration lost model data: %#v", after)
	}
	if e = s.SetModelConfigCompatibility(t.Context(), row.ID, "supported"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(file)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	after, e = s.GetModelConfig(t.Context(), row.ID)
	if e != nil || after.CompatibilityStatus != "supported" {
		t.Fatal("migration repeated", e)
	}
}
