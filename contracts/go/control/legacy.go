// Package control defines the existing Go control-plane HTTP envelopes.
// Portable input, result and observation payloads use generated contracts.
// Legacy DTOs freeze the old wire representation; database projections belong
// to Server and are never imported here. No HTTP client or filesystem I/O lives here.
package control

import (
	"axiom/contracts"
	"encoding/json"
)

type LegacyTask struct {
	ID                         int64                         `json:"id"`
	ScanNo                     string                        `json:"scanNo"`
	SourcePath                 string                        `json:"sourcePath"`
	Provider                   string                        `json:"provider"`
	Model                      string                        `json:"model"`
	BaseURL                    string                        `json:"baseUrl"`
	ScanMode                   string                        `json:"scanMode"`
	IncludePaths               string                        `json:"includePaths"`
	ScanFocus                  string                        `json:"scanFocus"`
	ValidationMode             string                        `json:"validationMode"`
	BusinessContext            string                        `json:"businessContext"`
	TimeoutMinutes             int                           `json:"timeoutMinutes"`
	DeepMaxDiscoveryRuns       int                           `json:"deepMaxDiscoveryRuns"`
	DeepStopAfterNoNew         int                           `json:"deepStopAfterNoNew"`
	MaxCostUSD                 *float64                      `json:"maxCostUsd"`
	Workers                    *int                          `json:"workers"`
	ObservationContractVersion int                           `json:"observationContractVersion"`
	Progress                   *contracts.ProgressSnapshotV2 `json:"progress,omitempty"`
	ProgressDiagnostic         *contracts.ContractError      `json:"progressDiagnostic,omitempty"`
	ResultContractVersion      int                           `json:"resultContractVersion"`
	InputContractVersion       int                           `json:"inputContractVersion"`
	SourceContentHash          string                        `json:"sourceContentHash,omitempty"`
	Status                     string                        `json:"status"`
	CurrentStage               string                        `json:"currentStage"`
	ProgressJSON               string                        `json:"progressJson"`
	AgentID                    string                        `json:"agentId"`
	Attempt                    int                           `json:"attempt"`
	FindingCount               int                           `json:"findingCount"`
	ErrorCode                  string                        `json:"errorCode"`
	ErrorMessage               string                        `json:"errorMessage"`
	OutputPath                 string                        `json:"outputPath"`
	SourceSnapshotID           int64                         `json:"sourceSnapshotId"`
	ModelConfigID              int64                         `json:"modelConfigId"`
	ProjectID                  int64                         `json:"projectId"`
	ProjectName                string                        `json:"projectName"`
	CreatedAt                  string                        `json:"createdAt"`
	StartedAt                  string                        `json:"startedAt"`
	FinishedAt                 string                        `json:"finishedAt"`
	ClaimedFrom                string                        `json:"claimedFrom,omitempty"`
}

type LegacyFinding struct {
	Normalized  json.RawMessage `json:"normalized,omitempty"`
	ID          int64           `json:"id"`
	ScanID      int64           `json:"scanId"`
	Fingerprint string          `json:"fingerprint"`
	Title       string          `json:"title"`
	Severity    string          `json:"severity"`
	Category    string          `json:"category"`
	CWE         []string        `json:"cwe"`
	FilePath    string          `json:"filePath"`
	StartLine   *int            `json:"startLine"`
	EndLine     *int            `json:"endLine"`
	Description string          `json:"description"`
	Evidence    string          `json:"evidence"`
	Remediation string          `json:"remediation"`
	Confidence  *float64        `json:"confidence"`
	Status      string          `json:"status"`
	Raw         json.RawMessage `json:"raw"`
}

type LegacyLeaseRef struct {
	TaskID  int64  `json:"taskId"`
	Attempt int    `json:"attempt"`
	AgentID string `json:"agentId"`
}
type LegacyLease struct {
	Assignment    *contracts.ExecutionAssignment `json:"assignment,omitempty"`
	Task          *LegacyTask                    `json:"task"`
	CredentialEnv string                         `json:"credentialEnv"`
	Credential    string                         `json:"credential,omitempty"`
	WireAPI       string                         `json:"wireApi"`
	DisplayName   string                         `json:"displayName"`
	Workers       int                            `json:"workers,omitempty"`
	LeaseSeconds  int                            `json:"leaseSeconds"`
}
type LegacyHeartbeat struct {
	LegacyLeaseRef
	Stage        string `json:"stage"`
	ProgressJSON string `json:"progressJson"`
}
type LegacyCompletion struct {
	Coverage     json.RawMessage `json:"coverage,omitempty"`
	ProgressJSON string          `json:"progressJson,omitempty"`
	LegacyLeaseRef
	Status       string          `json:"status"`
	ErrorCode    string          `json:"errorCode"`
	ErrorMessage string          `json:"errorMessage"`
	Findings     []LegacyFinding `json:"findings"`
}
