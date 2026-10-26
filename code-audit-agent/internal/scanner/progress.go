package scanner

import (
	"axiom/contracts"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func ReadContractProgress(output string, execution contracts.ExecutionRef) (*contracts.ProgressSnapshotV2, error) {
	f, e := os.Open(filepath.Join(output, LayoutProgress))
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > contracts.MaxProgressBytes {
		return nil, fmt.Errorf("progress is not a bounded regular document")
	}
	raw, e := io.ReadAll(io.LimitReader(f, contracts.MaxProgressBytes+1))
	if e != nil {
		return nil, e
	}
	v, e := contracts.DecodeProgressSnapshotV2(raw, &execution)
	if e != nil {
		return nil, e
	}
	if raw, ok := v.Extensions.Value["standardInterim"]; ok {
		if _, err := contracts.DecodeInterimObservation(raw, &execution); err != nil {
			return nil, fmt.Errorf("invalid interim observation contract")
		}
	}
	return &v, nil
}
