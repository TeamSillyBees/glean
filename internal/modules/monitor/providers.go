package monitor

import (
	"time"

	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// Provide 显式装配离线模拟监控服务；注册不运行任务、不启动计时器或发起网络请求。
func Provide(injector *do.Injector) {
	do.Provide(injector, func(i *do.Injector) (*Service, error) {
		return NewService(do.MustInvoke[*ent.Client](i), do.MustInvoke[*idgen.Generator](i), DefaultConfig(), DemoSources(time.Now().UTC(), false), MockReviewer{}, MockPublisher{})
	})
	do.Provide(injector, func(i *do.Injector) (*Handler, error) { return NewHandler(do.MustInvoke[*Service](i)), nil })
	do.Provide(injector, func(i *do.Injector) (*Module, error) { return NewModule(do.MustInvoke[*Handler](i)), nil })
}
