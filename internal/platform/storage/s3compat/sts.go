package s3compat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/teamsillybees/glean/internal/platform/storage"
)

// STS 使用 AWS STS 生成临时凭证。
type STS struct {
	cfg    storage.Config
	client *sts.Client
}

// NewSTS 创建 AWS STS 服务。
func NewSTS(ctx context.Context, cfg storage.Config) (*STS, error) {
	cfg = storage.ConfigWithDefaults(cfg)
	if cfg.Provider != storage.ProviderAWSS3 {
		return nil, fmt.Errorf("%w: 仅 aws_s3 支持 AWS STS", storage.ErrUnsupported)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &STS{cfg: cfg, client: sts.NewFromConfig(awsCfg)}, nil
}

// GenerateToken 通过 AssumeRole 生成临时凭证。
func (s *STS) GenerateToken(ctx context.Context, input storage.STSTokenInput) (*storage.STSToken, error) {
	roleARN := firstNonEmpty(input.RoleARN, s.cfg.S3.RoleARN)
	if roleARN == "" {
		return nil, fmt.Errorf("%w: storage.s3.role_arn 不能为空", storage.ErrInvalidConfig)
	}
	duration := input.Duration
	if duration == 0 {
		duration = s.cfg.STS.Duration
	}
	sessionName := firstNonEmpty(input.RoleSessionName, s.cfg.STS.RoleSessionName)
	policy := input.Policy
	if policy == "" {
		var err error
		policy, err = buildAWSPolicy(storage.DefaultBucket(s.cfg, input.Bucket), input.Permission, input.Prefixes)
		if err != nil {
			return nil, err
		}
	}
	request := &sts.AssumeRoleInput{
		RoleArn:         aws.String(roleARN),
		RoleSessionName: aws.String(sessionName),
		DurationSeconds: aws.Int32(int32(duration.Seconds())),
		Policy:          aws.String(policy),
	}
	if externalID := firstNonEmpty(input.ExternalID, s.cfg.S3.ExternalID); externalID != "" {
		request.ExternalId = aws.String(externalID)
	}
	response, err := s.client.AssumeRole(ctx, request)
	if err != nil {
		return nil, err
	}
	if response.Credentials == nil {
		return nil, fmt.Errorf("%w: AWS STS 未返回 credentials", storage.ErrInvalidConfig)
	}
	return &storage.STSToken{
		Provider:        storage.ProviderAWSS3,
		AccessKeyID:     aws.ToString(response.Credentials.AccessKeyId),
		AccessKeySecret: aws.ToString(response.Credentials.SecretAccessKey),
		SecurityToken:   aws.ToString(response.Credentials.SessionToken),
		Expiration:      aws.ToTime(response.Credentials.Expiration),
		Bucket:          storage.DefaultBucket(s.cfg, input.Bucket),
		Region:          s.cfg.S3.Region,
		Endpoint:        s.cfg.S3.Endpoint,
	}, nil
}

func buildAWSPolicy(bucket string, permission storage.Permission, prefixes []string) (string, error) {
	if bucket == "" {
		return "", fmt.Errorf("%w: bucket 不能为空", storage.ErrInvalidConfig)
	}
	actions := []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket", "s3:AbortMultipartUpload", "s3:ListMultipartUploadParts"}
	switch permission {
	case storage.PermissionReadOnly:
		actions = []string{"s3:GetObject", "s3:ListBucket"}
	case storage.PermissionWriteOnly:
		actions = []string{"s3:PutObject", "s3:DeleteObject", "s3:AbortMultipartUpload", "s3:ListMultipartUploadParts"}
	}
	resources := []string{"arn:aws:s3:::" + bucket}
	if len(prefixes) == 0 {
		resources = append(resources, "arn:aws:s3:::"+bucket+"/*")
	} else {
		for _, prefix := range prefixes {
			prefix = strings.Trim(prefix, "/")
			if prefix == "" {
				resources = append(resources, "arn:aws:s3:::"+bucket+"/*")
				continue
			}
			resources = append(resources, "arn:aws:s3:::"+bucket+"/"+prefix+"/*")
		}
	}
	policy := map[string]any{
		"Version": "2012-10-17",
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
