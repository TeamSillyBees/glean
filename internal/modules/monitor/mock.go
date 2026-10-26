package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// MockReviewer 是可复现的离线占位审核器，绝不生成真实科学审核或纳入结论。
type MockReviewer struct{}

// Review 返回明确待审核的模拟输出；不声称调用了模型。
func (MockReviewer) Review(ctx context.Context, record Record) (Review, error) {
	if err := ctx.Err(); err != nil {
		return Review{}, err
	}
	summary := "模型阅读占位输出；尚未进行真实科学审核。"
	if record.CoreDiscovery {
		summary = "核心期刊发现，主题相关性和科学纳入均待真实审核；当前仅为模型占位输出。"
	}
	return Review{Provider: "deterministic-mock", Model: "mock-no-model", PromptVersion: "mock-v1", IsMock: true, ScientificReviewed: false, InclusionStatus: "pending", Summary: summary}, nil
}

// MockPublisher 仅允许模拟输出投影到监控快照，不写历史论文目录。
type MockPublisher struct{}

// Publish 验证模拟边界；幂等结果由服务在同一快照中标记。
func (MockPublisher) Publish(ctx context.Context, _ idgen.ID, records []Publication) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, record := range records {
		if !record.Record.IsMock || !record.Review.IsMock || record.Review.ScientificReviewed || record.Review.InclusionStatus != "pending" {
			return fmt.Errorf("mock publisher refuses non-mock scientific conclusions")
		}
	}
	return nil
}

// FixtureSource 使用内存中的来源版本演练完整分页、时间窗与增量行为。
type FixtureSource struct {
	SourceName string
	Records    []Record
	PageSize   int
	Failure    error
}

// Name 返回夹具来源名。
func (s *FixtureSource) Name() string { return s.SourceName }

// IsMock 明确声明该来源没有进行外部采集。
func (s *FixtureSource) IsMock() bool { return true }

// Fetch 对夹具按主题、固定时间窗分页；用于离线端到端验证而非空桩。
func (s *FixtureSource) Fetch(ctx context.Context, request FetchRequest) (Page, error) {
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	if s.Failure != nil {
		return Page{}, s.Failure
	}
	offset := 0
	var err error
	if request.Cursor != "" {
		offset, err = strconv.Atoi(request.Cursor)
		if err != nil || offset < 0 {
			return Page{}, &SourceError{Kind: "cursor", Message: "invalid fixture cursor"}
		}
	}
	records := []Record{}
	for _, record := range s.Records {
		if record.SourceUpdatedAt.Before(request.Window.Start) || !record.SourceUpdatedAt.Before(request.Window.End) {
			continue
		}
		match := false
		for _, topic := range record.Topics {
			if topic == request.Template.Topic {
				match = true
			}
		}
		if match {
			records = append(records, record)
		}
	}
	if offset > len(records) {
		return Page{}, &SourceError{Kind: "cursor", Message: "fixture cursor outside result"}
	}
	size := s.PageSize
	if size < 1 {
		size = 2
	}
	end := offset + size
	if end > len(records) {
		end = len(records)
	}
	page := Page{Records: records[offset:end], Complete: end == len(records)}
	if !page.Complete {
		page.NextCursor = strconv.Itoa(end)
	}
	return page, nil
}

// DemoSources 返回四个离线来源以及可演练的新版本与正式出版事件；全部证据均标记 MOCK。
func DemoSources(now time.Time, incremental bool) []Source {
	base := now.UTC().Add(-10 * 24 * time.Hour)
	definitions := []struct{ name, topic string }{{"crossref", "biomed-ai"}, {"pubmed", "biomed-ai"}, {"arxiv", "llm-agent"}, {"biorxiv", "biomed-worldmodel"}}
	result := make([]Source, 0, len(definitions))
	for _, definition := range definitions {
		kind := "preprint"
		if definition.name == "crossref" || definition.name == "pubmed" {
			kind = "online"
		}
		record := Record{Source: definition.name, SourceID: "mock-" + definition.name + "-001", DOI: "10.0000/glean-mock-" + definition.name, Title: "[MOCK] " + definition.topic + " research pipeline fixture", Abstract: "Synthetic fixture only; not a real scientific paper.", Version: "1", Topics: []string{definition.topic}, SourceUpdatedAt: base, PublicationEvents: []PublicationEvent{{Kind: kind, Date: base.Add(-time.Hour), Evidence: "MOCK fixture; not verified publication"}}, Raw: json.RawMessage(`{"mock":true,"fixture":"baseline-v1"}`)}
		records := []Record{record}
		if incremental {
			incrementalAt := now.Add(72 * time.Hour)
			updated := record
			updated.Version = "2"
			updated.SourceUpdatedAt = incrementalAt.Add(-time.Hour)
			updated.Title += " — revised"
			updated.Raw = json.RawMessage(`{"mock":true,"fixture":"incremental-v2"}`)
			if definition.name == "biorxiv" {
				updated.PublicationEvents = append(append([]PublicationEvent(nil), updated.PublicationEvents...), PublicationEvent{Kind: "online", Date: incrementalAt.Add(-2 * time.Hour), Evidence: "MOCK formal-publication event fixture"})
			}
			fresh := record
			fresh.SourceID += "-new"
			fresh.DOI += "-new"
			fresh.Title = "[MOCK] New incremental candidate"
			fresh.SourceUpdatedAt = incrementalAt.Add(-30 * time.Minute)
			records = append(records, updated, fresh)
		}
		result = append(result, &FixtureSource{SourceName: definition.name, Records: records, PageSize: 1})
	}
	return result
}

// RunDemo 执行有限离线演练；不连接数据库、第三方来源或模型服务。
func RunDemo(ctx context.Context, now time.Time, node int64, scenario string) (Snapshot, error) {
	if scenario != "baseline" && scenario != "incremental" && scenario != "failure" {
		return Snapshot{}, fmt.Errorf("unknown offline scenario")
	}
	ids, err := idgen.NewGenerator(node)
	if err != nil {
		return Snapshot{}, err
	}
	cfg := DefaultConfig()
	cfg.MaxPagesPerCall = 100
	sources := DemoSources(now, false)
	if scenario == "failure" {
		sources[1].(*FixtureSource).Failure = &SourceError{Kind: "invalid_response", Message: "MOCK malformed source fixture"}
	}
	service, err := NewOfflineService(ids, cfg, sources, MockReviewer{}, MockPublisher{})
	if err != nil {
		return Snapshot{}, err
	}
	return runDemoScenario(ctx, service, now, scenario)
}

func runDemoScenario(ctx context.Context, service *Service, now time.Time, scenario string) (Snapshot, error) {
	snapshot, err := service.RunOnce(ctx, now)
	if err != nil {
		return Snapshot{}, err
	}
	if scenario == "incremental" {
		service.sources = DemoSources(now, true)
		return service.RunOnce(ctx, now.Add(72*time.Hour))
	}
	return snapshot, nil
}
