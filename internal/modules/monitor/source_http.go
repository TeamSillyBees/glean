package monitor

import (
	"context"
	"fmt"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultSourceNames 返回实际支持的官方来源，不含任何模拟适配器。
func DefaultSourceNames() []string {
	return []string{"crossref", "pubmed", "arxiv", "biorxiv", "europepmc", "medrxiv"}
}

// SourceHTTPServices 返回启动层统一 HTTP 工厂所需的官方 URL，禁止底层自动重试和隐式联系人。
func SourceHTTPServices(contact string) map[string]httpclient.ServiceConfig {
	userAgent := "Glean/1.0 literature-monitor"
	if contact = strings.TrimSpace(contact); contact != "" {
		userAgent += " (contact: " + contact + ")"
	}
	hosts := map[string]string{"crossref": "https://api.crossref.org", "pubmed": "https://eutils.ncbi.nlm.nih.gov", "arxiv": "https://oaipmh.arxiv.org", "biorxiv": "https://api.biorxiv.org", "medrxiv": "https://api.biorxiv.org", "europepmc": "https://www.ebi.ac.uk"}
	result := map[string]httpclient.ServiceConfig{}
	for name, base := range hosts {
		result["glean-"+name] = httpclient.ServiceConfig{BaseURL: base, Timeout: 45 * time.Second, MaxResponseBodySize: 16 << 20, Headers: map[string]string{"User-Agent": userAgent}, Retry: httpclient.RetryConfig{Enabled: false, Count: 0}}
	}
	return result
}

// NewHTTPSourceSet 只注入平台 Executor；网络请求仅由有界采集或显式连通性测试触发。
func NewHTTPSourceSet(factory *httpclient.Factory, names []string) ([]Source, error) {
	if factory == nil {
		return nil, bizerrors.BadRequest("source HTTP factory is required")
	}
	if len(names) == 0 {
		names = DefaultSourceNames()
	}
	sources := make([]Source, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if seen[name] {
			return nil, bizerrors.BadRequest("duplicate source")
		}
		seen[name] = true
		client, err := factory.Get("glean-" + name)
		if err != nil {
			return nil, err
		}
		provider := name
		if name == "medrxiv" {
			provider = "biorxiv"
		}
		executor := &pacedExecutor{base: client, gate: sourceRequestGate(provider)}
		var source Source
		switch name {
		case "crossref":
			source = NewCrossrefSource(executor)
		case "pubmed":
			source = NewPubMedSource(executor)
		case "arxiv":
			source = NewArXivSource(executor)
		case "biorxiv":
			source = NewBioRxivSource(executor)
		case "europepmc":
			source = NewEuropePMCSource(executor)
		case "medrxiv":
			source = NewMedRxivSource(executor)
		default:
			return nil, bizerrors.BadRequest("unsupported source")
		}
		sources = append(sources, source)
	}
	return sources, nil
}

type requestGate struct {
	once         sync.Once
	token        chan struct{}
	next         time.Time
	backoffUntil time.Time
	blocked      bool
	status       int
}

var sourceGates = struct {
	sync.Mutex
	gates map[string]*requestGate
}{gates: map[string]*requestGate{}}

func sourceRequestGate(provider string) *requestGate {
	sourceGates.Lock()
	defer sourceGates.Unlock()
	gate := sourceGates.gates[provider]
	if gate == nil {
		gate = &requestGate{}
		sourceGates.gates[provider] = gate
	}
	return gate
}

type pacedExecutor struct {
	base httpclient.Executor
	gate *requestGate
}

// Do 对同一上游串行执行 HTTP，开始时间至少间隔三秒；共享 Retry-After，取消等待不发送请求。
func (p *pacedExecutor) Do(ctx context.Context, method, path string, opts ...httpclient.RequestOption) (*httpclient.Response, error) {
	p.gate.once.Do(func() { p.gate.token = make(chan struct{}, 1) })
	select {
	case p.gate.token <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-p.gate.token }()
	if p.gate.blocked {
		return nil, &SourceError{Kind: "provider_forbidden", StatusCode: 403, Message: "provider access is blocked; operator action required"}
	}
	if remaining := time.Until(p.gate.backoffUntil); remaining > 0 {
		return nil, &SourceError{Kind: "provider_backoff", StatusCode: p.gate.status, RetryAfter: remaining, Retryable: true, Message: "shared provider retry window is not due"}
	}
	if delay := time.Until(p.gate.next); delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.gate.next = time.Now().Add(3 * time.Second)
	response, err := p.base.Do(ctx, method, path, opts...)
	if response != nil {
		delay := ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
		if response.StatusCode == 429 || response.StatusCode == 503 || (response.StatusCode == 403 && delay > 0) {
			if delay <= 0 {
				delay = time.Minute
			}
			p.gate.backoffUntil = time.Now().Add(delay)
			p.gate.status = response.StatusCode
		}
		if response.StatusCode == 403 && delay <= 0 {
			p.gate.blocked = true
			p.gate.status = 403
		}
	}
	return response, err
}

// SourceHTTPFactoryOptions 在每个实际 RoundTrip（包括重定向）强制官方 HTTPS GET 来源边界。
func SourceHTTPFactoryOptions() []httpclient.FactoryOption {
	options := []httpclient.FactoryOption{}
	for name, service := range SourceHTTPServices("") {
		origin, _ := url.Parse(service.BaseURL)
		options = append(options, httpclient.WithServiceHooks(name, officialSourceHook(origin.Hostname())))
	}
	return options
}
func officialSourceHook(host string) httpclient.RequestHook {
	return func(request *http.Request) error {
		if request.Response != nil || request.URL == nil || request.Method != http.MethodGet || request.URL.Scheme != "https" || request.URL.User != nil || !strings.EqualFold(request.URL.Hostname(), host) || (request.URL.Port() != "" && request.URL.Port() != "443") {
			return fmt.Errorf("literature source request must use its official HTTPS origin without redirects")
		}
		return nil
	}
}
