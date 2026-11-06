package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanmonitorstate"
	"github.com/teamsillybees/glean/internal/data/ent/gleansearchoccurrence"
	"github.com/teamsillybees/glean/internal/data/ent/gleansearchrequest"
	"github.com/teamsillybees/glean/internal/data/ent/gleansearchrun"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// RequestAudit 保存单次真实尝试；未知计数保持 null，不从计划制造成功记录。
type RequestAudit struct {
	RunID             idgen.ID          `json:"run_id"`
	QueryID           string            `json:"query_id"`
	RequestID         idgen.ID          `json:"request_id"`
	Source            string            `json:"source"`
	Route             string            `json:"route"`
	Method            string            `json:"method"`
	Endpoint          string            `json:"endpoint"`
	QueryVersion      string            `json:"query_version"`
	PlanVersion       string            `json:"plan_version"`
	RequestedAt       time.Time         `json:"requested_at"`
	FinishedAt        *time.Time        `json:"finished_at"`
	Params            map[string]string `json:"params_redacted"`
	HTTPStatus        *int              `json:"http_status"`
	Status            string            `json:"status"`
	ReportedHits      *int              `json:"reported_hits"`
	ReturnedCount     *int              `json:"returned_count"`
	NormalizedCount   *int              `json:"normalized_count"`
	UniqueSourceIDs   *int              `json:"unique_source_ids"`
	CumulativeUnique  *int              `json:"cumulative_unique"`
	CursorIn          string            `json:"cursor_in"`
	CursorOut         string            `json:"cursor_out"`
	Window            Window            `json:"partition"`
	PageExhausted     bool              `json:"page_exhausted"`
	TerminalEvidence  string            `json:"terminal_evidence"`
	TruncationReason  string            `json:"truncation_reason"`
	NextAction        string            `json:"next_action"`
	RawSHA256         string            `json:"raw_sha256"`
	RawPath           string            `json:"raw_path"`
	RawRetention      string            `json:"raw_retention"`
	ErrorKind         string            `json:"error_kind,omitempty"`
	RetryAfterSeconds int64             `json:"retry_after_seconds,omitempty"`
	QueryTranslation  string            `json:"query_translation,omitempty"`
	Warnings          json.RawMessage   `json:"source_warnings,omitempty"`
	Attempted         bool              `json:"attempted"`
}

// OccurrenceAudit 记录来源出现而不替代论文身份，原始响应只保留哈希。
type OccurrenceAudit struct {
	ID           idgen.ID  `json:"occurrence_id"`
	RunID        idgen.ID  `json:"run_id"`
	RequestID    idgen.ID  `json:"request_id"`
	QueryID      string    `json:"query_id"`
	QueryVersion string    `json:"query_version"`
	Ordinal      int       `json:"ordinal"`
	Source       string    `json:"source"`
	SourceID     string    `json:"source_id"`
	RecordKey    string    `json:"record_key"`
	DiscoveredAt time.Time `json:"discovered_at"`
	RawSHA256    string    `json:"raw_sha256"`
	Record       Record    `json:"record"`
}

type auditContextKey struct{}
type fetchAudit struct {
	service  *Service
	state    *monitorState
	run      *Run
	template SearchTemplate
	window   Window
	cursor   string
	attempts []*RequestAudit
}

func (a *fetchAudit) begin(ctx context.Context, source, path string, params map[string]string) (*RequestAudit, error) {
	if a.template.Protocol != nil {
		used, err := a.service.archivedProtocolRequests(ctx, a.run, a.template)
		if err != nil {
			return nil, err
		}
		if used >= effectiveProtocolBudget(a.run, a.template).MaxRequests {
			return nil, &SourceError{Kind: "protocol_request_budget", Message: "frozen protocol request budget exhausted"}
		}
	}
	q := a.template.QueryID
	if q == "" {
		q = a.template.ID
	}
	version := a.template.QueryVersion
	if version == "" {
		version = a.run.TemplateVersion
	}
	base := SourceHTTPServices("")["glean-"+source].BaseURL
	clean := map[string]string{}
	for k, v := range params {
		switch strings.ToLower(k) {
		case "api_key", "apikey", "token", "access_token", "authorization", "key", "webenv", "query_key", "email", "mailto":
			clean[k] = "[redacted]"
		default:
			clean[k] = v
		}
	}
	// 路径只由来源适配器生成；仍剥离查询与用户信息，防止未来 adapter 把凭据写入账本。
	u, err := url.Parse(base + path)
	if err != nil {
		return nil, fmt.Errorf("invalid source audit endpoint")
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	r := &RequestAudit{RunID: a.run.ID, RequestID: a.service.ids.NextID(), QueryID: q, QueryVersion: version, PlanVersion: a.run.TemplateVersion, Source: source, Route: "topic", Method: "GET", Endpoint: u.String(), Params: clean, RequestedAt: a.service.clock().UTC(), Status: "pending", Window: a.window, CursorIn: safeAuditCursor(a.cursor), NextAction: "请求结果未知；恢复时先检查请求、出现和目录投影，再决定重试。", RawRetention: "仅保存响应哈希与许可允许的题录；未保存完整 HTTP 响应文件。"}
	if a.template.Core {
		r.Route = "venue"
	}
	if err := a.service.beginAudit(ctx, a.state, r); err != nil {
		return nil, err
	}
	a.attempts = append(a.attempts, r)
	return r, nil
}

func (a *fetchAudit) received(r *RequestAudit, response *httpclient.Response, err error) {
	r.Attempted = true
	if response != nil {
		code := response.StatusCode
		r.HTTPStatus = &code
		h := sha256.Sum256(response.Body)
		r.RawSHA256 = hex.EncodeToString(h[:])
		r.RetryAfterSeconds = int64(ParseRetryAfter(response.Header.Get("Retry-After"), a.service.clock()).Seconds())
		if code >= 200 && code < 300 {
			auditResponseMetadata(r, response.Body)
		}
	}
	var se *SourceError
	if errors.As(err, &se) && (se.Kind == "provider_backoff" || se.Kind == "provider_forbidden") {
		r.Attempted = false
	}
}

func (a *fetchAudit) finish(ctx context.Context, page Page, fetchErr error) error {
	if len(a.attempts) == 0 {
		return nil
	}
	request := a.attempts[len(a.attempts)-1]
	var occurrences []OccurrenceAudit
	for _, r := range a.attempts {
		at := a.service.clock().UTC()
		r.FinishedAt = &at
		r.CursorOut = safeAuditCursor(page.NextCursor)
		// PubMed 一页包含 esearch 与 efetch。前置请求的响应计数是来源 ID 数，不能复制最终论文计数或终页证明。
		if r != request && r.HTTPStatus != nil && *r.HTTPStatus >= 200 && *r.HTTPStatus < 300 {
			r.Status = "success"
			r.NextAction = "前置请求已返回；本页归一化结果与终止证据见后续请求。"
			continue
		}
		if fetchErr != nil {
			r.Status = "failed"
			r.ErrorKind = "transport"
			r.NextAction = "保留冻结计划与当前游标；检查安全错误分类并按退避时间恢复。"
			var se *SourceError
			if errors.As(fetchErr, &se) {
				r.ErrorKind = se.Kind
				if se.StatusCode == 401 || se.StatusCode == 403 || se.Kind == "disabled" {
					r.Status = "blocked"
					r.NextAction = "检查来源访问权限或启用状态；不得绕过访问限制。"
				}
			}
			if errors.Is(fetchErr, context.Canceled) || errors.Is(fetchErr, context.DeadlineExceeded) {
				r.Status = "uncertain"
				r.NextAction = "调用结果不确定；先核对已有出现与目录投影，再恢复相同冻结窗口。"
			}
			r.TruncationReason = r.ErrorKind
		} else {
			r.Status = "success"
			r.NextAction = "继续当前来源游标或下一分区；采集完成不代表内容已审读。"
			n := len(page.Records)
			r.NormalizedCount = &n
			if r.ReturnedCount == nil {
				r.ReturnedCount = &n
			}
			seen := map[string]bool{}
			for _, record := range page.Records {
				if record.SourceID != "" {
					seen[record.SourceID] = true
				}
			}
			u := len(seen)
			r.UniqueSourceIDs = &u
			if page.Complete {
				r.PageExhausted = true
				r.TerminalEvidence = fmt.Sprintf("%s adapter validated its source-specific terminal response; response_sha256=%s; returned=%d; remaining_cursor=empty", r.Source, r.RawSHA256, *r.ReturnedCount)
			}
		}
	}
	if fetchErr == nil {
		for i, record := range page.Records {
			record.Source = request.Source
			record.AcquiredAt = a.service.clock().UTC()
			record.TemplateVersion = a.run.TemplateVersion
			record.CoreDiscovery = a.template.Core
			record.Topics = uniqueStrings(append(record.Topics, a.template.Topic))
			key, _ := recordIdentity(record)
			hash := sha256.Sum256(record.Raw)
			// 归档保留规范题录和原值哈希；原始来源对象仍由目录观察保存，不复制全文。
			record.Raw = nil
			record.Discoveries = nil
			occurrences = append(occurrences, OccurrenceAudit{ID: a.service.ids.NextID(), RunID: a.run.ID, RequestID: request.RequestID, QueryID: request.QueryID, QueryVersion: request.QueryVersion, Ordinal: i, Source: request.Source, SourceID: record.SourceID, RecordKey: key, DiscoveredAt: record.AcquiredAt, RawSHA256: hex.EncodeToString(hash[:]), Record: record})
		}
	}
	// 外部请求结束后即使调用者取消，仍使用有限清理预算保存实际结果。
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return a.service.finishAudit(finishCtx, a.attempts, occurrences)
}

func safeAuditCursor(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "pm2:") {
		c, err := decodePubmedCursor(value)
		if err == nil {
			c.WebEnv = "[redacted]"
			c.Key = "[redacted]"
			b, _ := json.Marshal(c)
			return "pubmed-redacted:" + string(b)
		}
	}
	return value
}

func auditResponseMetadata(r *RequestAudit, raw []byte) {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return
	}
	count := func(raw json.RawMessage) *int {
		var n int
		if json.Unmarshal(raw, &n) == nil && n >= 0 {
			return &n
		}
		var v string
		if json.Unmarshal(raw, &v) == nil {
			n, e := strconv.Atoi(v)
			if e == nil && n >= 0 {
				return &n
			}
		}
		return nil
	}
	switch r.Source {
	case "crossref":
		var m struct {
			Total json.RawMessage   `json:"total-results"`
			Items []json.RawMessage `json:"items"`
		}
		if json.Unmarshal(root["message"], &m) == nil && m.Items != nil {
			n := len(m.Items)
			r.ReturnedCount = &n
			r.ReportedHits = count(m.Total)
		}
	case "pubmed":
		var m struct {
			Count       json.RawMessage `json:"count"`
			IDs         []string        `json:"idlist"`
			Translation string          `json:"querytranslation"`
			Warnings    json.RawMessage `json:"warninglist"`
		}
		if json.Unmarshal(root["esearchresult"], &m) == nil {
			r.ReportedHits = count(m.Count)
			n := len(m.IDs)
			r.ReturnedCount = &n
			r.QueryTranslation = m.Translation
			r.Warnings = m.Warnings
		}
	case "europepmc":
		var m struct {
			Results []json.RawMessage `json:"result"`
		}
		if json.Unmarshal(root["resultList"], &m) == nil {
			n := len(m.Results)
			r.ReturnedCount = &n
			r.ReportedHits = count(root["hitCount"])
		}
	case "biorxiv", "medrxiv":
		var records []json.RawMessage
		if json.Unmarshal(root["collection"], &records) == nil {
			n := len(records)
			r.ReturnedCount = &n
		}
		var messages []map[string]json.RawMessage
		if json.Unmarshal(root["messages"], &messages) == nil && len(messages) > 0 {
			r.ReportedHits = count(messages[0]["total"])
		}
	}
}

func (s *Service) beginAudit(ctx context.Context, state *monitorState, r *RequestAudit) error {
	if s.memory != nil {
		s.memory.mu.Lock()
		defer s.memory.mu.Unlock()
		if s.memory.auditRequests == nil {
			s.memory.auditRequests = map[idgen.ID]RequestAudit{}
		}
		s.memory.auditRequests[r.RequestID] = *r
		return nil
	}
	raw, _ := json.Marshal(r)
	return data.WithinTx(ctx, s.client, func(ctx context.Context, c *ent.Client) error {
		if _, err := c.GleanMonitorState.Query().Where(gleanmonitorstate.ID(state.id), gleanmonitorstate.Revision(state.revision)).ForShare().Only(ctx); err != nil {
			return fmt.Errorf("audit lease fence failed")
		}
		if _, err := c.GleanSearchRun.Query().Where(gleansearchrun.ID(r.RunID), gleansearchrun.StateKey(s.config.StateKey)).ForShare().Only(ctx); err != nil {
			return err
		}
		return c.GleanSearchRequest.Create().SetID(r.RequestID).SetRunID(r.RunID).SetQueryID(r.QueryID).SetSource(r.Source).SetStatus(r.Status).SetPayload(raw).Exec(ctx)
	})
}

func (s *Service) finishAudit(ctx context.Context, requests []*RequestAudit, occurrences []OccurrenceAudit) error {
	if s.memory != nil {
		s.memory.mu.Lock()
		defer s.memory.mu.Unlock()
		s.memory.auditOccurrences = append(s.memory.auditOccurrences, occurrences...)
		for _, r := range requests {
			if r.NormalizedCount != nil {
				seen := map[string]bool{}
				for _, o := range s.memory.auditOccurrences {
					if o.RunID == r.RunID && o.Source == r.Source && o.SourceID != "" {
						seen[o.SourceID] = true
					}
				}
				n := len(seen)
				r.CumulativeUnique = &n
			}
			s.memory.auditRequests[r.RequestID] = *r
		}
		return nil
	}
	return data.WithinTx(ctx, s.client, func(ctx context.Context, c *ent.Client) error {
		if len(requests) == 0 {
			return nil
		}
		// 运行行串行化出现写入与累计计数，避免并发完成页互相漏计。
		if _, err := c.GleanSearchRun.Query().Where(gleansearchrun.ID(requests[0].RunID), gleansearchrun.StateKey(s.config.StateKey)).ForUpdate().Only(ctx); err != nil {
			return err
		}
		for _, r := range requests {
			if _, err := c.GleanSearchRequest.Query().Where(gleansearchrequest.ID(r.RequestID), gleansearchrequest.RunID(r.RunID), gleansearchrequest.Status("pending")).ForUpdate().Only(ctx); err != nil {
				return err
			}
		}
		for _, o := range occurrences {
			raw, _ := json.Marshal(o)
			if err := c.GleanSearchOccurrence.Create().SetID(o.ID).SetRunID(o.RunID).SetRequestID(o.RequestID).SetOrdinal(o.Ordinal).SetSource(o.Source).SetSourceID(o.SourceID).SetRecordKey(o.RecordKey).SetPayload(raw).Exec(ctx); err != nil {
				return err
			}
		}
		for _, r := range requests {
			if r.NormalizedCount != nil {
				// 累计范围为同一运行、同一来源的已归一化 source_id；不是跨源论文总量。
				n, err := c.GleanSearchOccurrence.Query().Where(gleansearchoccurrence.RunID(r.RunID), gleansearchoccurrence.Source(r.Source), gleansearchoccurrence.SourceIDNEQ("")).Aggregate(func(sel *sql.Selector) string {
					return sql.Count(sql.Distinct(sel.C(gleansearchoccurrence.FieldSourceID)))
				}).Int(ctx)
				if err != nil {
					return err
				}
				r.CumulativeUnique = &n
			}
			raw, _ := json.Marshal(r)
			if err := c.GleanSearchRequest.UpdateOneID(r.RequestID).SetStatus(r.Status).SetPayload(raw).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}
