package worker

import (
	"axiom/code-audit-agent/internal/scanner"
	"axiom/contracts"
	"axiom/contracts/control"
	"errors"
)

func buildCompletion(ref contracts.ExecutionRef, agent string, result *scanner.Result, err error, canceled, timeout bool, configurationCode string) *contracts.ExecutionCompletion {
	v := &contracts.ExecutionCompletion{SchemaVersion: 1, Execution: ref, AgentID: contracts.Identifier(agent), ExecutionStatus: contracts.TerminalStatusFailed, Process: contracts.ProcessExit{ResultCommitted: false}}
	failure := func(code contracts.ErrorCode, message string) {
		v.Error = contracts.Some(contracts.ContractError{Code: code, Origin: contracts.ErrorOriginAgent, Message: message, Retryable: false})
	}
	if result != nil {
		v.Process = result.Process
		if result.Full != nil {
			v.Result = contracts.Some(*result.Full)
		}
	}
	switch {
	case canceled:
		v.ExecutionStatus = contracts.TerminalStatusCanceled
		failure(contracts.ErrorCodeCanceled, "canceled by user; any committed SDK result is retained separately")
	case timeout:
		v.ExecutionStatus = contracts.TerminalStatusTimeout
		failure(contracts.ErrorCodeTimeout, "scan exceeded configured time limit; any committed SDK result is retained separately")
	case err != nil:
		code := contracts.ErrorCodeUnknown
		if errors.Is(err, scanner.ErrInvalidResult) {
			code = contracts.ErrorCodeInvalidResult
		}
		if configurationCode == "configuration_error" {
			code = contracts.ErrorCodeConfigurationError
		}
		failure(code, err.Error())
	case result != nil && result.Full != nil:
		v.ExecutionStatus = result.Full.Status
		if result.Full.Error.Present {
			v.Error = result.Full.Error
		}
		if scanner.ProcessMismatch(v.Process, v.ExecutionStatus) {
			// Policy: a verified committed result wins over a late process failure.
			// Preserve the process outcome and show the discrepancy in Web.
			if !v.Error.Present {
				failure(contracts.ErrorCodeUnknown, "verified committed result retained despite a different process exit")
			}
		}
	default:
		failure(contracts.ErrorCodeInvalidResult, "scanner produced no committed result")
	}
	return v
}

func completionFindingCount(v *contracts.ExecutionCompletion, legacy control.LegacyCompletion) int {
	if v != nil && v.Result.Present {
		return len(v.Result.Value.Findings)
	}
	return len(legacy.Findings)
}
