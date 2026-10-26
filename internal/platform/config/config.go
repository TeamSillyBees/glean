package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const (
	maskedValue      = "***"
	maxSanitizeDepth = 32
)

type sanitizeVisit struct {
	kind   reflect.Kind
	typeOf reflect.Type
	ptr    uintptr
}

// LoaderOptions 描述配置加载入口所需的最小输入。
type LoaderOptions struct {
	Env        string
	ConfigDir  string
	ConfigName string
	EnvPrefix  string
	Defaults   map[string]any
}

// Validator 允许业务配置在反序列化后执行自己的校验逻辑。
type Validator interface {
	Validate() error
}

// LoadInto 从默认值、可选基础 YAML、可选环境 YAML 和环境变量中加载业务自定义配置结构。
func LoadInto[T any](opts LoaderOptions) (*T, error) {
	normalized := normalizeOptions(opts)

	v := viper.New()
	v.SetConfigType("yaml")
	v.AddConfigPath(normalized.ConfigDir)
	v.SetEnvPrefix(normalized.EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	for key, value := range normalized.Defaults {
		v.SetDefault(key, value)
	}
	var cfg T
	if configTypeHasKey(reflect.TypeOf(cfg), "app", "env") {
		v.Set("app.env", normalized.Env)
	}
	if err := bindConfigEnvKeys(v, reflect.TypeOf(cfg), ""); err != nil {
		return nil, err
	}

	v.SetConfigName(normalized.ConfigName)
	if err := readOptionalConfig(v); err != nil {
		return nil, fmt.Errorf("读取基础配置文件 %s.yaml 失败: %w", normalized.ConfigName, err)
	}

	envConfigName := fmt.Sprintf("%s.%s", normalized.ConfigName, normalized.Env)
	v.SetConfigName(envConfigName)
	if err := mergeOptionalConfig(v); err != nil {
		return nil, fmt.Errorf("读取环境配置文件 %s.yaml 失败: %w", envConfigName, err)
	}

	if err := v.UnmarshalExact(&cfg, viper.DecodeHook(mapstructure.StringToTimeDurationHookFunc())); err != nil {
		return nil, fmt.Errorf("反序列化配置失败: %w", err)
	}

	if validator, ok := any(&cfg).(Validator); ok {
		if err := validator.Validate(); err != nil {
			return nil, err
		}
	}

	return &cfg, nil
}

// Sanitize 返回适合打印到日志的配置副本，敏感字段会被脱敏。
func Sanitize(value any, fields []string) map[string]any {
	sanitized := sanitizeValue(
		reflect.ValueOf(value),
		"",
		newSensitiveFieldSet(fields),
		0,
		make(map[sanitizeVisit]struct{}),
	)
	if result, ok := sanitized.(map[string]any); ok {
		return result
	}
	return map[string]any{
		"value": sanitized,
	}
}

// normalizeOptions 为配置加载入口补齐默认环境、目录和环境变量前缀。
func normalizeOptions(opts LoaderOptions) LoaderOptions {
	if opts.Env == "" {
		opts.Env = strings.TrimSpace(os.Getenv("APP_ENV"))
		if opts.Env == "" {
			opts.Env = "dev"
		}
	}
	if opts.ConfigDir == "" {
		opts.ConfigDir = filepath.Join(".", "configs")
	}
	if opts.ConfigName == "" {
		opts.ConfigName = "config"
	}
	if opts.EnvPrefix == "" {
		opts.EnvPrefix = "INITRA"
	}
	if opts.Defaults == nil {
		opts.Defaults = map[string]any{}
	}
	return opts
}

// readOptionalConfig 读取当前 Viper 配置文件，缺失文件时跳过。
func readOptionalConfig(v *viper.Viper) error {
	if err := v.ReadInConfig(); err != nil {
		if isConfigFileNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// mergeOptionalConfig 合并当前 Viper 配置文件，缺失文件时跳过。
func mergeOptionalConfig(v *viper.Viper) error {
	if err := v.MergeInConfig(); err != nil {
		if isConfigFileNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// isConfigFileNotFound 判断错误是否表示配置文件不存在。
func isConfigFileNotFound(err error) bool {
	var notFound viper.ConfigFileNotFoundError
	return errors.As(err, &notFound)
}

// bindConfigEnvKeys 递归绑定配置结构字段，确保纯环境变量配置也能被反序列化。
func bindConfigEnvKeys(v *viper.Viper, valueType reflect.Type, prefix string) error {
	if valueType == nil {
		return nil
	}
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if valueType.Kind() != reflect.Struct {
		return nil
	}

	for i := 0; i < valueType.NumField(); i++ {
		field := valueType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, ok := configFieldName(field)
		if !ok {
			continue
		}
		key := joinConfigKey(prefix, name)
		fieldType := field.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		if fieldType.Kind() == reflect.Struct {
			if err := bindConfigEnvKeys(v, fieldType, key); err != nil {
				return err
			}
			continue
		}
		if err := v.BindEnv(key); err != nil {
			return fmt.Errorf("绑定环境变量 %s 失败: %w", key, err)
		}
	}
	return nil
}

// joinConfigKey 拼接 Viper 使用的点分隔配置键。
func joinConfigKey(prefix string, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func configTypeHasKey(valueType reflect.Type, path ...string) bool {
	for valueType != nil && valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if valueType == nil || valueType.Kind() != reflect.Struct || len(path) == 0 {
		return false
	}
	for i := 0; i < valueType.NumField(); i++ {
		field := valueType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, ok := configFieldName(field)
		if !ok || name != path[0] {
			continue
		}
		if len(path) == 1 {
			return true
		}
		return configTypeHasKey(field.Type, path[1:]...)
	}
	return false
}

func sanitizeValue(
	value reflect.Value,
	key string,
	sensitiveFields map[string]struct{},
	depth int,
	seen map[sanitizeVisit]struct{},
) any {
	if isSensitiveField(key, sensitiveFields) || depth >= maxSanitizeDepth {
		return maskedValue
	}
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return sanitizeValue(value.Elem(), key, sensitiveFields, depth+1, seen)
	}
	if release, repeated := enterSanitizeValue(value, seen); repeated {
		return maskedValue
	} else if release != nil {
		defer release()
	}

	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		return sanitizeValue(value.Elem(), key, sensitiveFields, depth+1, seen)
	case reflect.Struct:
		return sanitizeStruct(value, sensitiveFields, depth, seen)
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		return sanitizeMap(value, sensitiveFields, depth, seen)
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil
		}
		items := make([]any, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			items = append(items, sanitizeValue(value.Index(i), "", sensitiveFields, depth+1, seen))
		}
		return items
	case reflect.String:
		return sanitizeConfigString(value.String(), sensitiveFields)
	default:
		if value.CanInterface() {
			return value.Interface()
		}
		return nil
	}
}

func sanitizeStruct(
	value reflect.Value,
	sensitiveFields map[string]struct{},
	depth int,
	seen map[sanitizeVisit]struct{},
) map[string]any {
	result := make(map[string]any)
	valueType := value.Type()
	for i := 0; i < value.NumField(); i++ {
		field := valueType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, ok := configFieldName(field)
		if !ok {
			continue
		}
		result[name] = sanitizeValue(value.Field(i), name, sensitiveFields, depth+1, seen)
	}
	return result
}

func sanitizeMap(
	value reflect.Value,
	sensitiveFields map[string]struct{},
	depth int,
	seen map[sanitizeVisit]struct{},
) map[string]any {
	result := make(map[string]any, value.Len())
	iter := value.MapRange()
	for iter.Next() {
		key := fmt.Sprint(iter.Key().Interface())
		result[key] = sanitizeValue(iter.Value(), key, sensitiveFields, depth+1, seen)
	}
	return result
}

func sanitizeConfigString(value string, sensitiveFields map[string]struct{}) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value
	}
	changed := false
	if parsed.User != nil {
		parsed.User = url.User(maskedValue)
		changed = true
	}
	query := parsed.Query()
	for key := range query {
		if isSensitiveField(key, sensitiveFields) {
			query.Set(key, maskedValue)
			changed = true
		}
	}
	if !changed {
		return value
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func enterSanitizeValue(value reflect.Value, seen map[sanitizeVisit]struct{}) (func(), bool) {
	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if value.IsNil() {
			return nil, false
		}
	default:
		return nil, false
	}
	visit := sanitizeVisit{kind: value.Kind(), typeOf: value.Type(), ptr: value.Pointer()}
	if _, ok := seen[visit]; ok {
		return nil, true
	}
	seen[visit] = struct{}{}
	return func() { delete(seen, visit) }, false
}

func configFieldName(field reflect.StructField) (string, bool) {
	for _, tagName := range []string{"mapstructure", "yaml", "json"} {
		tag := field.Tag.Get(tagName)
		if tag == "-" {
			return "", false
		}
		if tag != "" {
			name := strings.Split(tag, ",")[0]
			if name != "" {
				return name, true
			}
		}
	}
	return toSnakeName(field.Name), true
}

func toSnakeName(name string) string {
	var builder strings.Builder
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) {
			builder.WriteByte('_')
		}
		builder.WriteRune(unicode.ToLower(r))
	}
	return builder.String()
}

func newSensitiveFieldSet(fields []string) map[string]struct{} {
	defaults := []string{
		"password",
		"passwd",
		"pwd",
		"token",
		"secret",
		"credential",
		"authorization",
		"access_key",
		"secret_key",
		"private_key",
		"cookie",
		"session",
		"dsn",
		"data_source_name",
		"database_url",
		"connection_string",
	}
	result := make(map[string]struct{}, len(defaults)+len(fields))
	for _, field := range append(defaults, fields...) {
		normalized := normalizeSensitiveField(field)
		if normalized != "" {
			result[normalized] = struct{}{}
		}
	}
	return result
}

func isSensitiveField(key string, sensitiveFields map[string]struct{}) bool {
	normalized := normalizeSensitiveField(key)
	if normalized == "" {
		return false
	}
	if _, ok := sensitiveFields[normalized]; ok {
		return true
	}
	for field := range sensitiveFields {
		switch field {
		case "password", "passwd", "pwd", "secret", "credential", "authorization",
			"accesskey", "secretkey", "privatekey", "cookie", "session", "dsn",
			"datasourcename", "databaseurl", "connectionstring":
			if strings.Contains(normalized, field) {
				return true
			}
		case "token":
			if normalized == "token" || strings.HasSuffix(normalized, "token") {
				return true
			}
		}
	}
	return false
}

func normalizeSensitiveField(field string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(field)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
