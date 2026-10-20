package scanner

import (
	"axiom/contracts"
	"context"
	"errors"
	"os"
	"path/filepath"
)

func prepareSDKParameters(ctx context.Context, req Request) (string, error) {
	caps, e := ProbeContracts(ctx, req.ScannerDir, req.NodePath)
	if e != nil || caps.ParametersVersion != 1 {
		return "", errors.New("Scanner does not support SDK parameters v1")
	}
	raw, e := contracts.EncodeJSON(req.SDKParameters)
	if e != nil {
		return "", e
	}
	value, e := contracts.DecodeSDKParametersAssignment(raw, &req.Job.Execution)
	if e != nil {
		return "", e
	}
	if input := value.Parameters.ComponentInput; input.Present {
		if req.ComponentScan == nil {
			return "", errors.New("Component SDK inputs require component execution")
		}
		if e := materializeKnowledge(req.OutputPath, contracts.ScanInput{SchemaVersion: 1, Knowledge: input.Value.Knowledge}); e != nil {
			return "", e
		}
	}
	directory := filepath.Join(req.OutputPath, "runtime")
	if e := os.MkdirAll(directory, 0700); e != nil {
		return "", e
	}
	info, e := os.Lstat(directory)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("SDK parameters runtime directory must not be a symlink")
	}
	file, e := os.CreateTemp(directory, ".sdk-parameters-")
	if e != nil {
		return "", e
	}
	defer os.Remove(file.Name())
	if _, e = file.Write(raw); e != nil {
		file.Close()
		return "", e
	}
	if e = file.Sync(); e != nil {
		file.Close()
		return "", e
	}
	if e = file.Close(); e != nil {
		return "", e
	}
	target := filepath.Join(directory, "sdk-parameters.json")
	if e = os.Rename(file.Name(), target); e != nil {
		return "", e
	}
	return target, nil
}
