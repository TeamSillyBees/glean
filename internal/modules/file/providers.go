package file

import (
	"github.com/samber/do"
	platformstorage "github.com/teamsillybees/glean/internal/platform/storage"
)

const (
	fileServiceServiceName = "file.service"
	fileHandlerServiceName = "file.handler"
)

// Provide 使用 do 注册 file 示例模块依赖。
func Provide(injector *do.Injector, maxFileSize int64) {
	do.ProvideNamed(injector, fileServiceServiceName, func(i *do.Injector) (*Service, error) {
		storageService := do.MustInvoke[platformstorage.Service](i)
		return NewService(storageService), nil
	})
	do.ProvideNamed(injector, fileHandlerServiceName, func(i *do.Injector) (*Handler, error) {
		service := do.MustInvokeNamed[*Service](i, fileServiceServiceName)
		return NewHandler(service), nil
	})
	do.Provide(injector, func(i *do.Injector) (*Module, error) {
		handler := do.MustInvokeNamed[*Handler](i, fileHandlerServiceName)
		return NewModule(handler, maxFileSize), nil
	})
}
