// Package abstractenrichment 校验有界历史摘要补全证据，不推断科学审核或阅读状态。
package abstractenrichment

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/teamsillybees/glean/internal/baseline"
	"golang.org/x/net/html"
)

// BaselineSHA256 固定本次补全所针对的不可变基线清单。
const BaselineSHA256 = "7329c72fe6ddc7726c76177cda1641ce71d1090bf7b7b0fe4aa5e11b0935d916"

// ParserVersion 标识保留比较符号和内联文本的提取语义。
const ParserVersion = "historical-abstract-block-aware-v1"
const maxResponseBytes = 4 << 20

// Spec 只含公开身份与证据定位，不含工作区路径或请求头。
type Spec struct {
	Topic          string `json:"topic"`
	OriginalID     string `json:"originalId"`
	DOI            string `json:"doi"`
	Title          string `json:"title"`
	Source         string `json:"source"`
	SourceID       string `json:"sourceId"`
	SourceURL      string `json:"sourceUrl"`
	SourceVersion  string `json:"sourceVersion"`
	ResponseSHA256 string `json:"responseHash"`
	AbstractSHA256 string `json:"abstractHash"`
	ParserVersion  string `json:"parserVersion"`
	SourceField    string `json:"sourceField"`
	Completeness   string `json:"completeness"`
}

// Record 是通过原始响应校验得到的公开安全摘要，不包含科研判定。
type Record struct {
	Spec
	Abstract string `json:"abstract"`
}

var specs = []Spec{
	{Topic: "biomed-worldmodel", OriginalID: "BWE4E4BDE173", DOI: "10.1021/acs.jcim.4c01838", Title: "Sampling Conformational Ensembles of Highly Dynamic Proteins via Generative Deep Learning.", Source: "europepmc", SourceID: "MED:39984300", SourceURL: "https://europepmc.org/article/MED/39984300", SourceVersion: "cached-source-field", ResponseSHA256: "7003c4f26487922f09b69a4efb0f9f558a6cff767231014f08d48865c2de5d28", AbstractSHA256: "3d57eade8287455842b7c10fc0a46dbc8ea966a68db562e24a8ae52a961e59d0", ParserVersion: ParserVersion, SourceField: "$.resultList.result[6].abstractText", Completeness: "source-field-complete"},
	{Topic: "biomed-ai", OriginalID: "BAIE4C9614D01", DOI: "10.1146/annurev-cellbio-111524-111428", Title: "From Pixels to Discovery: Microscopy Image Analysis in the Age of Artificial Intelligence.", Source: "europepmc", SourceID: "MED:42823348", SourceURL: "https://europepmc.org/article/MED/42823348", SourceVersion: "cached-source-field", ResponseSHA256: "20da8227ce2c4002ed5813bff18bb446d425c43c31bff19826959f4beb716356", AbstractSHA256: "db69b594660542aecbc30e6050eaa8df8a01a9a28473b0252354dcd43e1b864e", ParserVersion: ParserVersion, SourceField: "$[0].record.abstractText", Completeness: "source-field-complete"},
	{Topic: "llm-agent", OriginalID: "P1286", DOI: "10.1007/978-981-92-1468-6_37", Title: "RLKGC: Reinforcement Learning Retrieval with Large Language Models for Knowledge Graph Completion", Source: "springer", SourceID: "10.1007/978-981-92-1468-6_37", SourceURL: "https://link.springer.com/chapter/10.1007/978-981-92-1468-6_37", SourceVersion: "cached-publisher-section", ResponseSHA256: "c1ee055dad979b525b7064f2ddb1dff61b98c58c970993ad51b1713cec4f221b", AbstractSHA256: "7bd1f983935766b1755c27269eaf36d25dcd266f121885da3862778297e179d8", ParserVersion: ParserVersion, SourceField: "section[data-title=Abstract][aria-labelledby=Abs1]", Completeness: "source-field-complete"},
}

// Specs 返回三个已核验身份的独立副本；新增范围必须显式审核。
func Specs() []Spec { return append([]Spec(nil), specs...) }
func lookup(id string) (Spec, error) {
	if id == "BAI2E6C229787" {
		return Spec{}, fmt.Errorf("THPFF is quarantined for unresolved content correspondence")
	}
	for _, s := range specs {
		if s.OriginalID == id {
			return s, nil
		}
	}
	return Spec{}, fmt.Errorf("identity is outside the verified three-record allowlist")
}
func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

// ValidateRecord 拒绝伪造身份、来源版本、完整性标签及与固定原文哈希不符的正文。
func ValidateRecord(r Record) error {
	s, err := lookup(r.OriginalID)
	if err != nil {
		return err
	}
	if r.Spec != s {
		return fmt.Errorf("record provenance differs from pinned verified source")
	}
	if digest([]byte(r.Abstract)) != s.AbstractSHA256 {
		return fmt.Errorf("abstract checksum mismatch")
	}
	return nil
}

// Verify 直接从固定原始响应重新提取摘要；没有网络、模型或数据库副作用。
func Verify(originalID string, response []byte) (Record, error) {
	s, err := lookup(originalID)
	if err != nil {
		return Record{}, err
	}
	if len(response) > maxResponseBytes || digest(response) != s.ResponseSHA256 {
		return Record{}, fmt.Errorf("source response checksum mismatch")
	}
	var text string
	if s.Source == "springer" {
		text, err = extractSpringer(response, s)
	} else {
		text, err = extractEuropePMC(response, s)
	}
	if err != nil {
		return Record{}, err
	}
	r := Record{Spec: s, Abstract: text}
	if err = ValidateRecord(r); err != nil {
		return Record{}, err
	}
	return r, nil
}

type epmcRecord struct {
	DOI      string `json:"doi"`
	ID       string `json:"id"`
	Source   string `json:"source"`
	Title    string `json:"title"`
	Abstract string `json:"abstractText"`
}

func extractEuropePMC(response []byte, s Spec) (string, error) {
	z, err := gzip.NewReader(bytes.NewReader(response))
	if err != nil {
		return "", fmt.Errorf("invalid compressed source response")
	}
	defer z.Close()
	body, err := io.ReadAll(io.LimitReader(z, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return "", fmt.Errorf("invalid or oversized source response")
	}
	var r epmcRecord
	if s.OriginalID == "BWE4E4BDE173" {
		var envelope struct {
			ResultList struct {
				Result []epmcRecord `json:"result"`
			} `json:"resultList"`
		}
		if json.Unmarshal(body, &envelope) != nil || len(envelope.ResultList.Result) <= 6 {
			return "", fmt.Errorf("source record locator missing")
		}
		r = envelope.ResultList.Result[6]
	} else {
		var envelope []struct {
			Record epmcRecord `json:"record"`
		}
		if json.Unmarshal(body, &envelope) != nil || len(envelope) == 0 {
			return "", fmt.Errorf("source record locator missing")
		}
		r = envelope[0].Record
	}
	if r.DOI != s.DOI || r.Source+":"+r.ID != s.SourceID || normalized(r.Title) != normalized(s.Title) {
		return "", fmt.Errorf("source identifier or title mismatch")
	}
	return plainText(r.Abstract), nil
}
func normalized(s string) string { return strings.Join(strings.Fields(s), " ") }
func block(tag string) bool {
	switch tag {
	case "p", "br", "title", "h1", "h2", "h3", "h4", "h5", "h6", "sec", "label", "div", "li", "section":
		return true
	}
	return false
}

// plainText 使用 HTML tokenizer 而非标签正则，避免吞掉统计比较式正文。
func plainText(s string) string {
	z := html.NewTokenizer(strings.NewReader(s))
	var out strings.Builder
	for {
		kind := z.Next()
		switch kind {
		case html.ErrorToken:
			return normalized(out.String())
		case html.TextToken:
			out.Write(z.Text())
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			if block(string(name)) {
				out.WriteByte(' ')
			}
		}
	}
}
func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}
func extractSpringer(response []byte, s Spec) (string, error) {
	document, err := html.Parse(bytes.NewReader(response))
	if err != nil {
		return "", fmt.Errorf("invalid publisher HTML")
	}
	var doi, title string
	var sections []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" {
			switch attr(n, "name") {
			case "citation_doi":
				doi = attr(n, "content")
			case "citation_title":
				title = attr(n, "content")
			}
		}
		if n.Type == html.ElementNode && n.Data == "section" && attr(n, "data-title") == "Abstract" && attr(n, "aria-labelledby") == "Abs1" {
			sections = append(sections, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(document)
	if doi != s.DOI || normalized(title) != normalized(s.Title) || len(sections) != 1 {
		return "", fmt.Errorf("publisher DOI, title or unique abstract section mismatch")
	}
	var encoded bytes.Buffer
	if html.Render(&encoded, sections[0]) != nil {
		return "", fmt.Errorf("publisher section cannot be decoded")
	}
	text := plainText(encoded.String())
	if !strings.HasPrefix(text, "Abstract ") {
		return "", fmt.Errorf("publisher abstract heading missing")
	}
	return strings.TrimPrefix(text, "Abstract "), nil
}

// VerifyBaseline 验证固定基线字节和三个精确成员关系，不写入或修正任何基线字段。
func VerifyBaseline(dir string) error {
	body, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("cannot read baseline manifest")
	}
	if digest(body) != BaselineSHA256 {
		return fmt.Errorf("baseline manifest checksum mismatch")
	}
	m, err := baseline.ReadManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("invalid baseline manifest")
	}
	found := map[string]bool{}
	for _, shard := range m.Shards {
		compressed, err := os.ReadFile(filepath.Join(dir, shard.File))
		if err != nil {
			return fmt.Errorf("cannot read baseline shard")
		}
		if len(compressed) != shard.Bytes || digest(compressed) != shard.SHA256 {
			return fmt.Errorf("baseline shard checksum mismatch")
		}
		z, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return fmt.Errorf("invalid baseline shard")
		}
		scan := bufio.NewScanner(z)
		scan.Buffer(make([]byte, 65536), 16<<20)
		for scan.Scan() {
			var r struct {
				Topic      string `json:"topic"`
				OriginalID string `json:"originalId"`
				DOI        string `json:"doi"`
				Title      string `json:"title"`
			}
			if json.Unmarshal(scan.Bytes(), &r) != nil {
				z.Close()
				return fmt.Errorf("invalid baseline record")
			}
			for _, s := range specs {
				if r.OriginalID == s.OriginalID {
					if found[s.OriginalID] || r.Topic != s.Topic || r.DOI != s.DOI || normalized(r.Title) != normalized(s.Title) {
						z.Close()
						return fmt.Errorf("baseline membership identity mismatch")
					}
					found[s.OriginalID] = true
				}
			}
		}
		scanErr := scan.Err()
		closeErr := z.Close()
		if scanErr != nil || closeErr != nil {
			return fmt.Errorf("cannot decode baseline shard")
		}
	}
	if len(found) != len(specs) {
		return fmt.Errorf("verified baseline memberships missing")
	}
	return nil
}
