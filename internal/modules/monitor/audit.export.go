package monitor

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/retrieval"
)

type exportFile struct {
	Name   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type auditCoverage struct {
	Source           string           `json:"source"`
	QueryID          string           `json:"query_id"`
	QueryVersion     string           `json:"query_version"`
	Status           string           `json:"status"`
	RequestIDs       []idgen.ID       `json:"request_ids"`
	Fetched          *int             `json:"fetched"`
	Unique           *int             `json:"unique_source_ids"`
	HasMore          bool             `json:"has_more"`
	TerminalEvidence []string         `json:"terminal_evidence"`
	TruncationReason string           `json:"truncation_reason"`
	NextAction       string           `json:"next_action"`
	Window           Window           `json:"window"`
	Partitions       []map[string]any `json:"partitions"`
}

// ExportAuditZIP 导出完整有界账本；过大时明确失败，完整分页 API 始终可用。
func (s *Service) ExportAuditZIP(ctx context.Context, id idgen.ID) ([]byte, error) {
	run, err := s.auditRun(ctx, id)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	jsonFile := func(name string, value any) error {
		b, e := json.MarshalIndent(value, "", "  ")
		if e != nil {
			return e
		}
		files[name] = append(b, '\n')
		return nil
	}
	protocols := []retrieval.Protocol{}
	concepts := []map[string]any{}
	plans := []map[string]any{}
	for _, t := range run.SearchTemplates {
		if t.Protocol != nil {
			protocols = append(protocols, *t.Protocol)
			concepts = append(concepts, map[string]any{"profile_id": t.Protocol.ProfileID, "profile_version": t.Protocol.ProfileVersion, "concepts": t.Protocol.Concepts})
			for _, q := range t.Protocol.ExternalQueries {
				plans = append(plans, map[string]any{"origin": "external_plan", "query": q, "status": "planned", "reported_hits": nil, "fetched": nil, "next_action": "执行器未执行此原生外部查询；不得复用 Glean 子集完成状态。"})
			}
		}
	}
	if err = jsonFile("protocol.json", map[string]any{"run_id": id, "protocols": protocols, "legacy_templates": run.SearchTemplates, "budget_changes": run.BudgetChanges, "note": "空 protocols 表示旧运行没有完整协议，不能补造历史范围。"}); err != nil {
		return nil, err
	}
	_ = jsonFile("concepts.json", concepts)
	coverage := map[string]*auditCoverage{}
	queryForKey := map[string]*QueryProgress{}
	key := func(source, q, v string) string { return source + "/" + q + "@" + v }
	for source, p := range run.Sources {
		for _, q := range p.Queries {
			k := key(source, q.QueryID, q.QueryVersion)
			queryForKey[k] = q
			coverage[k] = &auditCoverage{Source: source, QueryID: q.QueryID, QueryVersion: q.QueryVersion, Status: "planned", RequestIDs: []idgen.ID{}, TerminalEvidence: []string{}, Window: q.CoverageWindow, Partitions: []map[string]any{}, NextAction: "尚未执行；检查来源启用、范围与预算。"}
			plans = append(plans, map[string]any{"origin": "glean_compiled", "query_id": q.QueryID, "query_version": q.QueryVersion, "source": source, "native_query": q.Query, "seed_queries": q.Queries, "mode": q.Mode, "window": q.CoverageWindow, "warnings": q.Warnings, "status": "planned", "reported_hits": nil, "fetched": nil})
		}
	}
	for _, q := range run.UnavailableQueries {
		k := key(q.Source, q.QueryID, q.QueryVersion)
		coverage[k] = &auditCoverage{Source: q.Source, QueryID: q.QueryID, QueryVersion: q.QueryVersion, Status: "blocked", RequestIDs: []idgen.ID{}, TerminalEvidence: []string{}, Partitions: []map[string]any{}, TruncationReason: "source_unavailable", NextAction: "启用来源后创建明确有界的新计划；未调用不代表零命中。"}
	}
	_ = jsonFile("query-plan.json", plans)
	var requestLines, occurrenceLines, recordLines, edgeLines, screeningLines bytes.Buffer
	totalBytes := 0
	line := func(w *bytes.Buffer, v any) error {
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		totalBytes += len(b) + 1
		if totalBytes > 128<<20 {
			return bizerrors.BadRequest("审计包超过 128 MiB；请使用完整分页 requests、occurrences 和 catalog 接口导出")
		}
		w.Write(b)
		w.WriteByte('\n')
		return nil
	}
	partitionEnds := map[string]map[string]string{}
	requested := 0
	attempted := 0
	unresolved := 0
	for after := idgen.ID(0); ; {
		rows, e := s.auditRequests(ctx, id, after, 500)
		if e != nil {
			return nil, e
		}
		for _, r := range rows {
			after = r.RequestID
			requested++
			if r.Attempted {
				attempted++
			}
			if r.Status == "pending" {
				r.Status = "uncertain"
				r.NextAction = "发送前已登记，但没有持久结果；核对原始请求和已有目录记录后再重试。"
			}
			if r.Status == "uncertain" {
				unresolved++
			}
			k := key(r.Source, r.QueryID, r.QueryVersion)
			c := coverage[k]
			if c == nil {
				c = &auditCoverage{Source: r.Source, QueryID: r.QueryID, QueryVersion: r.QueryVersion, Status: "partial", RequestIDs: []idgen.ID{}, Partitions: []map[string]any{}, TerminalEvidence: []string{}}
				coverage[k] = c
			}
			c.RequestIDs = append(c.RequestIDs, r.RequestID)
			c.Status = "partial"
			c.HasMore = true
			c.NextAction = r.NextAction
			if r.Status == "blocked" {
				c.Status = "blocked"
			}
			if r.TruncationReason != "" {
				c.TruncationReason = r.TruncationReason
			} else if r.Status == "success" {
				// 失败尝试保留在 requests；同一冻结游标成功续查后不再把旧错误当作当前覆盖缺口。
				c.TruncationReason = ""
			}
			partition := r.Window.Start.Format(time.RFC3339Nano) + "/" + r.Window.End.Format(time.RFC3339Nano)
			if partitionEnds[k] == nil {
				partitionEnds[k] = map[string]string{}
			}
			if r.Status == "success" && r.PageExhausted && r.TerminalEvidence != "" {
				partitionEnds[k][partition] = r.TerminalEvidence
				c.TerminalEvidence = append(c.TerminalEvidence, r.TerminalEvidence)
			} else {
				delete(partitionEnds[k], partition)
			}
			c.Partitions = append(c.Partitions, map[string]any{"window": r.Window, "request_id": r.RequestID, "cursor_in": r.CursorIn, "cursor_out": r.CursorOut, "params_redacted": r.Params, "status": r.Status, "page_exhausted": r.PageExhausted, "terminal_evidence": r.TerminalEvidence})
			if e = line(&requestLines, r); e != nil {
				return nil, e
			}
		}
		if len(rows) < 500 {
			break
		}
	}
	fetched := 0
	uniqueSource := map[string]bool{}
	uniqueQuery := map[string]map[string]bool{}
	normalized := map[string]bool{}
	abstracts := map[string]bool{}
	for after := idgen.ID(0); ; {
		rows, e := s.auditOccurrences(ctx, id, after, 500)
		if e != nil {
			return nil, e
		}
		for _, o := range rows {
			after = o.ID
			fetched++
			sid := o.Source + ":" + o.SourceID
			uniqueSource[sid] = true
			normalized[o.RecordKey] = true
			if o.Record.Abstract != "" {
				abstracts[sid] = true
			}
			k := key(o.Source, o.QueryID, o.QueryVersion)
			if uniqueQuery[k] == nil {
				uniqueQuery[k] = map[string]bool{}
			}
			if o.SourceID != "" {
				uniqueQuery[k][o.SourceID] = true
			}
			if c := coverage[k]; c != nil {
				if c.Fetched == nil {
					v := 0
					c.Fetched = &v
				}
				*c.Fetched++
			}
			// 逐次出现仍可追溯到原值哈希；外发包默认不携带摘要正文。
			o.Record.Abstract = ""
			o.Record.Raw = nil
			if e = line(&occurrenceLines, o); e != nil {
				return nil, e
			}
		}
		if len(rows) < 500 {
			break
		}
	}
	coverageRows := []auditCoverage{}
	for k, c := range coverage {
		if len(c.RequestIDs) > 0 {
			if c.Fetched == nil {
				n := 0
				c.Fetched = &n
			}
			u := len(uniqueQuery[k])
			c.Unique = &u
		}
		q := queryForKey[k]
		if q != nil && (q.Status == "completed" || q.Status == "completed_empty") && len(c.RequestIDs) > 0 && q.CompletedPartitions > 0 && len(partitionEnds[k]) >= q.CompletedPartitions && c.TruncationReason == "" && unresolved == 0 {
			c.Status = "complete"
			c.HasMore = false
			c.NextAction = "此有界查询分页已有终止证据；继续独立证据审核与未执行路线。"
		} else if q != nil && q.Status != "not_started" && len(c.RequestIDs) == 0 {
			c.Status = "partial"
			c.HasMore = true
			c.TruncationReason = "legacy_request_audit_missing"
			c.NextAction = "旧快照没有逐次请求证据，不能据计数声明已完成；按冻结范围补验。"
		}
		coverageRows = append(coverageRows, *c)
	}
	sort.Slice(coverageRows, func(i, j int) bool {
		return key(coverageRows[i].Source, coverageRows[i].QueryID, coverageRows[i].QueryVersion) < key(coverageRows[j].Source, coverageRows[j].QueryID, coverageRows[j].QueryVersion)
	})
	var canonicalCount *int
	decisions := map[string]int{}
	abstractReviewed, fulltextReviewed := 0, 0
	catalogGap := "目录导出服务未配置；不能推断纳排或用户状态计数。"
	if reader, ok := s.publisher.(CatalogAuditReader); ok {
		count := 0
		for after := idgen.ID(0); ; {
			rows, e := reader.RetrievalExportPage(ctx, id, after, 200)
			if e != nil {
				return nil, e
			}
			for _, row := range rows {
				after = row.ID
				count++
				if e = line(&recordLines, row.Record); e != nil {
					return nil, e
				}
				for _, edge := range row.IdentityEdges {
					if e = line(&edgeLines, edge); e != nil {
						return nil, e
					}
				}
				readAbstract, readFull := false, false
				for _, review := range row.Screening {
					if d, ok := review["decision"].(string); ok {
						decisions[d]++
					}
					if raw, e := json.Marshal(review["manual_review"]); e == nil {
						var v struct {
							Evidence *retrieval.ScreeningEvidence `json:"evidence"`
						}
						_ = json.Unmarshal(raw, &v)
						if v.Evidence != nil {
							readAbstract = readAbstract || v.Evidence.ReadingLevel == "abstract_reviewed"
							readFull = readFull || v.Evidence.ReadingLevel == "fulltext_reviewed"
						}
					}
					if e = line(&screeningLines, review); e != nil {
						return nil, e
					}
				}
				if readAbstract {
					abstractReviewed++
				}
				if readFull {
					fulltextReviewed++
				}
			}
			if len(rows) < 200 {
				break
			}
		}
		canonicalCount = &count
		catalogGap = ""
	}
	files["requests.jsonl"] = requestLines.Bytes()
	files["occurrences.jsonl"] = occurrenceLines.Bytes()
	files["records.jsonl"] = recordLines.Bytes()
	files["identity-edges.jsonl"] = edgeLines.Bytes()
	files["screening.jsonl"] = screeningLines.Bytes()
	_ = jsonFile("coverage.json", map[string]any{"run_id": id, "run_status": run.Status, "snapshot_at": s.clock().UTC(), "queries": coverageRows, "request_entries": requested, "attempted_requests": attempted, "unresolved_requests": unresolved, "fetched_occurrences": fetched, "unique_source_records": len(uniqueSource), "normalized_source_versions": len(normalized), "canonical_records": canonicalCount, "canonical_study_families": nil, "abstracts_available": len(abstracts), "abstracts_reviewed_with_manual_evidence": abstractReviewed, "fulltexts_reviewed_with_manual_evidence": fulltextReviewed, "profile_decisions": decisions, "catalog_gap": catalogGap, "catalog_semantics": "current catalog and current user's preferences at export time; frozen collection plan remains in protocol.json", "reported_hits": "per request only; never summed across queries or sources"})
	files["report.md"] = []byte(fmt.Sprintf("# Glean 检索审计\n\n运行 %s，状态 %s。\n\n登记 %d 次调用，其中 %d 次已尝试发送；%d 次结果不确定。取得 %d 次来源出现、%d 个唯一来源标识、%d 个规范来源版本。\n\n完成只指 coverage.json 中具备请求与终止证据的有界查询。候选、去重、摘要取得、审读和正式纳入独立统计，不给出总体召回率。外部查询保留 planned。\n\n全文与摘要正文未随包再分发；raw_path 为空时没有响应文件，哈希不代表缓存存在。目录为导出时的状态，包含当前用户隐藏偏好与范围外理由。\n\n%s\n", id, run.Status, requested, attempted, unresolved, fetched, len(uniqueSource), len(normalized), catalogGap))
	manifest := []exportFile{}
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		h := sha256.Sum256(files[name])
		manifest = append(manifest, exportFile{Name: name, Bytes: len(files[name]), SHA256: hex.EncodeToString(h[:])})
	}
	_ = jsonFile("manifest.json", map[string]any{"schema": "glean-audit-v1", "run_id": id, "complete_export": true, "files": manifest, "warnings": []string{"complete_export 仅表示账本文件完整导出，不代表检索或科学审核完成。", "历史缺失的请求、正文或身份关系不会被补造成成功。"}})
	names = append(names, "manifest.json")
	var output bytes.Buffer
	z := zip.NewWriter(&output)
	for _, name := range names {
		w, e := z.Create(name)
		if e != nil {
			return nil, e
		}
		if _, e = w.Write(files[name]); e != nil {
			return nil, e
		}
	}
	if err = z.Close(); err != nil {
		return nil, err
	}
	// CRC 和清单在交付前检查；损坏的包不会作为成功响应返回。
	check, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		return nil, err
	}
	for _, f := range check.File {
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		_, e = io.Copy(io.Discard, r)
		_ = r.Close()
		if e != nil {
			return nil, e
		}
	}
	return output.Bytes(), nil
}

func auditNextAction(status string) string {
	if strings.Contains(status, "budget") {
		return "保留游标；明确增加冻结协议的预算后继续。"
	}
	return "检查来源状态和剩余覆盖缺口。"
}
