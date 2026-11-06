package monitor

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
)

const sourceConnectionTimeout = 15 * time.Second

type sourceConnectionTester interface {
	checkConnection(context.Context) (int, error)
}

// TestSourceConnections 由管理员显式触发，每个来源最多一次只读请求，不加载或保存采集状态。
func (s *Service) TestSourceConnections(ctx context.Context) (SourceConnectionsVO, error) {
	principal, ok := platformauth.PrincipalFromContext(ctx)
	if !ok || principal.UserID <= 0 || !principal.IsSuperAdmin {
		return SourceConnectionsVO{}, bizerrors.CatalogForbidden()
	}
	if err := ctx.Err(); err != nil {
		return SourceConnectionsVO{}, err
	}
	if s.connectionTestMu == nil {
		return SourceConnectionsVO{}, bizerrors.BadRequest("来源连通性测试尚未配置。")
	}
	// 防止重复点击或多个页面同时把一次检测扩大成多轮外部请求。
	if !s.connectionTestMu.TryLock() {
		return SourceConnectionsVO{}, bizerrors.BadRequest("正在测试来源连通性，请稍后再试。")
	}
	defer s.connectionTestMu.Unlock()
	probeCtx, cancel := context.WithTimeout(ctx, sourceConnectionTimeout)
	defer cancel()
	items := make([]SourceConnectionVO, len(s.sources))
	var group sync.WaitGroup
	for i, source := range s.sources {
		group.Go(func() {
			started := time.Now()
			result := SourceConnectionVO{Source: source.Name(), Status: "skipped", Message: "该来源未配置真实连通性测试。"}
			if probe, supported := source.(sourceConnectionTester); supported && s.config.RealFetchEnabled && !isMockSource(source) {
				status, err := probe.checkConnection(probeCtx)
				result = sourceConnectionResult(source.Name(), status, err)
			}
			result.ElapsedMS = time.Since(started).Milliseconds()
			result.CheckedAt = time.Now().UTC()
			items[i] = result
		})
	}
	group.Wait()
	if err := ctx.Err(); err != nil {
		return SourceConnectionsVO{}, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Source < items[j].Source })
	return SourceConnectionsVO{Items: items}, nil
}

func sourceConnectionResult(source string, status int, err error) SourceConnectionVO {
	result := SourceConnectionVO{Source: source, StatusCode: status, Status: "connected", Message: "已收到有效的来源 API 响应。"}
	if err == nil {
		return result
	}
	result.Status, result.Message = "network_error", "服务器无法连接该来源，请稍后重试。"
	var sourceErr *SourceError
	var networkErr net.Error
	var dnsErr *net.DNSError
	var certificateErr x509.CertificateInvalidError
	var authorityErr x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkErr) && networkErr.Timeout():
		result.Status, result.Message = "timeout", "请求超时，未能确认连通，请稍后重试。"
	case errors.Is(err, context.Canceled):
		result.Status, result.Message = "skipped", "测试已取消。"
	case errors.As(err, &sourceErr):
		if sourceErr.RetryAfter > 0 {
			retryAt := time.Now().UTC().Add(sourceErr.RetryAfter)
			result.RetryAt = &retryAt
		}
		switch sourceErr.Kind {
		case "provider_backoff":
			result.Status, result.Message = "deferred", "来源要求等待后再试，本次未发出请求。"
		case "provider_forbidden":
			result.Status, result.Message = "blocked", "来源访问已被限制，本次未发出请求，请管理员检查服务器访问权限。"
		case "invalid_response":
			result.Status, result.Message = "invalid_response", "已收到响应，但不是有效的来源 API 数据。"
		case "http":
			switch status {
			case 401, 403:
				result.Status, result.Message = "blocked", fmt.Sprintf("来源拒绝访问（HTTP %d），请检查服务器访问权限。", status)
			case 429:
				result.Status, result.Message = "rate_limited", "来源请求过于频繁（HTTP 429），请稍后重试。"
			default:
				result.Status, result.Message = "unavailable", fmt.Sprintf("来源服务返回 HTTP %d，请稍后重试。", status)
			}
		}
	case errors.As(err, &dnsErr):
		result.Message = "来源域名解析失败，请检查服务器网络或 DNS。"
	case errors.As(err, &certificateErr), errors.As(err, &authorityErr), errors.As(err, &hostnameErr):
		result.Message = "来源 TLS 证书校验失败，请检查服务器网络与时间。"
	}
	return result
}
