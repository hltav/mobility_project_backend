package integration

import (
	"context"
	"database/sql"
	"fmt"
	"mobility_processor/internal/gtfs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func stage3(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv("PGSQL_DATABASE") != "open_transit_test" {
		t.Skip("Stage 3 isolated database required")
	}
	return open(t)
}
func streamOptions(t *testing.T) gtfs.StreamOptions {
	return gtfs.StreamOptions{Options: options(t), ChunkRows: 2, BatchRows: 2}
}
func TestStage3StreamIdempotencyAndReadyRemainsUnpublished(t *testing.T) {
	db := stage3(t)
	ctx := context.Background()
	dir := fixture(t)
	opt := streamOptions(t)
	one, e := gtfs.NewImporter(db).RunStream(ctx, dir, opt)
	if e != nil {
		t.Fatal(e)
	}
	again, e := gtfs.NewImporter(db).RunStream(ctx, dir, opt)
	if e != nil || !again.Reused || again.FeedVersionID != one.FeedVersionID {
		t.Fatal(again, e)
	}
	if count(t, db, "gtfs.shape_points", one.FeedVersionID) != 4 || count(t, db, "gtfs.frequencies", one.FeedVersionID) != 3 || count(t, db, "gtfs.journey_patterns", one.FeedVersionID) != 3 || count(t, db, "gtfs.active_routes", one.FeedVersionID) != 0 {
		t.Fatal("duplicate or premature publication")
	}
	if _, e = db.Exec(`SELECT ingest.recover_feed($1,'wrong','wrong','wrong','fixture','invalid')`, one.FeedVersionID); e == nil {
		t.Fatal("recovered ready/identity mismatch")
	}
}
func TestStage3BatchFailureCheckpointRecoverySameIdentity(t *testing.T) {
	db := stage3(t)
	ctx := context.Background()
	dir := fixture(t)
	opt := streamOptions(t)
	name := fmt.Sprintf("s3_failure_%d", time.Now().UnixNano())
	if _, e := db.Exec(`CREATE FUNCTION public.` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.shape_point_sequence=20 THEN RAISE EXCEPTION 'fixture batch failure'; END IF; RETURN NEW; END $$`); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`CREATE TRIGGER ` + name + ` BEFORE INSERT ON gtfs.shape_points FOR EACH ROW EXECUTE FUNCTION public.` + name + `() `); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Exec(`DROP TRIGGER IF EXISTS ` + name + ` ON gtfs.shape_points`)
		db.Exec(`DROP FUNCTION IF EXISTS public.` + name + `()`)
	})
	one, e := gtfs.NewImporter(db).RunStream(ctx, dir, opt)
	if e == nil {
		t.Fatal("batch fault ignored")
	}
	if n := count(t, db, "gtfs.shape_points", one.FeedVersionID); n != 2 {
		t.Fatal("atomic batch not checkpointed", n)
	}
	var checkpoint string
	if e = db.QueryRow(`SELECT checkpoint_manifest->>'shapes.txt' FROM ingest.import_runs WHERE id=$1`, one.RunID).Scan(&checkpoint); e != nil || checkpoint != "2" {
		t.Fatal(checkpoint, e)
	}
	if count(t, db, "gtfs.active_routes", one.FeedVersionID) != 0 {
		t.Fatal("partial feed visible")
	}
	if _, e = db.Exec(`SELECT ingest.assert_snapshot_counts(id,counts_manifest->'expected_table_rows') FROM ingest.feed_versions WHERE id=$1`, one.FeedVersionID); e == nil {
		t.Fatal("partial snapshot passed reconciliation")
	}
	if _, e = gtfs.NewRepository(db).Promote(ctx, one.SourceID, one.FeedVersionID, 0, "fixture", "must reject incomplete"); e == nil {
		t.Fatal("partial feed promoted")
	}

	conn, e := db.Conn(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = conn.ExecContext(ctx, `SET ROLE mobility_importer`); e != nil {
		t.Fatal(e)
	}
	if _, e = conn.ExecContext(ctx, `UPDATE ingest.feed_versions SET state='staging' WHERE id=$1`, one.FeedVersionID); e == nil {
		t.Fatal("direct failed transition allowed")
	}
	conn.ExecContext(ctx, `RESET ROLE`)
	conn.Close()
	if _, e = gtfs.NewImporter(db).RunStream(ctx, dir, opt); e == nil {
		t.Fatal("implicit recovery allowed")
	}
	if _, e = db.Exec(`DROP TRIGGER ` + name + ` ON gtfs.shape_points`); e != nil {
		t.Fatal(e)
	}
	opt.Resume = true
	opt.Actor = "fixture"
	opt.RecoveryReason = "fault removed; same checksum and normalizer"
	two, e := gtfs.NewImporter(db).RunStream(ctx, dir, opt)
	if e != nil || two.FeedVersionID != one.FeedVersionID || two.RunID == one.RunID {
		t.Fatal(two, e)
	}
	if count(t, db, "gtfs.shape_points", two.FeedVersionID) != 4 {
		t.Fatal("recovery duplicated points")
	}
	var events int
	db.QueryRow(`SELECT count(*) FROM ingest.feed_recovery_events WHERE feed_version_id=$1`, one.FeedVersionID).Scan(&events)
	if events != 1 {
		t.Fatal("recovery audit absent", events)
	}
}
func TestStage3RecoveryAfterMaterializationDoesNotResetGeometry(t *testing.T) {
	db := stage3(t)
	ctx := context.Background()
	opt := streamOptions(t)
	dir := fixture(t)
	name := fmt.Sprintf("s3_final_%d", time.Now().UnixNano())
	if _, e := db.Exec(`CREATE FUNCTION public.` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='validating' AND NEW.profile_version='open-transit.schedule.stage3.v1' THEN RAISE EXCEPTION 'fixture finalization fault'; END IF; RETURN NEW; END $$`); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`CREATE TRIGGER ` + name + ` BEFORE UPDATE ON ingest.feed_versions FOR EACH ROW EXECUTE FUNCTION public.` + name + `() `); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Exec(`DROP TRIGGER IF EXISTS ` + name + ` ON ingest.feed_versions`)
		db.Exec(`DROP FUNCTION IF EXISTS public.` + name + `()`)
	})
	r, e := gtfs.NewImporter(db).RunStream(ctx, dir, opt)
	if e == nil {
		t.Fatal("finalization fault ignored")
	}
	if count(t, db, "gtfs.active_routes", r.FeedVersionID) != 0 {
		t.Fatal("failed feed visible")
	}
	if _, e = db.Exec(`DROP TRIGGER ` + name + ` ON ingest.feed_versions`); e != nil {
		t.Fatal(e)
	}
	opt.Resume = true
	opt.Actor = "fixture"
	opt.RecoveryReason = "retry finalization"
	done, e := gtfs.NewImporter(db).RunStream(ctx, dir, opt)
	if e != nil || done.FeedVersionID != r.FeedVersionID {
		t.Fatal(done, e)
	}
	var valid int
	db.QueryRow(`SELECT count(*) FROM gtfs.shapes WHERE feed_version_id=$1 AND geometry_status='valid'`, r.FeedVersionID).Scan(&valid)
	if valid != 2 {
		t.Fatal("geometry checkpoint lost")
	}
}
func TestStage3RealSamplePermissionsAndDatasetBlocker(t *testing.T) {
	db := stage3(t)
	ctx := context.Background()
	t.Setenv("PGSQL_ROLE", "mobility_importer")
	restricted := open(t)
	opt := streamOptions(t)
	opt.MaxRows = 20000
	opt.ChunkRows = 128
	opt.BatchRows = 128
	result, e := gtfs.NewImporter(restricted).RunStream(ctx, "../gtfs/testdata/sptrans_real_sample", opt)
	if e != nil {
		t.Fatal(e)
	}
	if count(t, db, "gtfs.shape_points", result.FeedVersionID) < 1824 {
		t.Fatal("real sample missing large shape")
	}
	bad := streamOptions(t)
	bad.MaxRows = 20000
	if _, e = gtfs.NewImporter(restricted).RunStream(ctx, "../gtfs/testdata/sptrans_invalid_single_stop", bad); e == nil || !strings.Contains(e.Error(), "626A-10-1") {
		t.Fatal("real defect not diagnosed", e)
	}
	opt.ExpectedChecksum = "unapproved"
	if _, e = gtfs.NewImporter(restricted).RunStream(ctx, "../gtfs/testdata/sptrans_real_sample", opt); e == nil {
		t.Fatal("unapproved artifact accepted")
	}
}
func TestStage3CatalogRoleCannotStartIngestion(t *testing.T) {
	_ = stage3(t)
	t.Setenv("PGSQL_ROLE", "mobility_processor_catalog")
	db := open(t)
	if _, e := gtfs.NewImporter(db).RunStream(context.Background(), fixture(t), streamOptions(t)); e == nil || !strings.Contains(e.Error(), "permissions") {
		t.Fatal("restricted role started ingestion", e)
	}
}

func TestStage3CrashHelper(t *testing.T) {
	if os.Getenv("MOBILITY_STAGE3_CRASH_HELPER") != "1" {
		t.Skip("subprocess only")
	}
	db := stage3(t)
	opt := gtfs.StreamOptions{Options: gtfs.Options{Provider: "fixture", SourceName: os.Getenv("MOBILITY_STAGE3_CRASH_SOURCE"), MaxRows: 100}, ChunkRows: 2, BatchRows: 2}
	_, e := gtfs.NewImporter(db).RunStream(context.Background(), "../gtfs/testdata/schedule", opt)
	if e != nil {
		t.Fatal(e)
	}
}
func TestStage3ProcessInterruptionResumesCommittedBatches(t *testing.T) {
	db := stage3(t)
	ctx := context.Background()
	name := fmt.Sprintf("s3_sleep_%d", time.Now().UnixNano())
	if _, e := db.Exec(`CREATE FUNCTION public.` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.4); RETURN NEW; END $$`); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`CREATE TRIGGER ` + name + ` BEFORE INSERT ON gtfs.shape_points FOR EACH ROW EXECUTE FUNCTION public.` + name + `() `); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Exec(`DROP TRIGGER IF EXISTS ` + name + ` ON gtfs.shape_points`)
		db.Exec(`DROP FUNCTION IF EXISTS public.` + name + `()`)
	})
	source := fmt.Sprintf("crash-%d", time.Now().UnixNano())
	child := exec.Command(os.Args[0], "-test.run=^TestStage3CrashHelper$")
	child.Env = append(os.Environ(), "MOBILITY_STAGE3_CRASH_HELPER=1", "MOBILITY_STAGE3_CRASH_SOURCE="+source)
	if e := child.Start(); e != nil {
		t.Fatal(e)
	}
	killed := false
	t.Cleanup(func() {
		if !killed {
			child.Process.Kill()
			child.Wait()
		}
	})
	deadline := time.Now().Add(15 * time.Second)
	var feed int64
	var checkpoint int
	for time.Now().Before(deadline) {
		e := db.QueryRow(`SELECT v.id,coalesce((r.checkpoint_manifest->>'shapes.txt')::int,0) FROM ingest.feed_sources s JOIN ingest.feed_versions v ON v.feed_source_id=s.id JOIN ingest.import_runs r ON r.feed_version_id=v.id WHERE s.provider='fixture' AND s.name=$1 ORDER BY r.id DESC LIMIT 1`, source).Scan(&feed, &checkpoint)
		if e == nil && checkpoint >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if checkpoint != 2 {
		t.Fatal("child did not reach committed batch", checkpoint)
	}
	if e := child.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	child.Wait()
	killed = true
	// Remove only this test's delay trigger; the dead process left a real running metadata row.
	if _, e := db.Exec(`DROP TRIGGER ` + name + ` ON gtfs.shape_points`); e != nil {
		t.Fatal(e)
	}
	opt := gtfs.StreamOptions{Options: gtfs.Options{Provider: "fixture", SourceName: source, MaxRows: 100}, ChunkRows: 2, BatchRows: 2, Resume: true, Actor: "fixture", RecoveryReason: "process interrupted"}
	result, e := gtfs.NewImporter(db).RunStream(ctx, "../gtfs/testdata/schedule", opt)
	if e != nil || result.FeedVersionID != feed {
		t.Fatal(result, e)
	}
	var code string
	if e = db.QueryRow(`SELECT error_code FROM ingest.import_runs WHERE feed_version_id=$1 AND id<>$2 ORDER BY id DESC LIMIT 1`, feed, result.RunID).Scan(&code); e != nil || code != "interrupted_run" {
		t.Fatal(code, e)
	}
	if count(t, db, "gtfs.shape_points", feed) != 4 || count(t, db, "gtfs.active_routes", feed) != 0 {
		t.Fatal("interrupted recovery unsafe")
	}
}

func TestStage3ChangedFeedIsolationKeepsPriorPublication(t *testing.T) {
	db := stage3(t)
	ctx := context.Background()
	dir := fixture(t)
	opt := streamOptions(t)
	one, e := gtfs.NewImporter(db).RunStream(ctx, dir, opt)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = gtfs.NewRepository(db).Promote(ctx, one.SourceID, one.FeedVersionID, 0, "fixture", "baseline fixture only"); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "trips.txt")
	bytes, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, []byte(strings.ReplaceAll(string(bytes), "Main", "Next headsign")), 0600); e != nil {
		t.Fatal(e)
	}
	two, e := gtfs.NewImporter(db).RunStream(ctx, dir, opt)
	if e != nil || two.FeedVersionID == one.FeedVersionID {
		t.Fatal(two, e)
	}
	if count(t, db, "gtfs.active_routes", one.FeedVersionID) != 1 || count(t, db, "gtfs.active_routes", two.FeedVersionID) != 0 {
		t.Fatal("new candidate replaced active pointer")
	}
	var first, second int64
	if e = db.QueryRow(`SELECT id FROM gtfs.stops WHERE feed_version_id=$1 AND gtfs_stop_id='000123'`, one.FeedVersionID).Scan(&first); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`SELECT id FROM gtfs.stops WHERE feed_version_id=$1 AND gtfs_stop_id='000123'`, two.FeedVersionID).Scan(&second); e != nil || first == second {
		t.Fatal("snapshot identities merged", e)
	}
}
