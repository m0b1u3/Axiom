package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"time"
)

var diagnosticRecordName = regexp.MustCompile(`^[a-f0-9]{32}\.json$`)

func (s *Server) structuredDiagnosticCatalog(w http.ResponseWriter, r *http.Request, _ *store.User) {
	catalog, e := contracts.FixedStructuredDiagnosticCatalog()
	if e != nil {
		httputil.Error(w, 503, 50300, "固定诊断模板无法读取，请同步更新 Server 与 Web")
		return
	}
	httputil.OK(w, map[string]any{"catalog": catalog, "catalogSha256": contracts.StructuredDiagnosticCatalogHash})
}

// Diagnostic receipts occupy a separate private directory. No task, snapshot,
// report, identity, model configuration, or SDK working directory is opened.
func (s *Server) structuredRecordRoot(create bool) (*os.Root, error) {
	if s.QueueLimits.OutputRoot == "" {
		return nil, errors.New("diagnostic storage not configured")
	}
	base := filepath.Dir(filepath.Clean(s.QueueLimits.OutputRoot))
	for _, name := range []string{"diagnostics", "structured-output"} {
		base = filepath.Join(base, name)
		if create {
			if e := os.Mkdir(base, 0700); e != nil && !errors.Is(e, os.ErrExist) {
				return nil, e
			}
		}
		info, e := os.Lstat(base)
		if errors.Is(e, os.ErrNotExist) && !create {
			return nil, nil
		}
		if e != nil {
			return nil, e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("diagnostic directory is not private")
		}
	}
	return os.OpenRoot(base)
}
func (s *Server) createStructuredDiagnostic(w http.ResponseWriter, r *http.Request, _ *store.User) {
	defer r.Body.Close()
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if e != nil {
		httputil.Error(w, 413, 41300, "诊断请求超过固定模板允许的大小")
		return
	}
	request, e := contracts.DecodeStructuredDiagnosticRequest(raw, nil)
	if e != nil {
		httputil.Error(w, 400, 40000, "诊断仅接受已知离线场景与协议，不接受密钥、URL、自定义正文或真实执行")
		return
	}
	if request.Protocol != "responses" {
		httputil.Error(w, 400, 40000, "诊断仅支持 Responses；Chat 协议适配已移除")
		return
	}
	id := make([]byte, 16)
	if _, e = rand.Read(id); e != nil {
		httputil.Error(w, 503, 50300, "无法创建诊断记录标识")
		return
	}
	record, e := contracts.RunStructuredDiagnostic(request, hex.EncodeToString(id), time.Now().UTC().Format(time.RFC3339Nano))
	if errors.Is(e, contracts.ErrStructuredCatalogChanged) {
		httputil.Error(w, 409, 40900, "固定诊断模板已更新，请重新加载预览后再运行离线对照")
		return
	}
	if e != nil {
		httputil.Error(w, 503, 50300, "离线对照未通过合同检查，没有调用模型")
		return
	}
	root, e := s.structuredRecordRoot(true)
	if e != nil {
		httputil.Error(w, 503, 50300, "脱敏诊断记录目录不可写，没有调用模型；请检查数据目录权限")
		return
	}
	defer root.Close()
	data, e := contracts.EncodeJSON(record)
	if e != nil {
		httputil.Error(w, 503, 50300, "无法生成脱敏诊断记录")
		return
	}
	temporary := "." + record.ID + ".tmp"
	name := record.ID + ".json"
	f, e := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		httputil.Error(w, 503, 50300, "脱敏诊断记录写入失败，没有调用模型")
		return
	}
	defer root.Remove(temporary)
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		e = os.Rename(filepath.Join(root.Name(), temporary), filepath.Join(root.Name(), name))
	}
	if e != nil {
		httputil.Error(w, 503, 50300, "脱敏诊断记录未保存，请检查存储空间与权限；没有调用模型")
		return
	}
	// The file is synced before publication. A directory sync protects rename
	// durability on supported filesystems; never claim success if it fails.
	dir, e := root.Open(".")
	if e == nil {
		e = dir.Sync()
		dir.Close()
	}
	if e != nil {
		httputil.Error(w, 503, 50300, "诊断记录已发布但持久化确认失败，请刷新记录检查，勿重复提交")
		return
	}
	httputil.Write(w, 201, httputil.Envelope{Code: 0, Message: "success", Data: record})
}
func readStructuredRecord(root *os.Root, name string) (contracts.StructuredDiagnosticRecord, error) {
	var empty contracts.StructuredDiagnosticRecord
	info, e := root.Lstat(name)
	if e != nil || !info.Mode().IsRegular() {
		return empty, errors.New("invalid diagnostic file")
	}
	f, e := root.Open(name)
	if e != nil {
		return empty, e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 16385))
	if e != nil || len(raw) > 16384 {
		return empty, errors.New("invalid diagnostic size")
	}
	record, e := contracts.DecodeStructuredDiagnosticRecord(raw, nil)
	if e != nil || record.ID+".json" != name {
		return empty, errors.New("invalid diagnostic binding")
	}
	if record.CatalogSHA256 == contracts.StructuredDiagnosticCatalogHash {
		expected, e := contracts.RunStructuredDiagnostic(contracts.StructuredDiagnosticRequestForRecord(record), record.ID, record.CreatedAt)
		if e != nil || !reflect.DeepEqual(expected, record) {
			return empty, errors.New("diagnostic receipt changed")
		}
	}
	return record, nil
}
func (s *Server) listStructuredDiagnostics(w http.ResponseWriter, r *http.Request, _ *store.User) {
	page, size := int64(1), int64(10)
	for _, p := range []struct {
		name  string
		value *int64
		max   int64
	}{{"page", &page, 9007199254740991}, {"pageSize", &size, 50}} {
		if v := r.URL.Query().Get(p.name); v != "" {
			n, e := strconv.ParseInt(v, 10, 64)
			if e != nil || n < 1 || n > p.max {
				httputil.Error(w, 400, 40000, "诊断记录分页参数无效")
				return
			}
			*p.value = n
		}
	}
	result := contracts.StructuredDiagnosticPage{SchemaVersion: 1, Page: page, PageSize: size, Items: []contracts.StructuredDiagnosticRecord{}}
	root, e := s.structuredRecordRoot(false)
	if e != nil {
		httputil.Error(w, 503, 50300, "脱敏诊断记录目录暂不可读，请检查权限")
		return
	}
	if root == nil {
		httputil.OK(w, result)
		return
	}
	defer root.Close()
	dir, e := root.Open(".")
	if e != nil {
		httputil.Error(w, 503, 50300, "无法列出脱敏诊断记录")
		return
	}
	defer dir.Close()
	entries, e := dir.ReadDir(-1)
	if e != nil {
		httputil.Error(w, 503, 50300, "无法列出脱敏诊断记录")
		return
	}
	all := []contracts.StructuredDiagnosticRecord{}
	for _, entry := range entries {
		if !diagnosticRecordName.MatchString(entry.Name()) {
			continue
		}
		record, e := readStructuredRecord(root, entry.Name())
		if e != nil {
			httputil.Error(w, 503, 50300, "诊断记录完整性检查失败，原文件已保留；请由管理员检查诊断目录")
			return
		}
		all = append(all, record)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt == all[j].CreatedAt {
			return all[i].ID > all[j].ID
		}
		a, _ := time.Parse(time.RFC3339Nano, all[i].CreatedAt)
		b, _ := time.Parse(time.RFC3339Nano, all[j].CreatedAt)
		return a.After(b)
	})
	result.Total = int64(len(all))
	start := (page - 1) * size
	if start < result.Total {
		end := start + size
		if end > result.Total {
			end = result.Total
		}
		result.Items = all[start:end]
	}
	httputil.OK(w, result)
}
