package observability

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ReadinessChecker 描述一个必须在限定时间内完成的就绪检查。
type ReadinessChecker interface {
	CheckReadiness(ctx context.Context) error
}

// ReadinessCheckFunc 将函数适配为 ReadinessChecker。
type ReadinessCheckFunc func(ctx context.Context) error

// CheckReadiness 执行就绪检查函数。
func (f ReadinessCheckFunc) CheckReadiness(ctx context.Context) error {
	return f(ctx)
}

// ReadinessRegistry 保存应用的必要依赖检查项。
type ReadinessRegistry struct {
	mu     sync.RWMutex
	checks []readinessCheck
	names  map[string]struct{}
}

type readinessCheck struct {
	name    string
	timeout time.Duration
	checker ReadinessChecker
}

// NewReadinessRegistry 创建空的就绪检查注册表。
func NewReadinessRegistry() *ReadinessRegistry {
	return &ReadinessRegistry{names: make(map[string]struct{})}
}

// Register 注册一个带独立超时的必要依赖检查项。
func (r *ReadinessRegistry) Register(name string, timeout time.Duration, checker ReadinessChecker) error {
	if r == nil {
		return fmt.Errorf("readiness registry 不能为空")
	}
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return fmt.Errorf("readiness checker 名称不能为空")
	case timeout <= 0:
		return fmt.Errorf("readiness checker %q timeout 必须大于 0", name)
	case checker == nil:
		return fmt.Errorf("readiness checker %q 不能为空", name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.names[name]; exists {
		return fmt.Errorf("readiness checker %q 已注册", name)
	}
	r.names[name] = struct{}{}
	r.checks = append(r.checks, readinessCheck{name: name, timeout: timeout, checker: checker})
	return nil
}

// Check 并行执行全部检查；任一必要依赖失败或超时都会返回错误。
func (r *ReadinessRegistry) Check(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	checks := append([]readinessCheck(nil), r.checks...)
	r.mu.RUnlock()
	if len(checks) == 0 {
		return nil
	}

	type result struct {
		index int
		err   error
	}
	results := make(chan result, len(checks))
	for index, check := range checks {
		go func(index int, check readinessCheck) {
			results <- result{index: index, err: runReadinessCheck(ctx, check)}
		}(index, check)
	}

	errs := make([]error, len(checks))
	for range checks {
		item := <-results
		errs[item.index] = item.err
	}
	return errors.Join(errs...)
}

func runReadinessCheck(parent context.Context, check readinessCheck) error {
	ctx, cancel := context.WithTimeout(parent, check.timeout)
	defer cancel()

	result := make(chan error, 1)
	go func() {
		result <- check.checker.CheckReadiness(ctx)
	}()

	select {
	case err := <-result:
		if err != nil {
			return fmt.Errorf("%s: %w", check.name, err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", check.name, ctx.Err())
	}
}
