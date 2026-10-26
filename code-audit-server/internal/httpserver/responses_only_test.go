package httpserver

import (
	"axiom/code-audit-server/internal/providers"
	"axiom/code-audit-server/internal/store"
	"strings"
	"testing"
)

func TestResponsesOnlyModelResolution(t *testing.T) {
	for _, p := range providers.All() {
		if p.WireAPI != "responses" {
			t.Fatalf("provider %s still defaults to %s", p.Key, p.WireAPI)
		}
	}
	row := &store.ModelConfigRow{ID: 1, Provider: "custom", Model: "fixture-only", BaseURL: "https://fixture.invalid/v1", WireAPI: "chat"}
	if _, err := resolveModel(createScanRequest{}, row); err == nil || !strings.Contains(err.Error(), "Chat 协议适配已移除") {
		t.Fatalf("legacy Chat configuration accepted: %v", err)
	}
	row.WireAPI = "responses"
	value, err := resolveModel(createScanRequest{}, row)
	if err != nil || value.WireAPI != "responses" {
		t.Fatal(value, err)
	}
}
