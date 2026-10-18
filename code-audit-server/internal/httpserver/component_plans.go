package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

func componentBody(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if e != nil || contracts.CheckJSONText(raw) != nil {
		httputil.Error(w, 400, 40000, "组件计划请求无效或超过 1 MiB")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		httputil.Error(w, 400, 40000, "组件计划字段无效，请检查版本、路径和操作参数")
		return false
	}
	return true
}
func componentError(w http.ResponseWriter, e error) {
	if errors.Is(e, store.ErrComponentConflict) || errors.Is(e, store.ErrComponentInvalid) || errors.Is(e, store.ErrStorageProtected) || errors.Is(e, store.ErrBudgetUnknown) || errors.Is(e, store.ErrBudgetExhausted) || errors.Is(e, store.ErrRecoveryConflict) {
		httputil.Error(w, 409, 40900, e.Error())
		return
	}
	httputil.Error(w, 400, 40000, "组件计划操作未完成："+e.Error())
}
func (s *Server) createComponentPlan(w http.ResponseWriter, r *http.Request, user *store.User) {
	var body struct {
		SnapshotID int64                           `json:"snapshotId"`
		Title      string                          `json:"title"`
		Document   contracts.ComponentPlanDocument `json:"document"`
	}
	if !componentBody(w, r, &body) {
		return
	}
	if body.SnapshotID <= 0 {
		httputil.Error(w, 400, 40000, "请先选择不可变源码快照")
		return
	}
	v, e := s.Store.CreateComponentPlan(r.Context(), body.SnapshotID, body.Title, body.Document, user.ID)
	if e != nil {
		componentError(w, e)
		return
	}
	httputil.OK(w, v)
}
func (s *Server) listComponentPlans(w http.ResponseWriter, r *http.Request, _ *store.User) {
	snapshot, e := strconv.ParseInt(r.URL.Query().Get("snapshotId"), 10, 64)
	page, e2 := componentPage(r)
	if e != nil || snapshot <= 0 || e2 != nil {
		httputil.Error(w, 400, 40000, "组件计划需要有效快照编号与页码")
		return
	}
	ids, total, page, e := s.Store.ComponentPlanIDs(r.Context(), snapshot, page)
	if e != nil {
		componentError(w, e)
		return
	}
	items := []contracts.ComponentPlanSnapshot{}
	for _, id := range ids {
		p, e := s.Store.GetComponentPlan(r.Context(), id)
		if e != nil {
			componentError(w, e)
			return
		}
		items = append(items, p)
	}
	httputil.OK(w, map[string]any{"items": items, "total": total, "page": page, "pageSize": 50})
}
func componentPage(r *http.Request) (int, error) {
	if r.URL.Query().Get("page") == "" {
		return 1, nil
	}
	n, e := strconv.Atoi(r.URL.Query().Get("page"))
	if n < 1 {
		e = errors.New("invalid page")
	}
	return n, e
}
func (s *Server) componentPlan(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "计划编号无效")
		return
	}
	v, e := s.Store.ComponentView(r.Context(), id)
	if e != nil {
		httputil.Error(w, 404, 40400, "组件计划或归档结果不可读取")
		return
	}
	encoded, e := contracts.EncodeJSON(v)
	if e != nil || contracts.ValidateJSON(contracts.KindComponentPlanView, encoded, nil) != nil {
		httputil.Error(w, 503, 50300, "组件计划的状态或执行身份无法确认，请重新读取")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, v)
}
func (s *Server) updateComponentPlan(w http.ResponseWriter, r *http.Request, user *store.User) {
	id, ok := pathID(r, "id")
	var body struct {
		Revision int64                           `json:"revision"`
		Document contracts.ComponentPlanDocument `json:"document"`
	}
	if !ok {
		httputil.Error(w, 400, 40000, "计划编号无效")
		return
	}
	if !componentBody(w, r, &body) {
		return
	}
	v, e := s.Store.SaveComponentDraft(r.Context(), id, body.Revision, body.Document, user.ID, "manual")
	if e != nil {
		componentError(w, e)
		return
	}
	httputil.OK(w, v)
}
func (s *Server) createComponentOperation(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "计划编号无效")
		return
	}
	var body struct {
		SDKIdentityID string `json:"sdkIdentityId"`
		Revision      int64  `json:"revision"`
		Kind          string `json:"kind"`
		ModelConfigID int64  `json:"modelConfigId"`
		Acknowledge   bool   `json:"acknowledgeUnknownCost"`
	}
	if !componentBody(w, r, &body) {
		return
	}
	plan, e := s.Store.GetComponentPlan(r.Context(), id)
	if e != nil {
		httputil.Error(w, 404, 40400, "组件计划不存在")
		return
	}
	if body.Revision != plan.Revision {
		componentError(w, store.ErrComponentConflict)
		return
	}
	snapshot, _ := strconv.ParseInt(string(plan.Source.SnapshotID), 10, 64)
	snap, e := s.Store.GetSnapshot(r.Context(), snapshot)
	if e != nil {
		componentError(w, e)
		return
	}
	t := store.Task{ScanNo: fmt.Sprintf("COMPONENT-%d", time.Now().UnixNano()), SourceSnapshotID: snapshot, SourcePath: snap.SnapshotPath, SourceContentHash: string(plan.Source.ContentHash), ProjectID: snap.ProjectID, Provider: "openai", Model: "not-applicable", ScanMode: "standard", TimeoutMinutes: 5, IncludePaths: "[]", ScanFocus: "[]", ValidationMode: "default", InputContractVersion: contracts.ComponentPlanningInputVersion, ResultContractVersion: 1, ObservationContractVersion: 1, BudgetAccounting: true, ScanInput: &contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}}
	op := contracts.ComponentPlanningOperation{SchemaVersion: 1, Execution: contracts.ExecutionRef{TaskID: "1", Attempt: 1}, PlanID: plan.ID, Revision: plan.Revision, Source: plan.Source}
	switch body.Kind {
	case "normalize":
		if body.Acknowledge || body.ModelConfigID != 0 || body.SDKIdentityID != "" {
			httputil.Error(w, 400, 40000, "本地路径校验不接受模型或未知费用确认")
			return
		}
		op.Kind = "normalize"
		op.Document = &plan.Document
		model := contracts.ResolvedModel{ProviderID: "openai", ConnectionType: "openai", Model: "not-applicable", WireAPI: "responses", CredentialEnv: "OPENAI_API_KEY", ConfigurationRevision: "component-normalization-local-v1"}
		policy := contracts.ScanPolicy{Mode: "standard", TimeBudget: contracts.TimeBudget{ScanSeconds: 300}, IncludePaths: []contracts.RelativePath{}, Focus: []contracts.Focus{}, Validation: contracts.ValidationPolicy{Mode: "default"}}
		raw, _ := contracts.EncodeJSON(model)
		t.ModelSnapshotJSON = string(raw)
		raw, _ = contracts.EncodeJSON(policy)
		t.PolicySnapshotJSON = string(raw)
	case "suggest":
		if !body.Acknowledge || body.ModelConfigID <= 0 {
			httputil.Error(w, 400, 40000, "自动规划会调用模型，SDK 0.1.25 不提供费用回调或上限；请先选择模型并确认未知费用")
			return
		}
		t, e = s.prepareScan(r, createScanRequest{SDKIdentityID: body.SDKIdentityID, SourceSnapshotID: snapshot, ModelConfigID: body.ModelConfigID, ScanMode: "standard", TimeoutMinutes: 15})
		if e != nil {
			componentError(w, e)
			return
		}
		t.GitTarget = nil
		t.InputContractVersion = contracts.ComponentPlanningInputVersion
		t.ScanInput = &contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}
		t.MaxCostUSD = nil
		t.MaxTotalCostUSD = nil
		t.ScanNo = fmt.Sprintf("COMPONENT-%d", time.Now().UnixNano())
		op.Kind = "suggest"
		op.AcknowledgeUnknownCost = true
	default:
		httputil.Error(w, 400, 40000, "组件操作须为 normalize 或 suggest")
		return
	}
	t.ComponentPlanning = &op
	next, e := s.Store.InsertPending(r.Context(), t)
	if e != nil {
		componentError(w, e)
		return
	}
	task, e := s.Store.Get(r.Context(), next)
	if e != nil {
		httputil.Error(w, 503, 50300, "操作已创建，暂时无法读取，请刷新计划")
		return
	}
	task.SourcePath = ""
	task.OutputPath = ""
	httputil.OK(w, task)
}
func (s *Server) applyComponentSuggestion(w http.ResponseWriter, r *http.Request, user *store.User) {
	id, ok := pathID(r, "id")
	var body struct {
		Revision     int64  `json:"revision"`
		TaskID       string `json:"taskId"`
		ResultSHA256 string `json:"resultSha256"`
	}
	if !ok {
		httputil.Error(w, 400, 40000, "计划编号无效")
		return
	}
	if !componentBody(w, r, &body) {
		return
	}
	task, e := strconv.ParseInt(body.TaskID, 10, 64)
	if e != nil || task <= 0 {
		httputil.Error(w, 400, 40000, "建议执行编号无效")
		return
	}
	v, status, e := s.Store.ComponentOperationResult(r.Context(), task)
	if e != nil || v == nil || status != "completed" || v.Error != nil || v.Operation.Kind != "suggest" || v.Operation.PlanID != contracts.TaskID(fmt.Sprint(id)) || v.Operation.Revision != body.Revision || store.ComponentResultDigest(v) != body.ResultSHA256 {
		componentError(w, store.ErrComponentConflict)
		return
	}
	plan, e := s.Store.SaveComponentDraft(r.Context(), id, body.Revision, *v.Document, user.ID, "sdk_suggested")
	if e != nil {
		componentError(w, e)
		return
	}
	httputil.OK(w, plan)
}
func (s *Server) freezeComponentPlan(w http.ResponseWriter, r *http.Request, user *store.User) {
	id, ok := pathID(r, "id")
	var body struct {
		Revision     int64  `json:"revision"`
		TaskID       string `json:"taskId"`
		ResultSHA256 string `json:"resultSha256"`
	}
	if !ok {
		httputil.Error(w, 400, 40000, "计划编号无效")
		return
	}
	if !componentBody(w, r, &body) {
		return
	}
	task, e := strconv.ParseInt(body.TaskID, 10, 64)
	if e != nil || task <= 0 {
		httputil.Error(w, 400, 40000, "校验执行编号无效")
		return
	}
	v, e := s.Store.FreezeComponentPlan(r.Context(), id, body.Revision, task, user.ID, body.ResultSHA256)
	if e != nil {
		componentError(w, e)
		return
	}
	httputil.OK(w, v)
}
func (s *Server) componentVersions(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	page, e := componentPage(r)
	if !ok || e != nil {
		httputil.Error(w, 400, 40000, "计划版本页码无效")
		return
	}
	v, e := s.Store.ComponentVersions(r.Context(), id, page)
	if e != nil {
		componentError(w, e)
		return
	}
	httputil.OK(w, v)
}
func (s *Server) componentVersion(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	version, e := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if !ok || e != nil || version < 1 {
		httputil.Error(w, 400, 40000, "冻结版本编号无效")
		return
	}
	v, e := s.Store.ComponentVersion(r.Context(), id, version)
	if e != nil {
		httputil.Error(w, 404, 40400, "冻结计划版本不存在或完整性校验失败")
		return
	}
	httputil.OK(w, v)
}
