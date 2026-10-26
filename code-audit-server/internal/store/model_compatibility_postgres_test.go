package store

import (
	"os"
	"testing"
)

func TestPostgresModelCompatibilityMigrationPreservesConfiguration(t *testing.T) {
	s := pgStore(t)
	model, e := s.CreateModelConfig(t.Context(), ModelConfigRow{Name: "Fixture preserved", Provider: "custom", Model: "fixture", BaseURL: "https://fixture.invalid", WireAPI: "responses", ApiKeyEncrypted: "encrypted-fixture", CompatibilityStatus: "supported"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.ExecContext(t.Context(), `DELETE FROM schema_migrations WHERE version='027_model_compatibility.sql'`); e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(os.Getenv("AXIOM_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	row, e := reopened.GetModelConfig(t.Context(), model.ID)
	if e != nil {
		t.Fatal(e)
	}
	if row.CompatibilityStatus != "experimental" || row.ApiKeyEncrypted != model.ApiKeyEncrypted || row.Model != model.Model || row.CreatedAt != model.CreatedAt {
		t.Fatalf("legacy migration changed configuration: %#v", row)
	}
	if e = reopened.SetModelConfigCompatibility(t.Context(), row.ID, "supported"); e != nil {
		t.Fatal(e)
	}
	again, e := Open(os.Getenv("AXIOM_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	row, e = again.GetModelConfig(t.Context(), model.ID)
	if e != nil || row.CompatibilityStatus != "supported" {
		t.Fatal("migration repeated", e)
	}
}
