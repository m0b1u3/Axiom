package store

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
	"errors"
)

type CompatibilityCandidate struct {
	AgentID    string                   `json:"agentId"`
	Online     bool                     `json:"online"`
	Ready      bool                     `json:"ready"`
	Missing    []string                 `json:"missing"`
	Diagnostic *contracts.ContractError `json:"diagnostic,omitempty"`
}
type TaskCompatibility struct {
	Required   control.Requirements     `json:"required"`
	Profile    string                   `json:"profile"`
	Reason     string                   `json:"reason"`
	Candidates []CompatibilityCandidate `json:"candidates"`
}

func evaluateTaskCompatibility(t QueueTask, agents []AgentPresence) *TaskCompatibility {
	v := &TaskCompatibility{Required: control.Requirements{Input: t.inputVersion, Result: t.resultVersion, Observation: t.observationVersion, Budget: t.budget}, Candidates: []CompatibilityCandidate{}}
	p, ok := control.RequiredProfile(v.Required)
	v.Profile = p.ID
	if !ok {
		v.Reason = "task_contract_unsupported"
	}
	inputOK, resultOK, observationOK, online, blocked, matrixBlocked, ready := false, false, false, false, false, false, false
	for _, a := range agents {
		caps := control.Capabilities{Input: a.InputCapabilities, Result: a.ResultCapabilities, Observation: a.ObservationCapabilities}
		missing := control.Missing(v.Required, caps)
		n := CompatibilityCandidate{AgentID: a.AgentID, Online: a.Online, Missing: missing, Ready: a.Online && len(missing) == 0}
		if a.Runtime != nil && a.Runtime.MatrixHash != control.MatrixHash {
			n.Ready = false
			n.Diagnostic = a.Runtime.Diagnostic
			n.Missing = append(n.Missing, "matrix")
		} else if a.Runtime != nil && a.Runtime.State == contracts.AgentRuntimeStateBlocked {
			n.Ready = false
			n.Diagnostic = a.Runtime.Diagnostic
			n.Missing = append(n.Missing, "scanner_preflight")
		}
		if a.Runtime != nil && ok && int(a.Runtime.ControlAPI) < p.ControlAPI {
			n.Ready = false
			n.Missing = append(n.Missing, "control_api")
		}
		v.Candidates = append(v.Candidates, n)
		if !a.Online {
			continue
		}
		online = true
		if a.Runtime != nil && a.Runtime.MatrixHash != control.MatrixHash {
			matrixBlocked = true
			continue
		}
		if a.Runtime != nil && a.Runtime.State == contracts.AgentRuntimeStateBlocked {
			blocked = true
			continue
		}
		if n.Ready {
			ready = true
		}
		if v.Required.Input == 0 || caps.Input.SupportsInput(1, v.Required.Budget) {
			inputOK = true
			if v.Required.Result == 0 || caps.Result.SupportsResult(1) {
				resultOK = true
				if v.Required.Observation == 0 || caps.Observation.SupportsObservation(1) {
					observationOK = true
				}
			}
		}
	}
	if ready {
		v.Reason = ""
		return v
	}
	if !ok {
		return v
	}
	switch {
	case !online:
		v.Reason = "agent_offline"
	case matrixBlocked && !inputOK:
		v.Reason = "matrix_incompatible"
	case blocked && !inputOK:
		v.Reason = "scanner_incompatible"
	case !inputOK:
		v.Reason = "input_incompatible"
	case !resultOK:
		v.Reason = "result_incompatible"
	case !observationOK:
		v.Reason = "observation_incompatible"
	default:
		v.Reason = "control_incompatible"
	}
	return v
}

var ErrAgentNotRegistered = errors.New("execution node must report presence before runtime")

func (s *Store) ReportAgentRuntime(ctx context.Context, id string, runtime contracts.AgentRuntime) error {
	raw, err := contracts.EncodeJSON(runtime)
	if err != nil {
		return err
	}
	if _, err = contracts.DecodeAgentRuntime(raw, nil); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE execution_agents SET runtime_json=?,runtime_seen_at=NOW() WHERE agent_id=?`, string(raw), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrAgentNotRegistered
	}
	return err
}
