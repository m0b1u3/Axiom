package store

import (
	"axiom/contracts"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
)

// validateTaskInput prevents any producer from persisting a budget that a legacy
// worker can silently ignore. The public API resolves these snapshots once.
func validateTaskInput(t Task) error {
	if (t.ComponentScan != nil) != (t.InputContractVersion == contracts.ComponentScanInputVersion) {
		return errors.New("组件审计合同标记不匹配")
	}
	if t.ComponentScan != nil {
		r := t.ComponentScan
		raw, _ := contracts.EncodeJSON(r)
		if _, e := contracts.DecodeComponentScanRequest(raw, nil); e != nil {
			return e
		}
		if t.GitTarget != nil || t.SDKOperation != nil || t.ComponentPlanning != nil || t.ParentState != nil || t.ScanMode != "standard" || t.ScanInput == nil || len(t.ScanInput.Knowledge) > 0 || t.MaxCostUSD == nil || t.MaxTotalCostUSD == nil {
			return errors.New("组件审计须绑定独立冻结计划、单次和任务累计预算")
		}
	}

	if (t.ComponentPlanning != nil) != (t.InputContractVersion == contracts.ComponentPlanningInputVersion) {
		return errors.New("组件规划合同标记不匹配")
	}
	if t.ComponentPlanning != nil {
		if t.SDKOperation != nil || t.GitTarget != nil || t.MaxCostUSD != nil || t.MaxTotalCostUSD != nil || t.ParentState != nil || t.ScanInput == nil || len(t.ScanInput.Knowledge) > 0 || t.ScanMode != "standard" {
			return errors.New("组件规划不得混合扫描、预算或其他操作")
		}
		raw, e := contracts.EncodeJSON(t.ComponentPlanning)
		if e != nil {
			return e
		}
		if _, e = contracts.DecodeComponentPlanningOperation(raw, nil); e != nil {
			return e
		}
	}

	if (t.GitTarget != nil) != (t.InputContractVersion == contracts.GitDiffInputVersion) {
		return errors.New("Git target contract marker mismatch")
	}
	if t.GitTarget != nil {
		raw, e := contracts.EncodeJSON(t.GitTarget)
		if e != nil {
			return e
		}
		if _, e = contracts.DecodeGitDiffTarget(raw, nil); e != nil {
			return e
		}
		var p contracts.ScanPolicy
		if e = json.Unmarshal([]byte(t.PolicySnapshotJSON), &p); e != nil {
			return e
		}
		if p.Mode != "standard" || len(p.IncludePaths) != 0 {
			return errors.New("Git 差异仅支持 Standard，不能叠加路径扫描")
		}
	}

	if t.SDKOperation != nil {
		raw, e := contracts.EncodeJSON(t.SDKOperation)
		if e != nil {
			return e
		}
		if _, e = contracts.DecodeSDKOperation(raw, nil); e != nil {
			return e
		}
		if t.SDKOperation.Kind == contracts.SDKOperationKindValidate && t.SDKOperationOrigin <= 0 {
			return errors.New("independent validation requires an immutable origin")
		}
	}
	if t.InputContractVersion != contracts.GitDiffInputVersion && (t.SDKOperation != nil) != (t.InputContractVersion == contracts.SDKOperationInputVersion) {
		return errors.New("SDK operation contract v3 marker mismatch")
	}

	if (t.InputContractVersion == contracts.ScanInputVersion || t.InputContractVersion == contracts.SDKOperationInputVersion || t.InputContractVersion == contracts.GitDiffInputVersion || t.InputContractVersion == contracts.ComponentPlanningInputVersion || t.InputContractVersion == contracts.ComponentScanInputVersion) && t.ScanInput == nil {
		return errors.New("missing scan input v2 document")
	}
	if t.ScanInput != nil && t.ScanInput.ParentScanID.Present {
		if t.ParentState == nil || t.ParentState.ScanID != t.ScanInput.ParentScanID.Value || t.ParentState.SourceHash != t.SourceContentHash {
			return errors.New("父扫描状态与本次输入不匹配")
		}
		raw, e := contracts.EncodeJSON(t.ParentState)
		if e != nil {
			return e
		}
		if _, e = contracts.DecodeSDKParentState(raw, nil); e != nil {
			return e
		}
	} else if t.ParentState != nil {
		return errors.New("unexpected parent SDK state")
	}
	if t.ScanInput != nil {
		if t.InputContractVersion != contracts.ScanInputVersion && t.InputContractVersion != contracts.SDKOperationInputVersion && t.InputContractVersion != contracts.GitDiffInputVersion && t.InputContractVersion != contracts.ComponentPlanningInputVersion && t.InputContractVersion != contracts.ComponentScanInputVersion || t.ResultContractVersion != 1 || t.ObservationContractVersion != 1 {
			return errors.New("advanced input requires current snapshot contracts")
		}
		if e := ValidateScanInput(t.ScanInput, t.ScanMode, t.ValidationMode); e != nil {
			return e
		}
	}
	if t.ObservationContractVersion != 0 && (t.ObservationContractVersion != 1 || (t.InputContractVersion != 1 && t.InputContractVersion != contracts.ScanInputVersion && t.InputContractVersion != contracts.SDKOperationInputVersion && t.InputContractVersion != contracts.GitDiffInputVersion && t.InputContractVersion != contracts.ComponentPlanningInputVersion && t.InputContractVersion != contracts.ComponentScanInputVersion) || t.ResultContractVersion != 1) {
		return errors.New("unsupported observation contract")
	}
	if t.ResultContractVersion != 0 && (t.ResultContractVersion != 1 || (t.InputContractVersion != 1 && t.InputContractVersion != contracts.ScanInputVersion && t.InputContractVersion != contracts.SDKOperationInputVersion && t.InputContractVersion != contracts.GitDiffInputVersion && t.InputContractVersion != contracts.ComponentPlanningInputVersion && t.InputContractVersion != contracts.ComponentScanInputVersion)) {
		return errors.New("unsupported result contract")
	}
	if t.MaxCostUSD != nil && (math.IsNaN(*t.MaxCostUSD) || math.IsInf(*t.MaxCostUSD, 0) || *t.MaxCostUSD <= 0 || *t.MaxCostUSD > 9007199254740991) {
		return errors.New("maxCostUsd must be a finite positive number")
	}
	if t.InputContractVersion == 0 {
		if t.MaxCostUSD != nil || t.Workers != nil {
			return errors.New("budget and workers require snapshot input contract")
		}
		return nil
	}
	if (t.InputContractVersion != int(contracts.Version) && t.InputContractVersion != contracts.ScanInputVersion && t.InputContractVersion != contracts.SDKOperationInputVersion && t.InputContractVersion != contracts.GitDiffInputVersion && t.InputContractVersion != contracts.ComponentPlanningInputVersion && t.InputContractVersion != contracts.ComponentScanInputVersion) || t.SourceSnapshotID <= 0 {
		return errors.New("unsupported input contract or missing source snapshot")
	}
	if digest, err := hex.DecodeString(t.SourceContentHash); err != nil || len(digest) != 32 {
		return errors.New("invalid snapshot content hash")
	}
	model, err := contracts.DecodeResolvedModel([]byte(t.ModelSnapshotJSON), nil)
	if err != nil {
		return err
	}
	policy, err := contracts.DecodeScanPolicy([]byte(t.PolicySnapshotJSON), nil)
	if err != nil {
		return err
	}
	if string(model.ProviderID) != t.Provider || model.Model != t.Model || model.BaseURL.Value != t.BaseURL || string(policy.Mode) != defaultMode(t.ScanMode) || policy.TimeBudget.ScanSeconds != int64(defaultTimeout(t.TimeoutMinutes))*60 {
		return errors.New("effective input differs from persisted task projection")
	}
	if policy.MaxCostUSD.Present != (t.MaxCostUSD != nil) || (t.MaxCostUSD != nil && policy.MaxCostUSD.Value != *t.MaxCostUSD) {
		return errors.New("budget differs from policy snapshot")
	}
	if policy.Workers.Present != (t.Workers != nil) || (t.Workers != nil && policy.Workers.Value != int64(*t.Workers)) {
		return errors.New("workers differ from policy snapshot")
	}
	return nil
}
