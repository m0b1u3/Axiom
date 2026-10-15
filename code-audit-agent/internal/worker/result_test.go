package worker

import (
	"axiom/code-audit-agent/internal/scanner"
	"axiom/contracts"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestCompletionPolicy(t *testing.T) {
	data, e := os.ReadFile("../../../contracts/fixtures/v1/result-rich.json")
	if e != nil {
		t.Fatal(e)
	}
	full, e := contracts.DecodeScanResult(data, nil)
	if e != nil {
		t.Fatal(e)
	}
	ref := contracts.ExecutionRef{TaskID: "9007199254740993", Attempt: 2}
	zero, late := int64(0), int64(10)
	for _, test := range []struct {
		name              string
		result            *scanner.Result
		err               error
		canceled, timeout bool
		status            contracts.TerminalStatus
		code              contracts.ErrorCode
	}{
		{"completed", &scanner.Result{Full: &full, Process: contracts.ProcessExit{Code: &zero, ResultCommitted: true}}, nil, false, false, "completed", ""},
		{"late-exit", &scanner.Result{Full: &full, Process: contracts.ProcessExit{Code: &late, ResultCommitted: true}}, nil, false, false, "completed", "unknown"},
		{"cancel-retains", &scanner.Result{Full: &full, Process: contracts.ProcessExit{Code: &zero, ResultCommitted: true}}, nil, true, false, "canceled", "canceled"},
		{"timeout-retains", &scanner.Result{Full: &full, Process: contracts.ProcessExit{Code: &zero, ResultCommitted: true}}, nil, false, true, "timeout", "timeout"},
		{"invalid-result", nil, scanner.ErrInvalidResult, false, false, "failed", "invalid_result"},
		{"missing-result", nil, nil, false, false, "failed", "invalid_result"},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := buildCompletion(ref, "fixture-agent", test.result, test.err, test.canceled, test.timeout, "")
			if v.ExecutionStatus != test.status || (test.code != "" && (!v.Error.Present || v.Error.Value.Code != test.code)) {
				t.Fatalf("wrong terminal policy: %+v", v)
			}
			if test.result != nil && (!v.Result.Present || len(v.Result.Value.Findings) != 1 || v.Result.Value.Status != "completed") {
				t.Fatal("original committed result was rewritten or discarded")
			}
			raw, e := json.Marshal(v)
			if e != nil {
				t.Fatal(e)
			}
			if e = contracts.ValidateJSON(contracts.KindExecutionCompletion, raw, &full.Execution); e != nil {
				t.Fatal(e)
			}
		})
	}
	v := buildCompletion(ref, "fixture-agent", nil, errors.New("fixture setup failure"), false, false, "configuration_error")
	if v.Error.Value.Code != "configuration_error" {
		t.Fatal("setup error was guessed from text")
	}
}
