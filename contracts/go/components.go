package contracts

import (
	"encoding/json"
	"reflect"
)

func validateComponentFields(kind Kind, object, execution map[string]any) error {
	if kind == KindComponentPlanView {
		plan := object["plan"].(map[string]any)
		if object["operation"] == nil {
			if object["operationTaskId"] != nil || object["operationStatus"] != "" || object["operationError"] != nil || object["result"] != nil || object["resultSha256"] != nil {
				return invalid("invalid_contract", kind)
			}
		} else {
			op := object["operation"].(map[string]any)
			ref := op["execution"].(map[string]any)
			if ref["taskId"] != object["operationTaskId"] || op["planId"] != plan["id"] || !reflect.DeepEqual(op["source"], plan["source"]) || op["revision"].(float64) > plan["revision"].(float64) {
				return invalid("identity_mismatch", kind)
			}
			if object["operationStatus"] == "" {
				return invalid("invalid_contract", kind)
			}
			if object["result"] != nil {
				raw, _ := json.Marshal(object["result"])
				expected := ExecutionRef{TaskID: TaskID(ref["taskId"].(string)), Attempt: int64(ref["attempt"].(float64))}
				if e := ValidateJSON(KindComponentPlanningResult, raw, &expected); e != nil {
					return e
				}
				if !reflect.DeepEqual(op, object["result"].(map[string]any)["operation"]) || object["resultSha256"] == nil {
					return invalid("identity_mismatch", kind)
				}
			} else if object["resultSha256"] != nil {
				return invalid("invalid_contract", kind)
			}
		}
	}
	if kind == KindScannerJobV7 || kind == KindComponentPlanningResult {
		key := "operation"
		if kind == KindScannerJobV7 {
			key = "componentPlanning"
		}
		raw, _ := json.Marshal(object[key])
		ref := ExecutionRef{TaskID: TaskID(execution["taskId"].(string)), Attempt: int64(execution["attempt"].(float64))}
		op, e := DecodeComponentPlanningOperation(raw, &ref)
		if e != nil {
			return e
		}
		if kind == KindScannerJobV7 {
			source := object["source"].(map[string]any)
			policy := object["policy"].(map[string]any)
			if string(op.Source.SnapshotID) != source["snapshotId"] || string(op.Source.ContentHash) != source["contentHash"] || policy["mode"] != "standard" || len(policy["includePaths"].([]any)) != 0 || policy["maxCostUsd"] != nil {
				return invalid("invalid_contract", kind)
			}
		}
	}
	if kind == KindComponentPlanCoverage {
		count, covered, overlaps := object["fileCount"].(float64), object["coveredFileCount"].(float64), object["overlapCount"].(float64)
		valid := count > 0 && covered == count && overlaps == 0 && len(object["emptyComponents"].([]any)) == 0 && len(object["duplicateNames"].([]any)) == 0
		if covered+float64(len(object["uncoveredPaths"].([]any))) != count || len(object["overlaps"].([]any)) != int(min(overlaps, 200)) || object["valid"] != valid {
			return invalid("invalid_contract", kind)
		}
	}
	if kind == KindComponentPlanningResult && object["coverage"] != nil {
		raw, _ := json.Marshal(object["coverage"])
		if _, e := DecodeComponentPlanCoverage(raw, nil); e != nil {
			return e
		}
	}
	if kind == KindComponentPlanVersion {
		raw, _ := json.Marshal(object["coverage"])
		v, e := DecodeComponentPlanCoverage(raw, nil)
		if e != nil {
			return e
		}
		if !v.Valid {
			return invalid("invalid_contract", kind)
		}
	}
	return nil
}
