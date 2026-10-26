package logx

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// jsonlRotationWriter 按日期和可选大小滚动写入 JSONL 文件。
type jsonlRotationWriter struct {
	basePath     string
	dateFormat   string
	maxSizeBytes int64
	mu           sync.Mutex
	file         *os.File
	currentDate  string
	currentIndex int
	currentSize  int64
}

// newJSONLRotationWriter 创建 JSONL 滚动文件 writer。
func newJSONLRotationWriter(path string, cfg RotationConfig) (*jsonlRotationWriter, error) {
	writer := &jsonlRotationWriter{
		basePath:     strings.TrimSpace(path),
		dateFormat:   firstNonEmpty(cfg.DateFormat, DefaultRotationDateFormat),
		maxSizeBytes: maxSizeBytes(cfg.MaxSizeMB),
	}
	if err := ensureJSONLParent(writer.basePath); err != nil {
		return nil, err
	}
	return writer, nil
}

// Write 写入一条 JSONL 日志，并在日期或大小阈值变化时切换文件。
func (w *jsonlRotationWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	date := time.Now().Format(w.dateFormat)
	if err := w.ensureFile(date, len(payload)); err != nil {
		return 0, err
	}
	written, err := w.file.Write(payload)
	w.currentSize += int64(written)
	return written, err
}

// Sync 刷新当前文件。
func (w *jsonlRotationWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}
	return w.file.Sync()
}

// Close 关闭当前文件。
func (w *jsonlRotationWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.closeCurrent()
}

// ensureFile 确保当前日期和大小阈值对应的文件已打开。
func (w *jsonlRotationWriter) ensureFile(date string, nextSize int) error {
	if w.file == nil || w.currentDate != date {
		if err := w.openDateFile(date); err != nil {
			return err
		}
	}
	if w.shouldRotateBySize(nextSize) {
		w.currentIndex++
		return w.openCurrentIndexedFile()
	}
	return nil
}

// openDateFile 打开指定日期下第一个可写文件。
func (w *jsonlRotationWriter) openDateFile(date string) error {
	if err := w.closeCurrent(); err != nil {
		return err
	}
	w.currentDate = date
	w.currentIndex = 0
	if w.maxSizeBytes > 0 {
		for {
			size, err := fileSize(w.pathFor(w.currentDate, w.currentIndex))
			if err != nil {
				return err
			}
			if size < w.maxSizeBytes {
				break
			}
			w.currentIndex++
		}
	}
	return w.openCurrentIndexedFile()
}

// openCurrentIndexedFile 打开当前日期和序号对应的文件。
func (w *jsonlRotationWriter) openCurrentIndexedFile() error {
	if err := w.closeCurrent(); err != nil {
		return err
	}
	path := w.pathFor(w.currentDate, w.currentIndex)
	if err := ensureJSONLParent(path); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file = file
	w.currentSize = info.Size()
	return nil
}

// shouldRotateBySize 判断下一次写入是否需要按大小切换文件。
func (w *jsonlRotationWriter) shouldRotateBySize(nextSize int) bool {
	return w.maxSizeBytes > 0 &&
		w.currentSize > 0 &&
		w.currentSize+int64(nextSize) > w.maxSizeBytes
}

// closeCurrent 关闭当前打开的文件。
func (w *jsonlRotationWriter) closeCurrent() error {
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	w.currentSize = 0
	return err
}

// pathFor 返回指定日期和序号对应的文件路径。
func (w *jsonlRotationWriter) pathFor(date string, index int) string {
	dir := filepath.Dir(w.basePath)
	ext := filepath.Ext(w.basePath)
	stem := strings.TrimSuffix(filepath.Base(w.basePath), ext)
	name := stem + "-" + date
	if index > 0 {
		name += "." + strconv.Itoa(index)
	}
	name += ext
	if dir == "." || dir == "" {
		return name
	}
	return filepath.Join(dir, name)
}

// maxSizeBytes 将 MB 配置转换成字节，非正数表示不按大小切分。
func maxSizeBytes(maxSizeMB int) int64 {
	if maxSizeMB <= 0 {
		return 0
	}
	return int64(maxSizeMB) * 1024 * 1024
}

// fileSize 返回文件大小；文件不存在时返回 0。
func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err == nil {
		return info.Size(), nil
	}
	if os.IsNotExist(err) {
		return 0, nil
	}
	return 0, err
}
