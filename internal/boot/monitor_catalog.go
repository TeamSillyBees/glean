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
		input := catalog.ObservationInput{CoreDiscovery: r.CoreDiscovery, RunID: runID, Source: r.Source, SourceID: r.SourceID, SourceURL: r.SourceURL, PMID: r.PMID, Version: r.Version, Title: r.Title, Abstract: r.Abstract, URL: r.URL, DOI: r.DOI, ArxivID: r.ArxivID, Venue: r.Journal, ISSNs: r.ISSNs, Authors: r.Authors, PublicationType: r.PublicationType, ObservedAt: r.AcquiredAt, PublicationEvents: events, Topics: r.Topics, TemplateVersion: monitor.TemplateVersion, Raw: r.Raw, Review: catalog.ReviewVO{Provider: publication.Review.Provider, Model: publication.Review.Model, PromptVersion: publication.Review.PromptVersion, ReviewedAt: publication.Review.ReviewedAt, IsMock: publication.Review.IsMock, ScientificReviewed: publication.Review.ScientificReviewed, InclusionStatus: publication.Review.InclusionStatus, Summary: publication.Review.Summary}}
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
	return config, SourceHTTPFactoryOptions()
}

// SourceHTTPFactoryOptions 为启动层和有限实时采集 CLI 提供一致的官方来源网络边界。
func SourceHTTPFactoryOptions() []httpclient.FactoryOption { return monitor.SourceHTTPFactoryOptions() }
