package httpserver

import (
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"encoding/json"
	"strconv"
)

// The public projection comes exclusively from the accepted heartbeat archive.
// Server does not open SDK state or merge a second progress document.
func enrichLiveProgress(task *store.Task) {
	if task.ObservationContractVersion != 1 {
		return
	}
	task.Progress = nil
	task.ProgressDiagnostic = nil
	expected := contracts.ExecutionRef{TaskID: contracts.TaskID(strconv.FormatInt(task.ID, 10)), Attempt: int64(task.Attempt)}
	if task.ProgressJSON != "" {
		p, e := contracts.DecodeProgressSnapshotV2([]byte(task.ProgressJSON), &expected)
		if e == nil {
			task.Progress = &p
		} else {
			task.ProgressDiagnostic = &contracts.ContractError{Code: contracts.ErrorCodeInvalidResult, Origin: contracts.ErrorOriginServer, Message: "已归档进度未通过合同校验", Retryable: false}
		}
	}
	if task.ProgressDiagnosticJSON != "" {
		var error contracts.ContractError
		if json.Unmarshal([]byte(task.ProgressDiagnosticJSON), &error) == nil {
			task.ProgressDiagnostic = &error
		}
	}
}
