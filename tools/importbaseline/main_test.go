package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teamsillybees/glean/internal/modules/catalog"
)

func TestValidateDSNDoesNotLeakCredentials(t *testing.T) {
	for _, key := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE"} {
		t.Setenv(key, "")
	}
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

func TestValidateDSNRejectsQueryOverridesAndAmbiguity(t *testing.T) {
	for _, key := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE"} {
		t.Setenv(key, "")
	}
	cases := []struct {
		name  string
		query string
	}{
		{"host", "sslmode=disable&host=external.example"},
		{"hostaddr", "sslmode=disable&hostaddr=203.0.113.8"},
		{"encoded_hostaddr", "sslmode=disable&host%61ddr=203.0.113.8"},
		{"port", "sslmode=disable&port=6432"},
		{"user", "sslmode=disable&user=another-user"},
		{"password", "sslmode=disable&password=do-not-echo-query-value"},
		{"dbname", "sslmode=disable&dbname=another-db"},
		{"database_alias", "sslmode=disable&database=another-db"},
		{"service", "sslmode=disable&service=another-service"},
		{"service_file", "sslmode=disable&servicefile=do-not-echo-service-path"},
		{"uppercase_override", "sslmode=disable&HOST=external.example"},
		{"empty_override", "sslmode=disable&host="},
		{"duplicate_sslmode", "sslmode=disable&sslmode=verify-full"},
		{"duplicate_equal_sslmode", "sslmode=disable&sslmode=disable"},
		{"encoded_duplicate_sslmode", "sslmode=disable&%73slmode=verify-full"},
		{"case_ambiguous_sslmode", "sslmode=disable&SSLMode=verify-full"},
		{"duplicate_tls_option", "sslmode=verify-full&sslrootcert=a&sslrootcert=b"},
		{"duplicate_runtime_option", "sslmode=verify-full&application_name=a&application_name=b"},
		{"invalid_escape", "sslmode=verify-full&application_name=%zz"},
		{"unescaped_semicolon", "sslmode=verify-full&application_name=a;b"},
		{"empty_key", "sslmode=verify-full&=value"},
		{"key_whitespace_injection", "sslmode=verify-full&host%20=external.example"},
		{"key_equals_injection", "sslmode=verify-full&host%3Dexternal.example%20options=value"},
		{"key_quote_injection", "sslmode=verify-full&application_name%27=value"},
		{"key_backslash_injection", "sslmode=verify-full&application_name%5C=value"},
		{"key_newline_injection", "sslmode=verify-full&application_name%0Ahost=value"},
		{"fragment", "sslmode=verify-full#do-not-echo-fragment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dsn := "postgres://do-not-echo-user:do-not-echo-password@127.0.0.1/catalog?" + tc.query
			err := validateDSN(dsn, true)
			if err == nil {
				t.Fatal("unsafe or ambiguous connection URL accepted")
			}
			if strings.Contains(err.Error(), "do-not-echo") || strings.Contains(err.Error(), dsn) || strings.Contains(err.Error(), "external.example") {
				t.Fatal("connection data leaked in validation error")
			}
		})
	}
}

func TestValidateDSNPreservesNormalTLSAndRuntimeOptions(t *testing.T) {
	for _, key := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE"} {
		t.Setenv(key, "")
	}
	cases := []struct {
		name, dsn string
		local     bool
	}{
		{"verified_external", "postgresql://user:encoded%40password@database.example:5432/catalog?sslmode=verify-full", false},
		{"tls_options", "postgres://database.example/catalog?sslmode=verify-full&sslrootcert=%2Fcerts%2Fca.pem&sslcert=%2Fcerts%2Fclient.pem&sslkey=%2Fcerts%2Fclient.key&ssl_min_protocol_version=TLSv1.2", false},
		{"runtime_options", "postgres://database.example/catalog?sslmode=verify-full&application_name=glean%20import&connect_timeout=10&search_path=glean&options=-c%20statement_timeout%3D0&TimeZone=UTC&custom.setting=enabled", false},
		{"escaped_value_not_key", "postgres://database.example/catalog?sslmode=verify-full&application_name=%27%20host%3Dexternal.example%20%5C", false},
		{"local_ipv4", "postgres://127.0.0.1:5432/catalog?sslmode=disable", true},
		{"local_name", "postgres://localhost:5432/catalog?sslmode=disable", true},
		{"local_ipv6", "postgres://[::1]:5432/catalog?sslmode=disable", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateDSN(tc.dsn, tc.local); err != nil {
				t.Fatalf("ordinary explicit connection rejected: %v", err)
			}
		})
	}
}

func TestValidateDSNRejectsInsecureEnvironmentRouting(t *testing.T) {
	for _, key := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE"} {
		t.Setenv(key, "")
	}
	for _, key := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "do-not-echo-environment-routing")
			if err := validateDSN("postgres://127.0.0.1/catalog?sslmode=disable", true); err == nil || strings.Contains(err.Error(), "do-not-echo") {
				t.Fatal("insecure environment routing accepted or environment value leaked")
			}
			if err := validateDSN("postgres://database.example/catalog?sslmode=verify-full", false); err != nil {
				t.Fatalf("verified external URL unexpectedly rejected: %v", err)
			}
		})
	}
}

func TestValidateDSNRejectsVerifiedExternalIdentityOverrides(t *testing.T) {
	for _, key := range []string{"host", "hostaddr", "port", "user", "password", "dbname", "database", "service", "servicefile"} {
		t.Run(key, func(t *testing.T) {
			dsn := "postgres://database.example/catalog?sslmode=verify-full&" + key + "=other"
			if err := validateDSN(dsn, false); err == nil {
				t.Fatal("verify-full must not bypass connection identity validation")
			}
		})
	}
}
