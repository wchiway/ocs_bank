// Command maintain operates only on an explicitly selected SQLite database.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/wchiway/ocs_bank/internal/store"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("ocs-maintain", flag.ContinueOnError)
	path := flags.String("database", "", "existing SQLite database; restore uses this as the backup source")
	action := flags.String("action", "stats", "stats, prune, backup or restore")
	destination := flags.String("output", "", "new destination path for backup/restore")
	apply := flags.Bool("apply", false, "apply one cleanup batch (default is preview)")
	model := flags.String("model", "", "limit cleanup to this exact model")
	days := flags.Int("retain-call-days", 0, "also remove older call records; 0 keeps all history")
	batch := flags.Int("batch-size", 1000, "maximum rows removed per table in one invocation (1-10000)")
	timeout := flags.Duration("timeout", 30*time.Second, "operation deadline (up to 1h)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *path == "" {
		return errors.New("-database is required; positional arguments are not accepted")
	}
	if *timeout <= 0 || *timeout > time.Hour || *days < 0 || *days > 36500 || *batch < 1 || *batch > 10000 {
		return errors.New("maintenance limits out of range")
	}
	if *action != "stats" && *action != "prune" && *action != "backup" && *action != "restore" {
		return errors.New("unknown action")
	}
	if *action != "prune" && (*apply || *model != "" || *days != 0) {
		return errors.New("cleanup flags require -action prune")
	}
	if (*action == "backup" || *action == "restore") != (*destination != "") {
		return errors.New("-output is required only for backup/restore")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if *action == "restore" {
		if err := store.Restore(ctx, *path, *destination); err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(map[string]bool{"restored": true})
	}
	s, err := store.OpenExisting(ctx, *path, *action == "prune" && *apply)
	if err != nil {
		return err
	}
	defer s.Close()
	var result any
	switch *action {
	case "stats":
		result, err = s.Statistics(ctx)
	case "prune":
		opts := store.PruneOptions{Model: *model, BatchSize: *batch, Apply: *apply}
		if *days > 0 {
			opts.CallsBefore = time.Now().Add(-time.Duration(*days) * 24 * time.Hour)
		}
		result, err = s.Prune(ctx, opts)
	case "backup":
		err = s.Backup(ctx, *destination)
		result = map[string]bool{"backed_up": true}
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
