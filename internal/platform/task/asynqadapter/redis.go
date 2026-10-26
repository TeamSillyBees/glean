package asynqadapter

import (
	"crypto/tls"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/teamsillybees/glean/internal/platform/redisx"
)

func redisConnOpt(cfg redisx.Config) (asynq.RedisConnOpt, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	switch cfg.Mode {
	case "", redisx.ModeStandalone:
		return asynq.RedisClientOpt{
			Addr:         cfg.Addr,
			Username:     cfg.Username,
			Password:     cfg.Password,
			DB:           cfg.DB,
			DialTimeout:  cfg.Timeout.Dial,
			ReadTimeout:  cfg.Timeout.Read,
			WriteTimeout: cfg.Timeout.Write,
			PoolSize:     cfg.Pool.Size,
			TLSConfig:    tlsConfig(cfg.TLS),
		}, nil
	case redisx.ModeSentinel:
		return asynq.RedisFailoverClientOpt{
			MasterName:       cfg.Sentinel.MasterName,
			SentinelAddrs:    cfg.Sentinel.Addrs,
			SentinelUsername: cfg.Sentinel.Username,
			SentinelPassword: cfg.Sentinel.Password,
			Username:         cfg.Username,
			Password:         cfg.Password,
			DB:               cfg.DB,
			DialTimeout:      cfg.Timeout.Dial,
			ReadTimeout:      cfg.Timeout.Read,
			WriteTimeout:     cfg.Timeout.Write,
			PoolSize:         cfg.Pool.Size,
			TLSConfig:        tlsConfig(cfg.TLS),
		}, nil
	default:
		return nil, fmt.Errorf("task redis mode %q 不受支持", cfg.Mode)
	}
}

func tlsConfig(cfg redisx.TLSConfig) *tls.Config {
	if !cfg.Enabled {
		return nil
	}
	return &tls.Config{
		ServerName:         cfg.ServerName,
		InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // 由调用方显式配置，常用于内网自签证书。
		MinVersion:         tls.VersionTLS12,
	}
}
