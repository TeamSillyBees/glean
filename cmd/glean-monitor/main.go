// Command glean-monitor performs bounded real literature scans; synthetic fixtures require explicit --demo.
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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/teamsillybees/glean/internal/boot"
	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanobservation"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaper"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/modules/catalog"
	"github.com/teamsillybees/glean/internal/modules/monitor"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "monitor:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("glean-monitor", flag.ContinueOnError)
	demo := flags.Bool("demo", false, "explicit synthetic fixture mode; normal mode uses real source APIs and PostgreSQL")
	retryFailed := flags.String("retry-failed", "", "explicitly resume classified transient failures from a retained run ID; original sources/window are preserved")
	doctor := flags.Bool("doctor", false, "check database, actor and real-source configuration without calling source APIs")
	since := flags.String("since", "", "inclusive real source update lower bound, RFC3339; omit with until to resume scheduled window")
	until := flags.String("until", "", "exclusive fixed real source watermark, RFC3339")
	sourceNames := flags.String("sources", strings.Join(monitor.DefaultSourceNames(), ","), "comma-separated official sources")
	maxPages := flags.Int("max-pages", 12, "maximum upstream request attempts for this invocation, 1-200")
	pageSize := flags.Int("page-size", 100, "requested records per upstream page, 1-1000; providers may impose their own page size")
	requireObservation := flags.Bool("require-observation", false, "fail unless the real run has a committed catalog observation; useful for a bounded live canary")
	contact := flags.String("contact", "", "optional explicit public-source contact; never inferred from your account")

	scenario := flags.String("scenario", "incremental", "mock fixture scenario: baseline, incremental, or failure")
	at := flags.String("at", "2026-10-26T12:00:00Z", "fixed baseline RFC3339 timestamp; reuse for idempotent replay")
	nodeDefault := int64(-1)
	if value, err := strconv.ParseInt(os.Getenv("INITRA_IDGEN_NODE"), 10, 64); err == nil {
		nodeDefault = value
	}
	node := flags.Int64("node", nodeDefault, "required unique Snowflake node for this instance (0-1023)")
	full := flags.Bool("full", false, "include synthetic raw records and mock review results")
	persist := flags.Bool("persist-mock", false, "explicitly write mock monitor state to INITRA_MONITOR_DATABASE_URL; never writes catalog")
	operator := flags.String("operator", os.Getenv("INITRA_MONITOR_OPERATOR_ID"), "existing enabled user ID for persistent-write audit")
	allowLocal := flags.Bool("allow-local-insecure", false, "allow sslmode=disable only for loopback development database")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *node < 0 || *node > 1023 {
		return fmt.Errorf("--node must explicitly identify this instance (0-1023)")
	}
	now, err := time.Parse(time.RFC3339, *at)
	if err != nil {
		return fmt.Errorf("invalid --at RFC3339 timestamp")
	}
	if *scenario != "baseline" && *scenario != "incremental" && *scenario != "failure" {
		return fmt.Errorf("unknown mock scenario")
	}
	var retryID idgen.ID
	if *retryFailed != "" {
		retryID, err = idgen.Parse(*retryFailed)
		if err != nil || retryID <= 0 {
			return fmt.Errorf("--retry-failed requires a positive run ID")
		}
		if *demo || *doctor || *since != "" || *until != "" {
			return fmt.Errorf("--retry-failed cannot be combined with --demo/--doctor/--since/--until")
		}
	}
	if !*demo {
		if *persist {
			return fmt.Errorf("--persist-mock requires explicit --demo; normal mode is real")
		}
		return runLive(ctx, out, *node, *operator, os.Getenv("INITRA_MONITOR_DATABASE_URL"), *allowLocal, *sourceNames, *since, *until, *maxPages, *pageSize, *contact, *doctor, *full, *requireObservation, retryID)
	}
	if *doctor || *since != "" || *until != "" || *requireObservation {
		return fmt.Errorf("--doctor/--since/--until apply to real mode, not --demo")
	}
	var snapshot monitor.Snapshot
	persistence := "in-memory demo only; no database writes"
	mode := "offline-mock"
	if *persist {
		snapshot, err = runPersistent(ctx, now, *node, *scenario, *operator, os.Getenv("INITRA_MONITOR_DATABASE_URL"), *allowLocal)
		persistence = "postgresql; separate demo state; normal real-source API remains untouched"
		mode = "persistent-mock"
	} else {
		snapshot, err = monitor.RunDemo(ctx, now, *node, *scenario)
	}
	if err != nil {
		return err
	}
	var output any = snapshot
	if !*full {
		output = struct {
			Mode              string                         `json:"mode"`
			Persistence       string                         `json:"persistence"`
			ScientificReview  bool                           `json:"scientificReview"`
			CandidateVersions int                            `json:"candidateVersions"`
			Schedule          monitor.Schedule               `json:"schedule"`
			Runs              []monitor.Run                  `json:"runs"`
			Sources           map[string]*monitor.Checkpoint `json:"sources"`
		}{Mode: mode, Persistence: persistence, CandidateVersions: len(snapshot.Records), Schedule: snapshot.Schedule, Runs: snapshot.Runs, Sources: snapshot.Sources}
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(output); err != nil {
		return fmt.Errorf("write output failed")
	}
	return nil
}

func runPersistent(ctx context.Context, now time.Time, node int64, scenario, operator, dsn string, allowLocal bool) (monitor.Snapshot, error) {
	if err := validateDSN(dsn, allowLocal); err != nil {
		return monitor.Snapshot{}, err
	}
	operatorID, err := idgen.Parse(operator)
	if err != nil || operatorID <= 0 {
		return monitor.Snapshot{}, fmt.Errorf("--persist-mock requires --operator with an existing enabled user ID")
	}
	runCtx, cancelRun := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelRun()
	ctx = runCtx
	ids, err := idgen.NewGenerator(node)
	if err != nil {
		return monitor.Snapshot{}, fmt.Errorf("invalid ID generator configuration")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return monitor.Snapshot{}, fmt.Errorf("database open failed; inspect private database configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = db.PingContext(pingCtx)
	cancel()
	if err != nil {
		return monitor.Snapshot{}, fmt.Errorf("database connection failed; inspect private database configuration")
	}
	client := data.NewEntClientFromDB(db, ids)
	defer client.Close()
	actorCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	exists, err := client.SysUser.Query().Where(sysuser.IDEQ(operatorID), sysuser.DeletedAtIsNil(), sysuser.IsEnableEQ(true)).Exist(actorCtx)
	cancel()
	if err != nil {
		return monitor.Snapshot{}, fmt.Errorf("operator lookup failed; apply migrations and inspect private database configuration")
	}
	if !exists {
		return monitor.Snapshot{}, fmt.Errorf("--operator must identify an existing enabled user")
	}
	snapshot, err := monitor.RunPersistentDemo(entx.WithOperatorID(ctx, operatorID), client, ids, now, scenario)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return snapshot, fmt.Errorf("mock run canceled or timed out; progress is durable and can be resumed with the same arguments")
	}
	if err != nil {
		return snapshot, fmt.Errorf("persistent mock run failed; inspect migrations/state or retry when the active monitor run finishes")
	}
	return snapshot, nil
}

func validateDSN(dsn string, allowLocal bool) error {
	u, err := url.Parse(dsn)
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" {
		return fmt.Errorf("INITRA_MONITOR_DATABASE_URL must be a PostgreSQL URL")
	}
	query, queryErr := url.ParseQuery(u.RawQuery)
	if queryErr != nil || u.Fragment != "" || strings.Trim(u.Path, "/") == "" {
		return fmt.Errorf("database URL has an invalid query, fragment or missing database name")
	}
	allowed := map[string]bool{"sslmode": true, "sslrootcert": true, "sslcert": true, "sslkey": true, "connect_timeout": true, "application_name": true, "search_path": true}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			return fmt.Errorf("database URL contains unsupported or duplicate connection options")
		}
	}
	if len(query["sslmode"]) != 1 {
		return fmt.Errorf("database URL requires exactly one explicit sslmode")
	}
	if query.Get("sslmode") == "verify-full" {
		return nil
	}
	host := u.Hostname()
	if query.Get("sslmode") == "disable" {
		for _, key := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE"} {
			if strings.TrimSpace(os.Getenv(key)) != "" {
				return fmt.Errorf("loopback plaintext connection rejects inherited endpoint indirection; unset PostgreSQL service/host-address overrides")
			}
		}
	}
	if allowLocal && (host == "localhost" || host == "127.0.0.1" || host == "::1") && query.Get("sslmode") == "disable" {
		return nil
	}
	return fmt.Errorf("database requires sslmode=verify-full; loopback development may explicitly allow sslmode=disable")
}

func runLive(ctx context.Context, out io.Writer, node int64, operator, dsn string, allowLocal bool, sourceNames, since, until string, maxPages, pageSize int, contact string, doctor, full, requireObservation bool, retryID idgen.ID) error {
	if pageSize < 1 || pageSize > 1000 {
		return fmt.Errorf("--page-size must be 1-1000")
	}
	if maxPages < 1 || maxPages > 200 {
		return fmt.Errorf("--max-pages must be 1-200")
	}
	names := []string{}
	allowed := map[string]bool{}
	for _, name := range monitor.DefaultSourceNames() {
		allowed[name] = true
	}
	seen := map[string]bool{}
	for _, name := range strings.Split(sourceNames, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		if !allowed[name] || seen[name] {
			return fmt.Errorf("--sources must contain distinct supported official sources")
		}
		seen[name] = true
		names = append(names, name)
	}
	var window *monitor.Window
	if since != "" || until != "" {
		end := time.Now().UTC()
		var err error
		if until != "" {
			end, err = time.Parse(time.RFC3339, until)
			if err != nil {
				return fmt.Errorf("invalid --until RFC3339 timestamp")
			}
		}
		start := end.Add(-30 * 24 * time.Hour)
		if since != "" {
			start, err = time.Parse(time.RFC3339, since)
			if err != nil {
				return fmt.Errorf("invalid --since RFC3339 timestamp")
			}
		}
		if !start.Before(end) || end.Sub(start) > 90*24*time.Hour || end.After(time.Now().UTC()) {
			return fmt.Errorf("scan window must be positive and at most 90 days")
		}
		window = &monitor.Window{Start: start.UTC(), End: end.UTC()}
	}
	if err := validateDSN(dsn, allowLocal); err != nil {
		return err
	}
	operatorID, err := idgen.Parse(operator)
	if err != nil || operatorID <= 0 {
		if !doctor {
			return fmt.Errorf("real mode requires --operator or INITRA_MONITOR_OPERATOR_ID for an existing enabled user")
		}
		operatorID = 0
	}
	if strings.ContainsAny(contact, "\r\n") {
		return fmt.Errorf("contact cannot contain header line breaks")
	}
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ctx = runCtx
	ids, err := idgen.NewGenerator(node)
	if err != nil {
		return fmt.Errorf("invalid ID generator configuration")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("database open failed; inspect private configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	pingCtx, pingCancel := context.WithTimeout(ctx, 15*time.Second)
	err = db.PingContext(pingCtx)
	pingCancel()
	if err != nil {
		return fmt.Errorf("database connection failed; inspect private configuration")
	}
	client := data.NewEntClientFromDB(db, ids)
	defer client.Close()
	factory, err := httpclient.NewFactory(httpclient.Config{Enabled: true, Services: monitor.SourceHTTPServices(contact)}, logx.NewNop(), monitor.SourceHTTPFactoryOptions()...)
	if err != nil {
		return fmt.Errorf("real source configuration invalid")
	}
	defer factory.ClearAll()
	options := monitor.ProviderOptions{Config: monitor.RuntimeConfig(), Sources: names, OperatorID: operatorID}
	options.Config.MaxPagesPerCall = maxPages
	options.Config.PageSize = pageSize
	catalogService := catalog.NewService(client)
	service, err := monitor.NewRuntimeService(client, ids, factory, options, boot.NewCatalogMonitorPublisher(catalogService, operatorID), boot.NewMonitorJournalProvider(catalogService))
	if err != nil {
		return fmt.Errorf("real monitor assembly failed; inspect configured official sources")
	}
	if doctor {
		diagnostics, err := service.Doctor(ctx)
		if err != nil {
			return fmt.Errorf("monitor diagnostic failed")
		}
		if err := json.NewEncoder(out).Encode(struct {
			Mode        string               `json:"mode"`
			Sources     []string             `json:"sources"`
			Diagnostics []monitor.Diagnostic `json:"diagnostics"`
		}{"real-diagnostic-no-fetch", names, diagnostics}); err != nil {
			return err
		}
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity == "error" {
				return fmt.Errorf("monitor is not ready; see diagnostic codes above")
			}
		}
		return nil
	}
	var snapshot monitor.Snapshot
	if retryID > 0 {
		snapshot, err = service.RetryFailedFinite(ctx, retryID, maxPages)
	} else if window != nil {
		snapshot, err = service.RunFinite(ctx, *window, maxPages)
	} else {
		snapshot, err = service.ResumeFinite(ctx, maxPages)
	}
	if err != nil {
		return fmt.Errorf("real scan interrupted or blocked; durable progress preserved; inspect actor/database or resume original sources/window")
	}
	reportSnapshot := snapshot
	if retryID > 0 {
		reportSnapshot, err = retryReportSnapshot(snapshot, retryID)
		if err != nil {
			return err
		}
	}
	observations, readErr := readRunObservations(ctx, client, reportSnapshot)
	if readErr != nil {
		return fmt.Errorf("scan persisted but observation report could not be read")
	}
	status := realRunStatus(reportSnapshot)
	result := map[string]any{"mode": "real", "status": status, "selectedRunId": selectedRunID(reportSnapshot), "llmReview": "mock; scientific inclusion remains pending", "schedule": snapshot.Schedule, "runs": snapshot.Runs, "sources": snapshot.Sources, "requestBudget": maxPages, "pageSize": pageSize, "observations": observations}
	if full {
		result["snapshot"] = snapshot
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return err
	}
	if status == "quarantined" {
		return fmt.Errorf("some records require quarantine review; valid records were committed and raw rejection evidence retained")
	}
	if status == "blocked" {
		return fmt.Errorf("source blocked or deferred by an upstream error; inspect safe source status and respect retry time")
	}
	if requireObservation && len(observations) == 0 {
		return fmt.Errorf("bounded scan produced no committed catalog observation; no synthetic fallback used")
	}
	return nil
}

type observationReport struct {
	PaperID            idgen.ID `json:"paperId"`
	ObservationID      idgen.ID `json:"observationId"`
	RunID              idgen.ID `json:"runId"`
	Source             string   `json:"source"`
	SourceID           string   `json:"sourceId"`
	Title              string   `json:"title"`
	ObservationTitle   string   `json:"observationTitle"`
	SourceURL          string   `json:"sourceUrl"`
	LLMIsMock          bool     `json:"llmIsMock"`
	ScientificReviewed bool     `json:"scientificReviewed"`
}

func readRunObservations(ctx context.Context, client *ent.Client, snapshot monitor.Snapshot) ([]observationReport, error) {
	result := []observationReport{}
	if len(snapshot.Runs) == 0 {
		return result, nil
	}
	run := snapshot.Runs[len(snapshot.Runs)-1]
	rows, err := client.GleanObservation.Query().Where(gleanobservation.RunID(run.ID)).Order(ent.Asc(gleanobservation.FieldID)).Limit(200).All(ctx)
	if err != nil {
		return nil, err
	}
	paperIDs := make([]idgen.ID, 0, len(rows))
	for _, row := range rows {
		paperIDs = append(paperIDs, row.PaperID)
	}
	titles := map[idgen.ID]string{}
	if len(paperIDs) > 0 {
		papers, err := client.GleanPaper.Query().Where(gleanpaper.IDIn(paperIDs...)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, paper := range papers {
			titles[paper.ID] = paper.Title
		}
	}
	for _, row := range rows {
		var input catalog.ObservationInput
		if err := json.Unmarshal(row.Metadata, &input); err != nil {
			return nil, err
		}
		result = append(result, observationReport{PaperID: row.PaperID, ObservationID: row.ID, RunID: row.RunID, Source: row.Source, SourceID: row.SourceID, Title: titles[row.PaperID], ObservationTitle: input.Title, SourceURL: input.SourceURL, LLMIsMock: input.Review.IsMock, ScientificReviewed: input.Review.ScientificReviewed})
	}
	return result, nil
}

func realRunStatus(snapshot monitor.Snapshot) string {
	if len(snapshot.Runs) == 0 {
		return "not-due"
	}
	run := snapshot.Runs[len(snapshot.Runs)-1]
	for name := range run.Sources {
		if checkpoint := snapshot.Sources[name]; checkpoint != nil && (checkpoint.Blocked || checkpoint.LastError != "") {
			return "blocked"
		}
	}
	for _, progress := range run.Sources {
		if progress.Quarantined > 0 {
			return "quarantined"
		}
	}
	if !run.FinishedAt.IsZero() {
		return "completed"
	}
	return "bounded-partial"
}

// retryReportSnapshot 仅筛选报告视图，不更改持久化历史；旧恢复运行不借用最新无关运行的状态。
func retryReportSnapshot(snapshot monitor.Snapshot, originalID idgen.ID) (monitor.Snapshot, error) {
	var target idgen.ID
	for _, run := range snapshot.Runs {
		if run.ID == originalID {
			target = run.RetriedBy
			break
		}
	}
	if target == 0 {
		for _, run := range snapshot.Runs {
			if run.RetryOf == originalID {
				if target != 0 {
					return monitor.Snapshot{}, fmt.Errorf("ambiguous retry report target")
				}
				target = run.ID
			}
		}
	}
	for _, run := range snapshot.Runs {
		if target != 0 && run.ID == target {
			view := snapshot
			view.Runs = []monitor.Run{run}
			if !run.FinishedAt.IsZero() {
				view.Sources = map[string]*monitor.Checkpoint{}
				for name, p := range run.Sources {
					view.Sources[name] = &monitor.Checkpoint{Source: name, Blocked: p.Failed, LastError: p.LastError}
				}
			}
			return view, nil
		}
	}
	return monitor.Snapshot{}, fmt.Errorf("retry report target is not retained")
}
func selectedRunID(snapshot monitor.Snapshot) idgen.ID {
	if len(snapshot.Runs) == 0 {
		return 0
	}
	return snapshot.Runs[len(snapshot.Runs)-1].ID
}
