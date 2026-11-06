package monitor

import (
	"context"
	"github.com/teamsillybees/glean/internal/data/ent/gleansearchrequest"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/retrieval"
	"strings"
	"time"
)

// BudgetChange 记录管理员对未结束运行的操作预算扩容，不改变冻结的研究协议、查询和水位。
type BudgetChange struct {
	Revision   int              `json:"revision"`
	Topic      string           `json:"topic"`
	Before     retrieval.Budget `json:"before"`
	After      retrieval.Budget `json:"after"`
	Reason     string           `json:"reason"`
	OperatorID idgen.ID         `json:"operatorId"`
	ChangedAt  time.Time        `json:"changedAt"`
}

// BudgetIncreaseBody 要求调用方带上已读操作版本，防止并发管理员覆盖额度。
type BudgetIncreaseBody struct {
	Topic            string           `json:"topic" maxLength:"80"`
	ExpectedRevision int              `json:"expectedRevision" minimum:"0"`
	Limits           retrieval.Budget `json:"limits"`
	Reason           string           `json:"reason" minLength:"1" maxLength:"1000"`
}

func effectiveProtocolBudget(run *Run, template SearchTemplate) retrieval.Budget {
	if template.Protocol == nil {
		return retrieval.Budget{}
	}
	for i := len(run.BudgetChanges) - 1; i >= 0; i-- {
		if run.BudgetChanges[i].Topic == template.Topic {
			return run.BudgetChanges[i].After
		}
	}
	return template.Protocol.Budget
}

// IncreaseProtocolBudget 只提高尚未结束运行的操作限额；调用本身不请求来源或模型。
func (s *Service) IncreaseProtocolBudget(ctx context.Context, id idgen.ID, body BudgetIncreaseBody) (Run, error) {
	principal, ok := platformauth.PrincipalFromContext(ctx)
	if !ok || !principal.IsSuperAdmin {
		return Run{}, bizerrors.CatalogForbidden()
	}
	if s.client != nil {
		exists, err := s.client.SysUser.Query().Where(sysuser.ID(principal.UserID), sysuser.DeletedAtIsNil(), sysuser.IsEnable(true), sysuser.IsSuperAdmin(true)).Exist(ctx)
		if err != nil {
			return Run{}, err
		}
		if !exists {
			return Run{}, bizerrors.CatalogForbidden()
		}
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" || len(body.Reason) > 3000 {
		return Run{}, bizerrors.BadRequest("提高预算必须提供简短理由")
	}
	ctx = entx.WithOperatorID(ctx, principal.UserID)
	state, err := s.acquire(ctx)
	if err != nil {
		return Run{}, err
	}
	defer s.release(ctx, state)
	for i := range state.Runs {
		run := &state.Runs[i]
		if run.ID != id {
			continue
		}
		if !run.FinishedAt.IsZero() {
			return Run{}, bizerrors.BadRequest("已结束运行不能修改预算")
		}
		if body.ExpectedRevision != len(run.BudgetChanges) {
			return Run{}, bizerrors.BadRequest("预算版本已变化，请刷新后重试")
		}
		for _, template := range run.SearchTemplates {
			if template.Topic != body.Topic || template.Protocol == nil {
				continue
			}
			before, after := effectiveProtocolBudget(run, template), body.Limits
			if after.MaxHistoryDays != before.MaxHistoryDays || after.MaxRequests < before.MaxRequests || after.MaxPages < before.MaxPages || after.MaxRecords < before.MaxRecords || after.MaxQueue < before.MaxQueue || after == before || after.MaxRequests > 1000000 || after.MaxPages > 1000000 || after.MaxRecords > 10000000 || after.MaxQueue > 1000000 {
				return Run{}, bizerrors.BadRequest("只能提高请求、页数、记录或队列预算；历史范围保持冻结且不能超过系统上限")
			}
			run.BudgetChanges = append(run.BudgetChanges, BudgetChange{Revision: len(run.BudgetChanges) + 1, Topic: body.Topic, Before: before, After: after, Reason: body.Reason, OperatorID: principal.UserID, ChangedAt: s.clock().UTC()})
			if err = s.save(ctx, state); err != nil {
				return Run{}, err
			}
			return publicRun(*run), nil
		}
		return Run{}, bizerrors.BadRequest("运行中没有该主题的冻结检索协议")
	}
	return Run{}, bizerrors.BadRequest("未找到可恢复的运行")
}

// archivedProtocolRequests 将发送前预留的未知请求也计入上限，崩溃不能退回已使用的请求配额。
func (s *Service) archivedProtocolRequests(ctx context.Context, run *Run, template SearchTemplate) (int, error) {
	if template.Protocol == nil {
		return 0, nil
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, source := range run.Sources {
		for _, q := range source.Queries {
			if q.Topic == template.Topic {
				ids = append(ids, q.QueryID)
				seen[q.QueryID] = true
			}
		}
	}
	if s.memory != nil {
		s.memory.mu.Lock()
		defer s.memory.mu.Unlock()
		n := 0
		for _, r := range s.memory.auditRequests {
			if r.RunID == run.ID && seen[r.QueryID] {
				n++
			}
		}
		return n, nil
	}
	return s.client.GleanSearchRequest.Query().Where(gleansearchrequest.RunID(run.ID), gleansearchrequest.QueryIDIn(ids...)).Count(ctx)
}

// protocolBudgetReason 在冻结协议的所有来源和路线间累计预算，不因恢复进程而重置。
// 记录和队列在完整来源页之后检查，最多超出一页，已取得的原始出现不会丢弃。
func protocolBudgetReason(state *monitorState, run *Run, template SearchTemplate) string {
	if template.Protocol == nil {
		return ""
	}
	b := effectiveProtocolBudget(run, template)
	requests, pages, records, queue := 0, 0, 0, 0
	for _, source := range run.Sources {
		for _, q := range source.Queries {
			if q.Topic == template.Topic {
				requests += q.Requests
				pages += q.Pages
				records += q.Observed
			}
		}
	}
	for _, c := range state.Records {
		if c.RunID != run.ID || c.Quarantine != nil {
			continue
		}
		for _, topic := range c.Record.Topics {
			if topic == template.Topic && (!c.Published || (c.Pipeline != nil && !pipelineTerminal(c.Pipeline))) {
				queue++
				break
			}
		}
	}
	if requests >= b.MaxRequests {
		return "protocol_request_budget"
	}
	if pages >= b.MaxPages {
		return "protocol_page_budget"
	}
	if records >= b.MaxRecords {
		return "protocol_record_budget"
	}
	if queue >= b.MaxQueue {
		return "protocol_queue_budget"
	}
	return ""
}
