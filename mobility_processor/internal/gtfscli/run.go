// Package gtfscli exposes explicit preparation/staging commands; publication is always separate.
package gtfscli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mobility_processor/internal/database"
	"mobility_processor/internal/gtfs"
	"mobility_processor/internal/gtfs/parser"
	"time"
)

func Run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("importgtfs", flag.ContinueOnError)
	flags.SetOutput(out)
	dir := flags.String("data-dir", "", "explicit immutable local GTFS directory")
	provider := flags.String("provider", "", "source provider namespace")
	source := flags.String("source", "", "stable source name")
	confirm := flags.Bool("confirm-write", false, "stage validated batches in PostgreSQL; never publish")
	budget := flags.Int("max-rows", 10000, "total row budget; raise explicitly after preflight")
	chunk := flags.Int("chunk-rows", 4096, "external sort chunk rows")
	batch := flags.Int("batch-rows", 1000, "SQL batch rows, 1..2000")
	temp := flags.String("temp-dir", "", "parent of private temporary spool directory")
	timeout := flags.Duration("timeout", 2*time.Hour, "overall preflight/import deadline")
	normalizer := flags.String("normalizer-version", gtfs.StreamNormalizerVersion, "transformation identity (never bump solely to retry)")
	expected := flags.String("expected-checksum", "", "approved source checksum; mismatch refuses writes")
	resume := flags.Bool("resume", false, "explicitly recover the same incomplete/failed feed with committed checkpoints")
	actor := flags.String("actor", "", "recovery actor reference")
	reason := flags.String("recovery-reason", "", "audited recovery reason")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if flags.NArg() != 0 || *dir == "" || *provider == "" || *source == "" {
		return fmt.Errorf("explicit --data-dir, --provider and --source required")
	}
	if *timeout <= 0 {
		return fmt.Errorf("positive timeout required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if !*confirm {
		p, e := parser.Prepare(ctx, *dir, parser.PrepareOptions{MaxRows: *budget, ChunkRows: *chunk, TempDir: *temp, CollectIssues: true})
		if e != nil {
			return e
		}
		defer p.Close()
		if *expected != "" && p.Checksum != *expected {
			return fmt.Errorf("source checksum differs from approved artifact")
		}
		if e = json.NewEncoder(out).Encode(map[string]any{"dry_run": true, "valid": len(p.Issues) == 0, "profile": gtfs.StreamProfileVersion, "plan": p}); e != nil {
			return e
		}
		if len(p.Issues) > 0 {
			return fmt.Errorf("GTFS preflight rejected %d data issue(s); no database opened", len(p.Issues))
		}
		return nil
	}
	db, e := database.OpenFromEnv(ctx)
	if e != nil {
		return e
	}
	defer db.Close()
	if e = database.CheckSchema(ctx, db); e != nil {
		return e
	}
	result, e := gtfs.NewImporter(db).RunStream(ctx, *dir, gtfs.StreamOptions{Options: gtfs.Options{Provider: *provider, SourceName: *source, NormalizerVersion: *normalizer, MaxRows: *budget}, ChunkRows: *chunk, BatchRows: *batch, TempDir: *temp, Resume: *resume, Actor: *actor, RecoveryReason: *reason, ExpectedChecksum: *expected})
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(result)
}
