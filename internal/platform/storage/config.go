package storage

import (
	"fmt"
	"strings"
	"time"
)

const maskedValue = "***"

// Config 描述统一文件与对象存储配置。
type Config struct {
	Enabled           bool          `mapstructure:"enabled"`
	Provider          Provider      `mapstructure:"provider"`
	Bucket            string        `mapstructure:"bucket"`
	PublicBaseURL     string        `mapstructure:"public_base_url"`
	PresignDefaultTTL time.Duration `mapstructure:"presign_default_ttl"`
	Local             LocalConfig   `mapstructure:"local"`
	S3                S3Config      `mapstructure:"s3"`
	Aliyun            AliyunConfig  `mapstructure:"aliyun"`
	Tencent           TencentConfig `mapstructure:"tencent"`
	STS               STSConfig     `mapstructure:"sts"`
}

// LocalConfig 描述本地文件系统存储配置。
type LocalConfig struct {
	RootDir           string   `mapstructure:"root_dir"`
	TempDir           string   `mapstructure:"temp_dir"`
	PublicBaseURL     string   `mapstructure:"public_base_url"`
	PrivateBaseURL    string   `mapstructure:"private_base_url"`
	GenerateDatePath  bool     `mapstructure:"generate_date_path"`
	AllowedExtensions []string `mapstructure:"allowed_extensions"`
	MaxSize           int64    `mapstructure:"max_size"`
}

// S3Config 描述 AWS S3 或 S3 兼容存储配置。
type S3Config struct {
	Region          string `mapstructure:"region"`
	Endpoint        string `mapstructure:"endpoint"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
	SessionToken    string `mapstructure:"session_token"`
	RoleARN         string `mapstructure:"role_arn"`
	ExternalID      string `mapstructure:"external_id"`
	UsePathStyle    bool   `mapstructure:"use_path_style"`
	CustomDomain    string `mapstructure:"custom_domain"`
	UseHTTPS        bool   `mapstructure:"use_https"`
}

// AliyunConfig 描述阿里云 OSS 配置。
type AliyunConfig struct {
	Endpoint        string `mapstructure:"endpoint"`
	Region          string `mapstructure:"region"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	AccessKeySecret string `mapstructure:"access_key_secret"`
	SecurityToken   string `mapstructure:"security_token"`
	RoleARN         string `mapstructure:"role_arn"`
	CustomDomain    string `mapstructure:"custom_domain"`
	UseHTTPS        bool   `mapstructure:"use_https"`
}

// TencentConfig 描述腾讯云 COS 配置。
type TencentConfig struct {
	Region        string `mapstructure:"region"`
	Endpoint      string `mapstructure:"endpoint"`
	SecretID      string `mapstructure:"secret_id"`
	SecretKey     string `mapstructure:"secret_key"`
	SessionToken  string `mapstructure:"session_token"`
	RoleARN       string `mapstructure:"role_arn"`
	CustomDomain  string `mapstructure:"custom_domain"`
	UseHTTPS      bool   `mapstructure:"use_https"`
	APPID         string `mapstructure:"appid"`
	BucketURLMode string `mapstructure:"bucket_url_mode"`
}

// STSConfig 描述临时授权默认参数。
type STSConfig struct {
	Enabled         bool          `mapstructure:"enabled"`
	Duration        time.Duration `mapstructure:"duration"`
	RoleSessionName string        `mapstructure:"role_session_name"`
	Policy          string        `mapstructure:"policy"`
}

// Validate 校验存储配置。
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	cfg := c.withDefaults()
	if cfg.PresignDefaultTTL < 0 {
		return fmt.Errorf("%w: storage.presign_default_ttl 不能为负数", ErrInvalidConfig)
	}
	switch cfg.Provider {
	case ProviderLocal:
		if strings.TrimSpace(cfg.Local.RootDir) == "" {
			return fmt.Errorf("%w: storage.local.root_dir 不能为空", ErrInvalidConfig)
		}
		if cfg.Local.MaxSize < 0 {
			return fmt.Errorf("%w: storage.local.max_size 不能为负数", ErrInvalidConfig)
		}
	case ProviderAWSS3:
		if strings.TrimSpace(cfg.Bucket) == "" {
			return fmt.Errorf("%w: storage.bucket 不能为空", ErrInvalidConfig)
		}
		if strings.TrimSpace(cfg.S3.Region) == "" {
			return fmt.Errorf("%w: storage.s3.region 不能为空", ErrInvalidConfig)
		}
		if err := validateCredentialPair("storage.s3.access_key_id", cfg.S3.AccessKeyID, "storage.s3.secret_access_key", cfg.S3.SecretAccessKey); err != nil {
			return err
		}
	case ProviderS3Compatible:
		if strings.TrimSpace(cfg.Bucket) == "" {
			return fmt.Errorf("%w: storage.bucket 不能为空", ErrInvalidConfig)
		}
		if strings.TrimSpace(cfg.S3.Endpoint) == "" {
			return fmt.Errorf("%w: storage.s3.endpoint 不能为空", ErrInvalidConfig)
		}
		if strings.TrimSpace(cfg.S3.Region) == "" {
			return fmt.Errorf("%w: storage.s3.region 不能为空", ErrInvalidConfig)
		}
		if err := validateCredentialPair("storage.s3.access_key_id", cfg.S3.AccessKeyID, "storage.s3.secret_access_key", cfg.S3.SecretAccessKey); err != nil {
			return err
		}
	case ProviderAliyunOSS:
		if strings.TrimSpace(cfg.Bucket) == "" {
			return fmt.Errorf("%w: storage.bucket 不能为空", ErrInvalidConfig)
		}
		if strings.TrimSpace(cfg.Aliyun.Endpoint) == "" {
			return fmt.Errorf("%w: storage.aliyun.endpoint 不能为空", ErrInvalidConfig)
		}
		if strings.TrimSpace(cfg.Aliyun.AccessKeyID) == "" {
			return fmt.Errorf("%w: storage.aliyun.access_key_id 不能为空", ErrInvalidConfig)
		}
		if strings.TrimSpace(cfg.Aliyun.AccessKeySecret) == "" {
			return fmt.Errorf("%w: storage.aliyun.access_key_secret 不能为空", ErrInvalidConfig)
		}
	case ProviderTencentCOS:
		if strings.TrimSpace(cfg.Bucket) == "" {
			return fmt.Errorf("%w: storage.bucket 不能为空", ErrInvalidConfig)
		}
		if strings.TrimSpace(cfg.Tencent.Endpoint) == "" && strings.TrimSpace(cfg.Tencent.Region) == "" {
			return fmt.Errorf("%w: storage.tencent.region 或 storage.tencent.endpoint 不能为空", ErrInvalidConfig)
		}
		if strings.TrimSpace(cfg.Tencent.SecretID) == "" {
			return fmt.Errorf("%w: storage.tencent.secret_id 不能为空", ErrInvalidConfig)
		}
		if strings.TrimSpace(cfg.Tencent.SecretKey) == "" {
			return fmt.Errorf("%w: storage.tencent.secret_key 不能为空", ErrInvalidConfig)
		}
	default:
		return fmt.Errorf("%w: storage.provider %q 不受支持", ErrInvalidConfig, cfg.Provider)
	}
	return nil
}

// SafeForLog 返回可安全写入日志的脱敏配置。
func (c Config) SafeForLog() map[string]any {
	cfg := c.withDefaults()
	return map[string]any{
		"enabled":             cfg.Enabled,
		"provider":            cfg.Provider,
		"bucket":              cfg.Bucket,
		"public_base_url":     cfg.PublicBaseURL,
		"presign_default_ttl": cfg.PresignDefaultTTL,
		"local":               cfg.Local,
		"s3":                  sanitizeS3Config(cfg.S3),
		"aliyun":              sanitizeAliyunConfig(cfg.Aliyun),
		"tencent":             sanitizeTencentConfig(cfg.Tencent),
		"sts":                 cfg.STS,
	}
}

func (c Config) withDefaults() Config {
	if c.Provider == "" {
		c.Provider = ProviderLocal
	}
	if c.PresignDefaultTTL == 0 {
		c.PresignDefaultTTL = 15 * time.Minute
	}
	if c.Local.RootDir == "" {
		c.Local.RootDir = "./uploads"
	}
	if c.Local.TempDir == "" {
		c.Local.TempDir = ".multipart"
	}
	if c.Local.PublicBaseURL == "" {
		c.Local.PublicBaseURL = c.PublicBaseURL
	}
	if c.Local.MaxSize == 0 {
		c.Local.MaxSize = 100 * 1024 * 1024
	}
	if c.STS.Duration == 0 {
		c.STS.Duration = time.Hour
	}
	if c.STS.RoleSessionName == "" {
		c.STS.RoleSessionName = "initra-storage-sts"
	}
	return c
}

func validateCredentialPair(idName string, idValue string, secretName string, secretValue string) error {
	idEmpty := strings.TrimSpace(idValue) == ""
	secretEmpty := strings.TrimSpace(secretValue) == ""
	if idEmpty != secretEmpty {
		if idEmpty {
			return fmt.Errorf("%w: %s 不能为空", ErrInvalidConfig, idName)
		}
		return fmt.Errorf("%w: %s 不能为空", ErrInvalidConfig, secretName)
	}
	return nil
}

func sanitizeS3Config(cfg S3Config) map[string]any {
	return map[string]any{
		"region":            cfg.Region,
		"endpoint":          cfg.Endpoint,
		"access_key_id":     maskIfSet(cfg.AccessKeyID),
		"secret_access_key": maskIfSet(cfg.SecretAccessKey),
		"session_token":     maskIfSet(cfg.SessionToken),
		"role_arn":          cfg.RoleARN,
		"external_id":       maskIfSet(cfg.ExternalID),
		"use_path_style":    cfg.UsePathStyle,
		"custom_domain":     cfg.CustomDomain,
		"use_https":         cfg.UseHTTPS,
	}
}

func sanitizeAliyunConfig(cfg AliyunConfig) map[string]any {
	return map[string]any{
		"endpoint":          cfg.Endpoint,
		"region":            cfg.Region,
		"access_key_id":     maskIfSet(cfg.AccessKeyID),
		"access_key_secret": maskIfSet(cfg.AccessKeySecret),
		"security_token":    maskIfSet(cfg.SecurityToken),
		"role_arn":          cfg.RoleARN,
		"custom_domain":     cfg.CustomDomain,
		"use_https":         cfg.UseHTTPS,
	}
}

func sanitizeTencentConfig(cfg TencentConfig) map[string]any {
	return map[string]any{
		"region":          cfg.Region,
		"endpoint":        cfg.Endpoint,
		"secret_id":       maskIfSet(cfg.SecretID),
		"secret_key":      maskIfSet(cfg.SecretKey),
		"session_token":   maskIfSet(cfg.SessionToken),
		"role_arn":        cfg.RoleARN,
		"custom_domain":   cfg.CustomDomain,
		"use_https":       cfg.UseHTTPS,
		"appid":           cfg.APPID,
		"bucket_url_mode": cfg.BucketURLMode,
	}
}

func maskIfSet(value string) string {
	if value == "" {
		return ""
	}
	return maskedValue
}
