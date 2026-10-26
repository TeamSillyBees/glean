package abstractenrichment

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func verifiedRecords(t *testing.T) []Record {
	t.Helper()
	b, err := os.ReadFile("testdata/verified_records.json")
	require.NoError(t, err)
	var records []Record
	require.NoError(t, json.Unmarshal(b, &records))
	return records
}

// TestPinnedRecordsArePublicSafe 验证测试中的真实摘要仍与固定来源身份和正文哈希完全一致。
func TestPinnedRecordsArePublicSafe(t *testing.T) {
	records := verifiedRecords(t)
	require.Len(t, records, 3)
	for _, r := range records {
		require.NoError(t, ValidateRecord(r))
		b, err := json.Marshal(r)
		require.NoError(t, err)
		for _, private := range []string{"/workspace/", "/home/", "cache_file", "Authorization", "rawSourceCache"} {
			require.NotContains(t, string(b), private)
		}
	}
	copy := Specs()
	copy[0].DOI = "changed"
	require.NotEqual(t, copy[0], Specs()[0])
}

// TestPinnedRecordRejectsTampering 防止沿用正确 ID 却调换来源、版本、完整性或正文。
func TestPinnedRecordRejectsTampering(t *testing.T) {
	r := verifiedRecords(t)[0]
	cases := map[string]func(*Record){"identity": func(v *Record) { v.OriginalID = "BAI2E6C229787" }, "doi": func(v *Record) { v.DOI = "10.1/other" }, "topic": func(v *Record) { v.Topic = "llm-agent" }, "text": func(v *Record) { v.Abstract += " changed" }, "hash": func(v *Record) { v.AbstractSHA256 = strings.Repeat("0", 64) }, "response": func(v *Record) { v.ResponseSHA256 = strings.Repeat("0", 64) }, "source": func(v *Record) { v.Source = "secondary-index" }, "sourceURL": func(v *Record) { v.SourceURL = "file:///private" }, "version": func(v *Record) { v.SourceVersion = "publisher-verified" }, "completeness": func(v *Record) { v.Completeness = "abstract-fragment" }, "parser": func(v *Record) { v.ParserVersion = "unreviewed" }, "field": func(v *Record) { v.SourceField = "wrong-field" }, "title": func(v *Record) { v.Title = "different" }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) { copy := r; mutate(&copy); require.Error(t, ValidateRecord(copy)) })
	}
}

// TestVerifyRejectsUnpinnedResponses 验证原始响应字节必须匹配预审计哈希，不接受已验证标记。
func TestVerifyRejectsUnpinnedResponses(t *testing.T) {
	for _, s := range Specs() {
		_, err := Verify(s.OriginalID, []byte(`{"verified":true}`))
		require.ErrorContains(t, err, "checksum mismatch")
	}
	_, err := Verify("BAI2E6C229787", nil)
	require.ErrorContains(t, err, "quarantined")
	_, err = Verify("unlisted", nil)
	require.ErrorContains(t, err, "allowlist")
}

// TestBlockAwareTextPreservesComparisons 验证比较符号和内联公式不被当作标签吞掉。
func TestBlockAwareTextPreservesComparisons(t *testing.T) {
	cases := map[string]string{"<h4>Results</h4>P < 0.05 and accuracy > 90%.": "Results P < 0.05 and accuracy > 90%.", "<p>H<sub>2</sub>O &amp; <b>AI</b>.</p>": "H2O & AI.", "<p><i>P</i> &lt;0.001; x&gt;1.</p><p>Next result.</p>": "P <0.001; x>1. Next result.", "p<0.05; 95% CI 1.2–2.3. <h4>Conclusions</h4>Useful.": "p<0.05; 95% CI 1.2–2.3. Conclusions Useful."}
	for input, want := range cases {
		require.Equal(t, want, plainText(input))
	}
}
func TestEuropePMCExactLocatorAndIdentity(t *testing.T) {
	s := Specs()[1]
	record := epmcRecord{DOI: s.DOI, ID: "42823348", Source: "MED", Title: s.Title, Abstract: "<p>P &lt;0.05 and x&gt;1.</p>"}
	encode := func(r epmcRecord) []byte {
		var b bytes.Buffer
		z := gzip.NewWriter(&b)
		require.NoError(t, json.NewEncoder(z).Encode([]any{map[string]any{"record": r}}))
		require.NoError(t, z.Close())
		return b.Bytes()
	}
	text, err := extractEuropePMC(encode(record), s)
	require.NoError(t, err)
	require.Equal(t, "P <0.05 and x>1.", text)
	record.DOI = "10.1234/wrong"
	_, err = extractEuropePMC(encode(record), s)
	require.ErrorContains(t, err, "identifier")
}
func TestSpringerRequiresExactUniqueSection(t *testing.T) {
	s := Specs()[2]
	head := `<meta name="citation_doi" content="` + s.DOI + `"><meta name="citation_title" content="` + s.Title + `">`
	section := `<section data-title="Abstract" aria-labelledby="Abs1"><h2>Abstract</h2><p>A <i>P</i> value &lt;0.05.</p></section>`
	text, err := extractSpringer([]byte(head+section), s)
	require.NoError(t, err)
	require.Equal(t, "A P value <0.05.", text)
	_, err = extractSpringer([]byte(head+section+section), s)
	require.Error(t, err)
	_, err = extractSpringer([]byte(strings.ReplaceAll(head, s.DOI, "10.1234/wrong")+section), s)
	require.Error(t, err)
}
func TestBaselinePinRejectsAlternateSnapshot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/manifest.json", []byte(`{}`), 0600))
	require.ErrorContains(t, VerifyBaseline(dir), "checksum mismatch")
}
