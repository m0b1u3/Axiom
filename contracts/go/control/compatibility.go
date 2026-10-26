package control

import (
	"axiom/contracts"
	"slices"
)

type Profile struct {
	ID          string `json:"id"`
	Input       int    `json:"input"`
	Result      int    `json:"result"`
	Observation int    `json:"observation"`
	ControlAPI  int    `json:"controlAPI"`
	Description int    `json:"description"`
	Job         int    `json:"job"`
	Progress    int    `json:"progress"`
	Event       int    `json:"event"`
}
type Requirements struct {
	SDKConcurrency bool `json:"sdkConcurrency,omitempty"`
	SDKParameters  bool `json:"sdkParameters,omitempty"`
	SDKOperation   bool `json:"sdkOperation,omitempty"`
	ScanInput      bool `json:"scanInput,omitempty"`
	Input          int  `json:"input"`
	Result         int  `json:"result"`
	Observation    int  `json:"observation"`
	Budget         bool `json:"budget"`
}
type Capabilities struct {
	SDKConcurrency bool
	SDKParameters  bool
	Input          contracts.InputCapabilities
	Result         contracts.ResultCapabilities
	Observation    contracts.ObservationCapabilities
}

func ControlAPIs() []int  { return append([]int(nil), controlAPIs...) }
func Profiles() []Profile { return append([]Profile(nil), profiles...) }
func RequiredProfile(v Requirements) (Profile, bool) {
	if v.SDKOperation && v.Input != contracts.SDKOperationInputVersion && v.Input != contracts.GitDiffInputVersion {
		return Profile{}, false
	}
	if v.ScanInput && ((v.Input != contracts.ScanInputVersion && v.Input != contracts.SDKOperationInputVersion && v.Input != contracts.GitDiffInputVersion && v.Input != contracts.ComponentPlanningInputVersion && v.Input != contracts.ComponentScanInputVersion) || v.Result != 1 || v.Observation != 1) {
		return Profile{}, false
	}
	for _, p := range profiles {
		if p.Input == v.Input && p.Result == v.Result && p.Observation == v.Observation && (!v.Budget || p.Input > 0) {
			return p, true
		}
	}
	return Profile{}, false
}

// Missing uses one node's complete capability set. Capabilities from different
// nodes must never be combined into a synthetic compatible executor.
func Missing(v Requirements, c Capabilities) []string {
	if _, ok := RequiredProfile(v); !ok {
		return []string{"task_contract_unsupported"}
	}
	missing := []string{}
	if v.SDKConcurrency && !c.SDKConcurrency {
		missing = append(missing, "sdk_concurrency")
	}
	if v.SDKParameters && !c.SDKParameters {
		missing = append(missing, "sdk_parameters")
	}
	if v.Input == contracts.ComponentScanInputVersion && !slices.Contains(c.Observation.JobVersions, contracts.ComponentScanJobVersion) {
		missing = append(missing, "component_scan")
	}
	if v.Input == contracts.ComponentPlanningInputVersion && !slices.Contains(c.Observation.JobVersions, contracts.ComponentPlanningJobVersion) {
		missing = append(missing, "component_planning")
	}
	if v.Input == contracts.GitDiffInputVersion && !slices.Contains(c.Observation.JobVersions, contracts.GitDiffJobVersion) {
		missing = append(missing, "git_diff")
	}
	if (v.SDKOperation || v.Input == contracts.SDKOperationInputVersion) && !slices.Contains(c.Observation.JobVersions, contracts.SDKOperationJobVersion) {
		missing = append(missing, "sdk_operation")
	}
	if (v.ScanInput || v.Input == contracts.ScanInputVersion || v.Input == contracts.SDKOperationInputVersion || v.Input == contracts.GitDiffInputVersion || v.Input == contracts.ComponentPlanningInputVersion || v.Input == contracts.ComponentScanInputVersion) && !slices.Contains(c.Observation.JobVersions, int64(contracts.ScanInputJobVersion)) {
		missing = append(missing, "scan_input")
	}
	if v.Input > 0 && !c.Input.SupportsInput(1, false) {
		missing = append(missing, "input")
	}
	if v.Budget && !c.Input.MaxCostUSD {
		missing = append(missing, "budget")
	}
	if v.Result == 1 && !c.Result.SupportsResult(1) {
		missing = append(missing, "result")
	}
	if v.Observation == 1 && !c.Observation.SupportsObservation(1) {
		missing = append(missing, "observation")
	}
	return missing
}
func (c Capabilities) ReadyFor(v Requirements) bool { return len(Missing(v, c)) == 0 }

type RuntimeReport struct {
	AgentID string                 `json:"agentId"`
	Runtime contracts.AgentRuntime `json:"runtime"`
}
type ServerDescription struct {
	SDKDeepResumeVersion  int    `json:"sdkDeepResumeVersion,omitempty"`
	SDKConcurrencyVersion int    `json:"sdkConcurrencyVersion,omitempty"`
	TaskDispatch          string `json:"taskDispatch,omitempty"`
	SDKParametersVersion  int    `json:"sdkParametersVersion,omitempty"`
	SDKIdentityVersion    int    `json:"sdkIdentityVersion,omitempty"`
	ControlAPIs           []int  `json:"controlAPIs"`
	MatrixHash            string `json:"matrixHash"`
}
