package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

func parentFile(root *os.Root, name string, limit int64) ([]byte, error) {
	f, e := root.Open(name)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	v, e := f.Stat()
	if e != nil || !v.Mode().IsRegular() || v.Size() > limit {
		return nil, errors.New("父扫描状态文件不符合大小或类型要求")
	}
	return io.ReadAll(io.LimitReader(f, limit+1))
}
func (s *Server) loadParentState(r *http.Request, id int64) (*contracts.SDKParentState, *store.Task, error) {
	t, e := s.Store.Get(r.Context(), id)
	if e != nil || !(t.Status == "completed" || t.Status == "partial") {
		return nil, nil, errors.New("父任务必须是已完成并保留完整结果的扫描")
	}
	archive, e := s.Store.AttemptResult(r.Context(), id, t.Attempt)
	if e != nil || archive.Result == nil {
		return nil, nil, errors.New("父任务没有可验证的执行结果")
	}
	root, e := os.OpenRoot(t.OutputPath)
	if e != nil {
		return nil, nil, errors.New("父扫描产物不可用")
	}
	defer root.Close()
	raw, e := parentFile(root, "normalized/parent-state.json", 16*1024*1024)
	if e != nil {
		return nil, nil, errors.New("该执行没有可用的父扫描状态；需要由新版 Scanner 生成，且状态不超过 8 MiB")
	}
	expected := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(t.Attempt)}
	state, e := contracts.DecodeSDKParentState(raw, &expected)
	if e != nil || state.SourceHash != t.SourceContentHash {
		return nil, nil, errors.New("父扫描状态的版本或执行身份不匹配")
	}
	commitRaw, e := parentFile(root, "normalized/result.commit.json", 1024*1024)
	if e != nil {
		return nil, nil, errors.New("父扫描结果提交标记不可用")
	}
	commit, e := contracts.DecodeResultCommit(commitRaw, &expected)
	if e != nil || string(commit.SHA256) != state.ResultSHA256 {
		return nil, nil, errors.New("父扫描状态与封存结果不匹配")
	}
	result, e := parentFile(root, "normalized/result.json", contracts.MaxDocumentBytes)
	if e != nil {
		return nil, nil, errors.New("父扫描封存结果不可用")
	}
	sum := sha256.Sum256(result)
	if hex.EncodeToString(sum[:]) != state.ResultSHA256 {
		return nil, nil, errors.New("父扫描结果内容已经改变")
	}
	bytes, e := base64.StdEncoding.DecodeString(state.Data)
	if e != nil || len(bytes) > 8*1024*1024 {
		return nil, nil, errors.New("父扫描状态大小不符合要求")
	}
	sum = sha256.Sum256(bytes)
	if hex.EncodeToString(sum[:]) != state.SHA256 {
		return nil, nil, errors.New("父扫描状态内容已经改变")
	}
	return &state, t, nil
}
func (s *Server) scanParentState(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "父扫描任务编号无效")
		return
	}
	state, t, e := s.loadParentState(r, id)
	if e != nil {
		httputil.Error(w, 409, 40900, e.Error())
		return
	}
	httputil.OK(w, map[string]any{"taskId": id, "attempt": t.Attempt, "scanId": state.ScanID, "sourceSnapshotId": t.SourceSnapshotID, "sdkVersion": state.SDKVersion})
}
