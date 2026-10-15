package worker

import (
	"axiom/code-audit-agent/internal/scanner"
	"axiom/code-audit-agent/pkg/execution"
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
	"encoding/json"
	"os"
)

func sendHeartbeat(ctx context.Context, c *execution.Client, t *execution.Lease, ref control.LegacyLeaseRef) (string, error) {
	if t.ObservationContractVersion == 1 {
		h := contracts.ExecutionHeartbeat{SchemaVersion: contracts.Version, Execution: t.Execution, AgentID: contracts.Identifier(ref.AgentID)}
		p, e := scanner.ReadContractProgress(t.OutputPath, h.Execution)
		if e == nil {
			h.Progress = p
		} else if !os.IsNotExist(e) {
			h.Diagnostic = contracts.Some(contracts.ContractError{Code: contracts.ErrorCodeInvalidResult, Origin: contracts.ErrorOriginAgent, Message: "扫描进度未通过大小、Schema 或执行身份校验；保留最近有效快照", Retryable: true})
		}
		return c.HeartbeatContract(ctx, h)
	}
	h := control.LegacyHeartbeat{LegacyLeaseRef: ref}
	if p, e := scanner.ReadProgress(t.OutputPath); e == nil {
		h.Stage = p.Phase
		b, _ := json.Marshal(p)
		h.ProgressJSON = string(b)
	}
	return c.Heartbeat(ctx, h)
}
