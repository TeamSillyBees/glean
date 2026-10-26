package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigValidateLocalWithDefaults(t *testing.T) {
	cfg := Config{
		Enabled:  true,
		Provider: ProviderLocal,
	}

	require.NoError(t, cfg.Validate())

	normalized := ConfigWithDefaults(cfg)
	require.Equal(t, "./uploads", normalized.Local.RootDir)
	require.Equal(t, int64(100*1024*1024), normalized.Local.MaxSize)
	require.Equal(t, 15*time.Minute, normalized.PresignDefaultTTL)
}

func TestConfigValidateRejectsIncompleteCloudConfig(t *testing.T) {
	cfg := Config{
		Enabled:  true,
		Provider: ProviderAliyunOSS,
		Bucket:   "assets",
		Aliyun: AliyunConfig{
			Endpoint:    "oss-cn-hangzhou.aliyuncs.com",
			AccessKeyID: "ak",
		},
	}

	require.ErrorContains(t, cfg.Validate(), "access_key_secret")
}

func TestConfigSafeForLogMasksSecrets(t *testing.T) {
	cfg := Config{
		Enabled:  true,
		Provider: ProviderAWSS3,
		Bucket:   "assets",
		S3: S3Config{
			Region:          "us-east-1",
			AccessKeyID:     "ak",
			SecretAccessKey: "secret",
			SessionToken:    "token",
		},
		Aliyun: AliyunConfig{
			AccessKeySecret: "aliyun-secret",
		},
		Tencent: TencentConfig{
			SecretID:  "sid",
			SecretKey: "skey",
		},
	}

	printable := cfg.SafeForLog()
	s3 := printable["s3"].(map[string]any)
	aliyun := printable["aliyun"].(map[string]any)
	tencent := printable["tencent"].(map[string]any)

	require.Equal(t, "***", s3["access_key_id"])
	require.Equal(t, "***", s3["secret_access_key"])
	require.Equal(t, "***", s3["session_token"])
	require.Equal(t, "***", aliyun["access_key_secret"])
	require.Equal(t, "***", tencent["secret_id"])
	require.Equal(t, "***", tencent["secret_key"])
}

func TestNormalizeObjectKeyRejectsTraversal(t *testing.T) {
	_, err := NormalizeObjectKey("../secret.txt")
	require.ErrorIs(t, err, ErrInvalidKey)

	key, err := NormalizeObjectKey("/images\\avatar.png")
	require.NoError(t, err)
	require.Equal(t, "images/avatar.png", key)
}
