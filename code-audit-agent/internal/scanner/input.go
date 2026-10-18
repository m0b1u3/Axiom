package scanner

import (
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const LayoutJob = "runtime/job.json"

type boundedOutput struct {
	data  []byte
	limit int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > b.limit {
		return 0, errors.New("scanner capability response too large")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func entryArguments(directory string) []string {
	if _, err := os.Stat(filepath.Join(directory, "dist/main.js")); err == nil {
		return []string{"dist/main.js"}
	}
	return []string{"--import", "tsx", "src/main.ts"}
}

// Probe does not supply scan credentials and must not load or start the SDK.
type Capabilities struct {
	IdentityVersion    int
	DescriptionVersion int
	Build              string
	Observation        contracts.ObservationCapabilities
	Input              contracts.InputCapabilities
	Result             contracts.ResultCapabilities
}

func ProbeContracts(ctx context.Context, directory, node string) (Capabilities, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, append(entryArguments(directory), "--describe")...)
	configureProcessGroup(cmd)
	cmd.Dir = directory
	cmd.Env = jobEnvironment(childEnvForCredential("", "", true, nil), "")
	out := &boundedOutput{limit: contracts.MaxProgressBytes}
	diagnostic := &boundedOutput{limit: contracts.MaxProgressBytes}
	cmd.Stdout, cmd.Stderr = out, diagnostic
	if err := cmd.Run(); err != nil {
		return Capabilities{}, fmt.Errorf("scanner input preflight failed: %w", err)
	}
	return parseDescription(out.data)
}

func parseDescription(raw []byte) (Capabilities, error) {
	var h struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		return Capabilities{}, errors.New("invalid Scanner description JSON")
	}
	var value any
	switch h.SchemaVersion {
	case 10:
		v, e := contracts.DecodeScannerDescriptionV10(raw, nil)
		if e != nil {
			return Capabilities{}, e
		}
		value = v
	case 1:
		v, e := contracts.DecodeScannerDescription(raw, nil)
		if e != nil {
			return Capabilities{}, e
		}
		value = v
	case 2:
		v, e := contracts.DecodeScannerDescriptionV2(raw, nil)
		if e != nil {
			return Capabilities{}, e
		}
		value = v
	case 3:
		v, e := contracts.DecodeScannerDescriptionV3(raw, nil)
		if e != nil {
			return Capabilities{}, e
		}
		value = v
	case contracts.ComponentScanDescriptionVersion:
		v, e := contracts.DecodeScannerDescriptionV9(raw, nil)
		if e != nil {
			return Capabilities{}, e
		}
		value = v
	case contracts.ComponentPlanningDescriptionVersion:
		v, e := contracts.DecodeScannerDescriptionV8(raw, nil)
		if e != nil {
			return Capabilities{}, e
		}
		value = v
	case contracts.GitDiffDescriptionVersion:
		v, e := contracts.DecodeScannerDescriptionV7(raw, nil)
		if e != nil {
			return Capabilities{}, e
		}
		value = v
	case contracts.SDKOperationDescriptionVersion:
		v, e := contracts.DecodeScannerDescriptionV6(raw, nil)
		if e != nil {
			return Capabilities{}, e
		}
		value = v
	case 5:
		v, e := contracts.DecodeScannerDescriptionV5(raw, nil)
		if e != nil {
			return Capabilities{}, e
		}
		value = v
	case 4:
		v, e := contracts.DecodeScannerDescriptionV4(raw, nil)
		if e != nil {
			return Capabilities{}, e
		}
		value = v
	default:
		return Capabilities{DescriptionVersion: h.SchemaVersion}, errors.New("unsupported Scanner description version")
	}
	canonical, e := json.Marshal(value)
	if e != nil {
		return Capabilities{}, e
	}
	var d struct {
		IdentityVersion int    `json:"sdkIdentityVersion"`
		Version         string `json:"version"`
		Runtime         struct {
			Versions struct{ Assignment, Job, Progress, Event, Result, Completion []int64 } `json:"versions"`
		} `json:"runtime"`
		Capabilities struct {
			MaxCostUSD         bool `json:"maxCostUsd"`
			JobFile            bool `json:"jobFile"`
			VersionedResults   bool `json:"versionedResults"`
			AtomicResultCommit bool `json:"atomicResultCommit"`
			StructuredEvents   bool `json:"structuredEvents"`
		} `json:"capabilities"`
	}
	if e = json.Unmarshal(canonical, &d); e != nil {
		return Capabilities{}, e
	}
	c := Capabilities{IdentityVersion: d.IdentityVersion, DescriptionVersion: h.SchemaVersion, Build: d.Version, Input: contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{}, JobVersions: []contracts.SchemaVersion{}, MaxCostUSD: d.Capabilities.MaxCostUSD}, Result: contracts.NoResultCapabilities(), Observation: contracts.NoObservationCapabilities()}
	if h.SchemaVersion >= 2 && d.Capabilities.JobFile {
		for _, v := range d.Runtime.Versions.Assignment {
			if v == 1 {
				c.Input.AssignmentVersions = append(c.Input.AssignmentVersions, 1)
			}
		}
		for _, v := range d.Runtime.Versions.Job {
			if v == 1 {
				c.Input.JobVersions = append(c.Input.JobVersions, 1)
			}
		}
	}
	if h.SchemaVersion >= 3 && d.Capabilities.VersionedResults && d.Capabilities.AtomicResultCommit {
		for _, v := range d.Runtime.Versions.Job {
			if v == 2 {
				c.Result.JobVersions = append(c.Result.JobVersions, 2)
			}
		}
		c.Result.ResultVersions = d.Runtime.Versions.Result
		c.Result.CompletionVersions = d.Runtime.Versions.Completion
		c.Result.CommitVersions = []int64{1}
	}
	if h.SchemaVersion >= 4 && d.Capabilities.StructuredEvents {
		for _, v := range d.Runtime.Versions.Job {
			if v == 3 || v == contracts.SDKOperationJobVersion || v == contracts.GitDiffJobVersion || v == contracts.ComponentPlanningJobVersion || v == contracts.ComponentScanJobVersion || (v == contracts.ScanInputJobVersion && h.SchemaVersion >= contracts.ScanInputDescriptionVersion) {
				c.Observation.JobVersions = append(c.Observation.JobVersions, v)
			}
		}
		c.Observation.ProgressVersions = d.Runtime.Versions.Progress
		c.Observation.EventVersions = d.Runtime.Versions.Event
		c.Observation.HeartbeatVersions = []int64{1}
	}
	return c, nil
}

func JobFromAssignment(a contracts.ExecutionAssignment, source, output string) (*contracts.ScannerJob, error) {
	job := contracts.ScannerJob{SchemaVersion: a.SchemaVersion, Execution: a.Execution, Source: contracts.LocalSource{Path: contracts.AbsolutePath(source), SnapshotID: a.Source.SnapshotID, ContentHash: a.Source.ContentHash}, Output: contracts.LocalOutput{Path: contracts.AbsolutePath(output)}, Model: a.Model, Policy: a.Policy}
	raw, err := json.Marshal(job)
	if err != nil {
		return nil, err
	}
	checked, err := contracts.DecodeScannerJob(raw, &a.Execution)
	return &checked, err
}

func prepareJob(ctx context.Context, req Request) (string, error) {
	job := req.Job
	if job == nil {
		return "", errors.New("scanner job is required")
	}
	raw, err := json.Marshal(job)
	if err != nil {
		return "", err
	}
	expected := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(req.TaskID)), Attempt: int64(req.Attempt)}
	if err = contracts.ValidateJSON(contracts.KindScannerJob, raw, &expected); err != nil {
		return "", err
	}
	if filepath.Clean(string(job.Output.Path)) != filepath.Clean(req.OutputPath) {
		return "", errors.New("scanner output differs from execution output")
	}
	if err = outsideSource(string(job.Source.Path), req.OutputPath); err != nil {
		return "", err
	}
	info, err := os.Stat(string(job.Source.Path))
	if err != nil || !info.IsDir() {
		return "", errors.New("source snapshot directory unavailable")
	}
	hash, _, _, err := sourceidentity.TreeHash(ctx, string(job.Source.Path))
	if err != nil {
		return "", err
	}
	if hash != string(job.Source.ContentHash) {
		return "", errors.New("source snapshot content changed")
	}
	caps, err := ProbeContracts(ctx, req.ScannerDir, req.NodePath)
	if err != nil {
		return "", err
	}
	version := false
	for _, v := range caps.Input.JobVersions {
		version = version || v == job.SchemaVersion
	}
	if !version || (job.Policy.MaxCostUSD.Present && !caps.Input.MaxCostUSD) {
		return "", errors.New("scanner does not support this input or budget; execution was not started")
	}
	if req.ResultContractVersion != 0 {
		if !caps.Result.SupportsResult(req.ResultContractVersion) {
			return "", errors.New("scanner does not support the negotiated result contract")
		}
		raw, err = json.Marshal(contracts.ScannerJobV2{SchemaVersion: 2, ResultSchemaVersion: contracts.Version, Execution: job.Execution, Source: job.Source, Output: job.Output, Model: job.Model, Policy: job.Policy})
		if err != nil {
			return "", err
		}
		if err = contracts.ValidateJSON(contracts.KindScannerJobV2, raw, &expected); err != nil {
			return "", err
		}
	}
	if req.ObservationContractVersion != 0 {
		if req.ResultContractVersion != 1 || !caps.Observation.SupportsObservation(req.ObservationContractVersion) {
			return "", errors.New("scanner does not support the negotiated observation contract; execution was not started")
		}
		raw, err = json.Marshal(contracts.ScannerJobV3{SchemaVersion: 3, ResultSchemaVersion: contracts.Version, ProgressSchemaVersion: 2, EventSchemaVersion: 2, Execution: job.Execution, Source: job.Source, Output: job.Output, Model: job.Model, Policy: job.Policy})
		if err != nil {
			return "", err
		}
		if err = contracts.ValidateJSON(contracts.KindScannerJobV3, raw, &expected); err != nil {
			return "", err
		}
	}
	if req.ScanInput != nil {
		if !supportsScanInput(caps) || req.ResultContractVersion != 1 || req.ObservationContractVersion != 1 {
			return "", errors.New("scanner does not support advanced input; execution was not started")
		}
		v := contracts.ScannerJobV4{SchemaVersion: 4, ResultSchemaVersion: 1, ProgressSchemaVersion: 2, EventSchemaVersion: 2, Execution: job.Execution, Source: job.Source, Output: job.Output, Model: job.Model, Policy: job.Policy, ScanInput: *req.ScanInput}
		if req.ParentState != nil {
			v.ParentState = contracts.Some(*req.ParentState)
		}
		raw, err = json.Marshal(v)
		if err != nil {
			return "", err
		}
		if _, err = contracts.DecodeScannerJobV4(raw, &expected); err != nil {
			return "", err
		}
		if err = materializeKnowledge(req.OutputPath, *req.ScanInput); err != nil {
			return "", err
		}
	}
	if req.SDKOperation != nil {
		if !containsOperationJob(caps) || req.ScanInput == nil || req.ResultContractVersion != 1 || req.ObservationContractVersion != 1 {
			return "", errors.New("SDK operation contracts unsupported")
		}
		v := contracts.ScannerJobV5{SchemaVersion: contracts.SDKOperationJobVersion, ResultSchemaVersion: 1, ProgressSchemaVersion: 2, EventSchemaVersion: 2, Execution: job.Execution, Source: job.Source, Output: job.Output, Model: job.Model, Policy: job.Policy, ScanInput: *req.ScanInput, Operation: *req.SDKOperation}
		raw, err = json.Marshal(v)
		if err != nil {
			return "", err
		}
		if _, err = contracts.DecodeScannerJobV5(raw, &expected); err != nil {
			return "", err
		}
	}
	if req.GitTarget != nil {
		if !hasGitJob(caps) || req.ScanInput == nil {
			return "", errors.New("Scanner 缺少 Git 差异合同能力，未启动 SDK")
		}
		source, e := materializeGitSource(ctx, string(job.Source.Path), req.OutputPath, string(job.Source.ContentHash), *req.GitTarget)
		if e != nil {
			return "", e
		}
		v := contracts.ScannerJobV6{SchemaVersion: 6, Execution: job.Execution, Source: job.Source, Output: job.Output, Model: job.Model, Policy: job.Policy, ResultSchemaVersion: 1, ProgressSchemaVersion: 2, EventSchemaVersion: 2, ScanInput: *req.ScanInput, GitTarget: *req.GitTarget}
		v.Source.Path = contracts.AbsolutePath(source)
		if req.ParentState != nil {
			v.ParentState = contracts.Some(*req.ParentState)
		}
		if req.SDKOperation != nil {
			v.Operation = contracts.Some(*req.SDKOperation)
		}
		raw, err = json.Marshal(v)
		if err != nil {
			return "", err
		}
		if _, err = contracts.DecodeScannerJobV6(raw, &expected); err != nil {
			return "", err
		}
	}
	if req.ComponentPlanning != nil {
		supported := false
		for _, v := range caps.Observation.JobVersions {
			supported = supported || v == contracts.ComponentPlanningJobVersion
		}
		if !supported || req.ScanInput == nil || req.ResultContractVersion != 1 || req.ObservationContractVersion != 1 || req.GitTarget != nil || req.SDKOperation != nil {
			return "", errors.New("Scanner 缺少组件规划合同能力，未启动 SDK")
		}
		source, e := materializeComponentSource(ctx, string(job.Source.Path), req.OutputPath, string(job.Source.ContentHash))
		if e != nil {
			return "", e
		}
		v := contracts.ScannerJobV7{SchemaVersion: 7, Execution: job.Execution, Source: job.Source, Output: job.Output, Model: job.Model, Policy: job.Policy, ResultSchemaVersion: 1, ProgressSchemaVersion: 2, EventSchemaVersion: 2, ScanInput: *req.ScanInput, ComponentPlanning: *req.ComponentPlanning}
		v.Source.Path = contracts.AbsolutePath(source)
		raw, e = json.Marshal(v)
		if e != nil {
			return "", e
		}
		if _, e = contracts.DecodeScannerJobV7(raw, &expected); e != nil {
			return "", e
		}
	}

	if req.ComponentScan != nil {
		supported := false
		for _, v := range caps.Observation.JobVersions {
			supported = supported || v == contracts.ComponentScanJobVersion
		}
		if !supported || req.ScanInput == nil || req.ResultContractVersion != 1 || req.ObservationContractVersion != 1 || req.GitTarget != nil || req.SDKOperation != nil || req.ComponentPlanning != nil {
			return "", errors.New("Scanner 缺少组件审计合同，未启动 SDK")
		}
		source, e := materializeComponentSource(ctx, string(job.Source.Path), req.OutputPath, string(job.Source.ContentHash))
		if e != nil {
			return "", e
		}
		v := contracts.ScannerJobV8{SchemaVersion: 8, Execution: job.Execution, Source: job.Source, Output: job.Output, Model: job.Model, Policy: job.Policy, ResultSchemaVersion: 1, ProgressSchemaVersion: 2, EventSchemaVersion: 2, ScanInput: *req.ScanInput, ComponentScan: *req.ComponentScan}
		v.Source.Path = contracts.AbsolutePath(source)
		raw, e = json.Marshal(v)
		if e != nil {
			return "", e
		}
		if _, e = contracts.DecodeScannerJobV8(raw, &expected); e != nil {
			return "", e
		}
	}
	name := filepath.Join(req.OutputPath, LayoutJob)
	if err = os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(name), ".job-*.tmp")
	if err != nil {
		return "", err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(append(raw, '\n')); err != nil {
		f.Close()
		return "", err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(temp, name); err != nil {
		return "", err
	}
	if req.ResultContractVersion == 1 {
		if err = os.Remove(filepath.Join(req.OutputPath, LayoutCommit)); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return name, nil
}

func resolvedLocation(value string) (string, error) {
	value = filepath.Clean(value)
	if resolved, err := filepath.EvalSymlinks(value); err == nil {
		return resolved, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(value)
	if parent == value {
		return "", errors.New("cannot resolve execution path")
	}
	resolved, err := resolvedLocation(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(value)), nil
}
func outsideSource(source, output string) error {
	src, err := resolvedLocation(source)
	if err != nil {
		return err
	}
	out, err := resolvedLocation(output)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(src, out)
	if err != nil {
		return err
	}
	if rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
		return errors.New("execution output must be outside the source snapshot")
	}
	return nil
}

func supportsScanInput(c Capabilities) bool {
	for _, n := range c.Observation.JobVersions {
		if n == contracts.ScanInputJobVersion {
			return true
		}
	}
	return false
}

func containsOperationJob(c Capabilities) bool {
	for _, v := range c.Observation.JobVersions {
		if v == contracts.SDKOperationJobVersion {
			return true
		}
	}
	return false
}

func hasGitJob(c Capabilities) bool {
	for _, v := range c.Observation.JobVersions {
		if v == contracts.GitDiffJobVersion {
			return true
		}
	}
	return false
}
