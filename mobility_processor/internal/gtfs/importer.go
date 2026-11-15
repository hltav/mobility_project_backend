package gtfs

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"mobility_processor/internal/gtfs/parser"
	"time"
)

const ProfileVersion = "open-transit.schedule.stage2.v1"
const NormalizerVersion = "go-pg-stage2.v1"

type Options struct {
	Provider, SourceName, NormalizerVersion string
	MaxRows                                 int
}
type Result struct {
	SourceID      int64          `json:"source_id"`
	FeedVersionID int64          `json:"feed_version_id"`
	RunID         int64          `json:"import_run_id,omitempty"`
	Reused        bool           `json:"reused"`
	Counts        map[string]int `json:"rows_by_file"`
}

type Importer struct{ repo *Repository }

func NewImporter(db *sql.DB) *Importer { return &Importer{repo: NewRepository(db)} }

// Run stages and validates a bounded Schedule snapshot. It never publishes or touches public registries/cache.
func (im *Importer) Run(ctx context.Context, dir string, opt Options) (result Result, err error) {
	if opt.Provider == "" || opt.SourceName == "" || opt.MaxRows <= 0 {
		return result, errors.New("provider, source name and positive row budget required")
	}
	if opt.NormalizerVersion == "" {
		opt.NormalizerVersion = NormalizerVersion
	}
	data, err := parser.LoadContext(ctx, dir, opt.MaxRows)
	if err != nil {
		return result, fmt.Errorf("validate GTFS: %w", err)
	}
	result.Counts = data.Counts
	conn, err := im.repo.db.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	lockKey := raw([]string{"gtfs", opt.Provider, opt.SourceName, data.Checksum, opt.NormalizerVersion, ProfileVersion})
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := conn.ExecContext(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lockKey); e != nil {
			// Never put a potentially locked session back in the pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			err = errors.Join(err, fmt.Errorf("release import lock: %w", e))
		}
	}()
	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return result, err
	}

	meta, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer meta.Rollback()
	err = meta.QueryRowContext(ctx, `INSERT INTO ingest.feed_sources(provider,name,source_kind,profile_version,created_at,updated_at) VALUES($1,$2,'gtfs_schedule',$3,now(),now())
 ON CONFLICT(provider,name) DO UPDATE SET name=EXCLUDED.name WHERE ingest.feed_sources.source_kind='gtfs_schedule' RETURNING id`, opt.Provider, opt.SourceName, ProfileVersion).Scan(&result.SourceID)
	if err != nil {
		return result, err
	}
	err = meta.QueryRowContext(ctx, `INSERT INTO ingest.feed_versions(feed_source_id,checksum_sha256,normalizer_version,profile_version,acquisition_at,created_at,updated_at)
 VALUES($1,$2,$3,$4,now(),now(),now()) ON CONFLICT(feed_source_id,checksum_sha256,normalizer_version,profile_version) DO NOTHING RETURNING id`, result.SourceID, data.Checksum, opt.NormalizerVersion, ProfileVersion).Scan(&result.FeedVersionID)
	var state string
	if errors.Is(err, sql.ErrNoRows) {
		err = meta.QueryRowContext(ctx, `SELECT id,state FROM ingest.feed_versions WHERE feed_source_id=$1 AND checksum_sha256=$2 AND normalizer_version=$3 AND profile_version=$4`, result.SourceID, data.Checksum, opt.NormalizerVersion, ProfileVersion).Scan(&result.FeedVersionID, &state)
	}
	if err != nil {
		return result, err
	}
	if state == "ready" {
		result.Reused = true
		return result, meta.Commit()
	}
	if state != "" && state != "staging" {
		return result, errors.New("feed is terminal or under validation; inspect run and use a new normalizer version after correction")
	}
	// A crashed process can leave metadata, but snapshot writes are atomic and cannot leave partial committed entities.
	_, err = meta.ExecContext(ctx, `UPDATE ingest.import_runs SET state='failed',error_code='interrupted_run',finished_at=now() WHERE feed_version_id=$1 AND state='running'`, result.FeedVersionID)
	if err != nil {
		return result, err
	}
	err = meta.QueryRowContext(ctx, `INSERT INTO ingest.import_runs(feed_version_id,state,started_at,created_at,updated_at) VALUES($1,'running',now(),now(),now()) RETURNING id`, result.FeedVersionID).Scan(&result.RunID)
	if err != nil {
		return result, err
	}
	if err = meta.Commit(); err != nil {
		return result, err
	}
	err = im.write(ctx, conn, result, data)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tx, e := conn.BeginTx(cleanup, nil)
		if e == nil {
			defer tx.Rollback()
			_, e = tx.ExecContext(cleanup, `UPDATE ingest.import_runs SET state='failed',error_code='snapshot_write_failed',finished_at=now(),updated_at=now() WHERE id=$1`, result.RunID)
			if e == nil {
				_, e = tx.ExecContext(cleanup, `INSERT INTO ingest.import_rejections(import_run_id,entity,reason_code) VALUES($1,'snapshot','snapshot_write_failed')`, result.RunID)
			}
			if e == nil {
				_, e = tx.ExecContext(cleanup, `UPDATE ingest.feed_versions SET state='failed',updated_at=now() WHERE id=$1 AND state='staging'`, result.FeedVersionID)
			}
			if e == nil {
				e = tx.Commit()
			}
		}
		if e != nil {
			err = errors.Join(err, fmt.Errorf("record failed import: %w", e))
		}
	}
	return result, err
}
func (im *Importer) write(ctx context.Context, conn *sql.Conn, result Result, data *parser.Dataset) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = im.repo.WriteSnapshot(ctx, tx, result.FeedVersionID, data); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE ingest.import_runs SET state='succeeded',row_counts=$2,finished_at=now(),updated_at=now() WHERE id=$1`, result.RunID, raw(data.Counts))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE ingest.feed_versions SET state='validating',counts_manifest=$2,updated_at=now() WHERE id=$1`, result.FeedVersionID, raw(data.Counts))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ingest.validation_results(feed_version_id,import_run_id,validator_version,rule_code,severity,passed,details)
 VALUES($1,$2,$3,'profile.complete','info',true,$4)`, result.FeedVersionID, result.RunID, NormalizerVersion, raw(map[string]any{"profile": ProfileVersion, "counts": data.Counts}))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE ingest.feed_versions SET state='ready',updated_at=now() WHERE id=$1`, result.FeedVersionID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) Promote(ctx context.Context, source, version, generation int64, actor, reason string) (int64, error) {
	var next int64
	err := r.db.QueryRowContext(ctx, `SELECT ingest.promote_feed($1,$2,$3,$4,$5)`, source, version, generation, actor, reason).Scan(&next)
	return next, err
}
