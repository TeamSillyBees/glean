// Command glean-monitor executes finite mock monitor scenarios; PostgreSQL writes require --persist-mock.
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
	"syscall"
	"time"

	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/modules/monitor"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
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
	scenario := flags.String("scenario", "incremental", "mock fixture scenario: baseline, incremental, or failure")
	at := flags.String("at", "2026-10-26T12:00:00Z", "fixed baseline RFC3339 timestamp; reuse for idempotent replay")
	node := flags.Int64("node", -1, "required unique Snowflake node for this instance (0-1023)")
	full := flags.Bool("full", false, "include synthetic raw records and mock review results")
	persist := flags.Bool("persist-mock", false, "explicitly write mock monitor state to INITRA_MONITOR_DATABASE_URL; never writes catalog")
	operator := flags.String("operator", "", "existing enabled user ID for persistent-write audit")
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
	var snapshot monitor.Snapshot
	persistence := "in-memory demo only; no database writes"
	mode := "offline-mock"
	if *persist {
		snapshot, err = runPersistent(ctx, now, *node, *scenario, *operator, os.Getenv("INITRA_MONITOR_DATABASE_URL"), *allowLocal)
		persistence = "postgresql; visible through authenticated monitor API; historical catalog untouched"
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
	if u.Query().Get("sslmode") == "verify-full" {
		return nil
	}
	host := u.Hostname()
	if allowLocal && (host == "localhost" || host == "127.0.0.1" || host == "::1") && u.Query().Get("sslmode") == "disable" {
		return nil
	}
	return fmt.Errorf("database requires sslmode=verify-full; loopback development may explicitly allow sslmode=disable")
}
