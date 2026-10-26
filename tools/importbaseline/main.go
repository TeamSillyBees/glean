// importbaseline 将真实历史目录提取为审计快照，或显式导入已迁移的 PostgreSQL。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/teamsillybees/glean/internal/baseline"
	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/modules/catalog"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

type sources []string

// String 返回已指定的来源参数。
func (s *sources) String() string { return strings.Join(*s, ",") }

// Set 收集可重复使用的来源参数。
func (s *sources) Set(v string) error { *s = append(*s, v); return nil }

var authoritative = map[string]baseline.SourceSpec{
	"llm-agent":         {Topic: "llm-agent", SHA256: "c8ddafd5965779f3adb1b38e552567bdb5e45b6757f7ee90cb537e8b8b047d07", Memberships: 4059},
	"biomed-ai":         {Topic: "biomed-ai", SHA256: "39ac575702596bf4938c0b20376e57982ed69ebb565a8e0be0247c66a6021054", Memberships: 31399},
	"biomed-worldmodel": {Topic: "biomed-worldmodel", SHA256: "1537951986e5f9a63b77e4c5457955623fad24d6bfb2110dbc283ce0366e5090", Memberships: 4037},
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "baseline:", err)
		os.Exit(1)
	}
}
func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("importbaseline", flag.ContinueOnError)
	journalDir := flags.String("journals", "data/journals", "versioned historical journal registry directory")
	mode := flags.String("mode", "verify", "verify | extract | import")
	dir := flags.String("dir", "data/baselines", "snapshot output/input directory")
	archive := flags.String("archive", ".baseline/archive", "private, ignored archive directory (extract only)")
	reportPath := flags.String("report", ".baseline/import-report.json", "private import report path")
	quarantinePath := flags.String("quarantine", ".baseline/quarantine.ndjson", "private failed-record output path")
	node := flags.Int64("node", -1, "required unique Snowflake node for database import")
	operator := flags.String("operator", "", "required existing operator ID for audit")
	allowLocal := flags.Bool("allow-local-insecure", false, "allow sslmode=disable only for loopback development database")
	var input sources
	flags.Var(&input, "source", "topic=HTML path; repeat for all three authoritative sources")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *mode == "extract" {
		if len(input) != 3 {
			return fmt.Errorf("extract requires all three -source topic=HTML arguments")
		}
		specs := []baseline.SourceSpec{}
		for _, arg := range input {
			topic, path, ok := strings.Cut(arg, "=")
			spec, known := authoritative[topic]
			if !ok || !known || path == "" {
				return fmt.Errorf("invalid source topic or path")
			}
			spec.Path = path
			specs = append(specs, spec)
		}
		m, err := baseline.Extract(specs, baseline.ExtractOptions{OutputDir: *dir, ArchiveDir: *archive})
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(map[string]any{"memberships": m.Memberships, "uniqueIdentityCandidates": m.UniqueIdentityCandidates, "identityConflicts": m.IdentityConflicts, "shards": len(m.Shards)})
	}
	if *mode != "verify" && *mode != "import" {
		return fmt.Errorf("unknown mode")
	}
	m, err := baseline.ReadManifest(filepath.Join(*dir, "manifest.json"))
	if err != nil {
		return err
	}
	if err := baseline.Verify(*dir, m); err != nil {
		return err
	}
	if *mode == "verify" {
		return json.NewEncoder(out).Encode(map[string]any{"verified": true, "memberships": m.Memberships, "shards": len(m.Shards)})
	}
	if *node < 0 || *node > 1023 {
		return fmt.Errorf("import requires explicit -node 0..1023 unique among active instances")
	}
	operatorID, err := idgen.Parse(*operator)
	if err != nil {
		return fmt.Errorf("import requires valid -operator ID")
	}
	dsn := os.Getenv("GLEAN_BASELINE_DATABASE_URL")
	if err := validateDSN(dsn, *allowLocal); err != nil {
		return err
	}
	generator, err := idgen.NewGenerator(*node)
	if err != nil {
		return fmt.Errorf("invalid ID generator configuration")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("database open failed; inspect private database configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = db.PingContext(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("database connection failed; inspect private database configuration")
	}
	client := data.NewEntClientFromDB(db, generator)
	defer client.Close()
	service := catalog.NewService(client)
	if err := os.MkdirAll(filepath.Dir(*reportPath), 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*quarantinePath), 0700); err != nil {
		return err
	}
	q, err := os.OpenFile(*quarantinePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer q.Close()
	importContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx = entx.WithOperatorID(importContext, operatorID)
	registeredVenues := map[string]bool{}
	journalEntries, err := baseline.ReadJournalRegistry(*journalDir)
	if err != nil {
		return fmt.Errorf("load verified historical journal registry: %w", err)
	}
	for _, entry := range journalEntries {
		encoded, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		var venue catalog.VenueVO
		if err = json.Unmarshal(encoded, &venue); err != nil {
			return err
		}
		if err = service.RegisterVenue(ctx, venue); err != nil {
			return fmt.Errorf("persist journal registry failed")
		}
		registeredVenues[venue.Name+":"+venue.RegistryVersion] = true
	}

	report, importErr := baseline.Import(ctx, *dir, m, func(ctx context.Context, line json.RawMessage) (bool, error) {
		var r catalog.ImportRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return false, err
		}
		var normalized baseline.Record
		if err := json.Unmarshal(line, &normalized); err != nil {
			return false, err
		}
		if venue, ok := baseline.VenueFromRecord(normalized); ok {
			key := venue.Name + ":" + venue.RegistryVersion
			if !registeredVenues[key] {
				encoded, err := json.Marshal(venue)
				if err != nil {
					return false, err
				}
				var dto catalog.VenueVO
				if err := json.Unmarshal(encoded, &dto); err != nil {
					return false, err
				}
				if err := service.RegisterVenue(ctx, dto); err != nil {
					return false, err
				}
				registeredVenues[key] = true
			}
		}
		result, err := service.Import(ctx, r)
		return result.Created, err
	}, q)
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*reportPath, append(body, '\n'), 0600); err != nil {
		return err
	}
	if err := json.NewEncoder(out).Encode(report); err != nil {
		return err
	}
	if importErr != nil {
		if errors.Is(importErr, baseline.ErrQuarantined) {
			return baseline.ErrQuarantined
		}
		return fmt.Errorf("import interrupted; inspect counts and private quarantine")
	}
	return nil
}
func validateDSN(dsn string, allowLocal bool) error {
	u, err := url.Parse(dsn)
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" {
		return fmt.Errorf("GLEAN_BASELINE_DATABASE_URL must be a PostgreSQL URL")
	}
	// URL.Query 会丢弃解析错误，且 pq 将查询键直接拼入连接参数；两者都不能作为安全边界。
	if u.Fragment != "" {
		return fmt.Errorf("database URL must not contain a fragment")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return fmt.Errorf("database URL contains malformed query parameters")
	}
	seen := make(map[string]bool, len(query))
	for key, values := range query {
		if !validDSNQueryKey(key) {
			return fmt.Errorf("database URL contains an invalid query parameter name")
		}
		canonical := strings.ToLower(key)
		if len(values) != 1 || seen[canonical] {
			return fmt.Errorf("database URL contains duplicate or ambiguous query parameters")
		}
		seen[canonical] = true
		// 连接目标与账号只接受 URL authority/path，禁止查询参数或 service 间接覆盖。
		switch canonical {
		case "host", "hostaddr", "port", "user", "password", "dbname", "database", "service", "servicefile":
			return fmt.Errorf("database URL query must not override connection identity or service routing")
		}
	}
	ssl := query.Get("sslmode")
	if ssl == "verify-full" {
		return nil
	}
	host := u.Hostname()
	if allowLocal && (host == "localhost" || host == "127.0.0.1" || host == "::1") && ssl == "disable" {
		// 即使 URL 中没有覆盖，pq 仍会读取这些环境参数并可能绕过回环地址。
		for _, key := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE"} {
			if os.Getenv(key) != "" {
				return fmt.Errorf("local insecure import forbids PGHOSTADDR, PGSERVICE and PGSERVICEFILE indirection")
			}
		}
		return nil
	}
	return fmt.Errorf("database requires sslmode=verify-full; loopback development may opt in to sslmode=disable")
}

// validDSNQueryKey 限定可安全传给 pq 的 ASCII 参数名，值仍允许正常 URL 转义。
func validDSNQueryKey(key string) bool {
	if key == "" {
		return false
	}
	for i, c := range key {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			continue
		}
		if i > 0 && (c >= '0' && c <= '9' || c == '.') {
			continue
		}
		return false
	}
	return true
}
