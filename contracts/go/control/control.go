package control

import "axiom/contracts"

type ClaimRequest struct {
	AgentID string `json:"agentId"`
}
type HeartbeatResult struct {
	Status string `json:"status"`
}
type Presence struct {
	AgentID string `json:"agentId"`
	Slots   int    `json:"slots"`
}
type InputClaimRequest struct {
	AgentID      string                      `json:"agentId"`
	Capabilities contracts.InputCapabilities `json:"capabilities"`
}

type InputPresence struct {
	AgentID      string                      `json:"agentId"`
	Slots        int                         `json:"slots"`
	Capabilities contracts.InputCapabilities `json:"capabilities"`
}

// Local paths are a materialization hint for this shared-volume deployment.
// They are deliberately outside the portable Assignment and are checked by Agent.
type InputLease struct {
	Assignment   *contracts.ExecutionAssignment `json:"assignment,omitempty"`
	SourcePath   string                         `json:"sourcePath,omitempty"`
	OutputPath   string                         `json:"outputPath,omitempty"`
	LeaseSeconds int                            `json:"leaseSeconds"`
	Legacy       *LegacyLease                   `json:"legacy,omitempty"`
}

type ContractClaimRequest struct {
	AgentID            string                       `json:"agentId"`
	InputCapabilities  contracts.InputCapabilities  `json:"inputCapabilities"`
	ResultCapabilities contracts.ResultCapabilities `json:"resultCapabilities"`
}

type ContractPresence struct {
	ContractClaimRequest
	Slots int `json:"slots"`
}

type ContractLease struct {
	InputLease
	ResultContractVersion int `json:"resultContractVersion"`
}
type ObservationClaimRequest struct {
	ContractClaimRequest
	ObservationCapabilities contracts.ObservationCapabilities `json:"observationCapabilities"`
}
type ObservationPresence struct {
	ObservationClaimRequest
	Slots int `json:"slots"`
}
type ObservationLease struct {
	ContractLease
	ObservationContractVersion int `json:"observationContractVersion"`
}
