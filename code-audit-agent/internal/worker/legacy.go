package worker

import (
	"axiom/code-audit-agent/internal/scanner"
	"axiom/contracts/control"
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// Only version-zero tasks take this path. Modern configuration comes directly
// from the generated Assignment, never from serialized database columns.
func (w *Worker) runLegacy(ctx context.Context, lease *control.LegacyLease) (*scanner.Result, error) {
	if lease == nil || lease.Task == nil {
		return nil, fmt.Errorf("legacy task is missing")
	}
	t := lease.Task
	var include, focus []string
	if t.IncludePaths != "" {
		if err := json.Unmarshal([]byte(t.IncludePaths), &include); err != nil {
			return nil, fmt.Errorf("invalid legacy scan paths: %w", err)
		}
	}
	if t.ScanFocus != "" {
		if err := json.Unmarshal([]byte(t.ScanFocus), &focus); err != nil {
			return nil, fmt.Errorf("invalid legacy scan focus: %w", err)
		}
	}
	overrides := map[string]string{}
	if lease.Credential != "" {
		overrides[lease.CredentialEnv] = lease.Credential
	}
	timeout := t.TimeoutMinutes
	if timeout <= 0 {
		timeout = 60
	}
	return scanner.Run(ctx, scanner.Request{TaskID: t.ID, SourcePath: t.SourcePath, OutputPath: t.OutputPath,
		Provider: t.Provider, Model: t.Model, BaseURL: t.BaseURL, ScanMode: t.ScanMode, IncludePaths: include, ScanFocus: focus,
		ValidationMode: t.ValidationMode, BusinessContext: t.BusinessContext, TimeoutMinutes: timeout,
		DeepMaxDiscoveryRuns: t.DeepMaxDiscoveryRuns, DeepStopAfterNoNew: t.DeepStopAfterNoNew, Workers: lease.Workers,
		Mock: os.Getenv("AXIOM_SCAN_MOCK") == "1", ScannerDir: w.Cfg.ScannerDir, NodePath: w.Cfg.NodePath,
		EnvOverrides: overrides, CredentialEnvName: lease.CredentialEnv, WireAPI: lease.WireAPI, DisplayName: lease.DisplayName})
}
