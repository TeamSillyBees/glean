package aliyunoss

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	aliyunsts "github.com/aliyun/alibaba-cloud-sdk-go/services/sts"
	"github.com/teamsillybees/glean/internal/platform/storage"
)

// STS 使用阿里云 STS 生成临时凭证。
type STS struct {
	cfg    storage.Config
	client *aliyunsts.Client
}

// NewSTS 创建阿里云 STS 服务。
func NewSTS(_ context.Context, cfg storage.Config) (*STS, error) {
	cfg = storage.ConfigWithDefaults(cfg)
	if cfg.Provider != storage.ProviderAliyunOSS {
		return nil, fmt.Errorf("%w: 仅 aliyun_oss 支持阿里云 STS", storage.ErrUnsupported)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client, err := aliyunsts.NewClientWithAccessKey(cfg.Aliyun.Region, cfg.Aliyun.AccessKeyID, cfg.Aliyun.AccessKeySecret)
	if err != nil {
		return nil, err
	}
	return &STS{cfg: cfg, client: client}, nil
}

// GenerateToken 通过 AssumeRole 生成临时凭证。
func (s *STS) GenerateToken(_ context.Context, input storage.STSTokenInput) (*storage.STSToken, error) {
	roleARN := firstNonEmpty(input.RoleARN, s.cfg.Aliyun.RoleARN)
	if roleARN == "" {
		return nil, fmt.Errorf("%w: storage.aliyun.role_arn 不能为空", storage.ErrInvalidConfig)
	}
	duration := input.Duration
	if duration == 0 {
		duration = s.cfg.STS.Duration
	}
	policy := input.Policy
	if policy == "" {
		var err error
		policy, err = buildAliyunPolicy(storage.DefaultBucket(s.cfg, input.Bucket), input.Permission, input.Prefixes)
		if err != nil {
			return nil, err
		}
	}
	request := aliyunsts.CreateAssumeRoleRequest()
	request.RoleArn = roleARN
	request.RoleSessionName = firstNonEmpty(input.RoleSessionName, s.cfg.STS.RoleSessionName)
	request.Policy = policy
	request.DurationSeconds = requests.Integer(strconv.FormatInt(int64(duration.Seconds()), 10))
	response, err := s.client.AssumeRole(request)
	if err != nil {
		return nil, err
	}
	expiration, _ := time.Parse(time.RFC3339, response.Credentials.Expiration)
	return &storage.STSToken{
		Provider:        storage.ProviderAliyunOSS,
		AccessKeyID:     response.Credentials.AccessKeyId,
		AccessKeySecret: response.Credentials.AccessKeySecret,
		SecurityToken:   response.Credentials.SecurityToken,
		Expiration:      expiration,
		Bucket:          storage.DefaultBucket(s.cfg, input.Bucket),
		Region:          s.cfg.Aliyun.Region,
		Endpoint:        s.cfg.Aliyun.Endpoint,
	}, nil
}

func buildAliyunPolicy(bucket string, permission storage.Permission, prefixes []string) (string, error) {
	if bucket == "" {
		return "", fmt.Errorf("%w: bucket 不能为空", storage.ErrInvalidConfig)
	}
	actions := []string{"oss:GetObject", "oss:PutObject", "oss:DeleteObject", "oss:ListObjects"}
	switch permission {
	case storage.PermissionReadOnly:
		actions = []string{"oss:GetObject", "oss:ListObjects"}
	case storage.PermissionWriteOnly:
		actions = []string{"oss:PutObject", "oss:DeleteObject"}
	}
	resources := []string{"acs:oss:*:*:" + bucket}
	if len(prefixes) == 0 {
		resources = append(resources, "acs:oss:*:*:"+bucket+"/*")
	} else {
		for _, prefix := range prefixes {
			prefix = strings.Trim(prefix, "/")
			if prefix == "" {
				resources = append(resources, "acs:oss:*:*:"+bucket+"/*")
				continue
			}
			resources = append(resources, "acs:oss:*:*:"+bucket+"/"+prefix+"/*")
		}
	}
	policy := map[string]any{
		"Version": "1",
		"Statement": []map[string]any{
			{
				"Effect":   "Allow",
				"Action":   actions,
				"Resource": resources,
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
