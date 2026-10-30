package httpserver

import (
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeIdentityCapabilityMatchesResolvedConnection(t *testing.T) {
	for _, tc := range []struct {
		provider, base, wire string
		supported            bool
	}{
		{"openai", "", "responses", true},
		{"openai", "https://api.openai.com/v1", "responses", true},
		{"openai", "https://api.openai.com/v1/responses/", "responses", true},
		{"openai", "https://fixture.invalid/v1", "responses", false},
		{"deepseek", "https://api.openai.com/v1", "responses", false},
		{"openai", "https://api.openai.com/v1.evil.invalid", "responses", false},
	} {
		row := store.ModelConfigRow{Provider: tc.provider, BaseURL: tc.base, WireAPI: tc.wire, Model: "Fixtures-Mock"}
		m, err := resolveModel(createScanRequest{}, &row)
		if err != nil {
			t.Fatal(err)
		}
		if modelResponse(row).SupportsSDKIdentity != tc.supported || (m.ConnectionType == "openai") != tc.supported {
			t.Fatalf("inconsistent capability for %+v: %+v", tc, m)
		}
	}
}

func TestConfiguredBudgetRequiresPricesBeforeCreatingScan(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "fixture.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snap, err := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "Fixtures-Mock.zip", ArchiveSha256: strings.Repeat("0", 64), ContentHash: strings.Repeat("1", 64), SnapshotPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Store: s}
	limit := 1.0
	zero := contracts.TokenUnitPrice("0")
	one := contracts.TokenUnitPrice("1")
	for _, tc := range []struct {
		name  string
		price *contracts.ModelPricing
		valid bool
	}{
		{"missing", nil, false},
		{"missing-output", &contracts.ModelPricing{SchemaVersion: 1, Currency: "USD", Unit: "per_million_tokens", CacheMode: "input_only", InputPerMillion: &one}, false},
		{"foreign-currency", &contracts.ModelPricing{SchemaVersion: 1, Currency: "CNY", Unit: "per_million_tokens", CacheMode: "input_only", InputPerMillion: &one, OutputPerMillion: &one}, false},
		{"zero-price", &contracts.ModelPricing{SchemaVersion: 1, Currency: "USD", Unit: "per_million_tokens", CacheMode: "input_only", InputPerMillion: &zero, OutputPerMillion: &zero}, true},
	} {
		model, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: tc.name, Provider: "deepseek", Model: "deepseek-flash", BaseURL: "http://127.0.0.1:1", WireAPI: "responses", ApiKeyEncrypted: "Fixtures-Mock-only", Pricing: tc.price})
		if e != nil {
			t.Fatal(e)
		}
		for _, total := range []bool{false, true} {
			input := createScanRequest{SourceSnapshotID: snap.ID, ModelConfigID: model.ID, ScanMode: "standard", TimeoutMinutes: 1}
			if total {
				input.MaxTotalCostUSD = &limit
			} else {
				input.MaxCostUSD = &limit
			}
			_, e = server.prepareScan(httptest.NewRequest("POST", "/", nil), input)
			if (e == nil) != tc.valid {
				t.Fatalf("%s total=%v: %v", tc.name, total, e)
			}
		}
		_, e = server.prepareScan(httptest.NewRequest("POST", "/", nil), createScanRequest{SourceSnapshotID: snap.ID, ModelConfigID: model.ID, ScanMode: "standard", TimeoutMinutes: 1})
		if e != nil {
			t.Fatalf("unbudgeted scan must remain available with unknown price: %v", e)
		}
	}
}
