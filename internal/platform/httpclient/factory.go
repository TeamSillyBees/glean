package httpclient

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

// Factory 按服务名创建并缓存 HTTP Client。
type Factory struct {
	cfg     Config
	logger  *logx.Logger
	hooks   map[string][]RequestHook
	mu      sync.Mutex
	clients map[string]*Client
}

// NewFactory 根据配置创建 HTTP Client 工厂。
func NewFactory(cfg Config, logger *logx.Logger, options ...FactoryOption) (*Factory, error) {
	if logger == nil {
		logger = logx.NewNop()
	}
	normalized := cfg.withDefaults()
	if err := normalized.Validate(); err != nil {
		return nil, err
	}
	factoryOptions, err := applyFactoryOptions(options)
	if err != nil {
		return nil, err
	}
	for serviceName := range factoryOptions.hooks {
		if _, ok := normalized.Services[serviceName]; !ok {
			return nil, fmt.Errorf("%w: request hooks reference %s", ErrServiceNotFound, serviceName)
		}
	}
	return &Factory{
		cfg:     normalized,
		logger:  logger,
		hooks:   factoryOptions.hooks,
		clients: make(map[string]*Client),
	}, nil
}

// Get 返回指定服务的缓存客户端；首次调用时按配置创建。
func (f *Factory) Get(serviceName string) (*Client, error) {
	if !f.cfg.Enabled {
		return nil, fmt.Errorf("%w: http_client.enabled=false", ErrDisabled)
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	if client, ok := f.clients[serviceName]; ok {
		return client, nil
	}

	serviceCfg, ok := f.cfg.Services[serviceName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrServiceNotFound, serviceName)
	}
	client, err := newClient(serviceName, f.cfg, serviceCfg.withDefaults(f.cfg), f.logger, f.hooks[serviceName])
	if err != nil {
		return nil, err
	}
	f.clients[serviceName] = client
	return client, nil
}

// Clear 关闭并移除指定服务的缓存客户端，后续 Get 会重新创建。
func (f *Factory) Clear(serviceName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if client, ok := f.clients[serviceName]; ok {
		client.closeIdleConnections()
		delete(f.clients, serviceName)
	}
}

// ClearAll 关闭并清空工厂当前缓存的全部客户端。
func (f *Factory) ClearAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name, client := range f.clients {
		client.closeIdleConnections()
		delete(f.clients, name)
	}
}

func newRestyClient(global Config, service ServiceConfig, hooks []RequestHook) *resty.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = proxyFunc(service.Proxy)
	transport.MaxIdleConns = global.MaxIdleConns
	transport.MaxIdleConnsPerHost = global.MaxIdleConnsPerHost
	transport.IdleConnTimeout = global.IdleConnTimeout
	hookedTransport := &hookTransport{base: transport, hooks: append([]RequestHook(nil), hooks...)}
	restyClient := resty.New().
		SetBaseURL(service.BaseURL).
		SetTimeout(service.Timeout).
		SetHeaders(service.Headers).
		SetTransport(hookedTransport).
		SetResponseBodyLimit(int(service.MaxResponseBodySize))

	if global.ConnectTimeout > 0 {
		dialer := &net.Dialer{Timeout: global.ConnectTimeout, KeepAlive: 30 * time.Second}
		transport.DialContext = dialer.DialContext
	}
	if service.Retry.Enabled && service.Retry.Count > 0 {
		restyClient.
			SetRetryCount(service.Retry.Count).
			SetRetryWaitTime(service.Retry.WaitTime).
			SetRetryMaxWaitTime(service.Retry.MaxWaitTime).
			AddRetryCondition(retryCondition(service.Retry))
	}
	return restyClient
}

func proxyFunc(proxy string) func(*http.Request) (*url.URL, error) {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return http.ProxyFromEnvironment
	}
	parsed, err := url.Parse(proxy)
	if err != nil {
		return http.ProxyFromEnvironment
	}
	return http.ProxyURL(parsed)
}
