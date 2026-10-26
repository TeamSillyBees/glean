package task

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/teamsillybees/glean/internal/platform/redisx"
)

// Backend 表示任务队列底层实现。
type Backend string

const (
	// BackendAsynq 表示使用 Asynq 作为任务队列后端。
	BackendAsynq Backend = "asynq"
)

const (
	// QueueCritical 是高优先级队列名称。
	QueueCritical = "critical"
	// QueueDefault 是默认队列名称。
	QueueDefault = "default"
	// QueueLow 是低优先级队列名称。
	QueueLow = "low"
)

// RetryStrategy 表示 Worker 级别重试延迟策略。
type RetryStrategy string

const (
	// RetryStrategyOfficial 表示保留 Asynq 官方默认重试策略。
	RetryStrategyOfficial RetryStrategy = "official"
	// RetryStrategyFixed 表示固定间隔重试。
	RetryStrategyFixed RetryStrategy = "fixed"
	// RetryStrategyLinear 表示线性递增间隔重试。
	RetryStrategyLinear RetryStrategy = "linear"
	// RetryStrategyExponential 表示指数递增间隔重试。
	RetryStrategyExponential RetryStrategy = "exponential"
)

// Config 描述任务队列发布、消费和调度配置。
type Config struct {
	Enabled   bool            `mapstructure:"enabled"`
	Backend   Backend         `mapstructure:"backend"`
	Redis     redisx.Config   `mapstructure:"redis"`
	Publisher PublisherConfig `mapstructure:"publisher"`
	Worker    WorkerConfig    `mapstructure:"worker"`
	Scheduler SchedulerConfig `mapstructure:"scheduler"`
}

// PublisherConfig 描述任务发布默认策略。
type PublisherConfig struct {
	DefaultQueue     string        `mapstructure:"default_queue"`
	DefaultMaxRetry  int           `mapstructure:"default_max_retry"`
	DefaultTimeout   time.Duration `mapstructure:"default_timeout"`
	DefaultRetention time.Duration `mapstructure:"default_retention"`
	EnforceBizKey    bool          `mapstructure:"enforce_biz_key"`
}

// WorkerConfig 描述任务消费端行为。
type WorkerConfig struct {
	Enabled                  bool             `mapstructure:"enabled"`
	Concurrency              int              `mapstructure:"concurrency"`
	ShutdownTimeout          time.Duration    `mapstructure:"shutdown_timeout"`
	HealthCheckInterval      time.Duration    `mapstructure:"health_check_interval"`
	DelayedTaskCheckInterval time.Duration    `mapstructure:"delayed_task_check_interval"`
	TaskCheckInterval        time.Duration    `mapstructure:"task_check_interval"`
	StrictPriority           bool             `mapstructure:"strict_priority"`
	Queues                   map[string]int   `mapstructure:"queues"`
	Retry                    RetryDelayConfig `mapstructure:"retry"`
}

// RetryDelayConfig 描述 Worker 自定义重试延迟策略。
type RetryDelayConfig struct {
	Strategy    RetryStrategy `mapstructure:"strategy"`
	Interval    time.Duration `mapstructure:"interval"`
	MaxInterval time.Duration `mapstructure:"max_interval"`
}

// SchedulerConfig 描述周期任务调度配置。
type SchedulerConfig struct {
	Enabled      bool          `mapstructure:"enabled"`
	SyncInterval time.Duration `mapstructure:"sync_interval"`
	Timezone     string        `mapstructure:"timezone"`
}

// Normalize 返回补齐默认值后的任务队列配置副本。
func (c Config) Normalize() Config {
	if c.Backend == "" {
		c.Backend = BackendAsynq
	}
	c.Publisher = c.Publisher.withDefaults()
	c.Worker = c.Worker.withDefaults()
	c.Scheduler = c.Scheduler.withDefaults()
	if c.Enabled {
		c.Redis.Enabled = true
	}
	if c.Redis.Mode == "" {
		c.Redis.Mode = redisx.ModeStandalone
	}
	if strings.TrimSpace(c.Redis.Addr) == "" && c.Redis.Mode == redisx.ModeStandalone {
		c.Redis.Addr = "127.0.0.1:6379"
	}
	return c
}

// Validate 校验任务队列配置。
func (c Config) Validate() error {
	cfg := c.Normalize()
	if !cfg.Enabled {
		return nil
	}
	if cfg.Backend != BackendAsynq {
		return fmt.Errorf("task.backend %q 不受支持", cfg.Backend)
	}
	if err := cfg.Redis.Validate(); err != nil {
		return fmt.Errorf("task.redis: %w", err)
	}
	if err := cfg.Publisher.validate(); err != nil {
		return err
	}
	if cfg.Worker.Enabled {
		if err := cfg.Worker.validate(); err != nil {
			return err
		}
	}
	if cfg.Scheduler.Enabled {
		if err := cfg.Scheduler.validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate 校验发布配置。
func (c PublisherConfig) Validate() error {
	return c.withDefaults().validate()
}

// Validate 校验 Worker 配置。
func (c WorkerConfig) Validate() error {
	return c.withDefaults().validate()
}

// Validate 校验调度器配置。
func (c SchedulerConfig) Validate() error {
	return c.withDefaults().validate()
}

// SafeForLog 返回可安全写入日志的脱敏配置副本。
func (c Config) SafeForLog() map[string]any {
	cfg := c.Normalize()
	return map[string]any{
		"enabled": cfg.Enabled,
		"backend": cfg.Backend,
		"redis":   cfg.Redis.SafeForLog(),
		"publisher": map[string]any{
			"default_queue":     cfg.Publisher.DefaultQueue,
			"default_max_retry": cfg.Publisher.DefaultMaxRetry,
			"default_timeout":   cfg.Publisher.DefaultTimeout,
			"default_retention": cfg.Publisher.DefaultRetention,
			"enforce_biz_key":   cfg.Publisher.EnforceBizKey,
		},
		"worker": map[string]any{
			"enabled":                     cfg.Worker.Enabled,
			"concurrency":                 cfg.Worker.Concurrency,
			"shutdown_timeout":            cfg.Worker.ShutdownTimeout,
			"health_check_interval":       cfg.Worker.HealthCheckInterval,
			"delayed_task_check_interval": cfg.Worker.DelayedTaskCheckInterval,
			"task_check_interval":         cfg.Worker.TaskCheckInterval,
			"strict_priority":             cfg.Worker.StrictPriority,
			"queues":                      cfg.Worker.Queues,
			"retry": map[string]any{
				"strategy":     cfg.Worker.Retry.Strategy,
				"interval":     cfg.Worker.Retry.Interval,
				"max_interval": cfg.Worker.Retry.MaxInterval,
			},
		},
		"scheduler": map[string]any{
			"enabled":       cfg.Scheduler.Enabled,
			"sync_interval": cfg.Scheduler.SyncInterval,
			"timezone":      cfg.Scheduler.Timezone,
		},
	}
}

func (c PublisherConfig) withDefaults() PublisherConfig {
	if strings.TrimSpace(c.DefaultQueue) == "" {
		c.DefaultQueue = QueueDefault
	}
	if c.DefaultTimeout == 0 {
		c.DefaultTimeout = 5 * time.Minute
	}
	if c.DefaultRetention == 0 {
		c.DefaultRetention = 24 * time.Hour
	}
	if c.DefaultMaxRetry == 0 {
		c.DefaultMaxRetry = 3
	}
	return c
}

func (c PublisherConfig) validate() error {
	if !ValidateQueueName(c.DefaultQueue) {
		return fmt.Errorf("task.publisher.default_queue %q 非法", c.DefaultQueue)
	}
	if c.DefaultMaxRetry < 0 {
		return fmt.Errorf("task.publisher.default_max_retry 不能小于 0")
	}
	if c.DefaultTimeout < 0 || c.DefaultRetention < 0 {
		return fmt.Errorf("task.publisher 默认超时和保留时间不能为负数")
	}
	return nil
}

func (c WorkerConfig) withDefaults() WorkerConfig {
	if c.Concurrency == 0 {
		c.Concurrency = 10
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
	if c.HealthCheckInterval == 0 {
		c.HealthCheckInterval = 15 * time.Second
	}
	if c.DelayedTaskCheckInterval == 0 {
		c.DelayedTaskCheckInterval = 5 * time.Second
	}
	if c.TaskCheckInterval == 0 {
		c.TaskCheckInterval = time.Second
	}
	if len(c.Queues) == 0 {
		c.Queues = map[string]int{
			QueueCritical: 6,
			QueueDefault:  3,
			QueueLow:      1,
		}
	}
	if c.Retry.Strategy == "" {
		c.Retry.Strategy = RetryStrategyOfficial
	}
	return c
}

func (c WorkerConfig) validate() error {
	if c.Concurrency <= 0 {
		return fmt.Errorf("task.worker.concurrency 必须大于 0")
	}
	if c.ShutdownTimeout < 0 || c.HealthCheckInterval < 0 ||
		c.DelayedTaskCheckInterval < 0 || c.TaskCheckInterval < 0 {
		return fmt.Errorf("task.worker 时间配置不能为负数")
	}
	if len(c.Queues) == 0 {
		return fmt.Errorf("task.worker.queues 不能为空")
	}
	for queue, weight := range c.Queues {
		if !ValidateQueueName(queue) {
			return fmt.Errorf("task.worker.queues 包含非法队列名 %q", queue)
		}
		if weight <= 0 {
			return fmt.Errorf("task.worker.queues.%s 权重必须大于 0", queue)
		}
	}
	if err := c.Retry.validate(); err != nil {
		return err
	}
	return nil
}

func (c RetryDelayConfig) validate() error {
	switch c.Strategy {
	case "", RetryStrategyOfficial, RetryStrategyFixed, RetryStrategyLinear, RetryStrategyExponential:
	default:
		return fmt.Errorf("task.worker.retry.strategy %q 不受支持", c.Strategy)
	}
	if c.Interval < 0 || c.MaxInterval < 0 {
		return fmt.Errorf("task.worker.retry 时间配置不能为负数")
	}
	return nil
}

func (c SchedulerConfig) withDefaults() SchedulerConfig {
	if c.SyncInterval == 0 {
		c.SyncInterval = 3 * time.Minute
	}
	if strings.TrimSpace(c.Timezone) == "" {
		c.Timezone = "Asia/Shanghai"
	}
	return c
}

func (c SchedulerConfig) validate() error {
	if c.SyncInterval < 0 {
		return fmt.Errorf("task.scheduler.sync_interval 不能为负数")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("task.scheduler.timezone %q 非法: %w", c.Timezone, err)
	}
	return nil
}

// ValidateQueueName 校验队列名是否符合框架约定。
func ValidateQueueName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
