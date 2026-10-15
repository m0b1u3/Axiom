package httpserver

import (
	"axiom/contracts"
	"bufio"
	"encoding/json"
	"io"
	"os"
	"sync"
)

type logEvent struct {
	Contract  *contracts.ScanEventV2 `json:"event,omitempty"`
	N         int                    `json:"n"`
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Kind      string                 `json:"kind"`
	Message   string                 `json:"message"`
}
type logBatch struct {
	RecordCount    int                      `json:"recordCount,omitempty"`
	InvalidRecords int                      `json:"invalidRecords,omitempty"`
	Format         string                   `json:"format,omitempty"`
	Execution      *contracts.ExecutionRef  `json:"execution,omitempty"`
	Diagnostic     *contracts.ContractError `json:"diagnostic,omitempty"`
	Events         []logEvent               `json:"events"`
	NextAfter      int                      `json:"nextAfter"`
	Total          int                      `json:"total"`
	HasMore        bool                     `json:"hasMore"`
}
type logIndex struct {
	mu                  sync.Mutex
	info                os.FileInfo
	offsets             []int64
	structuredExecution contracts.ExecutionRef
	indexedLines        int
	records             []structuredRecord
	invalidRecords      int
	maximumSequence     int64
}

var logIndexes = struct {
	sync.Mutex
	entries map[string]*logIndex
}{entries: make(map[string]*logIndex)}

// Retain only line offsets. Repeated polls read bytes appended since the last poll.
func readLogBatch(path string, after int) (logBatch, error) {
	result := logBatch{Events: []logEvent{}}
	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer f.Close()
	logIndexes.Lock()
	idx := logIndexes.entries[path]
	if idx == nil {
		if len(logIndexes.entries) >= 64 {
			for key := range logIndexes.entries {
				delete(logIndexes.entries, key)
				break
			}
		}
		idx = &logIndex{}
		logIndexes.entries[path] = idx
	}
	logIndexes.Unlock()
	idx.mu.Lock()
	defer idx.mu.Unlock()
	info, err := f.Stat()
	if err != nil {
		return result, err
	}
	if idx.info == nil || !os.SameFile(idx.info, info) || info.Size() < idx.info.Size() || (info.Size() == idx.info.Size() && !info.ModTime().Equal(idx.info.ModTime())) {
		idx.offsets = []int64{0}
		idx.indexedLines = 0
		idx.records = nil
		idx.invalidRecords = 0
		idx.maximumSequence = 0
	}
	idx.info = info
	offset := idx.offsets[len(idx.offsets)-1]
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return result, err
	}
	reader := bufio.NewReader(io.NewSectionReader(f, offset, info.Size()-offset))
	position := offset
	for {
		fragment, e := reader.ReadSlice('\n')
		position += int64(len(fragment))
		if e == bufio.ErrBufferFull {
			continue
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return result, e
		}
		idx.offsets = append(idx.offsets, position)
	}
	result.Total = len(idx.offsets) - 1
	if after > result.Total {
		after = 0
	}
	// Initial view shows recent history, matching the UI's retention limit.
	if after == 0 && result.Total > 1500 {
		after = result.Total - 1500
	}
	end := after + 500
	if end > result.Total {
		end = result.Total
	}
	batchBytes := int64(0)
	for n := after; n < end; n++ {
		length := idx.offsets[n+1] - idx.offsets[n]
		// A malformed oversized record must not cause an unbounded allocation.
		if length > 2*1024*1024 {
			continue
		}
		if batchBytes > 0 && batchBytes+length > 2*1024*1024 {
			end = n
			break
		}
		batchBytes += length
		data := make([]byte, int(length))
		if _, err = f.ReadAt(data, idx.offsets[n]); err != nil {
			return result, err
		}
		var event logEvent
		if json.Unmarshal(data, &event) != nil {
			continue
		}
		event.N = n + 1
		result.Events = append(result.Events, event)
	}
	result.NextAfter = end
	result.HasMore = end < result.Total
	return result, nil
}
