package scanner

import (
	"axiom/contracts"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const LayoutCommit = "normalized/result.commit.json"

var ErrInvalidResult = errors.New("scanner result is missing or invalid")

// A file becoming readable is not a commit. Verify the marker and bytes before
// decoding any findings, and bind both documents to the claimed attempt.
func ReadCommitted(output string, expected contracts.ExecutionRef) (*contracts.ScanResult, error) {
	raw, err := readResultFile(filepath.Join(output, LayoutCommit), contracts.MaxProgressBytes)
	if err != nil {
		return nil, err
	}
	marker, err := contracts.DecodeResultCommit(raw, &expected)
	if err != nil {
		return nil, err
	}
	data, err := readResultFile(filepath.Join(output, LayoutNormalized), contracts.MaxDocumentBytes)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != marker.SizeBytes {
		return nil, errors.New("result size differs from commit marker")
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != string(marker.SHA256) {
		return nil, errors.New("result SHA-256 differs from commit marker")
	}
	result, err := contracts.DecodeScanResult(data, &expected)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func readResultFile(name string, limit int) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("result artifact is not a bounded regular file")
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, current) {
		return nil, errors.New("result artifact changed during open")
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > limit {
		return nil, errors.New("result artifact exceeds byte limit")
	}
	return b, nil
}

func ProcessOutcome(state *os.ProcessState, committed bool) contracts.ProcessExit {
	out := contracts.ProcessExit{ResultCommitted: committed}
	if state == nil {
		return out
	}
	if code := state.ExitCode(); code >= 0 {
		v := int64(code)
		out.Code = &v
	} else {
		signal := state.String()
		out.Signal = &signal
	}
	return out
}

func ProcessMismatch(process contracts.ProcessExit, status contracts.TerminalStatus) bool {
	expected, ok := contracts.ExitCode(status)
	return !ok || process.Code == nil || *process.Code != int64(expected) || process.Signal != nil
}

func ResultDiagnostic(err error) string { return fmt.Sprint(err) }
