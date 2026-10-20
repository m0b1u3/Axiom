package execution

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
)

var AgentBuild = "development"

func (c *Client) capabilities() control.Capabilities {
	v := control.Capabilities{SDKConcurrency: c.SDKConcurrencyVersion == 1, SDKParameters: c.SDKParametersVersion == 1, Input: contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{}, JobVersions: []contracts.SchemaVersion{}}, Result: contracts.NoResultCapabilities(), Observation: contracts.NoObservationCapabilities()}
	if c.InputCapabilities != nil {
		v.Input = *c.InputCapabilities
	}
	if c.ResultCapabilities != nil {
		v.Result = *c.ResultCapabilities
	}
	if c.ObservationCapabilities != nil {
		v.Observation = *c.ObservationCapabilities
	}
	return v
}
func (c *Client) selectedAPI() int {
	if c.ControlAPI != 0 {
		return c.ControlAPI
	}
	if c.ObservationCapabilities != nil {
		return 4
	}
	if c.ResultCapabilities != nil {
		return 3
	}
	if c.InputCapabilities != nil {
		return 2
	}
	return 1
}

// Discover the Server before claiming. Only presence is safe to retry across
// endpoint versions: a failed/ambiguous claim may already own a paid execution.
func (c *Client) Negotiate(ctx context.Context, id string, slots int) error {
	req, e := http.NewRequestWithContext(ctx, "GET", c.URL+"/api/internal/executions/describe", nil)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	r, e := c.HTTP.Do(req)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode == 401 || r.StatusCode == 403 {
		return ErrUnauthorized
	}
	if r.StatusCode == 200 {
		var v struct {
			Code int                       `json:"code"`
			Data control.ServerDescription `json:"data"`
		}
		if e = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&v); e != nil {
			return e
		}
		digest, hashErr := hex.DecodeString(v.Data.MatrixHash)
		if v.Code != 0 || hashErr != nil || len(digest) != 32 || hex.EncodeToString(digest) != v.Data.MatrixHash {
			return &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Origin: contracts.ErrorOriginAgent, Message: "invalid Server compatibility descriptor", Retryable: false}
		}
		max := 0
		for _, n := range v.Data.ControlAPIs {
			if !slices.Contains(control.ControlAPIs(), n) {
				return &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Message: "unknown Server control API"}
			}
			if n > max {
				max = n
			}
		}
		if max == 0 {
			return errors.New("Server declares no supported control API")
		}
		if c.DispatchMode == control.DispatchUnbounded && (v.Data.TaskDispatch != control.DispatchUnbounded || max != 4) {
			return &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Origin: contracts.ErrorOriginAgent, Message: "Server 尚不支持无配额调度，请同步更新 Server 与 Agent；模型尚未启动。", Retryable: false}
		}
		if v.Data.SDKConcurrencyVersion != 1 {
			c.SDKConcurrencyVersion = 0
		}
		if v.Data.SDKParametersVersion != 1 {
			c.SDKParametersVersion = 0
		}
		if v.Data.SDKIdentityVersion != 1 {
			c.SDKIdentityVersion = 0
		}
		c.ControlAPI = max
		c.RuntimeEndpoint = true
		if v.Data.MatrixHash != control.MatrixHash {
			diagnostic := contracts.ContractError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Origin: contracts.ErrorOriginAgent, Message: "Server 与 Agent 的执行兼容矩阵不一致。请更新配套组件；模型尚未启动。", Retryable: false}
			// Publish only authenticated presence/diagnostics. Never claim work under
			// an incompatible matrix, even if the diagnostic endpoint fails.
			if e = c.Presence(ctx, id, slots); e == nil {
				_ = c.ReportRuntime(ctx, id, contracts.AgentRuntime{SchemaVersion: 1, AgentBuild: AgentBuild, ControlAPI: int64(max), State: contracts.AgentRuntimeStateBlocked, MatrixHash: control.MatrixHash, Diagnostic: &diagnostic})
			}
			return (*ProtocolError)(&diagnostic)
		}
		return c.Presence(ctx, id, slots)
	}
	if r.StatusCode != 404 && r.StatusCode != 405 {
		return errors.New("Server capability discovery failed")
	}
	if c.DispatchMode == control.DispatchUnbounded {
		return &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Origin: contracts.ErrorOriginAgent, Message: "Server 未声明无配额调度能力，请同步更新配套组件。", Retryable: false}
	}
	c.SDKIdentityVersion = 0
	c.SDKParametersVersion = 0
	c.SDKConcurrencyVersion = 0
	// An old Server has no descriptor. Probe known presence payloads, without
	// scan credentials or task claims. Never fall back on auth/network failures.
	apis := control.ControlAPIs()
	slices.Reverse(apis)
	for _, n := range apis {
		c.ControlAPI = n
		if e = c.Presence(ctx, id, slots); e == nil {
			return nil
		}
		var p *endpointUnavailableError
		if !errors.As(e, &p) {
			return e
		}
	}
	return &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Message: "no supported Server presence endpoint", Retryable: false}
}

func (c *Client) validateLease(l *Lease) (*Lease, error) {
	if !l.HasTask() {
		return l, nil
	}
	v := control.Requirements{SDKParameters: l.SDKParameters != nil, SDKOperation: l.SDKOperation != nil, ScanInput: l.ScanInput != nil, Result: l.ResultContractVersion, Observation: l.ObservationContractVersion}
	if l.Assignment != nil {
		v.Input = int(l.Assignment.SchemaVersion)
		if l.ScanInput != nil {
			v.Input = contracts.ScanInputVersion
		}
		if l.ComponentScan != nil {
			v.Input = contracts.ComponentScanInputVersion
		}
		if l.ComponentPlanning != nil {
			v.Input = contracts.ComponentPlanningInputVersion
		}
		if l.SDKOperation != nil {
			v.Input = contracts.SDKOperationInputVersion
		}
		if l.GitDiff != nil {
			v.Input = contracts.GitDiffInputVersion
		}
		var input *contracts.ScanInput
		if l.ScanInput != nil {
			input = &l.ScanInput.Input
		}
		v.SDKConcurrency = contracts.NeedsSDKConcurrency(l.Assignment.Policy, input, l.ComponentScan)
		v.Budget = l.Assignment.Policy.MaxCostUSD.Present
	}
	if l.Legacy != nil {
		v.Input = l.Legacy.Task.InputContractVersion
		v.Budget = l.Legacy.Task.MaxCostUSD != nil
	}
	p, ok := control.RequiredProfile(v)
	if !ok || p.ControlAPI > c.selectedAPI() || !c.capabilities().ReadyFor(v) || (v.Input > 0 && l.Assignment == nil) {
		return nil, &ProtocolError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Origin: contracts.ErrorOriginAgent, Message: "claimed task contracts are not supported by this Server/Agent/Scanner combination", Retryable: false}
	}
	return l, nil
}
func (c *Client) ReportRuntime(ctx context.Context, id string, v contracts.AgentRuntime) error {
	if !c.RuntimeEndpoint {
		return nil
	}
	raw, e := contracts.EncodeJSON(v)
	if e != nil {
		return e
	}
	if _, e = contracts.DecodeAgentRuntime(raw, nil); e != nil {
		return e
	}
	return c.callAt(ctx, "/api/internal/executions/", "runtime", control.RuntimeReport{AgentID: id, Runtime: v}, nil)
}
