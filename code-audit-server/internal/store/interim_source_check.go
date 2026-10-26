package store

import (
	"axiom/contracts"
	"axiom/sourceidentity"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var reviewCodeReference = regexp.MustCompile("\x60([^\x60\r\n]+?):([1-9][0-9]*)(?:[-–]([1-9][0-9]*))?\x60")
var reviewPlainReference = regexp.MustCompile(`(?:^|[ \t(])([A-Za-z0-9_.@/-]+\.[A-Za-z0-9_]+):([1-9][0-9]*)(?:-([1-9][0-9]*))?`)

// File existence is evidence about a reference, never proof of exploitability.
// The raw model report stays intact even when its citations prevent archiving.
func CheckInterimReviewSources(ctx context.Context, root, source, report string) contracts.InterimReviewSourceCheck {
	out := contracts.InterimReviewSourceCheck{SchemaVersion: 1, SourceContentHash: contracts.Digest(source), ReportSHA256: contracts.Digest(contracts.InterimReviewDigest([]byte(report))), State: "no_references", References: []contracts.InterimReviewSourceReference{}, Reason: "报告缺少可核对的路径:行号引用，请复核后补齐；未认定不存在漏洞。"}
	check, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	hash, _, _, err := sourceidentity.TreeHash(check, root)
	if err != nil || hash != source {
		out.State = "unavailable"
		out.Reason = "冻结源码缺失、哈希不匹配或校验超时；暂不能正式归档。"
		return out
	}
	fs, err := os.OpenRoot(root)
	if err != nil {
		out.State = "unavailable"
		out.Reason = "冻结源码暂不可读取。"
		return out
	}
	defer fs.Close()
	seen := map[string]bool{}
	matches := append(reviewCodeReference.FindAllStringSubmatch(report, -1), reviewPlainReference.FindAllStringSubmatch(report, -1)...)
	for _, m := range matches {
		p := strings.TrimPrefix(m[1], "./")
		start, e := strconv.ParseInt(m[2], 10, 64)
		if e != nil {
			out.State = "blocked"
			out.Reason = "报告的源码行号无效。"
			return out
		}
		end := start
		if m[3] != "" {
			end, e = strconv.ParseInt(m[3], 10, 64)
			if e != nil {
				out.State = "blocked"
				out.Reason = "报告的源码行号无效。"
				return out
			}
		}
		if len([]rune(p)) > 4096 || start > 9007199254740991 || end > 9007199254740991 {
			out.State = "blocked"
			out.Reason = "报告的路径或行号超出引用合同边界。"
			return out
		}
		key := fmt.Sprint(p, ":", start, "-", end)
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(out.References) >= 256 {
			out.State = "blocked"
			out.Reason = "源码引用超过本次校验容量，未将未检查的引用视为有效。"
			return out
		}
		ref := contracts.InterimReviewSourceReference{Path: p, StartLine: start, EndLine: end, State: "unavailable", Reason: "路径无效或超出冻结源码边界。"}
		if p != "" && !path.IsAbs(p) && !strings.ContainsAny(p, "\\\x00:") && path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../") {
			f, e := fs.Open(p)
			if os.IsNotExist(e) {
				ref.State = "missing"
				ref.Reason = "该文件不在冻结源码中，不能把模型引用当作已观察证据。"
			} else if e == nil {
				st, e := f.Stat()
				if e == nil && st.Mode().IsRegular() && st.Size() <= 4*1024*1024 {
					raw, e := io.ReadAll(io.LimitReader(f, 4*1024*1024+1))
					if e == nil && len(raw) <= 4*1024*1024 {
						digest := contracts.Digest(contracts.InterimReviewDigest(raw))
						ref.SHA256 = &digest
						lines := int64(bytes.Count(raw, []byte{'\n'}))
						if len(raw) > 0 && raw[len(raw)-1] != '\n' {
							lines++
						}
						if end < start || end > lines {
							ref.State = "out_of_range"
							ref.Reason = "行号超出该冻结文件的实际范围。"
						} else {
							ref.State = "observed"
							ref.Reason = "文件及行号存在；仅核对引用，不证明漏洞结论。"
						}
					}
				}
				f.Close()
			}
		}
		out.References = append(out.References, ref)
	}
	if len(out.References) == 0 {
		return out
	}
	out.State = "checked"
	out.Reason = "冻结版本、引用文件及行号已核对；模型解释与漏洞可利用性仍需依据原文判断。"
	for _, ref := range out.References {
		if ref.State != "observed" {
			out.State = "blocked"
			out.Reason = "报告包含缺失、越界或不可读取的源码引用，请重新复核；未改写 SDK 原文。"
			break
		}
	}
	return out
}
