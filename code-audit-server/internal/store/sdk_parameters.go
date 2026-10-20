package store

import (
	"axiom/contracts"
	"context"
	"encoding/json"
	"errors"
)

// Separate versioned sidecar: historical strict model/job schemas remain intact.
func validateSDKParameters(t *Task) error {
	if t.SDKParameters == nil {
		return nil
	}
	raw, e := contracts.EncodeJSON(t.SDKParameters)
	if e != nil {
		return e
	}
	if _, e = contracts.DecodeSDKScanParameters(raw, nil); e != nil {
		return e
	}
	if t.InputContractVersion < 1 || t.ResultContractVersion != 1 || t.ObservationContractVersion != 1 {
		return errors.New("SDK 参数需要版本化执行合同")
	}
	if v := t.SDKParameters.ComponentInput; v.Present {
		if t.ComponentScan == nil {
			return errors.New("组件知识与指令只能用于冻结计划的组件审计")
		}
		input := contracts.ScanInput{SchemaVersion: 1, Knowledge: v.Value.Knowledge, ScanPrompt: v.Value.ScanPrompt, PostScanPrompt: v.Value.PostScanPrompt}
		if e := ValidateScanInput(&input, "standard", "default"); e != nil {
			return e
		}
	}
	return nil
}

func (s *Store) loadSDKParameters(ctx context.Context, t *Task) error {
	var raw string
	if e := s.db.QueryRowContext(ctx, `SELECT sdk_parameters_json FROM scan_tasks WHERE id=?`, t.ID).Scan(&raw); e != nil {
		return e
	}
	if raw == "" {
		return nil
	}
	value, e := contracts.DecodeSDKScanParameters([]byte(raw), nil)
	if e != nil {
		return e
	}
	t.SDKParameters = &value
	return nil
}

func (s *Store) saveSDKParameters(ctx context.Context, tx *transaction, t *Task, id int64) error {
	if t.SDKOperationOrigin > 0 {
		var raw string
		if e := tx.QueryRowContext(ctx, `SELECT sdk_parameters_json FROM scan_tasks WHERE id=?`, t.SDKOperationOrigin).Scan(&raw); e != nil {
			return e
		}
		var expected *contracts.SDKScanParameters
		if raw != "" {
			v, e := contracts.DecodeSDKScanParameters([]byte(raw), nil)
			if e != nil {
				return e
			}
			expected = &v
		}
		if expected != nil && t.SDKOperation != nil && t.SDKOperation.Kind == contracts.SDKOperationKindValidate {
			expected.ComponentInput = contracts.Optional[contracts.ComponentSDKInput]{}
			if !expected.ReasoningEffort.Present {
				expected = nil
			}
		}
		if !sameJSON(expected, t.SDKParameters) {
			return errors.New("SDK 操作必须保留来源执行冻结的推理配置")
		}
	}
	if e := validateSDKParameters(t); e != nil {
		return e
	}
	if t.SDKParameters == nil {
		return nil
	}
	if v := t.SDKParameters.ComponentInput; v.Present {
		// Recheck immutable versions inside the same transaction that queues work.
		copyTask := *t
		copyTask.ScanInput = &contracts.ScanInput{SchemaVersion: 1, Knowledge: v.Value.Knowledge}
		if e := s.verifyKnowledgeReferences(ctx, tx, *copyTask.ScanInput); e != nil {
			return e
		}
	}
	raw, e := json.Marshal(t.SDKParameters)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE scan_tasks SET sdk_parameters_json=? WHERE id=?`, string(raw), id)
	return e
}
