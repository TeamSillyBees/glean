package monitor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"golang.org/x/net/html"
)

const materialMaxBytes = 4 << 20

var materialArxivID = regexp.MustCompile(`^(?:[0-9]{4}\.[0-9]{4,5}|[a-z-]+(?:\.[A-Z]{2})?/[0-9]{7})v[1-9][0-9]*$`)
var materialPMCID = regexp.MustCompile(`^PMC[1-9][0-9]*$`)
var materialVersion = regexp.MustCompile(`^[1-9][0-9]*$`)

// HTTPMaterialProvider 只读取官方公开全文，不追随正文中的链接或执行指令。
type HTTPMaterialProvider struct{ executor httpclient.Executor }

// NewHTTPMaterialProvider 注入统一HTTP执行器；生产必须使用MaterialHTTPFactoryOptions的请求边界。
func NewHTTPMaterialProvider(executor httpclient.Executor) *HTTPMaterialProvider {
	return &HTTPMaterialProvider{executor: executor}
}

// MaterialHTTPServices 返回有界、无自动重试的全文HTTP服务。
func MaterialHTTPServices(contact string) map[string]httpclient.ServiceConfig {
	ua := "Glean/1.0 public-material"
	if strings.TrimSpace(contact) != "" {
		ua += " (contact: " + strings.TrimSpace(contact) + ")"
	}
	return map[string]httpclient.ServiceConfig{"glean-material": {BaseURL: "https://www.ebi.ac.uk", Timeout: 30 * time.Second, MaxResponseBodySize: materialMaxBytes, Headers: map[string]string{"User-Agent": ua}, Retry: httpclient.RetryConfig{Enabled: false}}}
}

// MaterialHTTPFactoryOptions 在每次RoundTrip检查固定来源且拒绝重定向，避免SSRF。
func MaterialHTTPFactoryOptions() []httpclient.FactoryOption {
	return []httpclient.FactoryOption{httpclient.WithServiceHooks("glean-material", materialRequestHook)}
}
func materialRequestHook(r *http.Request) error {
	if r == nil || r.URL == nil || r.Response != nil || r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.User != nil || r.URL.RawQuery != "" || r.URL.Fragment != "" || (r.URL.Port() != "" && r.URL.Port() != "443") {
		return fmt.Errorf("invalid public material request")
	}
	p := r.URL.Path
	valid := false
	switch r.URL.Hostname() {
	case "arxiv.org":
		valid = strings.HasPrefix(p, "/html/") && materialArxivID.MatchString(strings.TrimPrefix(p, "/html/"))
	case "www.ebi.ac.uk":
		const prefix = "/europepmc/webservices/rest/"
		valid = strings.HasPrefix(p, prefix) && strings.HasSuffix(p, "/fullTextXML") && materialPMCID.MatchString(strings.TrimSuffix(strings.TrimPrefix(p, prefix), "/fullTextXML"))
	}
	if !valid {
		return fmt.Errorf("untrusted public material origin or path")
	}
	return nil
}

func materialDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func materialBlock(kind, text, source string, index int) MaterialBlock {
	return MaterialBlock{ID: fmt.Sprintf("%s-%04d", kind, index), Kind: kind, Text: text, SourceURL: source, SHA256: materialDigest([]byte(text))}
}
func finishMaterial(m Material) Material {
	var texts []string
	for _, b := range m.Blocks {
		texts = append(texts, b.Text)
	}
	m.Text = strings.Join(texts, "\n\n")
	m.SHA256 = materialDigest([]byte(m.Text))
	return m
}
func abstractMaterial(r Record) Material {
	source := r.SourceURL
	if source == "" {
		source = r.URL
	}
	m := Material{Scope: "metadata_only", SourceURL: source}
	if text := strings.TrimSpace(r.Title); text != "" {
		m.Blocks = append(m.Blocks, materialBlock("title", text, source, 1))
	}
	if text := strings.TrimSpace(r.Abstract); text != "" {
		m.Scope = "abstract_only"
		m.Blocks = append(m.Blocks, materialBlock("abstract", text, source, 1))
	}
	return finishMaterial(m)
}

// Material 获取版本匹配的公开正文，失败仍返回题录摘要及明确降级原因；临时失败标记可重试。
func (p *HTTPMaterialProvider) Material(ctx context.Context, r Record) (Material, error) {
	m := abstractMaterial(r)
	target, id, kind, reason := materialTarget(r)
	if reason != "" {
		m.Reason = reason
		return m, nil
	}
	if p == nil || p.executor == nil {
		m.Reason = "material_http_unavailable"
		m.Retryable = true
		return m, nil
	}
	if err := ctx.Err(); err != nil {
		return m, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := p.executor.Do(ctx, http.MethodGet, target, httpclient.WithTimeout(30*time.Second))
	if resp != nil && resp.StatusCode != http.StatusOK {
		m.Reason = fmt.Sprintf("full_text_http_%d", resp.StatusCode)
		m.Retryable = resp.StatusCode == 429 || resp.StatusCode == 408 || resp.StatusCode >= 500
		return m, nil
	}
	if err != nil || resp == nil {
		m.Reason = "full_text_request_failed"
		m.Retryable = true
		return m, nil
	}
	if len(resp.Body) == 0 || len(resp.Body) > materialMaxBytes {
		m.Reason = "full_text_invalid_size"
		return m, nil
	}
	var blocks []MaterialBlock
	var partial bool
	if kind == "pmc" {
		blocks, partial, m.License, err = parseMaterialJATS(resp.Body, id, target)
	} else {
		blocks, partial, err = parseMaterialArxiv(resp.Body, id, target)
		m.License = "unknown"
	}
	if err != nil || len(blocks) == 0 {
		m.Reason = "full_text_invalid_or_identity_mismatch"
		m.Retryable = true
		return m, nil
	}
	m.SourceURL = target
	m.RawSHA256 = materialDigest(resp.Body)
	m.Blocks = append(m.Blocks, blocks...)
	m.Scope = "fulltext"
	m.Reason = ""
	if partial {
		m.Scope = "partial"
		m.Reason = "non_text_content_not_extracted"
	}
	return finishMaterial(m), nil
}

func materialTarget(r Record) (target, id, kind, reason string) {
	if r.IsMock {
		return "", "", "", "mock_record_no_network"
	}
	if r.Source == "arxiv" {
		id = strings.TrimSpace(r.ArxivID)
		if id == "" {
			id = strings.TrimSpace(r.SourceID)
		}
		if materialVersion.MatchString(r.Version) {
			suffix := "v" + r.Version
			if materialArxivID.MatchString(id) {
				if !strings.HasSuffix(id, suffix) {
					return "", "", "", "version_identity_mismatch"
				}
			} else {
				id += suffix
			}
		}
		if !materialArxivID.MatchString(id) {
			return "", "", "", "versioned_arxiv_identity_required"
		}
		return "https://arxiv.org/html/" + id, id, "arxiv", ""
	}
	if r.Source == "europepmc" {
		var raw struct {
			PMCID string `json:"pmcid"`
		}
		_ = json.Unmarshal(r.Raw, &raw)
		id = raw.PMCID
		if id == "" && strings.HasPrefix(r.SourceID, "PMC:") {
			id = strings.TrimPrefix(r.SourceID, "PMC:")
		}
		if !materialPMCID.MatchString(id) {
			return "", "", "", "no_public_full_text_identifier"
		}
		if r.Version != "" {
			return "", "", "", "version_specific_pmc_material_unavailable"
		}
		return "https://www.ebi.ac.uk/europepmc/webservices/rest/" + id + "/fullTextXML", id, "pmc", ""
	}
	return "", "", "", "no_supported_public_full_text_source"
}

type materialXMLNode struct {
	XMLName  xml.Name
	Attr     []xml.Attr        `xml:",any,attr"`
	Text     string            `xml:",chardata"`
	Inner    string            `xml:",innerxml"`
	Children []materialXMLNode `xml:",any"`
}

func materialXMLText(n materialXMLNode) string {
	dec := xml.NewDecoder(strings.NewReader("<text>" + n.Inner + "</text>"))
	var out strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.CharData:
			out.Write(t)
		case xml.EndElement:
			if t.Name.Local == "p" || t.Name.Local == "title" {
				out.WriteByte(' ')
			}
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}
func materialXMLAttr(n materialXMLNode, key string) string {
	for _, a := range n.Attr {
		if a.Name.Local == key {
			return a.Value
		}
	}
	return ""
}
func parseMaterialJATS(data []byte, id, source string) ([]MaterialBlock, bool, string, error) {
	// 限制嵌套并拒绝内部实体声明；标准外部DTD仅作为不执行的声明保留。Go XML不会下载DTD。
	check := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	for {
		tok, e := check.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, false, "", e
		}
		switch v := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > 128 {
				return nil, false, "", fmt.Errorf("XML nesting limit")
			}
		case xml.EndElement:
			depth--
		case xml.Directive:
			if strings.Contains(string(v), "[") {
				return nil, false, "", fmt.Errorf("XML entity declarations forbidden")
			}
		}
	}
	var root materialXMLNode
	dec := xml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&root); err != nil || root.XMLName.Local != "article" {
		return nil, false, "", fmt.Errorf("invalid article XML")
	}
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, "", err
		}
		if chars, ok := token.(xml.CharData); !ok || strings.TrimSpace(string(chars)) != "" {
			return nil, false, "", fmt.Errorf("trailing XML")
		}
	}
	matched := false
	identityConflict := false
	for _, front := range root.Children {
		if front.XMLName.Local != "front" {
			continue
		}
		for _, meta := range front.Children {
			if meta.XMLName.Local != "article-meta" {
				continue
			}
			for _, node := range meta.Children {
				typ := materialXMLAttr(node, "pub-id-type")
				if node.XMLName.Local == "article-id" && (typ == "pmc" || typ == "pmcid") {
					if strings.TrimPrefix(materialXMLText(node), "PMC") == strings.TrimPrefix(id, "PMC") {
						matched = true
					} else {
						identityConflict = true
					}
				}
			}
		}
	}
	license := "unknown"
	partial := false
	var blocks []MaterialBlock
	var walk func(materialXMLNode, bool)
	walk = func(n materialXMLNode, body bool) {
		if n.XMLName.Local == "license" && (materialXMLText(n) != "" || materialXMLAttr(n, "href") != "") {
			license = materialXMLText(n)
			if link := materialXMLAttr(n, "href"); link != "" {
				license += " " + link
			}
		}
		if n.XMLName.Local == "body" {
			body = true
		}
		if body && (n.XMLName.Local == "fig" || n.XMLName.Local == "table-wrap" || n.XMLName.Local == "disp-formula" || n.XMLName.Local == "inline-formula" || n.XMLName.Local == "supplementary-material") {
			partial = true
		}
		if body && (n.XMLName.Local == "p" || n.XMLName.Local == "title") {
			if strings.Contains(n.Inner, "<inline-formula") || strings.Contains(n.Inner, "<graphic") {
				partial = true
			}
			if text := materialXMLText(n); text != "" {
				blockSource := source
				if anchor := materialXMLAttr(n, "id"); anchor != "" {
					blockSource += "#" + url.PathEscape(anchor)
				}
				blocks = append(blocks, materialBlock("body", text, blockSource, len(blocks)+1))
			}
			return
		}
		for _, c := range n.Children {
			walk(c, body)
		}
	}
	walk(root, false)
	if !matched || identityConflict {
		return nil, false, "", fmt.Errorf("PMC identity mismatch")
	}
	return blocks, partial, license, nil
}

func materialHTMLAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func materialHTMLText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "nav") {
		return ""
	}
	var s strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		s.WriteString(materialHTMLText(c))
		s.WriteByte(' ')
	}
	return strings.Join(strings.Fields(s.String()), " ")
}
func parseMaterialArxiv(data []byte, id, source string) ([]MaterialBlock, bool, error) {
	if !bytes.Contains(bytes.ToLower(data), []byte("</article>")) {
		return nil, false, fmt.Errorf("incomplete HTML article")
	}
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, false, err
	}
	matched := false
	identityConflict := false
	partial := false
	var blocks []MaterialBlock
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, body bool) {
		if n.Type == html.ElementNode && n.Data == "meta" && materialHTMLAttr(n, "name") == "citation_arxiv_id" {
			if materialHTMLAttr(n, "content") == id {
				matched = true
			} else {
				identityConflict = true
			}
		}
		if n.Type == html.ElementNode && n.Data == "link" && materialHTMLAttr(n, "rel") == "canonical" {
			u := materialHTMLAttr(n, "href")
			if u == source || u == "https://arxiv.org/abs/"+id {
				matched = true
			}
		}
		if n.Type == html.ElementNode && n.Data == "article" && strings.Contains(" "+materialHTMLAttr(n, "class")+" ", " ltx_document ") {
			body = true
		}
		if body && n.Type == html.ElementNode && (n.Data == "img" || n.Data == "math" || n.Data == "table") {
			partial = true
		}
		if body && n.Type == html.ElementNode && (n.Data == "p" || n.Data == "h2" || n.Data == "h3") {
			if text := materialHTMLText(n); text != "" {
				blockSource := source
				if anchor := materialHTMLAttr(n, "id"); anchor != "" {
					blockSource += "#" + url.PathEscape(anchor)
				}
				blocks = append(blocks, materialBlock("body", text, blockSource, len(blocks)+1))
			}
			// 继续检查公式/图片，但不重复提取子段落。
			var inspect func(*html.Node)
			inspect = func(c *html.Node) {
				if c.Type == html.ElementNode && (c.Data == "math" || c.Data == "img" || c.Data == "table") {
					partial = true
				}
				for x := c.FirstChild; x != nil; x = x.NextSibling {
					inspect(x)
				}
			}
			inspect(n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, body)
		}
	}
	walk(root, false)
	if !matched || identityConflict {
		return nil, false, fmt.Errorf("arxiv version identity mismatch")
	}
	return blocks, partial, nil
}
