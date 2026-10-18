package contracts

import (
	"encoding/json"
	"reflect"
)

func ComponentAllocations(r ComponentScanRequest) []int64 {
	n := int64(len(r.SelectedIndices))
	out := make([]int64, n)
	if n == 0 {
		return out
	}
	for i := range out {
		out[i] = min(r.ComponentLimitMicros, r.GrantMicros/n)
		if int64(i) < r.GrantMicros%n {
			out[i] = min(r.ComponentLimitMicros, r.GrantMicros/n+1)
		}
	}
	return out
}
func ComponentReserved(rows []ComponentScanReceipt) int64 {
	var n int64
	for _, r := range rows {
		if r.Status == "pending" || r.Status == "running" {
			used := int64(0)
			if r.ObservedMicros != nil {
				used = *r.ObservedMicros
			}
			n += max(0, r.AllocatedMicros-used)
		}
	}
	return n
}
func validateComponentScanFields(kind Kind, object map[string]any, execution map[string]any) error {
	raw, _ := json.Marshal(object)
	ref := ExecutionRef{}
	if execution != nil {
		ref = ExecutionRef{TaskID: TaskID(execution["taskId"].(string)), Attempt: int64(execution["attempt"].(float64))}
	}
	switch kind {
	case KindComponentScanRequest:
		var r ComponentScanRequest
		_ = json.Unmarshal(raw, &r)
		v, _ := json.Marshal(r.PlanVersion)
		if e := ValidateJSON(KindComponentPlanVersion, v, nil); e != nil {
			return e
		}
		a := ComponentAllocations(r)
		for i, index := range r.SelectedIndices {
			if index >= int64(len(r.PlanVersion.Document.Components)) || (i > 0 && index <= r.SelectedIndices[i-1]) || a[i] < 1 {
				return invalid("invalid_contract", kind)
			}
		}
	case KindComponentScanSnapshot:
		var s ComponentScanSnapshot
		_ = json.Unmarshal(raw, &s)
		sum, observed := int64(0), int64(0)
		ids := map[int64]bool{}
		for _, r := range s.Components {
			if ids[r.Index] || r.AllocatedMicros > 9007199254740991-sum {
				return invalid("invalid_contract", kind)
			}
			ids[r.Index] = true
			sum += r.AllocatedMicros
			if r.ObservedMicros != nil {
				if *r.ObservedMicros > 9007199254740991-observed {
					return invalid("invalid_contract", kind)
				}
				observed += *r.ObservedMicros
			}
			if (r.FinalMicros != nil && (r.ObservedMicros == nil || *r.FinalMicros > *r.ObservedMicros)) || ((r.ScanID == nil) != (r.ResultArtifact == nil)) || ((r.Status == "completed" || r.Status == "partial") && r.ResultArtifact == nil) || ((r.Status == "pending" || r.Status == "running") && r.FinalMicros != nil) {
				return invalid("invalid_contract", kind)
			}
		}
		if sum > s.GrantMicros || observed != s.ObservedMicros || ComponentReserved(s.Components) != s.ReservedMicros || ((s.Matching.Status == "not_started") != (s.Matching.CostState == "known")) {
			return invalid("invalid_contract", kind)
		}
		if s.Phase == "finished" {
			for _, r := range s.Components {
				if r.Status == "pending" || r.Status == "running" {
					return invalid("invalid_contract", kind)
				}
			}
		}
	case KindComponentScanResult:
		var r ComponentScanResult
		_ = json.Unmarshal(raw, &r)
		for k, v := range map[Kind]any{KindComponentScanRequest: r.Request, KindComponentScanSnapshot: r.Snapshot} {
			b, _ := json.Marshal(v)
			if e := ValidateJSON(k, b, &ref); e != nil {
				return e
			}
		}
		s, p := r.Snapshot, r.Request.PlanVersion
		a := ComponentAllocations(r.Request)
		if s.PlanID != p.PlanID || s.Version != p.Version || s.VersionSHA256 != p.SHA256 || !reflect.DeepEqual(s.Source, p.Source) || s.GrantMicros != r.Request.GrantMicros || len(s.Components) != len(a) || s.Phase != "finished" {
			return invalid("identity_mismatch", kind)
		}
		for i, c := range s.Components {
			if c.Index != r.Request.SelectedIndices[i] || c.AllocatedMicros != a[i] {
				return invalid("identity_mismatch", kind)
			}
		}
	case KindScannerJobV8:
		var j ScannerJobV8
		_ = json.Unmarshal(raw, &j)
		b, _ := json.Marshal(j.ComponentScan)
		if e := ValidateJSON(KindComponentScanRequest, b, &ref); e != nil {
			return e
		}
		if j.Source.SnapshotID != j.ComponentScan.PlanVersion.Source.SnapshotID || j.Source.ContentHash != j.ComponentScan.PlanVersion.Source.ContentHash || !j.Policy.MaxCostUSD.Present {
			return invalid("identity_mismatch", kind)
		}
		delta := j.Policy.MaxCostUSD.Value*1e6 - float64(j.ComponentScan.GrantMicros)
		if delta > 0.01 || delta < -0.01 {
			return invalid("identity_mismatch", kind)
		}
	case KindProgressSnapshotV2, KindScanResult:
		if ext, ok := object["extensions"].(map[string]any); ok {
			if value, ok := ext["componentScan"]; ok {
				b, _ := json.Marshal(value)
				k := KindComponentScanSnapshot
				if kind == KindScanResult {
					k = KindComponentScanResult
				}
				return ValidateJSON(k, b, &ref)
			}
		}
	}
	return nil
}
