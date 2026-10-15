package scanner

import (
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func inputFixture(t *testing.T) Request {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	output := filepath.Join(root, "output")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "a,b.ts"), []byte("// input fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, _, _, err := sourceidentity.TreeHash(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	job := &contracts.ScannerJob{SchemaVersion: 1, Execution: contracts.ExecutionRef{TaskID: "42", Attempt: 2}, Source: contracts.LocalSource{Path: contracts.AbsolutePath(source), SnapshotID: "7", ContentHash: contracts.Digest(hash)}, Output: contracts.LocalOutput{Path: contracts.AbsolutePath(output)},
		Model:  contracts.ResolvedModel{ProviderID: "openai", ConnectionType: contracts.ResolvedModelConnectionTypeOpenai, Model: "fixture-model", WireAPI: contracts.ResolvedModelWireAPIResponses, CredentialEnv: "OPENAI_API_KEY", ConfigurationRevision: "fixture"},
		Policy: contracts.ScanPolicy{Mode: contracts.ScanModeStandard, TimeBudget: contracts.TimeBudget{ScanSeconds: 60}, IncludePaths: []contracts.RelativePath{"a,b.ts"}, Focus: []contracts.Focus{}, Validation: contracts.ValidationPolicy{Mode: contracts.ValidationModeDefault}}}
	return Request{Job: job, TaskID: 42, Attempt: 2, OutputPath: output, NodePath: "must-not-launch-a-node-process"}
}

func TestJobRejectsBadIdentitySourceAndOutputBeforeStartingProcess(t *testing.T) {
	for _, name := range []string{"identity", "attempt", "source-changed", "nested-output"} {
		t.Run(name, func(t *testing.T) {
			req := inputFixture(t)
			switch name {
			case "identity":
				req.TaskID = 43
			case "attempt":
				req.Attempt = 3
			case "source-changed":
				os.WriteFile(filepath.Join(string(req.Job.Source.Path), "a,b.ts"), []byte("changed"), 0o600)
			case "nested-output":
				req.OutputPath = filepath.Join(string(req.Job.Source.Path), "output")
				req.Job.Output.Path = contracts.AbsolutePath(req.OutputPath)
			}
			_, err := prepareJob(context.Background(), req)
			if err == nil || strings.Contains(err.Error(), "must-not-launch") {
				t.Fatalf("invalid job reached the process boundary: %v", err)
			}
			if _, err := os.Stat(filepath.Join(req.OutputPath, LayoutJob)); !os.IsNotExist(err) {
				t.Fatal("invalid input created a job")
			}
		})
	}
}

func TestJobEnvironmentUsesOnlySelectedCredential(t *testing.T) {
	values := jobEnvironment([]string{"OPENAI_API_KEY=fixture-selected", "CODEX_API_KEY=fixture-other", "CHATGPT_ACCESS_TOKEN=fixture-login", "DEEPSEEK_API_KEY=fixture-other", "NEW_VENDOR_API_KEY=fixture-other", "PATH=fixture-path"}, "OPENAI_API_KEY")
	if strings.Join(values, "\n") != "OPENAI_API_KEY=fixture-selected\nPATH=fixture-path" {
		t.Fatal("ambient model credentials survived")
	}
}

func TestReadersKeepLargeStringIdentityCompatible(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{LayoutNormalized, LayoutProgress} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o700)
		os.WriteFile(filepath.Join(dir, path), []byte(`{"taskId":"9007199254740993","status":"completed","phase":"completed","findings":[]}`), 0o600)
	}
	result, err := ReadNormalized(dir)
	if err != nil || result.TaskID != 9007199254740993 {
		t.Fatalf("result identity %v %v", result, err)
	}
	progress, err := ReadProgress(dir)
	if err != nil || progress.TaskID != 9007199254740993 {
		t.Fatalf("progress identity %v %v", progress, err)
	}
	raw, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(raw), "9007199254740993") {
		t.Fatal("identity rounded")
	}
}
