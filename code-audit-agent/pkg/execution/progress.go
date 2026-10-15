package execution

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
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
	e := c.callAt(ctx, "/api/internal/v4/executions/", "claim", control.ObservationClaimRequest{ContractClaimRequest: control.ContractClaimRequest{AgentID: id, InputCapabilities: c.capabilities().Input, ResultCapabilities: c.capabilities().Result}, ObservationCapabilities: c.capabilities().Observation}, &v)
	if e != nil {
		return nil, e
	}
	lease, e := leaseFromInput(v.InputLease, id)
	if e != nil {
		return nil, e
	}
	lease.ResultContractVersion = v.ResultContractVersion
	lease.ObservationContractVersion = v.ObservationContractVersion
	return c.validateLease(lease)
}
