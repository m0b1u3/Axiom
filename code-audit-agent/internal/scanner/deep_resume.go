package scanner

import (
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// Only macOS system aliases are accepted; task-owned symlinks still fail.
func deepSystemPath(p string) string {
	p = filepath.Clean(p)
	if goruntime.GOOS == "darwin" && (p == "/var" || strings.HasPrefix(p, "/var/") || p == "/tmp" || strings.HasPrefix(p, "/tmp/")) {
		return "/private" + p
	}
	return p
}
func deepOrigin(output string, attempt int64) (string, error) {
	if attempt < 1 || !strings.HasPrefix(filepath.Base(output), "attempt-") {
		return "", errors.New("invalid Deep origin directory")
	}
	return filepath.Join(filepath.Dir(output), fmt.Sprintf("attempt-%d", attempt)), nil
}
func deepState(data, output string, origin contracts.ExecutionRef, identity *contracts.SDKIdentityRef) (string, error) {
	root, e := deepOrigin(output, origin.Attempt)
	if e != nil {
		return "", e
	}
	if identity == nil {
		return filepath.Join(root, "runtime", "sdk"), nil
	}
	sum := sha256.Sum256([]byte(identity.AgentID))
	return filepath.Join(data, ".sdk-identities", hex.EncodeToString(sum[:16]), identity.ID, fmt.Sprintf("generation-%d", identity.Generation), "executions", fmt.Sprint(origin.TaskID)+"-"+fmt.Sprint(origin.Attempt), "sdk"), nil
}
func prepareDeepSidecar(req Request, value any, kind contracts.Kind, name string) (string, error) {
	raw, e := contracts.EncodeJSON(value)
	if e != nil || contracts.ValidateJSON(kind, raw, &req.Job.Execution) != nil {
		return "", errors.New("invalid Deep resume sidecar")
	}
	directory := filepath.Join(req.OutputPath, "runtime")
	if e = os.MkdirAll(directory, 0700); e != nil {
		return "", e
	}
	real, e := filepath.EvalSymlinks(directory)
	if e != nil || real != deepSystemPath(directory) {
		return "", errors.New("Deep sidecar directory changed")
	}
	p := filepath.Join(directory, name+".json")
	if i, e := os.Lstat(p); e == nil && i.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("Deep sidecar cannot be a symlink")
	}
	return p, os.WriteFile(p, raw, 0600)
}
func RunDeepResumeProbe(ctx context.Context, claim contracts.DeepResumeProbeClaim, data, scannerDir, node string) (*contracts.DeepResumeProbeReport, error) {
	raw, e := contracts.EncodeJSON(claim)
	if e != nil || contracts.ValidateJSON(contracts.KindDeepResumeProbeClaim, raw, nil) != nil {
		return nil, errors.New("invalid Deep inspection claim")
	}
	if hash, _, _, e := sourceidentity.TreeHash(ctx, claim.SourcePath); e != nil || hash != claim.SourceHash {
		return nil, errors.New("Deep source snapshot changed")
	}
	runtime := filepath.Join(claim.OutputPath, "runtime")
	if e = os.MkdirAll(runtime, 0700); e != nil {
		return nil, e
	}
	if real, e := filepath.EvalSymlinks(runtime); e != nil || real != deepSystemPath(runtime) {
		return nil, errors.New("Deep inspection directory changed")
	}
	file, e := os.CreateTemp(runtime, ".deep-inspection-*.json")
	if e != nil {
		return nil, e
	}
	input := file.Name()
	defer os.Remove(input)
	if _, e = file.Write(raw); e != nil {
		file.Close()
		return nil, e
	}
	if e = file.Close(); e != nil {
		return nil, e
	}
	state, e := deepState(data, claim.OutputPath, claim.Origin, claim.Identity)
	if e != nil {
		return nil, e
	}
	cmd := exec.CommandContext(ctx, node, append(entryArguments(scannerDir), "--deep-resume-probe", input)...)
	configureProcessGroup(cmd)
	cmd.Dir = scannerDir
	env := jobEnvironment(childEnvForCredential("", "", true, nil), "")
	clean := env[:0]
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if key != "CODEX_SECURITY_STATE_DIR" && key != "AXIOM_SDK_IDENTITY_HOME" {
			clean = append(clean, kv)
		}
	}
	cmd.Env = scannerParentEnvironment(append(clean, "AXIOM_DEEP_RESUME_EXPECTED_STATE_ROOT="+state))
	out := &boundedOutput{limit: 128 * 1024}
	cmd.Stdout = out
	cmd.Stderr = nil
	if e = cmd.Run(); e != nil {
		return nil, errors.New("Deep native inspection process did not complete")
	}
	v, e := contracts.DecodeDeepResumeProbeReport(out.data, nil)
	if e != nil || v.ID != claim.ID || v.LeaseToken != claim.LeaseToken || v.AgentID != claim.AgentID || v.Inspection.Execution != claim.Execution {
		return nil, errors.New("foreign Deep inspection report")
	}
	return &v, nil
}
