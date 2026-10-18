package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func newSDKPreflight(origin *contracts.ExecutionRef) *contracts.SDKOperation {
	return &contracts.SDKOperation{SchemaVersion: 1, Execution: contracts.ExecutionRef{TaskID: "1", Attempt: 1}, Kind: contracts.SDKOperationKindPreflight, Origin: origin}
}
func (s *Server) createSDKOperation(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "任务编号无效")
		return
	}
	var body struct {
		Kind          string `json:"kind"`
		FindingID     string `json:"findingId"`
		OriginAttempt int64  `json:"originAttempt"`
		Acknowledge   bool   `json:"acknowledgeUnknownCost"`
	}
	if !smallJSON(w, r, &body) {
		return
	}
	source, e := s.Store.Get(r.Context(), id)
	if e != nil {
		httputil.Error(w, 404, 40400, "来源扫描不存在")
		return
	}
	if (source.SDKOperation != nil || source.ComponentPlanning != nil) || source.SourceSnapshotID <= 0 || source.InputContractVersion == 0 {
		httputil.Error(w, 409, 40900, "SDK 操作需要具有冻结配置的源码扫描")
		return
	}
	var origin *contracts.ExecutionRef
	if source.Attempt > 0 {
		origin = &contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(source.Attempt)}
	}
	op := newSDKPreflight(origin)
	t := *source
	switch body.Kind {
	case "preflight":
		if source.ComponentScan != nil {
			httputil.Error(w, 409, 40900, "组件计划使用独立组件执行；请从具体组件结果检查预检依据")
			return
		}
		if body.FindingID != "" || body.Acknowledge {
			httputil.Error(w, 400, 40000, "预检不接受漏洞或未知费用确认")
			return
		}
		if source.ScanInput != nil && source.ScanInput.ParentScanID.Present {
			t.ParentState, e = s.Store.TaskParentState(r.Context(), id)
			if e != nil {
				httputil.Error(w, 409, 40900, "冻结的父扫描状态不可用")
				return
			}
		}
	case "validate":
		if !body.Acknowledge {
			httputil.Error(w, 400, 40000, "独立复核费用未知且不受扫描费用上限控制，请先在 Web 确认")
			return
		}
		if origin == nil || body.OriginAttempt != origin.Attempt || !store.IsTerminalStatus(source.Status) {
			httputil.Error(w, 409, 40900, "请刷新并选择已结束的来源执行及其漏洞")
			return
		}
		finding, e := s.Store.FrozenFinding(r.Context(), id, body.FindingID)
		if e != nil {
			httputil.Error(w, 404, 40400, "选定漏洞不属于此扫描或证据不可用")
			return
		}
		op.Kind = contracts.SDKOperationKindValidate
		op.FindingID = &body.FindingID
		op.Finding = finding
		op.AcknowledgeUnknownCost = true
		// ValidationOptions supports repository + finding, not scan target/prompts/budget.
		// Preserve the exact source/model and explicitly use the entire source snapshot.
		p := contracts.ScanPolicy{Mode: contracts.ScanModeStandard, TimeBudget: contracts.TimeBudget{ScanSeconds: int64(min(source.TimeoutMinutes, 60)) * 60}, IncludePaths: []contracts.RelativePath{}, Focus: []contracts.Focus{}, Validation: contracts.ValidationPolicy{Mode: contracts.ValidationModeDefault}}
		raw, _ := contracts.EncodeJSON(p)
		t.PolicySnapshotJSON = string(raw)
		t.ScanMode = "standard"
		t.TimeoutMinutes = int(p.TimeBudget.ScanSeconds / 60)
		t.ValidationMode = "default"
		t.BusinessContext = ""
		t.IncludePaths = "[]"
		t.ScanFocus = "[]"
		t.Workers = nil
		t.MaxCostUSD = nil
		t.DeepMaxDiscoveryRuns = 0
		t.DeepStopAfterNoNew = 0
		t.ScanInput = nil
		t.ParentState = nil
	default:
		httputil.Error(w, 400, 40000, "SDK 操作须为 preflight 或 validate")
		return
	}
	t.ComponentScan = nil
	t.ID = 0
	t.SDKOperation = op
	t.SDKOperationOrigin = id
	t.MaxTotalCostUSD = nil
	t.BudgetAccounting = true
	t.ScanNo = fmt.Sprintf("SDK-%s-%s", body.Kind, time.Now().UTC().Format("20060102150405.000000000"))
	if t.ScanInput == nil {
		t.ScanInput = &contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}
	}
	t.InputContractVersion = contracts.SDKOperationInputVersion
	if t.GitTarget != nil {
		t.InputContractVersion = contracts.GitDiffInputVersion
	}
	next, e := s.Store.InsertPending(r.Context(), t)
	if e != nil {
		httputil.Error(w, 409, 40900, "SDK 操作创建失败："+e.Error())
		return
	}
	task, e := s.Store.Get(r.Context(), next)
	if e != nil {
		httputil.Error(w, 503, 50300, "操作已创建，暂时无法读取")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, task)
}
func (s *Server) listSDKOperations(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "任务编号无效")
		return
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 {
			httputil.Error(w, 400, 40000, "SDK 操作页码无效")
			return
		}
		page = n
	}
	values, e := s.Store.SDKOperations(r.Context(), id, page)
	if e != nil {
		httputil.Error(w, 503, 50300, "SDK 操作历史暂不可用")
		return
	}
	for i := range values.Items {
		values.Items[i].SourcePath = ""
		values.Items[i].OutputPath = ""
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, values)
}
