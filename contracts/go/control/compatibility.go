package control

import "axiom/contracts"

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
	Input       int  `json:"input"`
	Result      int  `json:"result"`
	Observation int  `json:"observation"`
	Budget      bool `json:"budget"`
}
type Capabilities struct {
	Input       contracts.InputCapabilities
	Result      contracts.ResultCapabilities
	Observation contracts.ObservationCapabilities
}

func ControlAPIs() []int  { return append([]int(nil), controlAPIs...) }
func Profiles() []Profile { return append([]Profile(nil), profiles...) }
func RequiredProfile(v Requirements) (Profile, bool) {
	for _, p := range profiles {
		if p.Input == v.Input && p.Result == v.Result && p.Observation == v.Observation && (!v.Budget || p.Input == 1) {
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
	if v.Input == 1 && !c.Input.SupportsInput(1, false) {
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
	ControlAPIs []int  `json:"controlAPIs"`
	MatrixHash  string `json:"matrixHash"`
}
