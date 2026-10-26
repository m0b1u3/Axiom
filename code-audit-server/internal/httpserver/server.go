package httpserver

import (
	"axiom/contracts"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/providers"
	"axiom/code-audit-server/internal/render"
	"axiom/code-audit-server/internal/report"
	"axiom/code-audit-server/internal/snapshot"
	"axiom/code-audit-server/internal/store"
)

type Server struct {
	StorageSpace      func(string) (int64, int64, error)
	StoragePurge      func(context.Context, string, string) error
	StorageCheckpoint func(string) error

	LegacyPasswordSecret string
	security             securityState
	AdminUsername        string
	AgentToken           string
	QueueLimits          store.ClaimLimits
	Store                *store.Store
	Secret               string
	SnapshotDir          string
	Limits               snapshot.Limits
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/diagnostics/structured-output", s.withAuth(s.structuredDiagnosticCatalog))
	mux.HandleFunc("GET /api/v1/diagnostics/structured-output/records", s.withAuth(s.listStructuredDiagnostics))
	mux.HandleFunc("POST /api/v1/diagnostics/structured-output/records", s.withAuth(s.createStructuredDiagnostic))
	mux.HandleFunc("GET /api/v1/sdk-identities", s.withAuth(s.listSDKIdentities))
	mux.HandleFunc("POST /api/v1/sdk-identities", s.withAuth(s.createSDKIdentity))
	mux.HandleFunc("GET /api/v1/sdk-identities/{id}/operations", s.withAuth(s.sdkIdentityOperations))
	mux.HandleFunc("POST /api/v1/sdk-identities/{id}/operations", s.withAuth(s.startSDKIdentityOperation))
	mux.HandleFunc("POST /api/v1/sdk-identity-operations/{id}/cancel", s.withAuth(s.cancelSDKIdentityOperation))
	mux.HandleFunc("POST /api/internal/sdk-identities/claim", s.agentOnly(s.claimSDKIdentity))
	mux.HandleFunc("POST /api/internal/sdk-identities/report", s.agentOnly(s.reportSDKIdentity))
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/v1/storage", s.withAuth(s.readStorage))
	mux.HandleFunc("PUT /api/v1/storage/policy", s.withAuth(s.updateStorage))
	mux.HandleFunc("GET /api/v1/storage/cleanup", s.withAuth(s.listCleanup))
	mux.HandleFunc("POST /api/v1/storage/cleanup", s.withAuth(s.previewCleanup))
	mux.HandleFunc("GET /api/v1/storage/cleanup/{job}", s.withAuth(s.readCleanup))
	mux.HandleFunc("POST /api/v1/storage/cleanup/{job}/execute", s.withAuth(s.executeCleanup))
	mux.HandleFunc("POST /api/v1/storage/cleanup/{job}/cancel", s.withAuth(s.cancelCleanup))
	mux.HandleFunc("GET /api/internal/executions/describe", s.agentOnly(s.describeExecutions))
	mux.HandleFunc("POST /api/internal/executions/runtime", s.agentOnly(s.reportAgentRuntime))
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("POST /api/internal/v3/executions/presence", s.agentOnly(s.reportContractAgent))
	mux.HandleFunc("POST /api/internal/v4/executions/claim", s.agentOnly(s.claimObservationExecution))
	mux.HandleFunc("POST /api/internal/v4/executions/presence", s.agentOnly(s.reportObservationAgent))
	mux.HandleFunc("POST /api/internal/v4/executions/heartbeat", s.agentOnly(s.heartbeatObservationExecution))
	mux.HandleFunc("POST /api/internal/v3/executions/claim", s.agentOnly(s.claimContractExecution))
	mux.HandleFunc("POST /api/internal/v3/executions/complete", s.agentOnly(s.completeContractExecution))
	mux.HandleFunc("POST /api/internal/v2/executions/presence", s.agentOnly(s.reportInputAgent))
	mux.HandleFunc("POST /api/internal/v2/executions/claim", s.agentOnly(s.claimInputExecution))
	mux.HandleFunc("POST /api/internal/executions/presence", s.agentOnly(s.reportAgent))
	mux.HandleFunc("GET /api/v1/executions/overview", s.withAuth(s.queueOverview))
	mux.HandleFunc("GET /api/v1/settings/execution", s.withAuth(s.queueSettings))
	mux.HandleFunc("PUT /api/v1/settings/execution", s.withAuth(s.saveQueueSettings))
	mux.HandleFunc("GET /api/v1/scans/{id}/coverage", s.withAuth(s.scanCoverage))
	mux.HandleFunc("POST /api/v1/scans/{id}/interim-reviews", s.withAuth(s.createInterimReview))
	mux.HandleFunc("GET /api/v1/scans/{id}/attempts/{attempt}/interim-reviews", s.withAuth(s.interimReviews))
	mux.HandleFunc("GET /api/v1/interim-review-groups/{group}", s.withAuth(s.interimReviewView))
	mux.HandleFunc("POST /api/v1/interim-review-groups/{group}/retry", s.withAuth(s.retryInterimReview))
	mux.HandleFunc("POST /api/v1/interim-review-groups/{group}/archive", s.withAuth(s.archiveInterimReview))
	mux.HandleFunc("GET /api/v1/findings/interim", s.withAuth(s.interimExecutionCatalog))
	mux.HandleFunc("GET /api/v1/scans/{id}/attempts/{attempt}/interim", s.withAuth(s.interimFindings))
	mux.HandleFunc("GET /api/v1/scans/{id}/attempts/{attempt}/interim/{artifact}", s.withAuth(s.interimEvidence))
	mux.HandleFunc("GET /api/v1/scans/{id}/attempts/{attempt}/sdk-sessions", s.withAuth(s.sdkSessions))
	mux.HandleFunc("GET /api/v1/scans/{id}/attempts/{attempt}/result", s.withAuth(s.attemptResult))
	mux.HandleFunc("GET /api/v1/scans/{id}/attempts/{attempt}/evidence-graph", s.withAuth(s.findingEvidenceGraph))
	mux.HandleFunc("GET /api/v1/scans/{id}/attempts/{attempt}/artifacts", s.withAuth(s.resultArtifact))
	mux.HandleFunc("GET /api/v1/scans/{id}/attempts/{attempt}/sdk-cost-estimate", s.withAuth(s.sdkCostEstimate))
	mux.HandleFunc("GET /api/v1/scans/{id}/attempts/{attempt}/sdk-history-links", s.withAuth(s.sdkHistoryLinks))
	mux.HandleFunc("POST /api/internal/executions/claim", s.agentOnly(s.claimExecution))
	mux.HandleFunc("POST /api/internal/executions/heartbeat", s.agentOnly(s.heartbeatExecution))
	mux.HandleFunc("POST /api/internal/executions/complete", s.agentOnly(s.completeExecution))
	mux.HandleFunc("GET /api/v1/component-plans", s.withAuth(s.listComponentPlans))
	mux.HandleFunc("POST /api/v1/component-plans", s.withAuth(s.createComponentPlan))
	mux.HandleFunc("GET /api/v1/component-plans/{id}", s.withAuth(s.componentPlan))
	mux.HandleFunc("PUT /api/v1/component-plans/{id}", s.withAuth(s.updateComponentPlan))
	mux.HandleFunc("POST /api/v1/component-plans/{id}/scans", s.withAuth(s.createComponentScan))
	mux.HandleFunc("GET /api/v1/scans/{id}/component-executions", s.withAuth(s.componentExecutions))
	mux.HandleFunc("POST /api/v1/scans/{id}/component-executions/retry", s.withAuth(s.retryComponentScan))
	mux.HandleFunc("POST /api/v1/component-plans/{id}/operations", s.withAuth(s.createComponentOperation))
	mux.HandleFunc("POST /api/v1/component-plans/{id}/apply-suggestion", s.withAuth(s.applyComponentSuggestion))
	mux.HandleFunc("POST /api/v1/component-plans/{id}/freeze", s.withAuth(s.freezeComponentPlan))
	mux.HandleFunc("GET /api/v1/component-plans/{id}/versions", s.withAuth(s.componentVersions))
	mux.HandleFunc("GET /api/v1/component-plans/{id}/versions/{version}", s.withAuth(s.componentVersion))
	mux.HandleFunc("POST /api/v1/scans/{id}/sdk-operations", s.withAuth(s.createSDKOperation))
	mux.HandleFunc("GET /api/v1/scans/{id}/sdk-operations", s.withAuth(s.listSDKOperations))
	mux.HandleFunc("GET /api/v1/scans/{id}/attempts", s.withAuth(s.scanAttempts))
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("GET /api/v1/auth/me", s.withAuth(s.me))
	mux.HandleFunc("POST /api/v1/auth/logout", s.withAuth(s.logout))
	mux.HandleFunc("GET /api/v1/auth/sessions", s.withAuth(s.sessions))
	mux.HandleFunc("DELETE /api/v1/auth/sessions/{sessionId}", s.withAuth(s.revokeSession))
	mux.HandleFunc("GET /api/v1/users", s.withAuth(s.accounts))
	mux.HandleFunc("POST /api/v1/users", s.withAuth(s.createAccount))
	mux.HandleFunc("PUT /api/v1/users/{id}", s.withAuth(s.updateAccount))
	mux.HandleFunc("GET /api/v1/operations", s.withAuth(s.operations))
	mux.HandleFunc("PUT /api/v1/user/password", s.withAuth(s.changePassword))
	mux.HandleFunc("GET /api/v1/projects", s.withAuth(s.listProjects))
	mux.HandleFunc("POST /api/v1/projects", s.withAuth(s.createProject))
	mux.HandleFunc("GET /api/v1/projects/{id}", s.withAuth(s.getProject))
	mux.HandleFunc("DELETE /api/v1/projects/{id}", s.withAuth(s.deleteProject))
	mux.HandleFunc("POST /api/v1/scans", s.withAuth(s.createScan))
	mux.HandleFunc("GET /api/v1/scans", s.withAuth(s.listScans))
	mux.HandleFunc("GET /api/v1/scans/{id}", s.withAuth(s.getScan))
	mux.HandleFunc("GET /api/v1/scans/{id}/deep-resume", s.withAuth(s.deepResumeSnapshot))
	mux.HandleFunc("POST /api/v1/scans/{id}/deep-resume/check", s.withAuth(s.deepResumeCheck))
	mux.HandleFunc("POST /api/v1/scans/{id}/deep-resume", s.withAuth(s.resumeDeep))
	mux.HandleFunc("POST /api/internal/deep-resume/claim", s.agentOnly(s.claimDeepResumeCheck))
	mux.HandleFunc("POST /api/internal/deep-resume/report", s.agentOnly(s.reportDeepResumeCheck))
	mux.HandleFunc("GET /api/v1/scans/{id}/parent-state", s.withAuth(s.scanParentState))
	mux.HandleFunc("GET /api/v1/scans/{id}/budget", s.withAuth(s.taskBudget))
	mux.HandleFunc("PUT /api/v1/scans/{id}/budget", s.withAuth(s.updateTaskBudget))
	mux.HandleFunc("PUT /api/v1/scans/{id}/budget/attempts/{attempt}", s.withAuth(s.reconcileTaskCost))
	mux.HandleFunc("GET /api/v1/scans/{id}/findings", s.withAuth(s.listFindings))
	mux.HandleFunc("GET /api/v1/scans/{id}/logs", s.withAuth(s.scanLogs))
	mux.HandleFunc("GET /api/v1/scans/{id}/checkpoints", s.withAuth(s.scanCheckpoints))
	mux.HandleFunc("GET /api/v1/scans/{id}/report", s.withAuth(s.exportReport))
	mux.HandleFunc("GET /api/v1/scans/{id}/report.pdf", s.withAuth(s.exportReportPDF))
	mux.HandleFunc("POST /api/v1/scans/{id}/cancel", s.withAuth(s.cancelScan))
	mux.HandleFunc("POST /api/v1/scans/{id}/retry", s.withAuth(s.retryScan))
	mux.HandleFunc("GET /api/v1/scans/{id}/recovery", s.withAuth(s.scanRecovery))
	mux.HandleFunc("POST /api/v1/scans/{id}/recovery", s.withAuth(s.recoverScan))
	mux.HandleFunc("DELETE /api/v1/scans/{id}", s.withAuth(s.deleteScan))
	mux.HandleFunc("POST /api/v1/git-snapshots", s.withAuth(s.uploadGitSnapshot))
	mux.HandleFunc("GET /api/v1/snapshots/{id}/git-diff", s.withAuth(s.gitDiffPreview))
	mux.HandleFunc("GET /api/v1/snapshots/{id}/git-diff/files/{file}", s.withAuth(s.gitDiffContent))
	mux.HandleFunc("POST /api/v1/uploads", s.withAuth(s.uploadSnapshot))
	mux.HandleFunc("GET /api/v1/knowledge", s.withAuth(s.listKnowledge))
	mux.HandleFunc("GET /api/v1/knowledge/versions/{id}", s.withAuth(s.knowledgeVersion))
	mux.HandleFunc("POST /api/v1/knowledge", s.withAuth(s.saveKnowledge))
	mux.HandleFunc("POST /api/v1/knowledge/{id}/versions", s.withAuth(s.saveKnowledge))
	mux.HandleFunc("PUT /api/v1/knowledge/{id}/archive", s.withAuth(s.archiveKnowledge))
	mux.HandleFunc("GET /api/v1/snapshots", s.withAuth(s.listSnapshots))
	mux.HandleFunc("DELETE /api/v1/snapshots/{id}", s.withAuth(s.deleteSnapshot))
	mux.HandleFunc("PUT /api/v1/snapshots/{id}/project", s.withAuth(s.assignSnapshotProject))
	mux.HandleFunc("PUT /api/v1/findings/{id}/status", s.withAuth(s.setFindingStatus))
	mux.HandleFunc("GET /api/v1/findings", s.withAuth(s.listFindingSummaries))
	mux.HandleFunc("GET /api/v1/scans/{id}/compare", s.withAuth(s.compareScan))
	mux.HandleFunc("GET /api/v1/dashboard", s.withAuth(s.dashboard))
	mux.HandleFunc("GET /api/v1/model-configs", s.withAuth(s.listModelConfigs))
	mux.HandleFunc("GET /api/v1/model-providers", s.withAuth(s.listModelProviders))
	mux.HandleFunc("POST /api/v1/model-configs", s.withAuth(s.createModelConfig))
	mux.HandleFunc("POST /api/v1/model-configs/fetch-models", s.withAuth(s.fetchProviderModels))
	mux.HandleFunc("PUT /api/v1/model-configs/{id}", s.withAuth(s.updateModelConfig))
	mux.HandleFunc("PUT /api/v1/model-configs/{id}/enable", s.withAuth(s.enableModelConfig))
	mux.HandleFunc("DELETE /api/v1/model-configs/{id}", s.withAuth(s.deleteModelConfig))
	mux.HandleFunc("POST /api/v1/model-configs/{id}/test", s.withAuth(s.testModelConfig))
	return mux
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request, _ *store.User) {
	items, err := s.Store.ListProjects(r.Context())
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	if items == nil {
		items = []store.Project{}
	}
	httputil.OK(w, items)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request, user *store.User) {
	var body struct {
		Name string `json:"name"`
	}
	if err := httputil.Decode(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		httputil.Error(w, http.StatusBadRequest, 40000, "name required")
		return
	}
	p, err := s.Store.CreateProject(r.Context(), strings.TrimSpace(body.Name), user.ID)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	httputil.OK(w, p)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	if err := s.Store.DeleteProject(r.Context(), id); err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	httputil.OK(w, map[string]any{"id": id})
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	p, err := s.Store.GetProject(r.Context(), id)
	if err != nil {
		httputil.Error(w, http.StatusNotFound, 40001, "project not found")
		return
	}
	httputil.OK(w, p)
}

func (s *Server) createScan(w http.ResponseWriter, r *http.Request, _ *store.User) {
	body, err := decodeScanRequest(w, r)
	if err != nil {
		httputil.Error(w, 400, 40000, err.Error())
		return
	}
	input, err := s.prepareScan(r, body)
	if err != nil {
		httputil.Error(w, 400, 40000, err.Error())
		return
	}
	if body.Operation == "preflight" {
		if input.SourceSnapshotID <= 0 {
			httputil.Error(w, 400, 40000, "SDK 预检需要先上传不可变代码快照")
			return
		}
		input.SDKOperation = newSDKPreflight(nil)
		input.ScanNo = "PREFLIGHT-" + strings.TrimPrefix(input.ScanNo, "SCAN-")
		input.MaxTotalCostUSD = nil
		input.InputContractVersion = contracts.SDKOperationInputVersion
		if input.GitTarget != nil {
			input.InputContractVersion = contracts.GitDiffInputVersion
		}
		if input.ScanInput == nil {
			input.ScanInput = &contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}
		}
	}
	id, err := s.Store.InsertPending(r.Context(), input)
	if err != nil {
		httputil.Error(w, 500, 50000, err.Error())
		return
	}
	task, err := s.Store.Get(r.Context(), id)
	if err != nil {
		httputil.Error(w, 500, 50000, err.Error())
		return
	}
	httputil.OK(w, task)
}

// queryInt64 读取可选整数 query 参数（>0 生效）。
func queryInt64(r *http.Request, key string) int64 {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (s *Server) listScans(w http.ResponseWriter, r *http.Request, _ *store.User) {
	if r.URL.Query().Has("page") {
		page, e1 := strconv.Atoi(r.URL.Query().Get("page"))
		size, e2 := strconv.Atoi(r.URL.Query().Get("pageSize"))
		if e1 != nil || e2 != nil || page < 1 || size < 1 || size > 100 {
			httputil.Error(w, 400, 40000, "invalid pagination")
			return
		}
		result, err := s.Store.ListScansPage(r.Context(), queryInt64(r, "projectId"), page, size, r.URL.Query().Get("keyword"), r.URL.Query().Get("status"))
		if err != nil {
			httputil.Error(w, 500, 50000, err.Error())
			return
		}
		httputil.OK(w, result)
		return
	}

	items, err := s.Store.ListScans(r.Context(), queryInt64(r, "projectId"))
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	if items == nil {
		items = []store.Task{}
	}
	for i := range items {
		enrichLiveProgress(&items[i])
	}
	httputil.OK(w, items)
}

func (s *Server) getScan(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	task, err := s.Store.Get(r.Context(), id)
	if err != nil {
		httputil.Error(w, http.StatusNotFound, 40002, "scan not found")
		return
	}
	enrichLiveProgress(task)
	httputil.OK(w, task)
}

func (s *Server) listFindings(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	items, err := s.Store.ListFindings(r.Context(), id)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	if items == nil {
		items = []store.Finding{}
	}
	httputil.OK(w, items)
}

func (s *Server) uploadSnapshot(w http.ResponseWriter, r *http.Request, u *store.User) {
	if e := s.Store.StorageLock(r.Context(), false, func() error { s.uploadSnapshotData(w, r, u); return nil }); e != nil {
		s.storageError(w, e)
	}
}
func (s *Server) uploadSnapshotData(w http.ResponseWriter, r *http.Request, _ *store.User) {
	limits := s.Limits
	if limits.MaxArchiveBytes == 0 {
		limits = snapshot.DefaultLimits()
	}
	r.Body = http.MaxBytesReader(w, r.Body, limits.MaxArchiveBytes+1<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httputil.Error(w, http.StatusBadRequest, 40000, "multipart form error: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, 40000, "missing file field")
		return
	}
	defer file.Close()
	if !snapshot.IsSupportedArchive(header.Filename) {
		httputil.Error(w, http.StatusBadRequest, 40000, "unsupported archive: only .zip / .tar.gz / .tgz")
		return
	}

	tmpPath, sha, _, err := snapshot.HashArchiveIn(file, limits, filepath.Join(s.SnapshotDir, ".uploads"))
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, 40000, "archive rejected: "+err.Error())
		return
	}
	defer os.Remove(tmpPath)

	// 可选项目归属：上传即归入项目（代码资产统一管理）
	var projectID int64
	if pv := strings.TrimSpace(r.FormValue("projectId")); pv != "" {
		pid, perr := strconv.ParseInt(pv, 10, 64)
		if perr != nil || pid <= 0 {
			httputil.Error(w, http.StatusBadRequest, 40000, "invalid projectId")
			return
		}
		if _, gerr := s.Store.GetProject(r.Context(), pid); gerr != nil {
			httputil.Error(w, http.StatusBadRequest, 40001, "project not found")
			return
		}
		projectID = pid
	}

	// 同内容去重：已入库的归档直接复用（不可变快照）；未挂项目的补挂
	if existing, err := s.Store.GetSnapshotByArchiveSha256(r.Context(), sha); err == nil {
		if projectID > 0 && existing.ProjectID == 0 {
			if row, ierr := s.Store.InsertSnapshot(r.Context(), store.SnapshotRow{
				ArchiveSha256: sha,
				ProjectID:     projectID,
			}); ierr == nil {
				httputil.OK(w, row)
				return
			}
		}
		httputil.OK(w, existing)
		return
	}

	dir := filepath.Join(s.SnapshotDir, "snap-"+sha[:16])
	res, err := snapshot.ExtractArchive(tmpPath, header.Filename, dir, limits)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, 40000, "extract rejected: "+err.Error())
		return
	}
	row, err := s.Store.InsertSnapshot(r.Context(), store.SnapshotRow{
		OriginalFilename: header.Filename,
		ArchiveSha256:    sha,
		ContentHash:      res.ContentHash,
		SnapshotPath:     res.SnapshotPath,
		FileCount:        res.FileCount,
		TotalSize:        res.TotalSize,
		ProjectID:        projectID,
	})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	httputil.OK(w, row)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request, _ *store.User) {
	ctx := r.Context()
	stats, err := s.Store.DashboardStats(ctx)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	scans, err := s.Store.ListScans(ctx, 0)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	ordinary := []store.Task{}
	for _, task := range scans {
		if task.SDKOperationKind == "" {
			ordinary = append(ordinary, task)
		}
	}
	scans = ordinary
	recent := scans
	if len(recent) > 5 {
		recent = recent[:5]
	}
	if recent == nil {
		// nil slice 序列化为 null,前端按数组消费——保证空态返回 []
		recent = []store.Task{}
	}
	// 最近一次可与历史对比的扫描 → 增量（-1 表示暂时无法对比）
	latestDelta := int64(-1)
	for i := range scans {
		cmp, err := s.buildCompare(ctx, &scans[i])
		if err != nil {
			continue
		}
		if cmp.PrevScanID > 0 {
			latestDelta = int64(len(cmp.Added))
			break
		}
	}

	// 最近记录表：每条扫描附严重度计数与耗时
	ids := make([]int64, len(recent))
	for i, t := range recent {
		ids[i] = t.ID
	}
	sevMap, err := s.Store.ScanSeverityMap(ctx, ids)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	type dashScan struct {
		store.Task
		Sev         store.SeverityCounts `json:"sev"`
		DurationSec int64                `json:"durationSec"`
	}
	recentOut := make([]dashScan, 0, len(recent))
	for _, t := range recent {
		recentOut = append(recentOut, dashScan{Task: t, Sev: sevMap[t.ID], DurationSec: scanDurationSec(&t)})
	}

	// 最近一次扫描面板 + 「较上次扫描」增量
	pair, err := s.Store.TerminalPair(ctx)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	pairIDs := make([]int64, len(pair))
	for i, p := range pair {
		pairIDs[i] = p.ID
	}
	pairSev, err := s.Store.ScanSeverityMap(ctx, pairIDs)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	var latest any
	var deltas map[string]any
	if len(pair) > 0 {
		cur := pair[0]
		// 完整任务行（状态/快照路径/时间）从 recent 之外也可能取不到，直接查
		if full, err := s.Store.Get(ctx, cur.ID); err == nil {
			latest = dashScan{Task: *full, Sev: pairSev[cur.ID], DurationSec: cur.DurationSec}
		}
		deltas = map[string]any{}
		if len(pair) > 1 {
			prev := pair[1]
			deltas["findings"] = int64(cur.FindingCount) - int64(prev.FindingCount)
			deltas["critical"] = pairSev[cur.ID].Critical - pairSev[prev.ID].Critical
			if prev.DurationSec > 0 {
				deltas["durationPct"] = float64(cur.DurationSec-prev.DurationSec) / float64(prev.DurationSec) * 100
			}
		}
	}

	trend, err := s.Store.DashboardTrend(ctx, 7)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	if trend == nil {
		trend = []store.TrendPoint{}
	}
	projectRisk, err := s.Store.DashboardProjectRisk(ctx)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}

	// 扫描引擎面板：最近一次终态扫描所用引擎 + 使用统计
	var engine any
	if len(pair) > 0 {
		cur := pair[0]
		var useCount int64
		useCount, _ = s.Store.CountScansByModel(ctx, cur.Provider, cur.Model)
		engine = map[string]any{
			"provider":       cur.Provider,
			"model":          cur.Model,
			"baseUrl":        cur.BaseURL,
			"avgDurationSec": stats.AvgDurationSec,
			"durationSec":    cur.DurationSec,
			"scanCount":      useCount,
		}
	}

	httputil.OK(w, map[string]any{
		"stats":       stats,
		"recent":      recentOut,
		"latestDelta": latestDelta,
		"latest":      latest,
		"deltas":      deltas,
		"trend":       trend,
		"projectRisk": projectRisk,
		"engine":      engine,
	})
}

// scanDurationSec 由 started/finished 计算耗时（秒）；缺任一端返回 0。
func scanDurationSec(t *store.Task) int64 {
	if t.StartedAt == "" || t.FinishedAt == "" {
		return 0
	}
	st, err1 := time.Parse(time.RFC3339, t.StartedAt)
	fi, err2 := time.Parse(time.RFC3339, t.FinishedAt)
	if err1 != nil || err2 != nil || !fi.After(st) {
		return 0
	}
	return int64(fi.Sub(st).Seconds())
}

func (s *Server) listModelConfigs(w http.ResponseWriter, r *http.Request, _ *store.User) {
	items, err := s.Store.ListModelConfigs(r.Context())
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	if items == nil {
		items = []store.ModelConfigRow{}
	}
	// 编辑视图需要看到「Key 是否已配置」：填充掩码提示（sk-…尾 4 位），密文不出进程。
	for i := range items {
		if items[i].ApiKeyEncrypted == "" {
			items[i].SDKIdentityOnly = true
			continue
		}
		if key, err := cryptokey.Decrypt(items[i].ApiKeyEncrypted); err == nil {
			items[i].ApiKeyHint = maskAPIKey(key)
		} else {
			items[i].ApiKeyHint = "（密钥无法解密）"
		}
	}
	httputil.OK(w, items)
}

// maskAPIKey：前 3 + … + 尾 4（cc-switch 式掩码），过短则全掩。
func maskAPIKey(key string) string {
	if len(key) <= 7 {
		return "•••"
	}
	return key[:3] + "…" + key[len(key)-4:]
}

// listModelProviders 返回模型供应商注册表（前端「添加模型」预设卡片数据源）。
func (s *Server) listModelProviders(w http.ResponseWriter, r *http.Request, _ *store.User) {
	httputil.OK(w, providers.All())
}

// enableModelConfig 独占启用：{enabled:true} 启用该配置并清掉其他；{enabled:false} 仅取消自身。
func (s *Server) enableModelConfig(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := httputil.Decode(r, &body); err != nil {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid body")
		return
	}
	enabled := body.Enabled == nil || *body.Enabled
	if _, err := s.Store.GetModelConfig(r.Context(), id); err != nil {
		httputil.Error(w, http.StatusBadRequest, 40004, "model config not found")
		return
	}
	if err := s.Store.SetModelConfigEnabled(r.Context(), id, enabled); err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	row, err := s.Store.GetModelConfig(r.Context(), id)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	httputil.OK(w, row)
}

// normalizeModelBaseURL accepts a full Responses endpoint; Chat endpoints are rejected.
func normalizeModelBaseURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	for _, suffix := range []string{"/responses"} {
		if strings.HasSuffix(u, suffix) {
			u = strings.TrimSuffix(u, suffix)
			u = strings.TrimRight(u, "/")
		}
	}
	return u
}

// friendlyUpstreamError 把 Go 底层错误翻译成用户能行动的中文提示。
func friendlyUpstreamError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(err.Error(), "Timeout") || strings.Contains(err.Error(), "deadline exceeded") {
		return "连接超时：端点不可达。若本机走代理上网，请给服务进程设置 HTTPS_PROXY 环境变量后重启"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "DNS 解析失败：检查域名是否正确，或需要代理（HTTPS_PROXY）"
	}
	return "request failed: " + err.Error()
}

// friendlyHTTPStatus 把上游非 2xx 翻成人话（不往界面糊原始 JSON）。不改用户填的地址，只如实描述结果。
func friendlyHTTPStatus(status int, modelsURL string) string {
	switch status {
	case 400:
		return "请求被拒绝（400）"
	case 401, 403:
		return "鉴权失败（" + strconv.Itoa(status) + "）：API Key 错误或没有访问权限"
	case 404:
		return "找不到（404）：" + modelsURL
	case 429:
		return "请求被限流（429）：稍后再试"
	case 500, 502, 503:
		return "上游服务异常（" + strconv.Itoa(status) + "）"
	}
	return "请求失败（HTTP " + strconv.Itoa(status) + "）：" + modelsURL
}

func (s *Server) createModelConfig(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var body struct {
		ReasoningEffort string `json:"reasoningEffort"`
		SDKIdentityOnly bool   `json:"sdkIdentityOnly"`
		Name            string `json:"name"`
		Provider        string `json:"provider"`
		BaseURL         string `json:"baseUrl"`
		Model           string `json:"model"`
		ApiKey          string `json:"apiKey"`
		Remark          string `json:"remark"`
		WebsiteURL      string `json:"websiteUrl"`
		WireAPI         string `json:"wireApi"`
	}
	if err := httputil.Decode(r, &body); err != nil || strings.TrimSpace(body.Name) == "" || (body.ApiKey == "" && !body.SDKIdentityOnly) {
		httputil.Error(w, http.StatusBadRequest, 40000, "name and apiKey required")
		return
	}
	if !validReasoningEffort(body.ReasoningEffort) {
		httputil.Error(w, 400, 40000, "推理强度无效，请选择支持的值或沿用 SDK 默认")
		return
	}
	// Provider 走注册表校验（解耦：新 provider 只改 providers.json）。
	// 型号与端点全部由用户填写，服务端不做隐式默认。
	if body.Provider == "" {
		body.Provider = "custom"
	}
	if _, known := providers.Lookup(body.Provider); !known {
		httputil.Error(w, http.StatusBadRequest, 40005, "unknown provider; see GET /api/v1/model-providers")
		return
	}
	if strings.TrimSpace(body.Model) == "" {
		httputil.Error(w, http.StatusBadRequest, 40000, "model required")
		return
	}
	if strings.TrimSpace(body.BaseURL) == "" && body.Provider != "openai" {
		// 表单会按预设预填（可改）；直接调 API 时必须显式给出
		httputil.Error(w, http.StatusBadRequest, 40000, "baseUrl required for this provider")
		return
	}
	if strings.HasSuffix(strings.TrimRight(strings.TrimSpace(body.BaseURL), "/"), "/chat/completions") {
		httputil.Error(w, http.StatusBadRequest, 40006, "Chat Completions 地址不再支持；请填写 Responses 的接口地址")
		return
	}
	if body.WireAPI != "" && body.WireAPI != "responses" {
		httputil.Error(w, http.StatusBadRequest, 40006, "Chat 协议适配已移除；wireApi 仅支持 responses")
		return
	}
	if body.SDKIdentityOnly && (body.ApiKey != "" || body.Provider != "openai" || (body.BaseURL != "" && normalizeModelBaseURL(body.BaseURL) != "https://api.openai.com/v1") || (body.WireAPI != "" && body.WireAPI != "responses")) {
		httputil.Error(w, 400, 40000, "仅原生 OpenAI Responses 模型可由 SDK 执行身份提供凭据")
		return
	}
	enc := ""
	var err error
	if !body.SDKIdentityOnly {
		enc, err = cryptokey.Encrypt(body.ApiKey)
	}
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, "encrypt failed")
		return
	}
	row, err := s.Store.CreateModelConfig(r.Context(), store.ModelConfigRow{
		SDKIdentityOnly: body.SDKIdentityOnly,
		ReasoningEffort: body.ReasoningEffort,
		Name:            strings.TrimSpace(body.Name),
		Provider:        strings.TrimSpace(body.Provider),
		BaseURL:         normalizeModelBaseURL(body.BaseURL),
		Model:           strings.TrimSpace(body.Model),
		ApiKeyEncrypted: enc,
		Remark:          strings.TrimSpace(body.Remark),
		WebsiteURL:      strings.TrimSpace(body.WebsiteURL),
		WireAPI:         "responses",
	})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	httputil.OK(w, row)
}

func (s *Server) updateModelConfig(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	var body struct {
		ReasoningEffort     *string `json:"reasoningEffort"`
		Name                string  `json:"name"`
		Provider            string  `json:"provider"`
		BaseURL             string  `json:"baseUrl"`
		Model               string  `json:"model"`
		ApiKey              string  `json:"apiKey"`
		Status              string  `json:"status"`
		CompatibilityStatus string  `json:"compatibilityStatus"`
		Remark              string  `json:"remark"`
		WebsiteURL          string  `json:"websiteUrl"`
		WireAPI             string  `json:"wireApi"`
	}
	if err := httputil.Decode(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		httputil.Error(w, http.StatusBadRequest, 40000, "name required")
		return
	}
	if strings.HasSuffix(strings.TrimRight(strings.TrimSpace(body.BaseURL), "/"), "/chat/completions") {
		httputil.Error(w, http.StatusBadRequest, 40006, "Chat Completions 地址不再支持；请填写 Responses 的接口地址")
		return
	}
	if body.WireAPI != "" && body.WireAPI != "responses" {
		httputil.Error(w, http.StatusBadRequest, 40006, "Chat 协议适配已移除；wireApi 仅支持 responses")
		return
	}
	previous, e := s.Store.GetModelConfig(r.Context(), id)
	if e != nil {
		httputil.Error(w, 404, 40002, "model config not found")
		return
	}
	effort := previous.ReasoningEffort
	if body.ReasoningEffort != nil {
		effort = *body.ReasoningEffort
	}
	if !validReasoningEffort(effort) {
		httputil.Error(w, 400, 40000, "推理强度无效，请选择支持的值或沿用 SDK 默认")
		return
	}
	enc := ""
	if body.ApiKey != "" {
		var err error
		enc, err = cryptokey.Encrypt(body.ApiKey)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, 50000, "encrypt failed")
			return
		}
	}
	if err := s.Store.UpdateModelConfig(r.Context(), id, store.ModelConfigRow{
		ReasoningEffort:     effort,
		Name:                strings.TrimSpace(body.Name),
		Provider:            strings.TrimSpace(body.Provider),
		BaseURL:             normalizeModelBaseURL(body.BaseURL),
		Model:               strings.TrimSpace(body.Model),
		ApiKeyEncrypted:     enc,
		Status:              strings.TrimSpace(body.Status),
		CompatibilityStatus: strings.TrimSpace(body.CompatibilityStatus),
		Remark:              strings.TrimSpace(body.Remark),
		WebsiteURL:          strings.TrimSpace(body.WebsiteURL),
		WireAPI:             "responses",
	}); err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	row, err := s.Store.GetModelConfig(r.Context(), id)
	if err != nil {
		httputil.Error(w, http.StatusNotFound, 40002, "model config not found")
		return
	}
	httputil.OK(w, row)
}

func (s *Server) deleteModelConfig(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	if err := s.Store.DeleteModelConfig(r.Context(), id); err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	httputil.OK(w, map[string]any{"id": id})
}

func (s *Server) testModelConfig(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	row, err := s.Store.GetModelConfig(r.Context(), id)
	if err != nil {
		httputil.Error(w, http.StatusNotFound, 40002, "model config not found")
		return
	}
	if row.WireAPI != "" && row.WireAPI != "responses" {
		httputil.Error(w, http.StatusBadRequest, 40006, "Chat 协议适配已移除；请先更新为支持 Responses 的模型配置")
		return
	}
	if row.ApiKeyEncrypted == "" {
		httputil.Error(w, http.StatusConflict, 40900, "此模型通过 SDK 执行身份提供凭据，请在 Web 身份管理中查询绑定节点的账户状态")
		return
	}
	apiKey, err := cryptokey.Decrypt(row.ApiKeyEncrypted)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, "decrypt failed")
		return
	}
	if row.BaseURL == "" || row.Model == "" {
		httputil.OK(w, map[string]any{"ok": false, "error": "baseUrl and model required"})
		return
	}
	base := strings.TrimRight(row.BaseURL, "/")
	endpoint := base + "/responses"
	payload := fmt.Sprintf(`{"model":%q,"input":"ping","max_output_tokens":4}`, row.Model)
	client := &http.Client{Timeout: 12 * time.Second}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := client.Do(req)
	if err != nil {
		httputil.OK(w, map[string]any{"ok": false, "error": friendlyUpstreamError(err), "scope": "connectivity_only", "compatibilityVerified": false})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Reachability does not verify SDK tools, Schema or report quality.
		httputil.OK(w, map[string]any{"ok": true, "status": resp.StatusCode, "scope": "connectivity_only", "compatibilityVerified": false, "message": "接口连接成功；本次仅验证连通性，尚未验证 SDK 工具、Schema 和完整复核能力。"})
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	httputil.OK(w, map[string]any{"ok": false, "status": resp.StatusCode, "error": strings.TrimSpace(string(body)), "scope": "connectivity_only", "compatibilityVerified": false})
}

// fetchProviderModels 从供应商拉取模型清单（cc-switch「获取模型列表」同款）。
// 端点解析：用户改了 Base URL → 用 baseUrl + /models；否则用注册表 modelsUrl。
// Key 解析：表单直填优先；编辑场景留空则用已存配置（modelConfigId）里加密的 key。
func (s *Server) fetchProviderModels(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var body struct {
		Provider      string `json:"provider"`
		BaseURL       string `json:"baseUrl"`
		ApiKey        string `json:"apiKey"`
		ModelConfigID int64  `json:"modelConfigId"`
	}
	if err := httputil.Decode(r, &body); err != nil {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid body")
		return
	}
	apiKey := strings.TrimSpace(body.ApiKey)
	if apiKey == "" && body.ModelConfigID > 0 {
		if row, err := s.Store.GetModelConfig(r.Context(), body.ModelConfigID); err == nil && row.ApiKeyEncrypted != "" {
			if k, err := cryptokey.Decrypt(row.ApiKeyEncrypted); err == nil {
				apiKey = k
			}
		}
	}
	preset, known := providers.Lookup(body.Provider)
	base := strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
	// 「获取列表/测试连接」要的是模型清单端点，与用户填的对话端点是两回事。
	// 已知供应商一律用注册表 modelsUrl（如智谱 …/v4/models）——用户那个框填的是对话地址，
	// 只用于保存后跑扫描，不参与拉列表。只有 custom 这类没有注册端点的，才按 OpenAI 约定补 /models。
	modelsURL := ""
	if known && preset.ModelsURL != "" {
		modelsURL = preset.ModelsURL
	} else if base != "" {
		modelsURL = base + "/models"
	}
	if modelsURL == "" {
		httputil.Error(w, http.StatusBadRequest, 40000, "请先填写接口地址")
		return
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, modelsURL, nil)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		httputil.OK(w, map[string]any{"ok": false, "error": friendlyUpstreamError(err)})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		hint := friendlyHTTPStatus(resp.StatusCode, modelsURL)
		httputil.OK(w, map[string]any{"ok": false, "status": resp.StatusCode, "error": hint})
		return
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		httputil.OK(w, map[string]any{"ok": false, "error": "该地址返回的不是模型列表（期望 OpenAI /models 格式的 JSON）"})
		return
	}
	models := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	sort.Strings(models)
	httputil.OK(w, map[string]any{"ok": true, "models": models})
}

func (s *Server) listFindingSummaries(w http.ResponseWriter, r *http.Request, _ *store.User) {
	f, page, paged, err := findingCatalogQuery(r.URL.Query())
	if err != nil {
		httputil.Error(w, 400, 40000, err.Error())
		return
	}
	if paged {
		result, err := s.Store.ListFindingSummariesPage(r.Context(), f, page)
		if err != nil {
			httputil.Error(w, 500, 50000, err.Error())
			return
		}
		httputil.OK(w, result)
		return
	}
	items, err := s.Store.ListFindingSummaries(r.Context(), f)
	if err != nil {
		httputil.Error(w, 500, 50000, err.Error())
		return
	}
	httputil.OK(w, items)
}

func (s *Server) setFindingStatus(w http.ResponseWriter, r *http.Request, _ *store.User) {
	fid, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := httputil.Decode(r, &body); err != nil || !store.FindingStatuses[body.Status] {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid status: open/confirmed/false_positive/fixed")
		return
	}
	ok, err := s.Store.SetFindingStatus(r.Context(), fid, body.Status)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	if !ok {
		httputil.Error(w, http.StatusNotFound, 40004, "finding not found")
		return
	}
	httputil.OK(w, map[string]any{"id": fid, "status": body.Status})
}

func (s *Server) compareScan(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	task, err := s.Store.Get(r.Context(), id)
	if err != nil {
		httputil.Error(w, http.StatusNotFound, 40002, "scan not found")
		return
	}
	if task.SDKOperation != nil || task.ComponentPlanning != nil {
		httputil.Error(w, 409, 40900, "SDK 独立操作没有扫描对比或扫描报告，请查看其原始操作报告")
		return
	}
	cmp, err := s.buildCompare(r.Context(), task)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	httputil.OK(w, map[string]any{
		"previousScanId": nilIfZero(cmp.PrevScanID),
		"added":          cmp.Added,
		"removed":        cmp.Removed,
		"unchanged":      cmp.Unchanged,
	})
}

func (s *Server) buildCompare(ctx context.Context, task *store.Task) (*report.Compare, error) {
	if task.ComponentScan != nil {
		return &report.Compare{Added: []store.Finding{}, Removed: []store.Finding{}}, nil
	}
	cur, err := s.Store.ListFindings(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	prev, err := s.Store.FindPreviousScan(ctx, task.ID, task.SourceSnapshotID, task.SourcePath)
	if err != nil {
		return &report.Compare{}, nil
	}
	prevFindings, err := s.Store.ListFindings(ctx, prev.ID)
	if err != nil {
		return nil, err
	}
	curFP := map[string]store.Finding{}
	for _, f := range cur {
		curFP[f.Fingerprint] = f
	}
	prevFP := map[string]store.Finding{}
	for _, f := range prevFindings {
		prevFP[f.Fingerprint] = f
	}
	added := flat(curFP, prevFP)
	return &report.Compare{
		PrevScanID: prev.ID,
		Added:      added,
		Removed:    flat(prevFP, curFP),
		Unchanged:  len(curFP) - len(added),
	}, nil
}

func nilIfZero(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func flat(from, to map[string]store.Finding) []store.Finding {
	out := []store.Finding{}
	for fp, f := range from {
		if _, exists := to[fp]; !exists {
			out = append(out, f)
		}
	}
	return out
}

func (s *Server) listSnapshots(w http.ResponseWriter, r *http.Request, _ *store.User) {
	f, page, paged, err := snapshotCatalogQuery(r.URL.Query())
	if err != nil {
		httputil.Error(w, 400, 40000, err.Error())
		return
	}
	if paged {
		result, err := s.Store.ListSnapshotsPage(r.Context(), f, page)
		if err != nil {
			httputil.Error(w, 500, 50000, err.Error())
			return
		}
		httputil.OK(w, result)
		return
	}
	// Historical projectId=0 means all projects on the array endpoint.
	projectID := int64(0)
	if f.ProjectID != nil {
		projectID = *f.ProjectID
	}
	items, err := s.Store.ListSnapshots(r.Context(), 200, projectID)
	if err != nil {
		httputil.Error(w, 500, 50000, err.Error())
		return
	}
	httputil.OK(w, items)
}

func (s *Server) deleteSnapshot(w http.ResponseWriter, r *http.Request, u *store.User) {
	s.deleteManaged(w, r, u, "snapshot")
}

// assignSnapshotProject 调整快照项目归属；其未归属扫描一并继承。
func (s *Server) assignSnapshotProject(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	var body struct {
		ProjectID int64 `json:"projectId"`
	}
	if err := httputil.Decode(r, &body); err != nil {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid body")
		return
	}
	if _, err := s.Store.GetSnapshot(r.Context(), id); err != nil {
		httputil.Error(w, http.StatusNotFound, 40003, "source snapshot not found")
		return
	}
	if body.ProjectID > 0 {
		if _, err := s.Store.GetProject(r.Context(), body.ProjectID); err != nil {
			httputil.Error(w, http.StatusBadRequest, 40001, "project not found")
			return
		}
	}
	if err := s.Store.UpdateSnapshotProject(r.Context(), id, body.ProjectID); err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	httputil.OK(w, map[string]any{"id": id, "projectId": body.ProjectID})
}

func (s *Server) deleteScan(w http.ResponseWriter, r *http.Request, u *store.User) {
	s.deleteManaged(w, r, u, "scan")
}

func (s *Server) cancelScan(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	ok, err := s.Store.RequestCancel(r.Context(), id)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	if !ok {
		httputil.Error(w, http.StatusConflict, 40901, "only pending/claimed/scanning tasks can be canceled")
		return
	}
	s.respondTask(w, r, id)
}

func (s *Server) retryScan(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	ok, err := s.Store.Retry(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrStorageProtected) {
			s.storageError(w, err)
			return
		}
		if errors.Is(err, store.ErrBudgetUnknown) || errors.Is(err, store.ErrBudgetExhausted) || errors.Is(err, store.ErrBudgetPolicy) {
			s.budgetError(w, err)
			return
		}
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	if !ok {
		httputil.Error(w, http.StatusConflict, 40902, "only terminal tasks (completed/partial/failed/canceled/timeout) can be retried")
		return
	}
	s.respondTask(w, r, id)
}

func (s *Server) respondTask(w http.ResponseWriter, r *http.Request, id int64) {
	task, err := s.Store.Get(r.Context(), id)
	if err != nil {
		httputil.Error(w, http.StatusNotFound, 40002, "scan not found")
		return
	}
	httputil.OK(w, task)
}

// scanLogs 从扫描产物读取 events.jsonl，支持 after=<行号> 增量拉取。
// 返回 { events: [{n, timestamp, level, kind, message}], nextAfter }
func (s *Server) scanLogs(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	task, err := s.Store.Get(r.Context(), id)
	if err != nil {
		httputil.OK(w, map[string]any{"events": []any{}, "nextAfter": 0})
		return
	}
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	if after < 0 {
		httputil.Error(w, 400, 40000, "invalid log cursor")
		return
	}
	output, e := s.logOutputPath(r, task)
	if e != nil {
		httputil.Error(w, 404, 40400, "scan attempt not found")
		return
	}
	if output == "" {
		httputil.OK(w, map[string]any{"events": []any{}, "nextAfter": 0, "total": 0})
		return
	}
	path := filepath.Join(output, "logs", "events.jsonl")
	var batch logBatch
	observationVersion, attempt := task.ObservationContractVersion, task.Attempt
	if value := r.URL.Query().Get("attempt"); value != "" {
		attempt, _ = strconv.Atoi(value)
		a, e := s.Store.GetAttempt(r.Context(), task.ID, attempt)
		if e != nil {
			httputil.Error(w, 404, 40400, "scan attempt not found")
			return
		}
		observationVersion = a.ObservationContractVersion
	}
	if observationVersion == 1 {
		batch, err = readStructuredLogBatch(path, after, contracts.ExecutionRef{TaskID: contracts.TaskID(strconv.FormatInt(task.ID, 10)), Attempt: int64(attempt)})
	} else {
		batch, err = readLogBatch(path, after)
		batch.Format = "legacy_jsonl"
	}
	if err != nil {
		if os.IsNotExist(err) {
			httputil.OK(w, logBatch{Events: []logEvent{}})
			return
		}
		httputil.Error(w, 500, 50000, "scan logs could not be read")
		return
	}
	httputil.OK(w, batch)
}

func (s *Server) exportReportPDF(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	task, findings, ok := s.reportInput(w, r, id)
	if !ok {
		return
	}
	var snap *store.SnapshotRow
	if task.SourceSnapshotID > 0 {
		if row, err := s.Store.GetSnapshot(r.Context(), task.SourceSnapshotID); err == nil {
			snap = row
		}
	}
	var cmp *report.Compare
	var err error
	if !r.URL.Query().Has("attempt") {
		cmp, err = s.buildCompare(r.Context(), task)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
			return
		}
	}
	html, err := report.Build(task, findings, snap, cmp)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	pdf, err := render.HTMLToPDF(r.Context(), html)
	if err != nil {
		httputil.Error(w, http.StatusNotImplemented, 50101, "PDF rendering unavailable: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="report-`+strconv.FormatInt(id, 10)+`.pdf"`)
	_, _ = w.Write(pdf)
}

func (s *Server) exportReport(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, http.StatusBadRequest, 40000, "invalid id")
		return
	}
	task, findings, ok := s.reportInput(w, r, id)
	if !ok {
		return
	}
	var snap *store.SnapshotRow
	if task.SourceSnapshotID > 0 {
		if row, err := s.Store.GetSnapshot(r.Context(), task.SourceSnapshotID); err == nil {
			snap = row
		}
	}
	var cmp *report.Compare
	var err error
	if !r.URL.Query().Has("attempt") {
		cmp, err = s.buildCompare(r.Context(), task)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
			return
		}
	}
	html, err := report.Build(task, findings, snap, cmp)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="report-`+strconv.FormatInt(id, 10)+`.html"`)
	_, _ = w.Write([]byte(html))
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func DefaultAdminPassword() string {
	if v := os.Getenv("AXIOM_ADMIN_PASSWORD"); v != "" {
		return v
	}
	return "admin"
}
