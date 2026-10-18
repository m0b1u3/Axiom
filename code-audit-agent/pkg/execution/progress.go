package execution

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
	"errors"
)

func (c *Client) HeartbeatContract(ctx context.Context, h contracts.ExecutionHeartbeat) (string, error) {
	raw, e := contracts.EncodeJSON(h)
	if e != nil {
		return "", e
	}
	if _, e = contracts.DecodeExecutionHeartbeat(raw, &h.Execution); e != nil {
		return "", e
	}
	var v struct {
		Status string `json:"status"`
	}
	e = c.callAt(ctx, "/api/internal/v4/executions/", "heartbeat", h, &v)
	return v.Status, e
}
func (c *Client) claimObservations(ctx context.Context, id string) (*Lease, error) {
	var v control.ObservationLease
	e := c.callAt(ctx, "/api/internal/v4/executions/", "claim", control.ObservationClaimRequest{SDKIdentityVersion: c.SDKIdentityVersion, ContractClaimRequest: control.ContractClaimRequest{AgentID: id, InputCapabilities: c.capabilities().Input, ResultCapabilities: c.capabilities().Result}, ObservationCapabilities: c.capabilities().Observation}, &v)
	if e != nil {
		return nil, e
	}
	lease, e := leaseFromInput(v.InputLease, id)
	if e != nil {
		return nil, e
	}
	if v.SDKIdentity != nil {
		raw, _ := contracts.EncodeJSON(v.SDKIdentity)
		checked, e := contracts.DecodeSDKTaskIdentity(raw, &lease.Execution)
		if e != nil || c.SDKIdentityVersion != 1 || checked.Identity.AgentID != id {
			return nil, errors.New("SDK identity capability or node binding mismatch")
		}
		lease.SDKIdentity = &checked
	}
	if v.ScanInput != nil {
		raw, e := contracts.EncodeJSON(v.ScanInput)
		if e != nil {
			return nil, e
		}
		checked, e := contracts.DecodeScanInputAssignment(raw, &lease.Execution)
		if e != nil {
			return nil, e
		}
		lease.ScanInput = &checked
	}
	if (v.InputContractVersion == contracts.ScanInputVersion || v.InputContractVersion == contracts.SDKOperationInputVersion || v.InputContractVersion == contracts.GitDiffInputVersion || v.InputContractVersion == contracts.ComponentPlanningInputVersion || v.InputContractVersion == contracts.ComponentScanInputVersion) != (v.ScanInput != nil) {
		return nil, &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Message: "missing or unexpected scan input v2 sidecar"}
	}
	if v.InputContractVersion != 0 && v.InputContractVersion != 1 && v.InputContractVersion != contracts.ScanInputVersion && v.InputContractVersion != contracts.SDKOperationInputVersion && v.InputContractVersion != contracts.GitDiffInputVersion && v.InputContractVersion != contracts.ComponentPlanningInputVersion && v.InputContractVersion != contracts.ComponentScanInputVersion {
		return nil, &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Message: "unknown scan input contract"}
	}
	if v.InputContractVersion != contracts.GitDiffInputVersion && (v.InputContractVersion == contracts.SDKOperationInputVersion) != (v.SDKOperation != nil) {
		return nil, &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Message: "missing or unexpected SDK operation contract"}
	}
	if v.SDKOperation != nil {
		raw, e := contracts.EncodeJSON(v.SDKOperation)
		if e != nil {
			return nil, e
		}
		op, e := contracts.DecodeSDKOperation(raw, &lease.Execution)
		if e != nil {
			return nil, e
		}
		lease.SDKOperation = &op
	}
	if (v.InputContractVersion == contracts.GitDiffInputVersion) != (v.GitDiff != nil) {
		return nil, &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Message: "missing or unexpected Git target"}
	}
	if v.GitDiff != nil {
		raw, e := contracts.EncodeJSON(v.GitDiff)
		if e != nil {
			return nil, e
		}
		binding, e := contracts.DecodeGitDiffAssignment(raw, &lease.Execution)
		if e != nil {
			return nil, e
		}
		if lease.Assignment == nil || binding.SourceSnapshotID != lease.Assignment.Source.SnapshotID || binding.SourceContentHash != lease.Assignment.Source.ContentHash {
			return nil, &ProtocolError{Code: contracts.ErrorCodeInvalidInput, Message: "Git source identity mismatch"}
		}
		lease.GitDiff = &binding
	}
	if (v.InputContractVersion == contracts.ComponentPlanningInputVersion) != (v.ComponentPlanning != nil) {
		return nil, &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Message: "组件规划合同缺失或版本不匹配"}
	}
	if v.ComponentPlanning != nil {
		raw, e := contracts.EncodeJSON(v.ComponentPlanning)
		if e != nil {
			return nil, e
		}
		op, e := contracts.DecodeComponentPlanningOperation(raw, &lease.Execution)
		if e != nil {
			return nil, e
		}
		if lease.Assignment == nil || op.Source.SnapshotID != lease.Assignment.Source.SnapshotID || op.Source.ContentHash != lease.Assignment.Source.ContentHash {
			return nil, &ProtocolError{Code: contracts.ErrorCodeInvalidInput, Message: "组件源码身份不匹配"}
		}
		lease.ComponentPlanning = &op
	}
	if (v.InputContractVersion == contracts.ComponentScanInputVersion) != (v.ComponentScan != nil) {
		return nil, &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Message: "组件审计合同缺失或版本不匹配"}
	}
	if v.ComponentScan != nil {
		raw, e := contracts.EncodeJSON(v.ComponentScan)
		if e != nil {
			return nil, e
		}
		op, e := contracts.DecodeComponentScanRequest(raw, &lease.Execution)
		if e != nil {
			return nil, e
		}
		if lease.Assignment == nil || op.PlanVersion.Source.SnapshotID != lease.Assignment.Source.SnapshotID || op.PlanVersion.Source.ContentHash != lease.Assignment.Source.ContentHash {
			return nil, &ProtocolError{Code: contracts.ErrorCodeInvalidInput, Message: "组件审计源码身份不匹配"}
		}
		lease.ComponentScan = &op
	}
	lease.ResultContractVersion = v.ResultContractVersion
	lease.ObservationContractVersion = v.ObservationContractVersion
	return c.validateLease(lease)
}
