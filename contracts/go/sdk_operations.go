package contracts

import "encoding/json"

func validateSDKOperationFields(kind Kind, object, execution map[string]any) error {
	if kind == KindScannerJobV5 || (kind == KindScannerJobV6 && object["operation"] != nil) {
		raw, _ := json.Marshal(object["operation"])
		if e := ValidateJSON(KindSDKOperation, raw, &ExecutionRef{TaskID: TaskID(execution["taskId"].(string)), Attempt: int64(execution["attempt"].(float64))}); e != nil {
			return e
		}
	}
	if kind == KindGitDiffTarget && object["baseCommit"] == object["headCommit"] {
		return invalid("invalid_contract", kind)
	}
	if kind == KindScannerJobV6 {
		raw, _ := json.Marshal(object["gitTarget"])
		if e := ValidateJSON(KindGitDiffTarget, raw, nil); e != nil {
			return e
		}
	}
	if kind == KindGitDiffAssignment || kind == KindGitDiffPreview || kind == KindGitDiffContent {
		raw, _ := json.Marshal(object["target"])
		if e := ValidateJSON(KindGitDiffTarget, raw, nil); e != nil {
			return e
		}
	}
	if kind == KindGitDiffPreview {
		total, page := object["total"].(float64), object["page"].(float64)
		start := total
		if page-1 <= total/50 {
			start = (page - 1) * 50
		}
		files := object["files"].([]any)
		if len(files) != int(min(50, total-start)) {
			return invalid("invalid_contract", kind)
		}
		for i, value := range files {
			if value.(map[string]any)["index"].(float64) != start+float64(i) {
				return invalid("invalid_contract", kind)
			}
		}
	}
	if kind == KindSDKSessionSummary {
		persisted, omitted, received := object["persistedCount"].(float64), object["omittedCount"].(float64), object["receivedCount"].(float64)
		if persisted+omitted != received || (object["state"] == "complete" && (omitted != 0 || object["reason"] != nil)) || (object["state"] == "incomplete" && (object["reason"] == nil || object["reason"] == "")) {
			return invalid("invalid_contract", kind)
		}
	}
	if kind == KindSDKOperationResult {
		if object["error"] == nil && ((object["kind"] == "preflight" && object["preflight"] == nil) || (object["kind"] == "validate" && object["validation"] == nil)) {
			return invalid("invalid_contract", kind)
		}
		if v, ok := object["preflight"].(map[string]any); ok {
			auth, ok := v["authentication"].(map[string]any)
			if !ok || auth["verified"] != false {
				return invalid("invalid_contract", kind)
			}
		}
	}
	return nil
}
