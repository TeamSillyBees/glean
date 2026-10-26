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
	ssl := u.Query().Get("sslmode")
	if ssl == "verify-full" {
		return nil
	}
	host := u.Hostname()
	if allowLocal && (host == "localhost" || host == "127.0.0.1" || host == "::1") && ssl == "disable" {
		return nil
	}
	return fmt.Errorf("database requires sslmode=verify-full; loopback development may opt in to sslmode=disable")
}
