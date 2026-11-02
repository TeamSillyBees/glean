package boot

import (
	"context"
	"fmt"

	"github.com/teamsillybees/glean/internal/modules/catalog"
	"github.com/teamsillybees/glean/internal/modules/monitor"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

type catalogObservationWriter interface {
	IngestObservation(context.Context, catalog.ObservationInput) (catalog.ObservationResult, error)
}

type catalogMonitorPublisher struct {
	catalog  catalogObservationWriter
	operator idgen.ID
}

// NewCatalogMonitorPublisher 将真实来源持久化到独立目录；审核 mock 不会污染论文来源真伪标记。
func NewCatalogMonitorPublisher(service *catalog.Service, operatorID idgen.ID) monitor.Publisher {
	if service == nil {
		return &catalogMonitorPublisher{operator: operatorID}
	}
	return &catalogMonitorPublisher{catalog: service, operator: operatorID}
}

// Publish 持久化真实来源并只把明确的单条坏数据归类为可隔离拒绝。
func (p *catalogMonitorPublisher) Publish(ctx context.Context, runID idgen.ID, publications []monitor.Publication) error {
	if p.catalog == nil || p.operator <= 0 {
		return fmt.Errorf("live catalog publisher requires an explicit active operator")
	}
	ctx = entx.WithOperatorID(ctx, p.operator)
	for _, publication := range publications {
		r := publication.Record
		if r.IsMock {
			return &monitor.RejectedRecordError{Kind: "mock_source_record"}
		}
		events := make([]catalog.PublicationEventVO, 0, len(r.PublicationEvents))
		for _, event := range r.PublicationEvents {
			events = append(events, catalog.PublicationEventVO{Kind: event.Kind, Date: event.Date, Evidence: event.Evidence})
		}
		input := catalog.ObservationInput{Pipeline: publication.Review.Pipeline, CoreDiscovery: r.CoreDiscovery, RunID: runID, Source: r.Source, SourceID: r.SourceID, SourceURL: r.SourceURL, PMID: r.PMID, Version: r.Version, Title: r.Title, Abstract: r.Abstract, URL: r.URL, DOI: r.DOI, ArxivID: r.ArxivID, Venue: r.Journal, ISSNs: r.ISSNs, Authors: r.Authors, PublicationType: r.PublicationType, ObservedAt: r.AcquiredAt, PublicationEvents: events, Topics: r.Topics, TemplateVersion: r.TemplateVersion, Raw: r.Raw, Review: catalog.ReviewVO{Pipeline: publication.Review.Pipeline, Provider: publication.Review.Provider, Model: publication.Review.Model, PromptVersion: publication.Review.PromptVersion, ReviewedAt: publication.Review.ReviewedAt, IsMock: publication.Review.IsMock, ScientificReviewed: publication.Review.ScientificReviewed, InclusionStatus: publication.Review.InclusionStatus, Summary: publication.Review.Summary}}
		if !r.SourceUpdatedAt.IsZero() {
			at := r.SourceUpdatedAt
			input.SourceUpdatedAt = &at
		}
		if _, err := p.catalog.IngestObservation(ctx, input); err != nil {
			if kind, rejected := catalog.ObservationRejectionKind(err); rejected {
				return &monitor.RejectedRecordError{Kind: kind}
			}
			return err
		}
	}
	return nil
}

type monitorJournalProvider struct{ catalog *catalog.Service }

// NewMonitorJournalProvider 只把有来源、指标年份与 ISSN 的真实注册记录交给发现适配器。
func NewMonitorJournalProvider(service *catalog.Service) monitor.JournalProvider {
	return &monitorJournalProvider{catalog: service}
}

// CoreJournals 提供有真实历史指标证据的发现目标，不批准新论文的发表资格。
func (p *monitorJournalProvider) CoreJournals(ctx context.Context) ([]monitor.JournalEntry, error) {
	records, err := p.catalog.CoreJournals(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]monitor.JournalEntry, 0, len(records))
	for _, v := range records {
		result = append(result, monitor.JournalEntry{DiscoveryTopics: v.DiscoveryTopics, QualificationVerified: v.QualificationVerified, Name: v.Name, ISSNs: v.ISSNs, JIF: v.JIF, MetricYear: v.MetricYear, MetricSource: v.MetricSource, IsMock: false})
	}
	return result, nil
}

func monitorHTTPConfig(cfg *Config) (httpclient.Config, []httpclient.FactoryOption) {
	config := cfg.HTTPClient
	config.Services = make(map[string]httpclient.ServiceConfig, len(cfg.HTTPClient.Services)+6)
	for name, service := range cfg.HTTPClient.Services {
		config.Services[name] = service
	}
	for name, service := range monitor.SourceHTTPServices(cfg.Monitor.Contact) {
		config.Services[name] = service
	}
	for name, service := range cfg.Monitor.Models.ModelHTTPServices() {
		config.Services[name] = service
	}
	for name, service := range monitor.MaterialHTTPServices(cfg.Monitor.Contact) {
		config.Services[name] = service
	}
	options := append(SourceHTTPFactoryOptions(), monitor.MaterialHTTPFactoryOptions()...)
	options = append(options, cfg.Monitor.Models.ModelHTTPFactoryOptions()...)
	return config, options
}

// SourceHTTPFactoryOptions 为启动层和有限实时采集 CLI 提供一致的官方来源网络边界。
func SourceHTTPFactoryOptions() []httpclient.FactoryOption { return monitor.SourceHTTPFactoryOptions() }

// catalogPipelineReader 保持归档读取与来源写入能力分离，返回完整原始材料。
type catalogPipelineReader interface {
	LoadPipelineObservation(context.Context, idgen.ID) (catalog.ObservationInput, error)
	FindPipelineObservation(context.Context, string, string, string) (catalog.ObservationInput, bool, error)
}

var _ monitor.PipelineArchive = (*catalogMonitorPublisher)(nil)

// LoadPipeline 恢复持久化候选的原始材料和当前处理档案，供热缓存淘汰后的查看和重试。
func (p *catalogMonitorPublisher) LoadPipeline(ctx context.Context, id idgen.ID) (monitor.Publication, error) {
	reader, ok := p.catalog.(catalogPipelineReader)
	if !ok {
		return monitor.Publication{}, fmt.Errorf("catalog pipeline archive unavailable")
	}
	input, err := reader.LoadPipelineObservation(ctx, id)
	if err != nil {
		return monitor.Publication{}, err
	}
	return archivedPublication(input), nil
}

// FindPipeline 根据来源及版本key复用已完成档案，避免采集重叠窗口重复模型费用。
func (p *catalogMonitorPublisher) FindPipeline(ctx context.Context, record monitor.Record, key string) (monitor.Publication, bool, error) {
	reader, ok := p.catalog.(catalogPipelineReader)
	if !ok {
		return monitor.Publication{}, false, fmt.Errorf("catalog pipeline archive unavailable")
	}
	input, found, err := reader.FindPipelineObservation(ctx, record.Source, record.SourceID, key)
	if err != nil || !found {
		return monitor.Publication{}, found, err
	}
	return archivedPublication(input), true, nil
}

func archivedPublication(input catalog.ObservationInput) monitor.Publication {
	events := make([]monitor.PublicationEvent, 0, len(input.PublicationEvents))
	for _, event := range input.PublicationEvents {
		events = append(events, monitor.PublicationEvent{Kind: event.Kind, Date: event.Date, Evidence: event.Evidence})
	}
	record := monitor.Record{TemplateVersion: input.TemplateVersion, CoreDiscovery: input.CoreDiscovery, SourceURL: input.SourceURL, PMID: input.PMID, ArxivID: input.ArxivID, Authors: input.Authors, PublicationType: input.PublicationType, Source: input.Source, SourceID: input.SourceID, DOI: input.DOI, Title: input.Title, Abstract: input.Abstract, URL: input.URL, Journal: input.Venue, Version: input.Version, Topics: input.Topics, ISSNs: input.ISSNs, AcquiredAt: input.ObservedAt, PublicationEvents: events, Raw: input.Raw}
	if input.SourceUpdatedAt != nil {
		record.SourceUpdatedAt = *input.SourceUpdatedAt
	}
	review := input.Review
	return monitor.Publication{Record: record, Review: monitor.Review{Pipeline: review.Pipeline, Provider: review.Provider, Model: review.Model, PromptVersion: review.PromptVersion, ReviewedAt: review.ReviewedAt, IsMock: review.IsMock, ScientificReviewed: review.ScientificReviewed, InclusionStatus: review.InclusionStatus, Summary: review.Summary}}
}
