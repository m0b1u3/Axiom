package httpserver_test

import (
	"axiom/code-audit-server/internal/store"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The actual Agent receives version-zero tasks through the unchanged v4
// envelope and calls the actual Scanner. Only the SDK instance is a fixture.
func runLegacyInputAcceptance(t *testing.T, s *store.Store, agent, root, url, agentToken, scratch string, snapshotID int64, source, encrypted, key string) {
	t.Helper()
	t.Run("legacy-provider-connection", func(t *testing.T) {
		model, err := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Legacy fixture", Provider: "deepseek", Model: "fixture-legacy-model", BaseURL: "https://legacy-fixture.invalid/v1", WireAPI: "chat", ApiKeyEncrypted: encrypted})
		if err != nil {
			t.Fatal(err)
		}
		id, err := s.InsertPending(t.Context(), store.Task{ScanNo: "fixture-legacy-connection", SourceSnapshotID: snapshotID, SourcePath: source,
			Provider: model.Provider, Model: model.Model, BaseURL: model.BaseURL, ModelConfigID: model.ID, ScanMode: "standard", TimeoutMinutes: 47,
			IncludePaths: `["源码/测试.ts"]`, ScanFocus: `["authz"]`})
		if err != nil {
			t.Fatal(err)
		}
		capture := filepath.Join(scratch, "legacy-sdk.json")
		runInputAgent(t, agent, root, url, agentToken, capture)
		task, err := s.Get(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status != "completed" || task.InputContractVersion != 0 || task.ResultContractVersion != 0 || task.ObservationContractVersion != 0 {
			t.Fatalf("legacy wire task did not complete: %+v", task)
		}
		raw, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Source string `json:"source"`
			Config struct {
				Overrides map[string]any `json:"codexOverrides"`
			} `json:"config"`
			Options          map[string]any `json:"options"`
			CredentialDigest string         `json:"credentialDigest"`
			Leaked           bool           `json:"leakedServerCredentials"`
		}
		if err = json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		scope, _ := json.Marshal(v.Options["target"])
		digest := sha256.Sum256([]byte(key))
		if v.Source != source || v.Config.Overrides["model"] != model.Model || string(scope) != `["源码/测试.ts"]` || v.CredentialDigest != hex.EncodeToString(digest[:]) || v.Leaked {
			t.Fatalf("legacy provider binding or scope changed: %s", raw)
		}
		findings, err := s.ListFindings(t.Context(), id)
		if err != nil || len(findings) != 1 || len(findings[0].Raw) == 0 {
			t.Fatal("legacy evidence lost", err, findings)
		}
	})
}
