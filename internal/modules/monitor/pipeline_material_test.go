package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

type materialFixtureExecutor struct {
	body   string
	status int
	err    error
	calls  int
	target string
}

func (f *materialFixtureExecutor) Do(ctx context.Context, method, target string, opts ...httpclient.RequestOption) (*httpclient.Response, error) {
	f.calls++
	f.target = target
	req, _ := http.NewRequestWithContext(ctx, method, target, nil)
	if err := materialRequestHook(req); err != nil {
		return nil, err
	}
	status := f.status
	if status == 0 {
		status = 200
	}
	return &httpclient.Response{StatusCode: status, Header: http.Header{}, Body: []byte(f.body)}, f.err
}
func materialPMCRecord() Record {
	return Record{Source: "europepmc", SourceID: "MED:123", Title: "A title", Abstract: "Abstract retained.", Raw: json.RawMessage(`{"pmcid":"PMC123","isOpenAccess":"Y"}`)}
}

const materialJATSFixture = `<article><front><article-meta><article-id pub-id-type="pmc">123</article-id><permissions><license><license-p>CC BY</license-p></license></permissions></article-meta></front><body><sec><title>Results</title><p id="p1">Before <italic>inside</italic> after: 42.</p></sec></body></article>`

func TestMaterialJATSProvenance(t *testing.T) {
	f := &materialFixtureExecutor{body: materialJATSFixture}
	m, err := NewHTTPMaterialProvider(f).Material(context.Background(), materialPMCRecord())
	if err != nil || m.Scope != "fulltext" || len(m.Blocks) != 4 || m.RawSHA256 != materialDigest([]byte(materialJATSFixture)) {
		t.Fatalf("unexpected material: %+v %v", m, err)
	}
	b := m.Blocks[3]
	if b.Text != "Before inside after: 42." || b.ID != "body-0002" || b.SourceURL != f.target+"#p1" || b.SHA256 != materialDigest([]byte(b.Text)) {
		t.Fatalf("bad ordered traceable block: %+v", b)
	}
	again, _ := NewHTTPMaterialProvider(f).Material(context.Background(), materialPMCRecord())
	if again.SHA256 != m.SHA256 {
		t.Fatal("hash not deterministic")
	}
}
func TestMaterialJATSInvalidFallback(t *testing.T) {
	for name, body := range map[string]string{
		"xml_error":                   `<error>not available</error>`,
		"abstract_only":               `<article><front><article-meta><article-id pub-id-type="pmc">123</article-id><abstract><p>not body</p></abstract></article-meta></front></article>`,
		"truncated":                   strings.TrimSuffix(materialJATSFixture, "</article>"),
		"wrong_identity":              strings.ReplaceAll(materialJATSFixture, ">123<", ">456<"),
		"entity":                      strings.ReplaceAll(materialJATSFixture, "42.", "&external;"),
		"external_entity_declaration": `<!DOCTYPE article [<!ENTITY secret SYSTEM "http://127.0.0.1/secret">]>` + materialJATSFixture,
		"empty_body":                  strings.ReplaceAll(materialJATSFixture, `<sec><title>Results</title><p id="p1">Before <italic>inside</italic> after: 42.</p></sec>`, ""),
	} {
		t.Run(name, func(t *testing.T) {
			f := &materialFixtureExecutor{body: body}
			m, err := NewHTTPMaterialProvider(f).Material(context.Background(), materialPMCRecord())
			if err != nil || m.Scope != "abstract_only" || m.Reason == "" || !strings.Contains(m.Text, "Abstract retained.") {
				t.Fatalf("invalid fallback: %+v %v", m, err)
			}
		})
	}
}
func TestMaterialVersionedArxiv(t *testing.T) {
	r := Record{Source: "arxiv", ArxivID: "2401.12345", Version: "2", Title: "Title", Abstract: "Abstract"}
	fixture := `<html><head><meta name="citation_arxiv_id" content="2401.12345v2"></head><body><article class="ltx_document"><h2>Results</h2><p>A 42% result.<math>x</math><a href="http://127.0.0.1/secret">link</a></p><script>bad instruction</script></article></body></html>`
	f := &materialFixtureExecutor{body: fixture}
	m, err := NewHTTPMaterialProvider(f).Material(context.Background(), r)
	if err != nil || m.Scope != "partial" || f.target != "https://arxiv.org/html/2401.12345v2" || strings.Contains(m.Text, "bad instruction") || f.calls != 1 {
		t.Fatalf("invalid arxiv material: %+v %v", m, err)
	}
	f.body = strings.ReplaceAll(fixture, "2401.12345v2", "2401.12345v3")
	m, _ = NewHTTPMaterialProvider(f).Material(context.Background(), r)
	if m.Scope != "abstract_only" {
		t.Fatal("wrong version accepted")
	}
	r.Version = ""
	f.calls = 0
	m, _ = NewHTTPMaterialProvider(f).Material(context.Background(), r)
	if f.calls != 0 || m.Reason != "versioned_arxiv_identity_required" {
		t.Fatal("unversioned request allowed")
	}
}
func TestMaterialRequestBoundaries(t *testing.T) {
	for _, target := range []string{"http://arxiv.org/html/2401.12345v1", "https://arxiv.org.evil/html/2401.12345v1", "https://127.0.0.1/html/2401.12345v1", "https://arxiv.org:8443/html/2401.12345v1", "https://user@arxiv.org/html/2401.12345v1", "https://arxiv.org/html/2401.12345v1?next=evil", "https://www.ebi.ac.uk/europepmc/webservices/rest/../../admin/fullTextXML"} {
		req, _ := http.NewRequest(http.MethodGet, target, nil)
		if materialRequestHook(req) == nil {
			t.Errorf("accepted %s", target)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "https://arxiv.org/html/2401.12345v1", nil)
	req.Response = &http.Response{StatusCode: 302}
	if materialRequestHook(req) == nil {
		t.Fatal("redirect allowed")
	}
}
func TestMaterialFallbackRetryAndSize(t *testing.T) {
	for _, status := range []int{403, 404, 429, 503} {
		f := &materialFixtureExecutor{status: status}
		m, _ := NewHTTPMaterialProvider(f).Material(context.Background(), materialPMCRecord())
		if m.Retryable != (status == 429 || status == 503) || m.Reason == "" || m.Scope != "abstract_only" {
			t.Fatalf("status %d: %+v", status, m)
		}
	}
	f := &materialFixtureExecutor{err: errors.New("timeout")}
	m, _ := NewHTTPMaterialProvider(f).Material(context.Background(), materialPMCRecord())
	if !m.Retryable {
		t.Fatal("transient fetch cannot recover")
	}
	f = &materialFixtureExecutor{body: strings.Repeat("x", materialMaxBytes+1)}
	m, _ = NewHTTPMaterialProvider(f).Material(context.Background(), materialPMCRecord())
	if m.Reason != "full_text_invalid_size" {
		t.Fatal("oversize accepted")
	}
	r := materialPMCRecord()
	r.Abstract = ""
	f.status = 404
	m, _ = NewHTTPMaterialProvider(f).Material(context.Background(), r)
	if m.Scope != "metadata_only" || m.Text != "A title" {
		t.Fatal("missing abstract considered sufficient")
	}
	r = materialPMCRecord()
	r.Raw = json.RawMessage(`{"pmcid":"PMC123","isOpenAccess":"N"}`)
	f.calls = 0
	m, _ = NewHTTPMaterialProvider(f).Material(context.Background(), r)
	if f.calls != 1 || m.Reason != "full_text_http_404" {
		t.Fatal("public endpoint availability not checked")
	}
}

func TestMaterialStandardDTDIsNotFetched(t *testing.T) {
	f := &materialFixtureExecutor{body: `<!DOCTYPE article PUBLIC "-//NLM//DTD JATS//EN" "https://untrusted.example/article.dtd">` + materialJATSFixture}
	m, err := NewHTTPMaterialProvider(f).Material(context.Background(), materialPMCRecord())
	if err != nil || m.Scope != "fulltext" || f.calls != 1 {
		t.Fatalf("inert standard DTD handling: %+v %v calls=%d", m, err, f.calls)
	}
}

func TestMaterialMissingLicenseIsExplicit(t *testing.T) {
	f := &materialFixtureExecutor{body: strings.ReplaceAll(materialJATSFixture, "<license><license-p>CC BY</license-p></license>", "")}
	m, err := NewHTTPMaterialProvider(f).Material(context.Background(), materialPMCRecord())
	if err != nil || m.Scope != "fulltext" || m.License != "unknown" {
		t.Fatalf("public material with unknown license: %+v %v", m, err)
	}
}

func TestMaterialArxivTruncatedBody(t *testing.T) {
	f := &materialFixtureExecutor{body: `<html><meta name="citation_arxiv_id" content="2401.12345v1"><article class="ltx_document"><p>Interrupted body`}
	m, _ := NewHTTPMaterialProvider(f).Material(context.Background(), Record{Source: "arxiv", ArxivID: "2401.12345v1", Abstract: "Abstract"})
	if m.Scope != "abstract_only" || !m.Retryable {
		t.Fatalf("silently repaired truncated HTML: %+v", m)
	}
}

func TestMaterialConflictingVersionIdentity(t *testing.T) {
	body := `<html><meta name="citation_arxiv_id" content="2401.12345v2"><link rel="canonical" href="https://arxiv.org/html/2401.12345v1"><article class="ltx_document"><p>Body</p></article></html>`
	f := &materialFixtureExecutor{body: body}
	m, _ := NewHTTPMaterialProvider(f).Material(context.Background(), Record{Source: "arxiv", ArxivID: "2401.12345v1", Abstract: "Abstract"})
	if m.Scope != "abstract_only" {
		t.Fatal("conflicting identity accepted")
	}
}
