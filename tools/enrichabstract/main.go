// enrichabstract 从本机三个固定来源响应重新校验历史摘要；默认不连接数据库。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/teamsillybees/glean/internal/abstractenrichment"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "abstract enrichment:", err)
		os.Exit(1)
	}
}
func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("enrichabstract", flag.ContinueOnError)
	mode := flags.String("mode", "verify", "verify (read-only) | apply (explicit database write)")
	node := flags.Int64("node", -1, "explicit unique Snowflake node for apply")
	operator := flags.String("operator", "", "explicit active audit operator for apply")
	allowLocal := flags.Bool("allow-local-insecure", false, "allow sslmode=disable only for loopback development database")
	dir := flags.String("baseline", "data/baselines", "immutable baseline directory")
	sourceMap := flags.String("sources", "", "private JSON object mapping the three original IDs to local response files")
	output := flags.String("out", "", "optional public-safe verified-record output JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *mode != "verify" && *mode != "apply" {
		return fmt.Errorf("mode must be verify or apply")
	}
	options := applyOptions{Node: *node, Operator: *operator, AllowLocal: *allowLocal}
	if *mode == "apply" {
		if err := options.validate(); err != nil {
			return err
		}
	}
	if *sourceMap == "" {
		return fmt.Errorf("-sources is required; no implicit cache scan is performed")
	}
	if err := abstractenrichment.VerifyBaseline(*dir); err != nil {
		return err
	}
	records, err := verifySources(*sourceMap)
	if err != nil {
		return err
	}
	if *output != "" {
		body, err := json.MarshalIndent(records, "", "  ")
		if err != nil {
			return err
		}
		file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("record output must be a new writable file; existing files are never overwritten")
		}
		_, writeErr := file.Write(append(body, '\n'))
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return fmt.Errorf("cannot write verified record output")
		}
	}
	if *mode == "apply" {
		result, applyErr := applyRecords(records, options)
		if encodeErr := json.NewEncoder(out).Encode(result); encodeErr != nil {
			return encodeErr
		}
		return applyErr
	}
	ids := []string{}
	for _, r := range records {
		ids = append(ids, r.OriginalID)
	}
	return json.NewEncoder(out).Encode(map[string]any{"mode": "verify", "verified": len(records), "originalIds": ids, "baselineHash": abstractenrichment.BaselineSHA256, "databaseWrites": 0, "networkCalls": 0, "limitation": "Exact cached source-field correspondence; not independent publisher completeness or scientific review."})
}
func verifySources(path string) ([]abstractenrichment.Record, error) {
	body, err := readBounded(path, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("cannot read private source map")
	}
	var paths map[string]string
	if json.Unmarshal(body, &paths) != nil || len(paths) != len(abstractenrichment.Specs()) {
		return nil, fmt.Errorf("source map must contain exactly the three allowlisted original IDs")
	}
	records := make([]abstractenrichment.Record, 0, len(paths))
	for _, s := range abstractenrichment.Specs() {
		p, ok := paths[s.OriginalID]
		if !ok || p == "" {
			return nil, fmt.Errorf("source map is missing an allowlisted original ID")
		}
		response, err := readBounded(p, 4<<20)
		if err != nil {
			return nil, fmt.Errorf("cannot read bounded source response for %s", s.OriginalID)
		}
		r, err := abstractenrichment.Verify(s.OriginalID, response)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.OriginalID, err)
		}
		records = append(records, r)
	}
	return records, nil
}
func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("file unavailable")
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, fmt.Errorf("invalid or oversized file")
	}
	return body, nil
}
