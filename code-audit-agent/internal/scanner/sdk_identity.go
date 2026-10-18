package scanner

import (
	"axiom/contracts"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Identity paths are derived locally from validated references. Server and Web
// never select a host credential path or receive a credential-file artifact.
func SDKIdentityHome(data string, ref contracts.SDKIdentityRef) (string, error) {
	raw, _ := contracts.EncodeJSON(ref)
	if contracts.ValidateJSON(contracts.KindSDKIdentityRef, raw, nil) != nil {
		return "", errors.New("invalid SDK identity")
	}
	root, e := filepath.Abs(data)
	if e != nil {
		return "", e
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		return "", e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256([]byte(ref.AgentID))
	parts := []string{".sdk-identities", hex.EncodeToString(sum[:16]), ref.ID, "generation-" + strconv.FormatInt(ref.Generation, 10)}
	for _, part := range parts {
		root = filepath.Join(root, part)
		if e = os.Mkdir(root, 0700); e != nil && !os.IsExist(e) {
			return "", e
		}
		info, e := os.Lstat(root)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("identity directory is not private")
		}
		if e = os.Chmod(root, 0700); e != nil {
			return "", e
		}
	}
	return root, nil
}
func RunSDKIdentity(ctx context.Context, claim contracts.SDKIdentityClaim, data, scannerDir, node string, onReport func(contracts.SDKIdentityReport) error) error {
	raw, e := contracts.EncodeJSON(claim)
	if e != nil || contracts.ValidateJSON(contracts.KindSDKIdentityClaim, raw, nil) != nil {
		return errors.New("invalid SDK identity claim")
	}
	home, e := SDKIdentityHome(data, claim.Operation.Identity)
	if e != nil {
		return e
	}
	input := filepath.Join(home, "operation-"+claim.Operation.ID+".json")
	if e = os.WriteFile(input, raw, 0600); e != nil {
		return e
	}
	defer os.Remove(input)
	args := append(entryArguments(scannerDir), "--identity-operation", input)
	cmd := exec.CommandContext(ctx, node, args...)
	stopProcessTimer := configureIdentityProcessGroup(cmd)
	defer stopProcessTimer()
	cmd.Dir = scannerDir
	env := jobEnvironment(childEnvForCredential("", "", true, nil), "")
	clean := env[:0]
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if key != "CODEX_SECURITY_STATE_DIR" && key != "AXIOM_SDK_IDENTITY_HOME" {
			clean = append(clean, kv)
		}
	}
	cmd.Env = scannerParentEnvironment(append(clean, "CODEX_SECURITY_STATE_DIR="+filepath.Join(home, "sdk"), "CODEX_HOME="+filepath.Join(home, "ambient-empty"), "AXIOM_SDK_IDENTITY_HOME="+home))
	// SDK stderr and stdout can contain account details. Only validated JSONL
	// reports reach the control plane; raw native output is never logged.
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	cmd.Stderr = nil
	if e = cmd.Start(); e != nil {
		return e
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	var reportErr error
	var completed bool
	var cleanupReport *contracts.SDKIdentityReport
	for scanner.Scan() {
		v, e := contracts.DecodeSDKIdentityReport(scanner.Bytes(), nil)
		if e != nil {
			reportErr = errors.New("invalid SDK identity report")
			break
		}
		if v.OperationID != claim.Operation.ID || v.IdentityID != claim.Operation.Identity.ID || v.AgentID != claim.Operation.Identity.AgentID || v.Generation != claim.Operation.Identity.Generation || v.LeaseToken != claim.LeaseToken {
			reportErr = errors.New("foreign SDK identity report")
			break
		}
		completed = v.Status == "completed"
		if claim.Operation.Action == "logout" && completed {
			saved := v
			cleanupReport = &saved
			continue
		}
		if e = onReport(v); e != nil {
			reportErr = e
			break
		}
	}
	if reportErr != nil {
		_ = cmd.Cancel()
	}
	waitErr := cmd.Wait()
	if scanner.Err() != nil {
		return errors.New("SDK identity event stream unavailable")
	}
	login := claim.Operation.Action == "login_api_key" || claim.Operation.Action == "login_device_code"
	if claim.Operation.Action == "logout" || (login && (!completed || ctx.Err() != nil || reportErr != nil)) {
		cleanup := home
		if claim.Operation.Action == "logout" {
			// Relogin advances the generation. Revocation clears every local
			// generation for this identity after all bound leases have stopped.
			cleanup = filepath.Dir(home)
		}
		if e := os.RemoveAll(cleanup); e != nil {
			return errors.New("SDK credential cleanup was not confirmed")
		}
	}
	if reportErr != nil {
		return reportErr
	}
	if waitErr != nil {
		return errors.New("SDK identity process did not complete")
	}
	if cleanupReport != nil && reportErr == nil && waitErr == nil {
		if e = onReport(*cleanupReport); e != nil {
			return e
		}
	}
	if !completed {
		return errors.New("SDK identity operation failed")
	}
	return nil
}
func prepareTaskIdentity(req Request) (string, string, error) {
	binding := req.SDKIdentity
	if binding == nil {
		return "", "", nil
	}
	raw, e := contracts.EncodeJSON(binding)
	expected := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(req.TaskID)), Attempt: int64(req.Attempt)}
	if e != nil || contracts.ValidateJSON(contracts.KindSDKTaskIdentity, raw, &expected) != nil || binding.Identity.AgentID != req.AgentID {
		return "", "", errors.New("SDK task identity binding differs from execution")
	}
	home, e := SDKIdentityHome(req.DataDir, binding.Identity)
	if e != nil {
		return "", "", e
	}
	marker, e := os.ReadFile(filepath.Join(home, "session.json"))
	if e != nil {
		return "", "", errors.New("SDK 身份凭据不可用，请在 Web 核对绑定节点的登录状态")
	}
	var stored contracts.SDKIdentityRef
	if json.Unmarshal(marker, &stored) != nil || stored != binding.Identity {
		return "", "", errors.New("SDK identity session reference differs")
	}
	file := filepath.Join(req.OutputPath, "runtime/sdk-identity.json")
	if e = os.WriteFile(file, raw, 0600); e != nil {
		return "", "", e
	}
	return file, home, nil
}
