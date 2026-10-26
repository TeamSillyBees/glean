package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"github.com/teamsillybees/glean/internal/modules/catalog"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDSNDoesNotLeakCredentials(t *testing.T) {
	cases := []struct {
		dsn         string
		local, good bool
	}{{"postgres://user:synthetic@db.example/catalog?sslmode=verify-full", false, true}, {"postgres://user:synthetic@127.0.0.1/catalog?sslmode=disable", true, true}, {"postgres://user:synthetic@db.example/catalog?sslmode=disable", true, false}, {"postgres://user:synthetic@127.0.0.1/catalog?sslmode=disable", false, false}, {"invalid", false, false}}
	for _, c := range cases {
		err := validateDSN(c.dsn, c.local)
		if (err == nil) != c.good {
			t.Fatalf("expected good=%v, got %v", c.good, err)
		}
	}
}
func TestExtractRequiresAllAuthoritativeInputs(t *testing.T) {
	if err := run([]string{"-mode", "extract"}, io.Discard); err == nil {
		t.Fatal("missing sources accepted")
	}
}

func TestActualHistoricalFixtureMatchesCatalogDTO(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "data", "baselines", "testdata", "records.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	counts := map[string]int{}
	for scanner.Scan() {
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		var record catalog.ImportRecord
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record.OriginalID == "" || record.Title == "" || len(record.Raw) == 0 || record.IsMock {
			t.Fatal("real fixture metadata lost")
		}
		counts[record.Topic]++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(counts) != 3 || counts["llm-agent"] != 4 || counts["biomed-ai"] != 4 || counts["biomed-worldmodel"] != 4 {
		t.Fatalf("wrong actual fixture distribution: %#v", counts)
	}
}
