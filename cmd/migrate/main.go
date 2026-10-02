package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/wchiway/ocs_bank/internal/config"
	"github.com/wchiway/ocs_bank/internal/legacy"
	"github.com/wchiway/ocs_bank/internal/provider"
	"github.com/wchiway/ocs_bank/internal/store"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	source := flags.String("source", "", "offline SQLite backup to inspect (required)")
	path := flags.String("config", "config.json", "target service configuration (apply only)")
	apply := flags.Bool("apply", false, "insert eligible records into target database")
	reviewed := flags.Bool("reviewed", false, "confirm eligible source answers were manually reviewed")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *source == "" {
		return errors.New("-source is required; use an offline backup, never the live database")
	}
	if *apply && !*reviewed {
		return errors.New("-apply requires -reviewed; inspect and review the source first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	report, items, err := legacy.Scan(ctx, *source)
	if err != nil {
		return err
	}
	if *apply {
		c, err := config.Load(*path)
		if err != nil {
			return err
		}
		a, _ := filepath.Abs(*source)
		b, _ := filepath.Abs(c.Database)
		sourceInfo, err := os.Stat(a)
		if err != nil {
			return err
		}
		targetInfo, statErr := os.Stat(b)
		if a == b || (statErr == nil && os.SameFile(sourceInfo, targetInfo)) {
			return errors.New("source and target databases must differ")
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		ai, err := provider.New(c)
		if err != nil {
			return err
		}
		defer ai.Close() // Configuration/namespace only; no model request.
		db, err := store.Open(ctx, c.Database, time.Duration(c.CacheTTLHours)*time.Hour)
		if err != nil {
			return err
		}
		defer db.Close()
		report.Imported, err = db.Import(ctx, ai.Namespace(), items)
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
