package execution

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrUnauthorized = errors.New("agent authentication rejected")

type Client struct {
	SDKConcurrencyVersion   int
	DispatchMode            string
	SDKParametersVersion    int
	SDKIdentityVersion      int
	URL, Token              string
	HTTP                    *http.Client
	InputCapabilities       *contracts.InputCapabilities
	ObservationCapabilities *contracts.ObservationCapabilities
	ResultCapabilities      *contracts.ResultCapabilities
	ControlAPI              int
	RuntimeEndpoint         bool
}

type ProtocolError contracts.ContractError

func (e *ProtocolError) Error() string { return string(e.Code) + ": " + e.Message }
func (e *ProtocolError) Is(target error) bool {
	t, ok := target.(*ProtocolError)
	return ok && t != nil && e.Code == t.Code
}

// Only a missing HTTP endpoint permits presence discovery to try an older route.
// A typed version error from an existing endpoint must remain an error.
type endpointUnavailableError struct{ status int }

func (e *endpointUnavailableError) Error() string {
	return fmt.Sprintf("execution endpoint HTTP %d", e.status)
}
func (e *endpointUnavailableError) Unwrap() error {
	return &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Origin: contracts.ErrorOriginServer, Message: "Server does not support this execution endpoint"}
}

var ErrLeaseLost = &ProtocolError{Code: contracts.ErrorCodeLeaseExpired, Origin: contracts.ErrorOriginServer, Message: "execution lease is no longer owned"}
var ErrCompletionConflict = &ProtocolError{Code: contracts.ErrorCodeCompletionConflict, Origin: contracts.ErrorOriginServer, Message: "completion content conflicts with the archived execution"}

func NewClient(url, token string) *Client {
	return &Client{URL: strings.TrimRight(url, "/"), Token: token, HTTP: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) call(ctx context.Context, action string, input, output any) error {
	return c.callAt(ctx, "/api/internal/executions/", action, input, output)
}
func (c *Client) callAt(ctx context.Context, prefix, action string, input, output any) error {
	b, e := contracts.EncodeJSON(input)
	if e != nil {
		return e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", c.URL+prefix+action, bytes.NewReader(b))
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+c.Token)
	r.Header.Set("Content-Type", "application/json")
	resp, e := c.HTTP.Do(r)
	if e != nil {
		return fmt.Errorf("control plane request failed: %w", e)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return ErrUnauthorized
	}
	if resp.StatusCode == 404 || resp.StatusCode == 405 {
		return &endpointUnavailableError{status: resp.StatusCode}
	}
	if resp.StatusCode != 200 {
		var fault struct {
			Code int `json:"code"`
			Data struct {
				Error *contracts.ContractError `json:"error"`
			} `json:"data"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&fault) == nil {
			if fault.Data.Error != nil {
				v := fault.Data.Error
				return (*ProtocolError)(v)
			}
			if resp.StatusCode == 409 && fault.Code == 40901 {
				return ErrLeaseLost
			}
			if resp.StatusCode == 409 && fault.Code == 40902 {
				return ErrCompletionConflict
			}
		}
		return fmt.Errorf("control plane HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&envelope); e != nil {
		return e
	}
	if envelope.Code != 0 {
		return errors.New("control plane rejected request")
	}
	if output != nil {
		return json.Unmarshal(envelope.Data, output)
	}
	return nil
}
func (c *Client) Claim(ctx context.Context, id string) (*Lease, error) {
	if c.selectedAPI() == 4 {
		return c.claimObservations(ctx, id)
	}
	if c.selectedAPI() == 3 {
		var v control.ContractLease
		if e := c.callAt(ctx, "/api/internal/v3/executions/", "claim", control.ContractClaimRequest{AgentID: id, InputCapabilities: c.capabilities().Input, ResultCapabilities: c.capabilities().Result}, &v); e != nil {
			return nil, e
		}
		lease, e := leaseFromInput(v.InputLease, id)
		if e != nil {
			return nil, e
		}
		lease.ResultContractVersion = v.ResultContractVersion
		return c.validateLease(lease)
	}
	if c.selectedAPI() == 2 {
		var v control.InputLease
		if err := c.callAt(ctx, "/api/internal/v2/executions/", "claim", control.InputClaimRequest{AgentID: id, Capabilities: c.capabilities().Input}, &v); err != nil {
			return nil, err
		}
		lease, e := leaseFromInput(v, id)
		if e != nil {
			return nil, e
		}
		return c.validateLease(lease)
	}
	var v control.LegacyLease
	e := c.call(ctx, "claim", control.ClaimRequest{AgentID: id}, &v)
	if e != nil {
		return nil, e
	}
	lease, e := leaseFromLegacy(&v, id)
	if e != nil {
		return nil, e
	}
	return c.validateLease(lease)
}
func (c *Client) Heartbeat(ctx context.Context, h control.LegacyHeartbeat) (string, error) {
	var v control.HeartbeatResult
	e := c.call(ctx, "heartbeat", h, &v)
	return v.Status, e
}
func (c *Client) Complete(ctx context.Context, v control.LegacyCompletion) error {
	return c.call(ctx, "complete", v, nil)
}
func (c *Client) CompleteContract(ctx context.Context, v contracts.ExecutionCompletion) error {
	return c.callAt(ctx, "/api/internal/v3/executions/", "complete", v, nil)
}

func (c *Client) Presence(ctx context.Context, id string, slots int) error {
	if c.selectedAPI() == 4 {
		return c.callAt(ctx, "/api/internal/v4/executions/", "presence", control.ObservationPresence{ObservationClaimRequest: control.ObservationClaimRequest{ContractClaimRequest: control.ContractClaimRequest{AgentID: id, InputCapabilities: c.capabilities().Input, ResultCapabilities: c.capabilities().Result}, ObservationCapabilities: c.capabilities().Observation}, DispatchMode: c.DispatchMode, Slots: slots}, nil)
	}
	if c.selectedAPI() == 3 {
		return c.callAt(ctx, "/api/internal/v3/executions/", "presence", control.ContractPresence{ContractClaimRequest: control.ContractClaimRequest{AgentID: id, InputCapabilities: c.capabilities().Input, ResultCapabilities: c.capabilities().Result}, Slots: slots}, nil)
	}
	if c.selectedAPI() == 2 {
		return c.callAt(ctx, "/api/internal/v2/executions/", "presence", control.InputPresence{AgentID: id, Slots: slots, Capabilities: c.capabilities().Input}, nil)
	}
	return c.call(ctx, "presence", control.Presence{AgentID: id, Slots: slots}, nil)
}
