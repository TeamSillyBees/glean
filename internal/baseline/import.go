package baseline

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrQuarantined 表示至少一条记录失败；成功记录可在重跑时安全跳过。
var ErrQuarantined = errors.New("baseline import has quarantined records")

// Apply 将一条已验证 JSON 交给业务服务，返回是否创建了主题成员关系。
type Apply func(context.Context, json.RawMessage) (bool, error)

// ImportReport 记录已尝试、成功、幂等跳过和隔离数量。
type ImportReport struct {
	Attempted   int            `json:"attempted"`
	Created     int            `json:"created"`
	Existing    int            `json:"existing"`
	Quarantined int            `json:"quarantined"`
	Complete    bool           `json:"complete"`
	ByTopic     map[string]int `json:"byTopic"`
}

// Quarantine 保存问题记录和定位，不把数据库连接信息或原始错误写入报告。
type Quarantine struct {
	File        string          `json:"file"`
	Line        int             `json:"line"`
	Topic       string          `json:"topic,omitempty"`
	OriginalID  string          `json:"originalId,omitempty"`
	Reason      string          `json:"reason"`
	Record      json.RawMessage `json:"record,omitempty"`
	InvalidLine string          `json:"invalidLine,omitempty"`
}

// ReadManifest 读取基线清单；拒绝路径穿越或重复分片。
func ReadManifest(path string) (Manifest, error) {
	var m Manifest
	body, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(body, &m); err != nil {
		return m, err
	}
	if m.Version != 1 || m.Memberships < 1 || len(m.Shards) == 0 {
		return m, fmt.Errorf("invalid baseline manifest")
	}
	seen := map[string]bool{}
	for _, s := range m.Shards {
		if s.File == "" || filepath.Base(s.File) != s.File || strings.ContainsAny(s.File, "/\\") || seen[s.File] || s.Records < 1 {
			return m, fmt.Errorf("invalid or duplicate shard")
		}
		seen[s.File] = true
	}
	return m, nil
}

// Verify 在任何数据库写入前核验字节哈希、所有原始主题 ID 及逐主题数量。
func Verify(dir string, m Manifest) error {
	total := 0
	counts := map[string]int{}
	ids := map[string][]string{}
	seen := map[string]bool{}
	identities := newIdentityIndex()
	for _, shard := range m.Shards {
		compressed, err := os.ReadFile(filepath.Join(dir, shard.File))
		if err != nil {
			return err
		}
		if len(compressed) != shard.Bytes || digest(compressed) != shard.SHA256 {
			return fmt.Errorf("shard checksum mismatch: %s", shard.File)
		}
		z, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return err
		}
		body, err := io.ReadAll(z)
		closeErr := z.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if digest(body) != shard.UncompressedSHA256 {
			return fmt.Errorf("uncompressed checksum mismatch: %s", shard.File)
		}
		count := 0
		err = walkLines(bytes.NewReader(body), func(line []byte, lineNo int) error {
			var r Record
			if err := json.Unmarshal(line, &r); err != nil {
				return fmt.Errorf("invalid record: %s:%d", shard.File, lineNo)
			}
			if err := ValidateRecord(r); err != nil {
				return fmt.Errorf("invalid record: %s:%d: %w", shard.File, lineNo, err)
			}
			if r.Topic != shard.Topic {
				return fmt.Errorf("shard topic mismatch")
			}
			key := r.Topic + "\x00" + r.OriginalID
			if seen[key] {
				return fmt.Errorf("duplicate topic original ID")
			}
			seen[key] = true
			identities.add(r)
			ids[r.Topic] = append(ids[r.Topic], r.OriginalID)
			counts[r.Topic]++
			count++
			return nil
		})
		if err != nil {
			return err
		}
		if count != shard.Records {
			return fmt.Errorf("shard record count mismatch")
		}
		total += count
	}
	if total != m.Memberships {
		return fmt.Errorf("total membership count mismatch")
	}
	unique, conflicts := identities.counts()
	if unique != m.UniqueIdentityCandidates || conflicts != m.IdentityConflicts {
		return fmt.Errorf("identity audit mismatch")
	}
	sourceTotal := 0
	seenSources := map[string]bool{}
	for _, source := range m.Sources {
		if seenSources[source.Topic] {
			return fmt.Errorf("duplicate source topic")
		}
		seenSources[source.Topic] = true
		sourceTotal += source.Memberships
		if source.DisplayTaxonomySHA256 != "" {
			body, err := readCompressed(filepath.Join(dir, source.Topic+".display-taxonomy.json.gz"))
			if err != nil {
				return err
			}
			if digest(body) != source.DisplayTaxonomySHA256 {
				return fmt.Errorf("display taxonomy checksum mismatch")
			}
		}
		if counts[source.Topic] != source.Memberships {
			return fmt.Errorf("source membership count mismatch")
		}
		sort.Strings(ids[source.Topic])
		body, _ := json.Marshal(ids[source.Topic])
		if digest(body) != source.OriginalIDsSHA256 {
			return fmt.Errorf("source original ID checksum mismatch")
		}
	}
	if sourceTotal != total || len(seenSources) != len(counts) {
		return fmt.Errorf("manifest source coverage mismatch")
	}
	return nil
}

// ValidateRecord 验证导入所需身份和来源，不新增科学结论。
func ValidateRecord(r Record) error {
	switch r.Topic {
	case "biomed-ai", "llm-agent", "biomed-worldmodel":
	default:
		return fmt.Errorf("unknown topic")
	}
	if r.OriginalID == "" || strings.TrimSpace(r.Title) == "" {
		return fmt.Errorf("missing original ID or title")
	}
	if len(r.Raw) == 0 {
		return fmt.Errorf("missing original scientific evidence")
	}
	if r.IsMock {
		return fmt.Errorf("historical baseline cannot be marked mock")
	}
	if r.ReadStatus != "unread" && r.ReadStatus != "reading" && r.ReadStatus != "read" {
		return fmt.Errorf("invalid reading state")
	}
	return nil
}

// Import 流式导入清单中的每条记录，逐条失败隔离且不会悄悄丢弃。
func Import(ctx context.Context, dir string, m Manifest, apply Apply, quarantine io.Writer) (ImportReport, error) {
	report := ImportReport{ByTopic: map[string]int{}}
	if apply == nil || quarantine == nil {
		return report, fmt.Errorf("apply and quarantine writer are required")
	}
	encoder := json.NewEncoder(quarantine)
	for _, shard := range m.Shards {
		f, err := os.Open(filepath.Join(dir, shard.File))
		if err != nil {
			return report, err
		}
		z, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return report, err
		}
		err = walkLines(z, func(line []byte, lineNo int) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			report.Attempted++
			var r Record
			reason := ""
			if err := json.Unmarshal(line, &r); err != nil {
				reason = "invalid_json"
			} else if err := ValidateRecord(r); err != nil {
				reason = "invalid_record"
			} else if r.Topic != shard.Topic {
				reason = "topic_mismatch"
			}
			if reason == "" {
				created, err := apply(ctx, json.RawMessage(line))
				if err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					reason = "import_failed"
				} else {
					if created {
						report.Created++
					} else {
						report.Existing++
					}
					report.ByTopic[r.Topic]++
					return nil
				}
			}
			report.Quarantined++
			q := Quarantine{File: shard.File, Line: lineNo, Topic: r.Topic, OriginalID: r.OriginalID, Reason: reason}
			if json.Valid(line) {
				q.Record = append(json.RawMessage(nil), line...)
			} else {
				clean, _ := sanitize(string(line), "")
				q.InvalidLine = clean.(string)
			}
			return encoder.Encode(q)
		})
		closeZ := z.Close()
		closeF := f.Close()
		if err != nil {
			return report, err
		}
		if closeZ != nil {
			return report, closeZ
		}
		if closeF != nil {
			return report, closeF
		}
	}
	report.Complete = report.Quarantined == 0 && report.Attempted == m.Memberships
	if report.Quarantined > 0 {
		return report, ErrQuarantined
	}
	if !report.Complete {
		return report, fmt.Errorf("import count differs from manifest")
	}
	return report, nil
}
func walkLines(r io.Reader, fn func([]byte, int) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			return fmt.Errorf("empty JSONL record at line %d", line)
		}
		if err := fn(scanner.Bytes(), line); err != nil {
			return err
		}
	}
	return scanner.Err()
}
