package httpserver

import (
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"encoding/base64"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestFrozenParametersAndCredentialRotationAtServerBoundary(t *testing.T) {
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	for _, scenario := range []string{"same-connection", "changed-destination", "changed-protocol", "changed-provider", "deleted-config"} {
		t.Run(scenario, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "fixture.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			oldKey, err := cryptokey.Encrypt("fixture-original-credential")
			if err != nil {
				t.Fatal(err)
			}
			row, err := st.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixture", Provider: "custom", BaseURL: "https://fixture.invalid/v1", WireAPI: "responses", Model: "fixture-frozen-model", ApiKeyEncrypted: oldKey})
			if err != nil {
				t.Fatal(err)
			}
			original, err := resolveModel(createScanRequest{}, row)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := contracts.EncodeJSON(original)
			if err != nil {
				t.Fatal(err)
			}
			task := &store.Task{ID: 9007199254740993, Attempt: 2, ModelConfigID: row.ID, SourceSnapshotID: 7, SourceContentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ModelSnapshotJSON: string(raw), CredentialSnapshotEncrypted: oldKey,
				PolicySnapshotJSON: `{"mode":"standard","timeBudget":{"scanSeconds":1800,"finalizationGraceSeconds":0},"includePaths":[],"focus":[],"validation":{"mode":"default"},"maxCostUsd":2.75}`}
			updated := *row
			updated.Model = "fixture-edited-after-queue"
			updated.ApiKeyEncrypted, err = cryptokey.Encrypt("fixture-rotated-credential")
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "changed-destination":
				updated.BaseURL = "https://other-fixture.invalid/v1"
			case "changed-protocol":
				updated.WireAPI = "chat"
			case "changed-provider":
				updated.Provider = "deepseek"
			}
			if scenario == "deleted-config" {
				if err = st.DeleteModelConfig(t.Context(), row.ID); err != nil {
					t.Fatal(err)
				}
			} else if err = st.UpdateModelConfig(t.Context(), row.ID, updated); err != nil {
				t.Fatal(err)
			}
			s := &Server{Store: st}
			key, err := s.taskCredential(httptest.NewRequest("POST", "/claim", nil), task)
			if err != nil {
				t.Fatal(err)
			}
			want := "fixture-original-credential"
			if scenario == "same-connection" {
				want = "fixture-rotated-credential"
			}
			if key != want {
				t.Fatal("credential rotation crossed frozen connection", scenario)
			}
			a, err := assignmentFromTask(task, "fixture-agent", 60, key)
			if err != nil {
				t.Fatal(err)
			}
			if a.Model.Model != "fixture-frozen-model" || a.Model.BaseURL.Value != original.BaseURL.Value || a.Model.WireAPI != original.WireAPI || !a.Policy.MaxCostUSD.Present || a.Policy.MaxCostUSD.Value != 2.75 || a.Credential.Value.Value != want {
				t.Fatal("queued input changed after model edit")
			}
		})
	}
}
