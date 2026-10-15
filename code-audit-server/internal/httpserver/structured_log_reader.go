package httpserver

import (
	"axiom/contracts"
	"bufio"
	"io"
	"os"
	"sort"
)

type structuredRecord struct {
	sequence       int64
	offset, length int64
}

// This index validates only appended complete records. Cursors are event sequences,
// not physical line numbers; duplicate writes and malformed lines cannot shift them.
func readStructuredLogBatch(path string, after int, expected contracts.ExecutionRef) (logBatch, error) {
	result := logBatch{Events: []logEvent{}, Format: "scan_event_v2", Execution: &expected}
	f, e := os.Open(path)
	if e != nil {
		return result, e
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
	info, e := f.Stat()
	if e != nil {
		return result, e
	}
	if idx.info == nil || !os.SameFile(idx.info, info) || info.Size() < idx.info.Size() || (info.Size() == idx.info.Size() && !info.ModTime().Equal(idx.info.ModTime())) || idx.structuredExecution != expected || len(idx.offsets) == 0 {
		idx.offsets = []int64{0}
		idx.indexedLines = 0
		idx.records = nil
		idx.invalidRecords = 0
		idx.maximumSequence = 0
		idx.structuredExecution = expected
	}
	idx.info = info
	offset := idx.offsets[len(idx.offsets)-1]
	reader := bufio.NewReader(io.NewSectionReader(f, offset, info.Size()-offset))
	position := offset
	for {
		fragment, err := reader.ReadSlice('\n')
		position += int64(len(fragment))
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, err
		}
		idx.offsets = append(idx.offsets, position)
	}
	for n := idx.indexedLines; n < len(idx.offsets)-1; n++ {
		offset, length := idx.offsets[n], idx.offsets[n+1]-idx.offsets[n]
		if length > contracts.MaxEventBytes {
			idx.invalidRecords++
			continue
		}
		raw := make([]byte, length)
		if _, e = f.ReadAt(raw, offset); e != nil {
			return result, e
		}
		event, err := contracts.DecodeScanEventV2(raw, &expected)
		if err != nil {
			idx.invalidRecords++
			continue
		}
		if event.Sequence <= idx.maximumSequence {
			continue
		}
		idx.records = append(idx.records, structuredRecord{event.Sequence, offset, length})
		idx.maximumSequence = event.Sequence
	}
	idx.indexedLines = len(idx.offsets) - 1
	result.InvalidRecords = idx.invalidRecords
	result.Total = int(idx.maximumSequence)
	result.RecordCount = len(idx.records)
	if result.InvalidRecords > 0 {
		result.Diagnostic = &contracts.ContractError{Code: contracts.ErrorCodeInvalidResult, Origin: contracts.ErrorOriginServer, Message: "部分扫描事件未通过大小、Schema 或执行身份校验，已跳过无效记录", Retryable: false}
	}
	if after > result.Total {
		after = 0
	}
	start := sort.Search(len(idx.records), func(n int) bool { return idx.records[n].sequence > int64(after) })
	if after == 0 && len(idx.records) > 1500 {
		start = len(idx.records) - 1500
	}
	end := min(start+500, len(idx.records))
	var batchBytes int64
	for n := start; n < end; n++ {
		v := idx.records[n]
		if batchBytes > 0 && batchBytes+v.length > contracts.MaxEventBytes {
			end = n
			break
		}
		batchBytes += v.length
		raw := make([]byte, v.length)
		if _, e = f.ReadAt(raw, v.offset); e != nil {
			return result, e
		}
		event, err := contracts.DecodeScanEventV2(raw, &expected)
		if err != nil {
			return result, err
		}
		result.Events = append(result.Events, logEvent{Contract: &event, N: int(event.Sequence), Timestamp: string(event.OccurredAt), Level: string(event.Level), Kind: string(event.Type), Message: event.Message})
		result.NextAfter = int(event.Sequence)
	}
	if result.NextAfter == 0 {
		result.NextAfter = after
	}
	result.HasMore = end < len(idx.records)
	return result, nil
}
