package execution

import (
	"axiom/contracts"
	"context"
	"errors"
)

func (c *Client) ClaimSDKIdentity(ctx context.Context, agent string) (*contracts.SDKIdentityClaim, error) {
	var raw *contracts.SDKIdentityClaim
	e := c.callAt(ctx, "/api/internal/sdk-identities/", "claim", struct {
		AgentID            string `json:"agentId"`
		SDKIdentityVersion int    `json:"sdkIdentityVersion"`
	}{agent, c.SDKIdentityVersion}, &raw)
	var missing *endpointUnavailableError
	if errors.As(e, &missing) {
		return nil, nil
	}
	if e != nil || raw == nil {
		return nil, e
	}
	b, e := contracts.EncodeJSON(raw)
	if e != nil {
		return nil, e
	}
	checked, e := contracts.DecodeSDKIdentityClaim(b, nil)
	if e != nil {
		return nil, e
	}
	if checked.Operation.Identity.AgentID != agent {
		return nil, errors.New("SDK identity belongs to another node")
	}
	return &checked, nil
}
func (c *Client) ReportSDKIdentity(ctx context.Context, v contracts.SDKIdentityReport) (string, error) {
	var out struct {
		Status string `json:"status"`
	}
	e := c.callAt(ctx, "/api/internal/sdk-identities/", "report", v, &out)
	return out.Status, e
}
