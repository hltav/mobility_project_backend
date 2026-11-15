package gtfs

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"mobility_processor/internal/gtfs/parser"
	"sort"
	"time"
)

const StreamProfileVersion = "open-transit.schedule.stage3.v1"
const StreamNormalizerVersion = "go-pg-stage3.v1"

type StreamOptions struct {
	Options
	ChunkRows, BatchRows  int
	TempDir               string
	Resume                bool
	Actor, RecoveryReason string
	ExpectedChecksum      string
}

func (im *Importer) RunStream(ctx context.Context, dir string, opt StreamOptions) (Result, error) {
	if opt.Provider == "" || opt.SourceName == "" || opt.BatchRows < 1 || opt.BatchRows > 2000 {
		return Result{}, fmt.Errorf("provider/source and batch size 1..2000 required")
	}
	if err := im.repo.RequireStreamSchema(ctx); err != nil {
		return Result{}, err
	}
	if opt.NormalizerVersion == "" {
		opt.NormalizerVersion = StreamNormalizerVersion
	}
	if opt.Resume && (opt.Actor == "" || opt.RecoveryReason == "") {
		return Result{}, fmt.Errorf("explicit resume actor and reason required")
	}
	plan, err := parser.Prepare(ctx, dir, parser.PrepareOptions{MaxRows: opt.MaxRows, ChunkRows: opt.ChunkRows, TempDir: opt.TempDir})
	if err != nil {
		return Result{}, err
	}
	defer plan.Close()
	if opt.ExpectedChecksum != "" && plan.Checksum != opt.ExpectedChecksum {
		return Result{}, fmt.Errorf("source checksum differs from approved artifact")
	}
	return im.runPrepared(ctx, dir, plan, opt)
}
func (im *Importer) runPrepared(ctx context.Context, dir string, plan *parser.PreparedFeed, opt StreamOptions) (result Result, err error) {
	result.Counts = plan.Stats.Counts
	conn, e := im.repo.db.Conn(ctx)
	if e != nil {
		return result, e
	}
	defer conn.Close()
	keys := []string{}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for i := len(keys) - 1; i >= 0; i-- {
			if _, e := conn.ExecContext(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, keys[i]); e != nil {
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
				err = errors.Join(err, fmt.Errorf("release stream lock: %w", e))
				break
			}
		}
	}()
	lock := func(key string) error {
		keys = append(keys, key)
		_, e := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, key)
		return e
	}
	if e = lock(raw([]string{"gtfs", opt.Provider, opt.SourceName, plan.Checksum, opt.NormalizerVersion, StreamProfileVersion})); e != nil {
		return result, e
	}
	tx, e := conn.BeginTx(ctx, nil)
	if e != nil {
		return result, e
	}
	defer tx.Rollback()
	e = tx.QueryRowContext(ctx, `INSERT INTO ingest.feed_sources(provider,name,source_kind,profile_version,created_at,updated_at) VALUES($1,$2,'gtfs_schedule',$3,now(),now())
 ON CONFLICT(provider,name) DO UPDATE SET profile_version=EXCLUDED.profile_version,updated_at=now() WHERE ingest.feed_sources.source_kind='gtfs_schedule' RETURNING id`, opt.Provider, opt.SourceName, StreamProfileVersion).Scan(&result.SourceID)
	if e != nil {
		return result, e
	}
	e = tx.QueryRowContext(ctx, `INSERT INTO ingest.feed_versions(feed_source_id,checksum_sha256,normalizer_version,profile_version,counts_manifest,acquisition_at,created_at,updated_at)
 VALUES($1,$2,$3,$4,$5,now(),now(),now()) ON CONFLICT(feed_source_id,checksum_sha256,normalizer_version,profile_version) DO NOTHING RETURNING id`, result.SourceID, plan.Checksum, opt.NormalizerVersion, StreamProfileVersion, raw(plan.Stats)).Scan(&result.FeedVersionID)
	created := e == nil
	if errors.Is(e, sql.ErrNoRows) {
		e = tx.QueryRowContext(ctx, `SELECT id FROM ingest.feed_versions WHERE feed_source_id=$1 AND checksum_sha256=$2 AND normalizer_version=$3 AND profile_version=$4`, result.SourceID, plan.Checksum, opt.NormalizerVersion, StreamProfileVersion).Scan(&result.FeedVersionID)
	}
	if e != nil {
		return result, e
	}
	if e = tx.Commit(); e != nil {
		return result, e
	}
	if e = lock(fmt.Sprintf("ingest:feed:%d", result.FeedVersionID)); e != nil {
		return result, e
	}
	var state string
	if e = conn.QueryRowContext(ctx, `SELECT state FROM ingest.feed_versions WHERE id=$1`, result.FeedVersionID).Scan(&state); e != nil {
		return result, e
	}
	if state == "ready" {
		result.Reused = true
		return result, nil
	}
	if !created && !opt.Resume {
		return result, fmt.Errorf("existing incomplete/failed feed requires --resume with actor and reason")
	}
	checkpoint := map[string]int{}
	tx, e = conn.BeginTx(ctx, nil)
	if e != nil {
		return result, e
	}
	defer tx.Rollback()
	if !created {
		var value []byte
		e = tx.QueryRowContext(ctx, `SELECT ingest.recover_feed($1,$2,$3,$4,$5,$6)`, result.FeedVersionID, plan.Checksum, opt.NormalizerVersion, StreamProfileVersion, opt.Actor, opt.RecoveryReason).Scan(&value)
		if e != nil {
			return result, e
		}
		if e = json.Unmarshal(value, &checkpoint); e != nil {
			return result, e
		}
	}
	e = tx.QueryRowContext(ctx, `INSERT INTO ingest.import_runs(feed_version_id,state,checkpoint_manifest,created_at,updated_at) VALUES($1,'running',$2,now(),now()) RETURNING id`, result.FeedVersionID, raw(checkpoint)).Scan(&result.RunID)
	if e != nil {
		return result, e
	}
	if e = tx.Commit(); e != nil {
		return result, e
	}
	e = im.writeStream(ctx, conn, dir, plan, result, opt, checkpoint)
	if e != nil {
		err = e
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		failureTx, recordErr := im.repo.db.BeginTx(cleanup, nil)
		if recordErr == nil {
			defer failureTx.Rollback()
			_, recordErr = failureTx.ExecContext(cleanup, `UPDATE ingest.import_runs SET state='failed',error_code='stream_write_failed',finished_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 AND state='running'`, result.RunID)
			if recordErr == nil {
				_, recordErr = failureTx.ExecContext(cleanup, `INSERT INTO ingest.import_rejections(import_run_id,entity,reason_code) VALUES($1,'stream','stream_write_failed')`, result.RunID)
			}
			if recordErr == nil {
				_, recordErr = failureTx.ExecContext(cleanup, `UPDATE ingest.feed_versions SET state='failed',updated_at=clock_timestamp() WHERE id=$1 AND state IN ('staging','validating')`, result.FeedVersionID)
			}
			if recordErr == nil {
				recordErr = failureTx.Commit()
			}
		}
		if recordErr != nil {
			err = errors.Join(err, fmt.Errorf("record streaming failure: %w", recordErr))
		}
		return result, err
	}
	return result, nil
}
func (im *Importer) writeStream(ctx context.Context, conn *sql.Conn, dir string, plan *parser.PreparedFeed, result Result, opt StreamOptions, checkpoint map[string]int) error {
	commit := func(key string, value int, write func(*sql.Tx) error) error {
		tx, e := conn.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = write(tx); e != nil {
			return e
		}
		next := make(map[string]int, len(checkpoint)+1)
		for k, v := range checkpoint {
			next[k] = v
		}
		next[key] = value
		_, e = tx.ExecContext(ctx, `UPDATE ingest.import_runs SET checkpoint_manifest=$2,updated_at=clock_timestamp() WHERE id=$1 AND state='running'`, result.RunID, raw(next))
		if e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		checkpoint[key] = value
		return nil
	}
	var ids *SnapshotIDs
	if checkpoint["metadata"] == 0 {
		e := commit("metadata", 1, func(tx *sql.Tx) error {
			var e error
			ids, e = im.repo.WriteMetadata(ctx, tx, result.FeedVersionID, plan.Metadata, plan.ShapeIDs)
			return e
		})
		if e != nil {
			return e
		}
	} else {
		var e error
		ids, e = im.repo.LoadIDs(ctx, conn, result.FeedVersionID)
		if e != nil {
			return e
		}
		if len(ids.Stops) != len(plan.Metadata.Stops) || len(ids.Routes) != len(plan.Metadata.Routes) || len(ids.Trips) != len(plan.Metadata.Trips) || len(ids.Shapes) != len(plan.ShapeIDs) {
			return fmt.Errorf("metadata checkpoint does not match persisted identities")
		}
	}
	for _, file := range []string{"shapes.txt", "stop_times.txt", "frequencies.txt"} {
		var persisted int
		if e := conn.QueryRowContext(ctx, "SELECT count(*) FROM gtfs."+map[string]string{"shapes.txt": "shape_points", "stop_times.txt": "stop_times", "frequencies.txt": "frequencies"}[file]+" WHERE feed_version_id=$1", result.FeedVersionID).Scan(&persisted); e != nil {
			return e
		}
		if persisted != checkpoint[file] {
			return fmt.Errorf("%s checkpoint/count mismatch", file)
		}
		seen := 0
		buffer := make([]parser.Raw, 0, opt.BatchRows)
		flush := func() error {
			if len(buffer) == 0 {
				return nil
			}
			e := commit(file, seen, func(tx *sql.Tx) error { return im.repo.WriteRows(ctx, tx, result.FeedVersionID, file, buffer, ids) })
			if e == nil {
				clear(buffer)
				buffer = buffer[:0]
			}
			return e
		}
		e := plan.Rows(ctx, file, func(r parser.Raw) error {
			seen++
			if seen <= checkpoint[file] {
				return nil
			}
			buffer = append(buffer, r)
			if len(buffer) == opt.BatchRows {
				return flush()
			}
			return nil
		})
		if e != nil {
			return e
		}
		if e = flush(); e != nil {
			return e
		}
		if seen != plan.Stats.Counts[file] {
			return fmt.Errorf("prepared stream count changed")
		}
	}
	// One bounded trip group per transaction; shapes are materialized in bounded groups of 100.
	groups := 0
	e := plan.Groups(ctx, "stop_times.txt", func(d *parser.Dataset) error {
		groups++
		if groups <= checkpoint["patterns"] {
			return nil
		}
		trip := plan.Index.Trips[d.StopTimes[0].GTFSTripID]
		return commit("patterns", groups, func(tx *sql.Tx) error {
			return im.repo.WritePattern(ctx, tx, result.FeedVersionID, trip, d.StopTimes, ids)
		})
	})
	if e != nil {
		return e
	}
	keys := make([]string, 0, len(plan.ShapeIDs))
	for key := range plan.ShapeIDs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for start := checkpoint["shape_geometries"]; start < len(keys); {
		end := min(start+100, len(keys))
		if e = commit("shape_geometries", end, func(tx *sql.Tx) error {
			return im.repo.MaterializeShapes(ctx, tx, result.FeedVersionID, keys[start:end], ids)
		}); e != nil {
			return e
		}
		start = end
	}
	current, e := parser.Checksum(ctx, dir)
	if e != nil {
		return e
	}
	if current != plan.Checksum {
		return fmt.Errorf("source changed after preflight; snapshot remains unpublished")
	}
	tx, e := conn.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `SELECT ingest.assert_snapshot_counts($1,$2)`, result.FeedVersionID, raw(plan.Stats.Expected))
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE ingest.import_runs SET state='succeeded',finished_at=clock_timestamp(),row_counts=$2,updated_at=clock_timestamp() WHERE id=$1`, result.RunID, raw(plan.Stats.Counts))
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE ingest.feed_versions SET state='validating',updated_at=clock_timestamp() WHERE id=$1`, result.FeedVersionID)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO ingest.validation_results(feed_version_id,import_run_id,validator_version,rule_code,severity,passed,details) VALUES($1,$2,$3,'profile.complete','info',true,$4)`, result.FeedVersionID, result.RunID, opt.NormalizerVersion, raw(map[string]any{"profile": StreamProfileVersion, "expected": plan.Stats.Expected, "checkpoint": checkpoint}))
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE ingest.feed_versions SET state='ready',updated_at=clock_timestamp() WHERE id=$1`, result.FeedVersionID)
	if e != nil {
		return e
	}
	return tx.Commit()
}

// Fail before metadata/data writes if the explicit Stage 3 migration or runtime grants are missing.
func (r *Repository) RequireStreamSchema(ctx context.Context) error {
	var ready bool
	err := r.db.QueryRowContext(ctx, `SELECT coalesce(has_function_privilege(current_user,to_regprocedure('ingest.recover_feed(bigint,text,text,text,text,text)'),'EXECUTE'),false)
 AND coalesce(has_function_privilege(current_user,to_regprocedure('ingest.assert_snapshot_counts(bigint,jsonb)'),'EXECUTE'),false)`).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("Stage 3 migration/recovery permissions missing; provision explicitly before importing")
	}
	tables := []string{"gtfs.agencies", "gtfs.routes", "gtfs.stops", "gtfs.services", "gtfs.calendars", "gtfs.service_exceptions", "gtfs.shapes", "gtfs.shape_points", "gtfs.journey_patterns", "gtfs.journey_pattern_stops", "gtfs.trips", "gtfs.stop_times", "gtfs.frequencies", "ingest.feed_sources", "ingest.feed_versions", "ingest.import_runs"}
	for _, table := range tables {
		if err = r.db.QueryRowContext(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT') AND has_table_privilege(current_user,$1,'INSERT') AND has_table_privilege(current_user,$1,'UPDATE')`, table).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return fmt.Errorf("required streaming read/write permissions missing on %s", table)
		}
	}
	return nil
}
