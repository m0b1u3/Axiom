package execution

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"encoding/json"
	"errors"
	"strconv"
)

// Lease is local execution state, not a database row. Modern tasks retain the
// validated Assignment; the legacy wire DTO is used only by the old adapter.
type Lease struct {
	SDKParameters              *contracts.SDKParametersAssignment
	SDKIdentity                *contracts.SDKTaskIdentity
	ComponentScan              *contracts.ComponentScanRequest
	ComponentPlanning          *contracts.ComponentPlanningOperation
	GitDiff                    *contracts.GitDiffAssignment
	SDKOperation               *contracts.SDKOperation
	ScanInput                  *contracts.ScanInputAssignment
	Execution                  contracts.ExecutionRef
	Assignment                 *contracts.ExecutionAssignment
	Legacy                     *control.LegacyLease
	SourcePath, OutputPath     string
	LeaseSeconds               int
	ResultContractVersion      int
	ObservationContractVersion int
}

func (l *Lease) HasTask() bool { return l != nil && l.Execution.TaskID != "" }

// The current Server issues signed 64-bit database IDs. Keep that deployment
// restriction at the process adapter, while the portable contract uses strings.
func (l *Lease) NumericTaskID() int64 {
	id, _ := strconv.ParseInt(string(l.Execution.TaskID), 10, 64)
	return id
}
func leaseFromLegacy(v *control.LegacyLease, agent string) (*Lease, error) {
	if v == nil || v.Task == nil {
		return &Lease{}, nil
	}
	t := v.Task
	if t.ID < 1 || t.Attempt < 1 || t.AgentID != agent || v.LeaseSeconds < 1 {
		return nil, errors.New("invalid legacy execution identity or lease")
	}
	return &Lease{Execution: contracts.ExecutionRef{TaskID: contracts.TaskID(strconv.FormatInt(t.ID, 10)), Attempt: int64(t.Attempt)},
		Legacy: v, SourcePath: t.SourcePath, OutputPath: t.OutputPath, LeaseSeconds: v.LeaseSeconds,
		ResultContractVersion: t.ResultContractVersion, ObservationContractVersion: t.ObservationContractVersion}, nil
}
func leaseFromInput(v control.InputLease, agent string) (*Lease, error) {
	if v.Assignment == nil {
		return leaseFromLegacy(v.Legacy, agent)
	}
	if v.Legacy != nil {
		return nil, errors.New("ambiguous input lease")
	}
	raw, err := json.Marshal(v.Assignment)
	if err != nil {
		return nil, err
	}
	a, err := contracts.DecodeExecutionAssignment(raw, nil)
	if err != nil {
		return nil, err
	}
	if string(a.Lease.AgentID) != agent || int(a.Lease.LeaseSeconds) != v.LeaseSeconds {
		return nil, errors.New("input lease belongs to another agent or has inconsistent duration")
	}
	if _, err := strconv.ParseInt(string(a.Execution.TaskID), 10, 64); err != nil {
		return nil, errors.New("unsupported task identity")
	}
	return &Lease{Execution: a.Execution, Assignment: &a, SourcePath: v.SourcePath, OutputPath: v.OutputPath, LeaseSeconds: v.LeaseSeconds}, nil
}
