package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"fmt"
	"net/http"
	"strconv"
)

func (s *Server) createComponentScan(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "组件计划编号无效")
		return
	}
	var body struct {
		KnowledgeVersionIDs []int64 `json:"knowledgeVersionIds"`
		ScanPrompt          string  `json:"scanPrompt"`
		PostScanPrompt      string  `json:"postScanPrompt"`
		SDKIdentityID       string  `json:"sdkIdentityId"`
		Version             int64   `json:"version"`
		VersionSHA256       string  `json:"versionSha256"`
		ModelConfigID       int64   `json:"modelConfigId"`
		Workers             int     `json:"workers"`
		TimeoutMinutes      int     `json:"timeoutMinutes"`
		MaxCostUSD          float64 `json:"maxCostUsd"`
		MaxTotalCostUSD     float64 `json:"maxTotalCostUsd"`
		ComponentCostUSD    float64 `json:"componentCostUsd"`
		Acknowledge         bool    `json:"acknowledgeMatchingCost"`
	}
	if !componentBody(w, r, &body) {
		return
	}
	if !body.Acknowledge || body.ModelConfigID < 1 || body.Workers < 1 || int64(body.Workers) > contracts.SDKMaxSafeInteger {
		httputil.Error(w, 400, 40000, "请选择模型，并填写正安全整数的并发组件数；同时确认跨组件匹配费用未知且不受组件扫描预算限制")
		return
	}
	component, e := store.BudgetLimitMicros(body.ComponentCostUSD)
	if e != nil {
		componentError(w, e)
		return
	}
	grant, e := store.BudgetLimitMicros(body.MaxCostUSD)
	if e != nil {
		componentError(w, e)
		return
	}
	total, e := store.BudgetLimitMicros(body.MaxTotalCostUSD)
	if e != nil {
		componentError(w, e)
		return
	}
	grant = min(grant, total)
	version, e := s.Store.ComponentVersion(r.Context(), id, body.Version)
	if e != nil || string(version.SHA256) != body.VersionSHA256 {
		componentError(w, store.ErrComponentConflict)
		return
	}
	snapshot, _ := strconv.ParseInt(string(version.Source.SnapshotID), 10, 64)
	t, e := s.prepareScan(r, createScanRequest{SDKIdentityID: body.SDKIdentityID, SourceSnapshotID: snapshot, ModelConfigID: body.ModelConfigID, ScanMode: "standard", TimeoutMinutes: body.TimeoutMinutes, MaxCostUSD: &body.MaxCostUSD, MaxTotalCostUSD: &body.MaxTotalCostUSD})
	if e != nil {
		componentError(w, e)
		return
	}
	if len(body.KnowledgeVersionIDs) > 0 || body.ScanPrompt != "" || body.PostScanPrompt != "" {
		docs, e := s.Store.ResolveKnowledge(r.Context(), body.KnowledgeVersionIDs)
		if e != nil {
			componentError(w, e)
			return
		}
		input := contracts.ComponentSDKInput{SchemaVersion: 1, Knowledge: docs}
		if body.ScanPrompt != "" {
			input.ScanPrompt = contracts.Some(body.ScanPrompt)
		}
		if body.PostScanPrompt != "" {
			input.PostScanPrompt = contracts.Some(body.PostScanPrompt)
		}
		if t.SDKParameters == nil {
			t.SDKParameters = &contracts.SDKScanParameters{SchemaVersion: 1}
		}
		t.SDKParameters.ComponentInput = contracts.Some(input)
	}
	t.GitTarget = nil
	t.InputContractVersion = contracts.ComponentScanInputVersion
	t.ScanInput = &contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}
	t.ScanNo = "COMPONENT-SCAN-" + t.ScanNo
	request := contracts.ComponentScanRequest{SchemaVersion: 1, Execution: contracts.ExecutionRef{TaskID: "1", Attempt: 1}, PlanVersion: version, Workers: int64(body.Workers), ComponentLimitMicros: component, GrantMicros: grant, AcknowledgeMatchingCost: true, SelectedIndices: []int64{}}
	for i := range version.Document.Components {
		request.SelectedIndices = append(request.SelectedIndices, int64(i))
	}
	t.ComponentScan = &request
	next, e := s.Store.InsertPending(r.Context(), t)
	if e != nil {
		componentError(w, e)
		return
	}
	httputil.OK(w, map[string]any{"id": next})
}
func (s *Server) componentExecutions(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "任务编号无效")
		return
	}
	t, e := s.Store.Get(r.Context(), id)
	if e != nil || t.ComponentScan == nil {
		httputil.Error(w, 404, 40400, "此任务没有组件执行记录")
		return
	}
	items, e := s.Store.ComponentAttempts(r.Context(), id)
	if e != nil {
		componentError(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, map[string]any{"request": t.ComponentScan, "attempts": items})
}
func (s *Server) retryComponentScan(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "任务编号无效")
		return
	}
	var body struct {
		Attempt        int     `json:"attempt"`
		ExpectedStatus string  `json:"expectedStatus"`
		Indices        []int64 `json:"indices"`
		Acknowledge    bool    `json:"acknowledgeReexecution"`
	}
	if !componentBody(w, r, &body) {
		return
	}
	if !body.Acknowledge || body.Attempt < 0 || !store.IsTerminalStatus(body.ExpectedStatus) {
		httputil.Error(w, 400, 40000, "请选择已结束执行中的失败组件，并确认重新调用模型及未知匹配费用")
		return
	}
	changed, e := s.Store.RetryComponents(r.Context(), id, body.Attempt, body.ExpectedStatus, body.Indices)
	if e != nil {
		componentError(w, e)
		return
	}
	if !changed {
		componentError(w, store.ErrComponentConflict)
		return
	}
	httputil.OK(w, map[string]any{"id": id, "status": "pending", "message": fmt.Sprintf("已为 %d 个组件创建新的执行排队请求", len(body.Indices))})
}
