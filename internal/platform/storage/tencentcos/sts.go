package tencentcos

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/platform/storage"
	tencentcommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	tencentsts "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sts/v20180813"
)

// STS 使用腾讯云 STS 生成临时凭证。
type STS struct {
	cfg    storage.Config
	client *tencentsts.Client
}

// NewSTS 创建腾讯云 STS 服务。
func NewSTS(_ context.Context, cfg storage.Config) (*STS, error) {
	cfg = storage.ConfigWithDefaults(cfg)
	if cfg.Provider != storage.ProviderTencentCOS {
		return nil, fmt.Errorf("%w: 仅 tencent_cos 支持腾讯云 STS", storage.ErrUnsupported)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	credential := tencentcommon.NewCredential(cfg.Tencent.SecretID, cfg.Tencent.SecretKey)
	client, err := tencentsts.NewClient(credential, cfg.Tencent.Region, profile.NewClientProfile())
	if err != nil {
		return nil, err
	}
	return &STS{cfg: cfg, client: client}, nil
}

// GenerateToken 通过 AssumeRole 生成临时凭证。
func (s *STS) GenerateToken(_ context.Context, input storage.STSTokenInput) (*storage.STSToken, error) {
	roleARN := firstNonEmpty(input.RoleARN, s.cfg.Tencent.RoleARN)
	if roleARN == "" {
		return nil, fmt.Errorf("%w: storage.tencent.role_arn 不能为空", storage.ErrInvalidConfig)
	}
	duration := input.Duration
	if duration == 0 {
		duration = s.cfg.STS.Duration
	}
	policy := input.Policy
	if policy == "" {
		var err error
		policy, err = buildTencentPolicy(s.cfg, storage.DefaultBucket(s.cfg, input.Bucket), input.Permission, input.Prefixes)
		if err != nil {
			return nil, err
		}
	}
	request := tencentsts.NewAssumeRoleRequest()
	request.RoleArn = new(roleARN)
	request.RoleSessionName = new(firstNonEmpty(input.RoleSessionName, s.cfg.STS.RoleSessionName))
	request.DurationSeconds = new(uint64(duration.Seconds()))
	request.Policy = new(policy)
	response, err := s.client.AssumeRole(request)
	if err != nil {
		return nil, err
	}
	if response.Response == nil || response.Response.Credentials == nil {
		return nil, fmt.Errorf("%w: 腾讯云 STS 未返回 credentials", storage.ErrInvalidConfig)
	}
	expiration := time.Time{}
	if response.Response.Expiration != nil {
		expiration, _ = time.Parse(time.RFC3339, *response.Response.Expiration)
	}
	credentials := response.Response.Credentials
	return &storage.STSToken{
		Provider:        storage.ProviderTencentCOS,
		AccessKeyID:     stringValue(credentials.TmpSecretId),
		AccessKeySecret: stringValue(credentials.TmpSecretKey),
		SecurityToken:   stringValue(credentials.Token),
		Expiration:      expiration,
		Bucket:          storage.DefaultBucket(s.cfg, input.Bucket),
		Region:          s.cfg.Tencent.Region,
		Endpoint:        s.cfg.Tencent.Endpoint,
	}, nil
}

func buildTencentPolicy(cfg storage.Config, bucket string, permission storage.Permission, prefixes []string) (string, error) {
	if bucket == "" {
		return "", fmt.Errorf("%w: bucket 不能为空", storage.ErrInvalidConfig)
	}
	if cfg.Tencent.APPID == "" {
		return "", fmt.Errorf("%w: storage.tencent.appid 不能为空，或显式传入 STS policy", storage.ErrInvalidConfig)
	}
	actions := []string{"name/cos:GetObject", "name/cos:PutObject", "name/cos:DeleteObject", "name/cos:ListMultipartUploads", "name/cos:UploadPart", "name/cos:CompleteMultipartUpload", "name/cos:AbortMultipartUpload"}
	switch permission {
	case storage.PermissionReadOnly:
		actions = []string{"name/cos:GetObject"}
	case storage.PermissionWriteOnly:
		actions = []string{"name/cos:PutObject", "name/cos:DeleteObject", "name/cos:ListMultipartUploads", "name/cos:UploadPart", "name/cos:CompleteMultipartUpload", "name/cos:AbortMultipartUpload"}
	}
	resources := []string{}
	if len(prefixes) == 0 {
		resources = append(resources, fmt.Sprintf("qcs::cos:%s:uid/%s:%s/*", cfg.Tencent.Region, cfg.Tencent.APPID, bucket))
	} else {
		for _, prefix := range prefixes {
			prefix = strings.Trim(prefix, "/")
			if prefix == "" {
				resources = append(resources, fmt.Sprintf("qcs::cos:%s:uid/%s:%s/*", cfg.Tencent.Region, cfg.Tencent.APPID, bucket))
				continue
			}
			resources = append(resources, fmt.Sprintf("qcs::cos:%s:uid/%s:%s/%s/*", cfg.Tencent.Region, cfg.Tencent.APPID, bucket, prefix))
		}
	}
	policy := map[string]any{
		"version": "2.0",
		"statement": []map[string]any{
			{
				"effect":   "allow",
				"action":   actions,
				"resource": resources,
			},
		},
	}
	data, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringPtr(value string) *string {
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
