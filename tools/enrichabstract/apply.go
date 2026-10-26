package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/teamsillybees/glean/internal/abstractenrichment"
	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/modules/catalog"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

type applyOptions struct {
	Node       int64
	Operator   string
	AllowLocal bool
}

func (o applyOptions) validate() error {
	if o.Node < 0 || o.Node > 1023 {
		return fmt.Errorf("apply requires explicit -node 0..1023 unique among active instances")
	}
	if _, err := idgen.Parse(o.Operator); err != nil {
		return fmt.Errorf("apply requires an explicit active -operator ID")
	}
	return validateDSN(os.Getenv("GLEAN_ENRICHMENT_DATABASE_URL"), o.AllowLocal)
}

type applyReport struct {
	Mode         string `json:"mode"`
	Verified     int    `json:"verified"`
	Created      int    `json:"created"`
	Existing     int    `json:"existing"`
	Complete     bool   `json:"complete"`
	FailedID     string `json:"failedId,omitempty"`
	BaselineHash string `json:"baselineHash"`
}

func applyRecords(records []abstractenrichment.Record, options applyOptions) (applyReport, error) {
	report := applyReport{Mode: "apply", Verified: len(records), BaselineHash: abstractenrichment.BaselineSHA256}
	if err := options.validate(); err != nil {
		return report, err
	}
	// 所有字节验证在进入此函数之前完成；数据库不存在成员关系时业务服务必须拒绝，不创建新论文。
	for _, r := range records {
		if err := abstractenrichment.ValidateRecord(r); err != nil {
			return report, err
		}
	}
	generator, err := idgen.NewGenerator(options.Node)
	if err != nil {
		return report, fmt.Errorf("cannot configure explicit node")
	}
	operator, _ := idgen.Parse(options.Operator)
	db, err := sql.Open("postgres", os.Getenv("GLEAN_ENRICHMENT_DATABASE_URL"))
	if err != nil {
		return report, fmt.Errorf("cannot open enrichment database")
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signalCtx, time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return report, fmt.Errorf("cannot connect to enrichment database; inspect private connection configuration")
	}
	client := data.NewEntClientFromDB(db, generator)
	defer client.Close()
	service := catalog.NewService(client)
	ctx = entx.WithOperatorID(ctx, operator)
	runID := generator.NextID()
	enrichedAt := time.Now().UTC()
	for _, r := range records {
		result, err := service.EnrichHistoricalAbstract(ctx, r, runID, enrichedAt)
		if err != nil {
			report.FailedID = r.OriginalID
			return report, fmt.Errorf("enrichment stopped for %s; previously completed records are idempotent on retry", r.OriginalID)
		}
		if result.Created {
			report.Created++
		} else {
			report.Existing++
		}
	}
	report.Complete = true
	return report, nil
}
func validateDSN(dsn string, allowLocal bool) error {
	u, err := url.Parse(dsn)
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Fragment != "" {
		return fmt.Errorf("GLEAN_ENRICHMENT_DATABASE_URL must be a PostgreSQL URL without a fragment")
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return fmt.Errorf("database URL has malformed query parameters")
	}
	allowed := map[string]bool{"sslmode": true, "sslrootcert": true, "sslcert": true, "sslkey": true, "connect_timeout": true, "application_name": true}
	for key, v := range values {
		if !allowed[key] || len(v) != 1 || strings.TrimSpace(key) != key {
			return fmt.Errorf("database URL has unknown, duplicate or ambiguous connection parameters")
		}
	}
	for _, key := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE"} {
		if os.Getenv(key) != "" {
			return fmt.Errorf("enrichment forbids connection-routing environment indirection")
		}
	}
	if values.Get("sslmode") == "verify-full" {
		return nil
	}
	host := u.Hostname()
	if allowLocal && (host == "localhost" || host == "127.0.0.1" || host == "::1") && values.Get("sslmode") == "disable" {
		return nil
	}
	return fmt.Errorf("database requires sslmode=verify-full; only explicit loopback development may disable TLS")
}
