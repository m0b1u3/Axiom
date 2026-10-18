package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"net/http"
	"strconv"
)

func (s *Server) nativeSDKArchive(w http.ResponseWriter, r *http.Request) (int64, *contracts.SDKNativeResult, bool) {
	id, ok := pathID(r, "id")
	attempt, e := strconv.Atoi(r.PathValue("attempt"))
	if !ok || e != nil || attempt < 1 {
		httputil.Error(w, 400, 40000, "扫描执行编号无效")
		return 0, nil, false
	}
	archive, e := s.Store.AttemptResult(r.Context(), id, attempt)
	if e != nil || archive.Result == nil {
		httputil.Error(w, 404, 40400, "本次执行没有可用的归档 SDK 结果")
		return 0, nil, false
	}
	native, e := contracts.NativeSDKResult(*archive.Result)
	if e != nil {
		httputil.Error(w, 409, 40900, "原生结果扩展的版本、身份或产物引用不受支持；完整归档仍可读取")
		return 0, nil, false
	}
	if native == nil {
		httputil.Error(w, 404, 40400, "此历史执行未记录新版 SDK 原生结果扩展")
		return 0, nil, false
	}
	w.Header().Set("Cache-Control", "no-store")
	return id, native, true
}
func (s *Server) sdkCostEstimate(w http.ResponseWriter, r *http.Request, u *store.User) {
	_, native, ok := s.nativeSDKArchive(w, r)
	if !ok {
		return
	}
	httputil.OK(w, native.UsageEstimate)
}
func (s *Server) sdkHistoryLinks(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, native, ok := s.nativeSDKArchive(w, r)
	if !ok {
		return
	}
	wanted := r.URL.Query().Get("scanId")
	allowed := native.ScanID != nil && *native.ScanID == wanted
	for _, f := range native.RepositoryHistory.Records {
		if f.ScanID == wanted {
			allowed = true
		}
		if f.KnownScanIds != nil {
			for _, v := range *f.KnownScanIds {
				if v == wanted {
					allowed = true
				}
			}
		}
	}
	if wanted == "" || !allowed {
		httputil.Error(w, 400, 40000, "原生 Scan ID 必须来自本次归档的历史关联")
		return
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		var e error
		page, e = strconv.Atoi(raw)
		if e != nil || page < 1 {
			httputil.Error(w, 400, 40000, "页码须为正整数")
			return
		}
	}
	t, e := s.Store.Get(r.Context(), id)
	if e != nil {
		httputil.Error(w, 404, 40400, "扫描任务不可用")
		return
	}
	links, e := s.Store.SDKExecutionLinks(r.Context(), wanted, t.SourceContentHash, page)
	if e != nil {
		httputil.Error(w, 503, 50300, "关联执行暂时无法读取，请重试")
		return
	}
	httputil.OK(w, links)
}
