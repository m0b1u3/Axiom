package store

import (
	"axiom/contracts"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// Only the Server's persistence boundary makes a searchable projection. Agent
// sends the entire generated DTO without copying SDK finding fields.
func (s *Store) CompleteContractExecution(ctx context.Context, full contracts.ExecutionCompletion) error {
	return s.completeContractExecution(ctx, full, nil)
}

func (s *Store) completeContractExecution(ctx context.Context, full contracts.ExecutionCompletion, recovery *resultRecovery) error {
	raw, err := contracts.EncodeJSON(full)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResult, err)
	}
	full, err = contracts.DecodeExecutionCompletion(raw, &full.Execution)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResult, err)
	}
	if full.Process.ResultCommitted != full.Result.Present {
		return ErrInvalidResult
	}
	id, err := strconv.ParseInt(string(full.Execution.TaskID), 10, 64)
	if err != nil {
		return ErrInvalidResult
	}
	if err := s.validateIdentityResult(ctx, id, full); err != nil {
		return err
	}
	if err := s.validateComponentScanCompletion(ctx, id, full); err != nil {
		return err
	}
	if err := s.validateComponentCompletion(ctx, id, full); err != nil {
		return err
	}
	if err := s.validateOperationCompletion(ctx, id, full); err != nil {
		return err
	}
	if err := s.validateGitCompletion(ctx, id, full); err != nil {
		return err
	}
	c := Completion{LeaseRef: LeaseRef{TaskID: id, Attempt: int(full.Execution.Attempt), AgentID: string(full.AgentID)}, Status: string(full.ExecutionStatus), contract: &full, Findings: []Finding{}, recovery: recovery}
	if full.Error.Present {
		c.ErrorCode = string(full.Error.Value.Code)
		c.ErrorMessage = full.Error.Value.Message
	}
	if full.Result.Present {
		result := full.Result.Value
		details := map[string]json.RawMessage{}
		for k, v := range result.Coverage.Details {
			details[k] = v
		}
		details["completeness"], _ = json.Marshal(result.Coverage.Completeness)
		c.Coverage, _ = json.Marshal(details)
		for _, f := range result.Findings {
			c.Findings = append(c.Findings, projectFinding(id, f))
		}
		if !full.Error.Present && result.Error.Present {
			c.ErrorCode = string(result.Error.Value.Code)
			c.ErrorMessage = result.Error.Value.Message
		}
	}
	return s.CompleteExecution(ctx, c)
}

func projectFinding(id int64, f contracts.Finding) Finding {
	var confidence *float64
	if f.Confidence.Present && f.Confidence.Value != nil && f.Confidence.Value.Value.Present {
		confidence = f.Confidence.Value.Value.Value
	}
	integer := func(v contracts.Optional[int64]) *int {
		if !v.Present {
			return nil
		}
		n := int(v.Value)
		return &n
	}
	normalized, _ := contracts.EncodeJSON(f)
	return Finding{ScanID: id, Fingerprint: f.Fingerprint, Title: f.Title, Severity: string(f.Severity), Description: f.Description, Category: f.Category.Value, CWE: f.Cwe.Value,
		FilePath: string(f.FilePath.Value), StartLine: integer(f.StartLine), EndLine: integer(f.EndLine), Evidence: f.Evidence.Value, Remediation: f.Remediation.Value, Confidence: confidence,
		Raw: f.Raw.Value, Normalized: normalized}
}

func completionJSON(c Completion) ([]byte, error) {
	if c.contract == nil {
		return json.Marshal(c)
	}
	var value any = c
	if c.contract != nil {
		value = c.contract
	}
	raw, err := contracts.EncodeJSON(value)
	if err != nil {
		return nil, err
	}
	// Canonicalize nested extension objects too: key order and number spelling
	// cannot turn a semantically identical retry into a conflict.
	var decoded any
	if err = json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	return contracts.EncodeJSON(decoded)
}

func sameJSON(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
