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
	SDKConcurrencyVersion int `json:"sdkConcurrencyVersion,omitempty"`
	SDKParametersVersion  int `json:"sdkParametersVersion,omitempty"`
	SDKIdentityVersion    int `json:"sdkIdentityVersion,omitempty"`
	ContractClaimRequest
	ObservationCapabilities contracts.ObservationCapabilities `json:"observationCapabilities"`
}
type ObservationPresence struct {
	ObservationClaimRequest
	// Zero slots is meaningful only with the explicitly negotiated unbounded mode.
	DispatchMode string `json:"dispatchMode,omitempty"`
	Slots        int    `json:"slots"`
}

const DispatchUnbounded = "unbounded"
const DispatchBounded = "bounded"

func ValidDispatchPresence(mode string, slots int) bool {
	return (mode == DispatchUnbounded && slots == 0) || ((mode == "" || mode == DispatchBounded) && slots >= 1 && int64(slots) <= contracts.SDKMaxSafeInteger)
}

type ObservationLease struct {
	SDKParametersVersion int                                   `json:"sdkParametersVersion,omitempty"`
	SDKParameters        *contracts.SDKParametersAssignment    `json:"sdkParameters,omitempty"`
	SDKIdentity          *contracts.SDKTaskIdentity            `json:"sdkIdentity,omitempty"`
	ComponentScan        *contracts.ComponentScanRequest       `json:"componentScan,omitempty"`
	ComponentPlanning    *contracts.ComponentPlanningOperation `json:"componentPlanning,omitempty"`
	GitDiff              *contracts.GitDiffAssignment          `json:"gitDiff,omitempty"`
	SDKOperation         *contracts.SDKOperation               `json:"sdkOperation,omitempty"`
	InputContractVersion int                                   `json:"inputContractVersion,omitempty"`
	ScanInput            *contracts.ScanInputAssignment        `json:"scanInput,omitempty"`
	ContractLease
	ObservationContractVersion int `json:"observationContractVersion"`
}
