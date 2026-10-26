package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ConfigWithDefaults 返回补齐默认值后的配置副本。
func ConfigWithDefaults(cfg Config) Config {
	return cfg.withDefaults()
}

// DefaultBucket 返回输入桶名或配置默认桶名。
func DefaultBucket(cfg Config, bucket string) string {
	if strings.TrimSpace(bucket) != "" {
		return strings.TrimSpace(bucket)
	}
	return strings.TrimSpace(cfg.Bucket)
}

// NormalizeObjectKey 清洗对象 key，拒绝绝对路径和路径穿越。
func NormalizeObjectKey(key string) (string, error) {
	normalized := strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
	normalized = strings.TrimLeft(normalized, "/")
	if normalized == "" {
		return "", fmt.Errorf("%w: key 不能为空", ErrInvalidKey)
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	return cleaned, nil
}

// FileNameFromKey 返回对象 key 中的文件名。
func FileNameFromKey(key string) string {
	key = strings.TrimRight(key, "/")
	if key == "" {
		return ""
	}
	return path.Base(key)
}

// ContentTypeFromName 根据文件名推断 MIME 类型。
func ContentTypeFromName(name string) string {
	if ext := filepath.Ext(name); ext != "" {
		return mime.TypeByExtension(ext)
	}
	return ""
}

// JoinURL 拼接 URL 前缀和对象 key。
func JoinURL(baseURL string, parts ...string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return ""
	}
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(strings.ReplaceAll(part, "\\", "/"), "/")
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return baseURL
	}
	return baseURL + "/" + strings.Join(cleaned, "/")
}

// NewObjectKey 生成按日期分层的对象 key。
func NewObjectKey(fileName string, now time.Time) (string, error) {
	if strings.TrimSpace(fileName) == "" {
		return "", fmt.Errorf("%w: filename 不能为空", ErrInvalidKey)
	}
	random, err := randomHex(16)
	if err != nil {
		return "", err
	}
	ext := filepath.Ext(fileName)
	return path.Join(now.Format("2006/01/02"), random+ext), nil
}

// SortUploadedParts 按分片编号排序并返回副本。
func SortUploadedParts(parts []UploadedPart) []UploadedPart {
	copied := append([]UploadedPart(nil), parts...)
	sort.Slice(copied, func(i int, j int) bool {
		return copied[i].PartNumber < copied[j].PartNumber
	})
	return copied
}

func randomHex(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
