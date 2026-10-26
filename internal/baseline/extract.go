package baseline

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SourceSpec 指定一个经人工确认的历史 HTML 基线。
type SourceSpec struct {
	Topic       string
	Path        string
	SHA256      string
	Memberships int
}

// SourceManifest 记录原始文件身份、完整归档哈希和未改判的分类分布。
type SourceManifest struct {
	Topic                 string                    `json:"topic"`
	HTMLSHA256            string                    `json:"htmlSha256"`
	CatalogSHA256         string                    `json:"catalogSha256"`
	Memberships           int                       `json:"memberships"`
	OriginalIDsSHA256     string                    `json:"originalIdsSha256"`
	Redactions            int                       `json:"privateProvenanceRedactions"`
	TypedOnlineDates      int                       `json:"typedOnlineDates"`
	TypedIssueDates       int                       `json:"typedIssueDates"`
	Classifications       map[string]map[string]int `json:"classifications"`
	DisplayTaxonomySHA256 string                    `json:"displayTaxonomySha256,omitempty"`
	Metadata              map[string]any            `json:"metadata"`
}

// Shard 描述可独立校验的压缩 JSONL 文件。
type Shard struct {
	File               string `json:"file"`
	Topic              string `json:"topic"`
	SHA256             string `json:"sha256"`
	UncompressedSHA256 string `json:"uncompressedSha256"`
	Records            int    `json:"records"`
	Bytes              int    `json:"bytes"`
}

// Manifest 记录所有成员关系，不将预期身份去重数冒充数据库导入结果。
type Manifest struct {
	Version                  int              `json:"version"`
	BaselineDate             string           `json:"baselineDate"`
	Memberships              int              `json:"memberships"`
	UniqueIdentityCandidates int              `json:"uniqueIdentityCandidates"`
	IdentityConflicts        int              `json:"identityConflicts"`
	Sources                  []SourceManifest `json:"sources"`
	Shards                   []Shard          `json:"shards"`
	RedactionPolicy          string           `json:"redactionPolicy"`
	DatePolicy               string           `json:"datePolicy"`
	DedupePolicy             string           `json:"dedupePolicy"`
}

// ExtractOptions 限定输出目录和每片上限，完整原始 JSON 仅存于独立私有归档目录。
type ExtractOptions struct {
	OutputDir       string
	ArchiveDir      string
	MaxShardBytes   int
	RecordsPerShard int
}

// Extract 验证来源后提取全部记录；任何错误均拒绝写入完成 manifest。
func Extract(specs []SourceSpec, opts ExtractOptions) (Manifest, error) {
	m := Manifest{Version: 1, BaselineDate: "2026-10-26", Sources: []SourceManifest{}, Shards: []Shard{}, RedactionPolicy: "Only private absolute paths, internal trace URLs/references, Library IDs and credential fields are explicitly redacted. Scientific fields and relative audit provenance remain. Exact original catalog JSON is retained separately in the ignored archive, identified by catalogSha256.", DatePolicy: "Only explicitly verified formal online/issue day-precision values are typed; partial/ambiguous dates remain in raw/evidence with original precision. No acceptance/submission/index date promotion.", DedupePolicy: "Exact normalized DOI/arXiv only. Topic+originalId retained. No title-only merge. Ambiguous conflicting identifier components must be quarantined on database import."}
	if len(specs) == 0 {
		return m, fmt.Errorf("no sources")
	}
	if opts.MaxShardBytes == 0 {
		opts.MaxShardBytes = 2_000_000
	}
	if opts.RecordsPerShard == 0 {
		opts.RecordsPerShard = 1000
	}
	if opts.MaxShardBytes < 1 || opts.RecordsPerShard < 1 {
		return m, fmt.Errorf("invalid shard limits")
	}
	if opts.OutputDir == "" || opts.ArchiveDir == "" {
		return m, fmt.Errorf("both output and private archive directories are required")
	}
	outputAbs, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return m, err
	}
	archiveAbs, err := filepath.Abs(opts.ArchiveDir)
	if err != nil {
		return m, err
	}
	relative, err := filepath.Rel(outputAbs, archiveAbs)
	if err != nil {
		return m, err
	}
	if relative == "." || (!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != "..") {
		return m, fmt.Errorf("private archive must not be inside published snapshot directory")
	}
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return m, err
	}
	if err := os.MkdirAll(opts.ArchiveDir, 0700); err != nil {
		return m, err
	}
	identities := newIdentityIndex()
	seenTopics := map[string]bool{}
	for _, spec := range specs {
		if seenTopics[spec.Topic] {
			return m, fmt.Errorf("duplicate topic")
		}
		seenTopics[spec.Topic] = true
		html, err := os.ReadFile(spec.Path)
		if err != nil {
			return m, fmt.Errorf("read %s source: %w", spec.Topic, err)
		}
		htmlHash := digest(html)
		if spec.SHA256 != "" && htmlHash != spec.SHA256 {
			return m, fmt.Errorf("%s HTML SHA-256 mismatch", spec.Topic)
		}
		catalog, err := CatalogJSON(html)
		if err != nil {
			return m, fmt.Errorf("%s: %w", spec.Topic, err)
		}
		archive, err := compress(catalog)
		if err != nil {
			return m, err
		}
		if err = os.WriteFile(filepath.Join(opts.ArchiveDir, spec.Topic+".catalog.json.gz"), archive, 0600); err != nil {
			return m, err
		}
		taxonomy, taxonomyJSON, err := ReadDisplayTaxonomy(html)
		if err != nil {
			return m, err
		}
		if len(taxonomyJSON) > 0 {
			zipped, err := compress(taxonomyJSON)
			if err != nil {
				return m, err
			}
			if err := os.WriteFile(filepath.Join(opts.OutputDir, spec.Topic+".display-taxonomy.json.gz"), zipped, 0644); err != nil {
				return m, err
			}
			if err := os.WriteFile(filepath.Join(opts.ArchiveDir, spec.Topic+".display-taxonomy.json.gz"), zipped, 0600); err != nil {
				return m, err
			}
		}
		records, meta, err := DecodeCatalog(catalog)
		if err != nil {
			return m, fmt.Errorf("%s: %w", spec.Topic, err)
		}
		if spec.Memberships > 0 && len(records) != spec.Memberships {
			return m, fmt.Errorf("%s expected %d records, got %d", spec.Topic, spec.Memberships, len(records))
		}
		sm := SourceManifest{Topic: spec.Topic, HTMLSHA256: htmlHash, CatalogSHA256: digest(catalog), Memberships: len(records), Classifications: map[string]map[string]int{}, Metadata: meta}
		if len(taxonomyJSON) > 0 {
			sm.DisplayTaxonomySHA256 = digest(taxonomyJSON)
		}
		ids := []string{}
		seen := map[string]bool{}
		lines := [][]byte{}
		part := 0
		flush := func() error {
			if len(lines) == 0 {
				return nil
			}
			var write func([][]byte) error
			write = func(items [][]byte) error {
				body := bytes.Join(items, []byte{})
				zipped, err := compress(body)
				if err != nil {
					return err
				}
				if len(zipped) > opts.MaxShardBytes && len(items) > 1 {
					half := len(items) / 2
					if err := write(items[:half]); err != nil {
						return err
					}
					return write(items[half:])
				}
				if len(zipped) > opts.MaxShardBytes {
					return fmt.Errorf("one record exceeds compressed shard limit")
				}
				part++
				name := fmt.Sprintf("%s.%03d.ndjson.gz", spec.Topic, part)
				if err := os.WriteFile(filepath.Join(opts.OutputDir, name), zipped, 0644); err != nil {
					return err
				}
				m.Shards = append(m.Shards, Shard{File: name, Topic: spec.Topic, SHA256: digest(zipped), UncompressedSHA256: digest(body), Records: len(items), Bytes: len(zipped)})
				return nil
			}
			err := write(lines)
			lines = nil
			return err
		}
		for _, raw := range records {
			r, n, err := Normalize(spec.Topic, raw)
			if err != nil {
				return m, fmt.Errorf("normalize %s: %w", spec.Topic, err)
			}
			applyDisplay(&r, taxonomy)
			if seen[r.OriginalID] {
				return m, fmt.Errorf("duplicate original ID in %s", spec.Topic)
			}
			seen[r.OriginalID] = true
			ids = append(ids, r.OriginalID)
			sm.Redactions += n
			if r.FormalOnlineDate != nil {
				sm.TypedOnlineDates++
			}
			if r.IssueDate != nil {
				sm.TypedIssueDates++
			}
			for _, key := range []string{"collection", "publication_eligibility", "topic_status", "eligibility_status", "inclusion_status", "scope_decision", "current_review_status", "evidence_level"} {
				for _, value := range list(r.Raw[key]) {
					if sm.Classifications[key] == nil {
						sm.Classifications[key] = map[string]int{}
					}
					sm.Classifications[key][value]++
				}
			}
			identities.add(r)
			line, err := json.Marshal(r)
			if err != nil {
				return m, err
			}
			lines = append(lines, append(line, '\n'))
			if len(lines) >= opts.RecordsPerShard {
				if err := flush(); err != nil {
					return m, err
				}
			}
		}
		if err := flush(); err != nil {
			return m, err
		}
		sort.Strings(ids)
		idBytes, _ := json.Marshal(ids)
		sm.OriginalIDsSHA256 = digest(idBytes)
		m.Sources = append(m.Sources, sm)
		m.Memberships += len(records)
	}
	m.UniqueIdentityCandidates, m.IdentityConflicts = identities.counts()
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, err
	}
	err = os.WriteFile(filepath.Join(opts.OutputDir, "manifest.json"), append(out, '\n'), 0644)
	return m, err
}

var catalogTag = regexp.MustCompile(`(?i)<script\b[^>]*\bid\s*=\s*["']catalog-data["'][^>]*>`)

// CatalogJSON 提取唯一 catalog-data JSON，不执行来源 HTML 中的代码。
func CatalogJSON(html []byte) ([]byte, error) {
	matches := catalogTag.FindAllIndex(html, -1)
	if len(matches) != 1 {
		return nil, fmt.Errorf("expected exactly one catalog-data script")
	}
	start := matches[0][1]
	end := bytes.Index(html[start:], []byte("</script>"))
	if end < 0 {
		return nil, fmt.Errorf("catalog-data script is not closed")
	}
	body := html[start : start+end]
	if !json.Valid(body) {
		return nil, fmt.Errorf("catalog-data is invalid JSON")
	}
	return body, nil
}

// DecodeCatalog 兼容普通 papers 和原阅读器的 strings/columns/rows 无损压缩格式。
func DecodeCatalog(body []byte) ([]map[string]any, map[string]any, error) {
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, nil, err
	}
	papers, ok := root["papers"].([]any)
	if packed, exists := root["packed"].(map[string]any); exists {
		if ok {
			return nil, nil, fmt.Errorf("ambiguous packed and papers payload")
		}
		strs, ok1 := packed["strings"].([]any)
		cols, ok2 := packed["columns"].([]any)
		rows, ok3 := packed["rows"].([]any)
		if !ok1 || !ok2 || !ok3 {
			return nil, nil, fmt.Errorf("invalid packed schema")
		}
		keys := make([]string, len(cols))
		seen := map[string]bool{}
		for i, col := range cols {
			s, ok := col.(string)
			if !ok || seen[s] {
				return nil, nil, fmt.Errorf("invalid packed column")
			}
			keys[i] = s
			seen[s] = true
		}
		papers = make([]any, len(rows))
		for i, row := range rows {
			cells, ok := row.([]any)
			if !ok || len(cells) != len(keys) {
				return nil, nil, fmt.Errorf("invalid packed row %d", i+1)
			}
			p := map[string]any{}
			for j, cell := range cells {
				v, err := decodePacked(cell, strs)
				if err != nil {
					return nil, nil, err
				}
				p[keys[j]] = v
			}
			papers[i] = p
		}
		ok = true
	}
	if !ok {
		return nil, nil, fmt.Errorf("missing papers or packed rows")
	}
	result := make([]map[string]any, len(papers))
	for i, paper := range papers {
		p, ok := paper.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("paper %d is not an object", i+1)
		}
		result[i] = p
	}
	meta := map[string]any{}
	for _, key := range []string{"title", "asof", "updated", "verification_asof", "search_asof_original", "cutoff", "effective_publication_window", "build_status", "query_started_at", "generated_at", "verification_cutoff", "search_coverage_cutoff"} {
		if v, ok := root[key]; ok {
			clean, _ := sanitize(v, key)
			meta[key] = clean
		}
	}
	return result, meta, nil
}
func decodePacked(v any, strs []any) (any, error) {
	switch x := v.(type) {
	case json.Number:
		n, err := x.Int64()
		if err == nil && n < 0 {
			i := -n - 1
			if i < 0 || i >= int64(len(strs)) {
				return nil, fmt.Errorf("packed string index out of bounds")
			}
			if _, ok := strs[i].(string); !ok {
				return nil, fmt.Errorf("packed string must be text")
			}
			return strs[i], nil
		}
		return x, nil
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			item, err := decodePacked(v, strs)
			if err != nil {
				return nil, err
			}
			out[i] = item
		}
		return out, nil
	case map[string]any:
		if number, ok := x["$number"]; ok {
			if len(x) != 1 {
				return nil, fmt.Errorf("invalid escaped packed number")
			}
			return number, nil
		}
		out := map[string]any{}
		for k, v := range x {
			item, err := decodePacked(v, strs)
			if err != nil {
				return nil, err
			}
			out[k] = item
		}
		return out, nil
	default:
		return v, nil
	}
}
func compress(body []byte) ([]byte, error) {
	var buf bytes.Buffer
	z, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err = z.Write(body); err != nil {
		return nil, err
	}
	if err = z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func readCompressed(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	return io.ReadAll(z)
}

type identityIndex struct {
	parents    []int
	doi, arxiv map[string]int
	aliases    map[int][2]string
	conflicts  int
}

func newIdentityIndex() *identityIndex {
	return &identityIndex{doi: map[string]int{}, arxiv: map[string]int{}, aliases: map[int][2]string{}}
}
func (x *identityIndex) root(i int) int {
	for x.parents[i] != i {
		x.parents[i] = x.parents[x.parents[i]]
		i = x.parents[i]
	}
	return i
}
func (x *identityIndex) add(r Record) {
	i := len(x.parents)
	x.parents = append(x.parents, i)
	d, hasD := x.doi[r.DOI]
	a, hasA := x.arxiv[r.ArxivID]
	hasD = hasD && r.DOI != ""
	hasA = hasA && r.ArxivID != ""
	conflict := hasD && hasA && x.root(d) != x.root(a)
	for _, match := range []struct {
		index int
		found bool
	}{{d, hasD}, {a, hasA}} {
		if !match.found {
			continue
		}
		prior := x.aliases[x.root(match.index)]
		if r.DOI != "" && prior[0] != "" && r.DOI != prior[0] {
			conflict = true
		}
		if r.ArxivID != "" && prior[1] != "" && r.ArxivID != prior[1] {
			conflict = true
		}
	}
	if conflict {
		x.conflicts++
	}
	aliases := [2]string{r.DOI, r.ArxivID}
	for _, match := range []struct {
		index int
		found bool
	}{{d, hasD}, {a, hasA}} {
		if !match.found {
			continue
		}
		prior := x.aliases[x.root(match.index)]
		for j := range aliases {
			if aliases[j] == "" {
				aliases[j] = prior[j]
			}
		}
		x.parents[x.root(i)] = x.root(match.index)
	}
	x.aliases[x.root(i)] = aliases
	if r.DOI != "" {
		x.doi[r.DOI] = i
	}
	if r.ArxivID != "" {
		x.arxiv[r.ArxivID] = i
	}
}
func (x *identityIndex) counts() (int, int) {
	n := 0
	for i := range x.parents {
		if x.root(i) == i {
			n++
		}
	}
	return n, x.conflicts
}
