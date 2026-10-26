package execution

import (
	"axiom/contracts"
	"context"
)

func (c *Client) ClaimDeepResumeCheck(ctx context.Context, agent string) (*contracts.DeepResumeProbeClaim, error) {
	var v *contracts.DeepResumeProbeClaim
	e := c.callAt(ctx, "/api/internal/deep-resume/", "claim", map[string]any{"agentId": agent, "version": c.SDKDeepResumeVersion}, &v)
	if e != nil || v == nil {
		return nil, e
	}
	raw, e := contracts.EncodeJSON(v)
	if e != nil {
		return nil, e
	}
	checked, e := contracts.DecodeDeepResumeProbeClaim(raw, nil)
	if e != nil || checked.AgentID != agent {
		return nil, ErrLeaseLost
	}
	return &checked, nil
}
func (c *Client) ReportDeepResumeCheck(ctx context.Context, v contracts.DeepResumeProbeReport) error {
	var out any
	return c.callAt(ctx, "/api/internal/deep-resume/", "report", v, &out)
}
