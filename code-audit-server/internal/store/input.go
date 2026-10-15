package store

import (
	"axiom/contracts"
	"encoding/hex"
	"errors"
	"math"
)

// validateTaskInput prevents any producer from persisting a budget that a legacy
// worker can silently ignore. The public API resolves these snapshots once.
func validateTaskInput(t Task) error {
	if t.ObservationContractVersion != 0 && (t.ObservationContractVersion != 1 || t.InputContractVersion != 1 || t.ResultContractVersion != 1) {
		return errors.New("unsupported observation contract")
	}
	if t.ResultContractVersion != 0 && (t.ResultContractVersion != 1 || t.InputContractVersion != 1) {
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
	if t.InputContractVersion != int(contracts.Version) || t.SourceSnapshotID <= 0 {
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
