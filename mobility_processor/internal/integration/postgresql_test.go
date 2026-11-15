package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"mobility_processor/internal/api"
	"mobility_processor/internal/database"
	"mobility_processor/internal/gtfs"
	"mobility_processor/internal/gtfs/parser"
	"mobility_processor/internal/lines"
	"mobility_processor/internal/matching"
	"mobility_processor/internal/sptrans"
	"mobility_processor/internal/stops"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func open(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv("PROCESSOR_POSTGRES_TESTS") != "1" {
		t.Skip("isolated PostgreSQL opt-in required")
	}
	cfg, err := database.ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database != "open_transit_test" || cfg.Host != "postgres" {
		t.Fatal("refusing writes outside the dedicated PostgreSQL test database/network")
	}
	db, err := database.OpenFromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = database.CheckSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}
func options(t *testing.T) gtfs.Options {
	return gtfs.Options{Provider: "fixture", SourceName: fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano()), MaxRows: 100}
}
func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	entries, e := os.ReadDir("../gtfs/testdata/schedule")
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range entries {
		b, e := os.ReadFile(filepath.Join("../gtfs/testdata/schedule", f.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, f.Name()), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	return dir
}
func count(t *testing.T, db *sql.DB, table string, feed int64) int {
	t.Helper()
	var n int
	if e := db.QueryRow(`SELECT count(*) FROM `+table+` WHERE feed_version_id=$1`, feed).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func TestPublicRepositoriesAndHTTPContracts(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	lr, sr := lines.NewRepository(db), stops.NewRepository(db)
	base := int(time.Now().UnixNano()%900000000) + 1000000000
	a := sptrans.RawLine{CL: base + 20, LT: fmt.Sprintf("00%d", base), TL: 21, SL: 1, TP: "Primary", TS: "Secondary"}
	b := a
	b.CL = base + 10
	b.SL = 2
	for _, v := range []sptrans.RawLine{a, b, a} {
		if err := lr.UpsertFromSPTrans(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	code := lines.FromSPTrans(a).Code
	for direction, want := range map[int]int{1: a.CL, 2: b.CL} {
		id, err := lr.ResolveSptransID(ctx, code, direction)
		if err != nil || id != want {
			t.Fatalf("SL=%d: id=%d err=%v", direction, id, err)
		}
	}
	if _, e := lr.ResolveSptransID(ctx, code, 0); e != lines.ErrAmbiguousLine {
		t.Fatal("ambiguous code picked an ID", e)
	}
	var id, tl, sl int
	var prefix string
	if e := db.QueryRow(`SELECT id,code_prefix,code_suffix,sptrans_direction FROM public.lines WHERE sptrans_line_id=$1`, a.CL).Scan(&id, &prefix, &tl, &sl); e != nil {
		t.Fatal(e)
	}
	if id == a.CL || prefix != a.LT || tl != 21 || sl != 1 {
		t.Fatal("CL/TL/SL/internal identity lost")
	}
	if e := lr.UpdateOriginCoordinates(ctx, id, 0, 0); e != nil {
		t.Fatal(e)
	}
	got, e := lr.Nearby(ctx, 0, 0, 0.1)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, l := range got {
		if l.SptransID == a.CL && l.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatal("nearby line omitted")
	}
	if _, e := lr.LinesForGeocoding(ctx, true, 10); e != nil {
		t.Fatal(e)
	}
	if _, e := lr.UpdateOriginCoordinatesByOrigin(ctx, "Primary", 0, 0); e != nil {
		t.Fatal(e)
	}
	sa := sptrans.Stop{CP: base + 100, NP: "Fixture A", Lat: 0, Lng: 0}
	sb := sptrans.Stop{CP: base + 101, NP: "Fixture B", Ed: "Address", Lat: 0, Lng: 0.0005}
	saved, linked, e := sr.UpsertStopsForLine(ctx, a.CL, []sptrans.Stop{sa, sb})
	if e != nil || saved != 2 || linked != 2 {
		t.Fatal(saved, linked, e)
	}
	_, linked, e = sr.UpsertStopsForLine(ctx, a.CL, []sptrans.Stop{sa, sb})
	if e != nil || linked != 0 {
		t.Fatal("duplicate links", linked, e)
	}
	var internal int
	db.QueryRow(`SELECT stop_id FROM public.stop_source_references WHERE source_kind='sptrans' AND sptrans_stop_code=$1`, sa.CP).Scan(&internal)
	if internal == sa.CP {
		t.Fatal("CP treated as PK")
	}
	near, e := sr.Nearby(ctx, 0, 0, 0.1, 100, true)
	if e != nil {
		t.Fatal(e)
	}
	found = false
	for _, s := range near {
		if s.SptransID == sa.CP {
			found = s.ID == internal && len(s.Lines) == 1 && s.Lines[0] == a.CL
		}
	}
	if !found {
		t.Fatal("stop contract ID/CL lost")
	}
	before, _ := sr.Count(ctx)
	_, _, e = sr.UpsertStopsForLine(ctx, a.CL, []sptrans.Stop{{CP: base + 102, NP: "Rollback", Lat: 0, Lng: 0}, {CP: 0, NP: "Invalid"}})
	after, _ := sr.Count(ctx)
	if e == nil || before != after {
		t.Fatal("line stops batch was not atomic", e)
	}
	if _, e = sr.UpsertLineStop(ctx, sptrans.LineStop{LineID: base + 999, StopCode: sa.CP}); e == nil {
		t.Fatal("missing CL silently accepted")
	}
	if e = sr.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = lr.Count(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = sr.CountLinks(ctx); e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	api.NewHandler(nil, lr, sr, nil).RegisterRoutes(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", fmt.Sprintf("/lines/resolve?code=%s&direction=1", code), nil))
	var body map[string]any
	json.Unmarshal(response.Body.Bytes(), &body)
	if response.Code != 200 || int(body["sptrans_id"].(float64)) != a.CL {
		t.Fatal(response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("POST", "/stops/nearby", bytes.NewBufferString(`{"lat":0,"lng":0,"radiusKm":0.1,"limit":100,"onlyWithLines":true}`)))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"sptrans_id"`) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/lines", nil))
	if response.Code != 404 {
		t.Fatal("new public endpoint activated")
	}
}
func TestGTFSVersionedIngestionIdempotencyAndPromotion(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	opt := options(t)
	dir := fixture(t)
	im := gtfs.NewImporter(db)
	one, e := im.Run(ctx, dir, opt)
	if e != nil {
		t.Fatal(e)
	}
	again, e := im.Run(ctx, dir, opt)
	if e != nil || !again.Reused || one.FeedVersionID != again.FeedVersionID {
		t.Fatal("reimport not idempotent", again, e)
	}
	for table, want := range map[string]int{"gtfs.agencies": 1, "gtfs.routes": 1, "gtfs.stops": 4, "gtfs.services": 2, "gtfs.calendars": 1, "gtfs.service_exceptions": 2, "gtfs.shapes": 2, "gtfs.shape_points": 4, "gtfs.trips": 4, "gtfs.stop_times": 11, "gtfs.frequencies": 3, "gtfs.journey_patterns": 3, "gtfs.journey_pattern_stops": 8} {
		if n := count(t, db, table, one.FeedVersionID); n != want {
			t.Fatalf("%s %d != %d", table, n, want)
		}
	}
	var runs int
	db.QueryRow(`SELECT count(*) FROM ingest.import_runs WHERE feed_version_id=$1`, one.FeedVersionID).Scan(&runs)
	if runs != 1 {
		t.Fatal("no-op created import run")
	}
	var textID string
	var arrival int
	var direction sql.NullInt64
	var distance sql.NullFloat64
	if e = db.QueryRow(`SELECT s.gtfs_stop_id,st.arrival_service_seconds,t.gtfs_direction_id FROM gtfs.stop_times st JOIN gtfs.stops s ON (s.feed_version_id,s.id)=(st.feed_version_id,st.gtfs_stop_pk) JOIN gtfs.trips t ON (t.feed_version_id,t.id)=(st.feed_version_id,st.trip_id) WHERE st.feed_version_id=$1 AND t.gtfs_trip_id='T-A' AND st.stop_sequence=0`, one.FeedVersionID).Scan(&textID, &arrival, &direction); e != nil {
		t.Fatal(e)
	}
	if textID != "000123" || arrival != 90000 || !direction.Valid || direction.Int64 != 0 {
		t.Fatal("GTFS values coerced")
	}
	if e = db.QueryRow(`SELECT gtfs_direction_id FROM gtfs.trips WHERE feed_version_id=$1 AND gtfs_trip_id='T-U'`, one.FeedVersionID).Scan(&direction); e != nil || direction.Valid {
		t.Fatal("unknown direction defaulted", e)
	}
	if e = db.QueryRow(`SELECT source_shape_distance FROM gtfs.shape_points p JOIN gtfs.shapes s ON(s.feed_version_id,s.id)=(p.feed_version_id,p.shape_id) WHERE p.feed_version_id=$1 AND s.gtfs_shape_id='SH-0' LIMIT 1`, one.FeedVersionID).Scan(&distance); e != nil || distance.Valid {
		t.Fatal("missing shape distance became zero", e)
	}
	var exact string
	if e = db.QueryRow(`SELECT source_shape_distance::text FROM gtfs.shape_points p JOIN gtfs.shapes s ON(s.feed_version_id,s.id)=(p.feed_version_id,p.shape_id) WHERE p.feed_version_id=$1 AND s.gtfs_shape_id='SH-1' AND shape_point_sequence=20`, one.FeedVersionID).Scan(&exact); e != nil || exact != "1.234567890123456789" {
		t.Fatal("source decimal precision lost", exact, e)
	}
	var wkt string
	db.QueryRow(`SELECT ST_AsText(path_geom) FROM gtfs.shapes WHERE feed_version_id=$1 AND gtfs_shape_id='SH-0'`, one.FeedVersionID).Scan(&wkt)
	if wkt != "LINESTRING(-46.63 -23.55,-46.631 -23.551)" {
		t.Fatal(wkt)
	}
	var maps int
	db.QueryRow(`SELECT count(*) FROM public.line_route_mappings WHERE feed_version_id=$1`, one.FeedVersionID).Scan(&maps)
	if maps != 0 {
		t.Fatal("automatic SPTrans mapping")
	}
	if n := count(t, db, "gtfs.active_routes", one.FeedVersionID); n != 0 {
		t.Fatal("unpublished snapshot visible")
	}
	repo := gtfs.NewRepository(db)
	gen, e := repo.Promote(ctx, one.SourceID, one.FeedVersionID, 0, "fixture", "contract test")
	if e != nil || gen != 1 {
		t.Fatal(gen, e)
	}
	if n := count(t, db, "gtfs.active_routes", one.FeedVersionID); n != 1 {
		t.Fatal("published snapshot hidden")
	}
	opt.NormalizerVersion = "fixture-v2"
	two, e := im.Run(ctx, dir, opt)
	if e != nil || two.FeedVersionID == one.FeedVersionID {
		t.Fatal(two, e)
	}
	if n := count(t, db, "gtfs.active_routes", two.FeedVersionID); n != 0 {
		t.Fatal("staging replaced active feed")
	}
	if _, e = repo.Promote(ctx, one.SourceID, two.FeedVersionID, 0, "fixture", "stale"); e == nil {
		t.Fatal("stale generation accepted")
	}
	if _, e = repo.Promote(ctx, one.SourceID, two.FeedVersionID, 1, "fixture", "next"); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.Promote(ctx, one.SourceID, one.FeedVersionID, 2, "fixture", "feed rollback"); e != nil {
		t.Fatal(e)
	}
}
func TestConcurrentFixtureReimportAndStopUpsert(t *testing.T) {
	db := open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := fixture(t)
	opt := options(t)
	results := make(chan gtfs.Result, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, e := gtfs.NewImporter(db).Run(ctx, dir, opt); results <- r; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var id int64
	reused := 0
	for r := range results {
		if id != 0 && id != r.FeedVersionID {
			t.Fatal("concurrent versions diverged")
		}
		id = r.FeedVersionID
		if r.Reused {
			reused++
		}
	}
	if reused != 1 {
		t.Fatal("expected one no-op")
	}
	cp := int(time.Now().UnixNano()%900000000) + 1000000000
	errs = make(chan error, 2)
	sr := stops.NewRepository(db)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- sr.UpsertStop(ctx, sptrans.Stop{CP: cp, NP: "Concurrent fixture", Lat: 0, Lng: 0})
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM public.stop_source_references WHERE sptrans_stop_code=$1 AND source_kind='sptrans'`, cp).Scan(&n)
	if n != 1 {
		t.Fatal("duplicate CP reference")
	}
}
func TestMatchingUsesFeedPatternsAndOnlyPendingSuggestions(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	result, e := gtfs.NewImporter(db).Run(ctx, fixture(t), options(t))
	if e != nil {
		t.Fatal(e)
	}
	cl := int(time.Now().UnixNano()%900000000) + 1000000000
	lr := lines.NewRepository(db)
	if e = lr.UpsertFromSPTrans(ctx, sptrans.RawLine{CL: cl, LT: "8000", TL: 10, SL: 2, TP: "Main", TS: "Return"}); e != nil {
		t.Fatal(e)
	}
	repo := matching.NewRepository(db, result.FeedVersionID)
	candidates, e := repo.LoadCandidates(ctx)
	if e != nil || len(candidates) != 3 {
		t.Fatal(len(candidates), e)
	}
	if _, e = matching.NewMatcher(repo).RunAll(ctx); e != nil {
		t.Fatal(e)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM public.line_route_mappings m JOIN public.lines l ON l.id=m.line_id WHERE m.feed_version_id=$1 AND l.sptrans_line_id=$2 AND m.status='pending'`, result.FeedVersionID, cl).Scan(&n)
	if n != 3 {
		t.Fatal("patterns dropped or automatic confirmation", n)
	}
	_, e = db.Exec(`UPDATE public.line_route_mappings SET status='confirmed' WHERE feed_version_id=$1`, result.FeedVersionID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = matching.NewMatcher(repo).RunAll(ctx); e != nil {
		t.Fatal(e)
	}
	db.QueryRow(`SELECT count(*) FROM public.line_route_mappings WHERE feed_version_id=$1 AND status='pending'`, result.FeedVersionID).Scan(&n)
	if n != 0 {
		t.Fatal("confirmed mapping overwritten")
	}
	// Same pattern PK in another feed must not be accepted.
	if e = repo.SaveMatches(ctx, []matching.Match{{FeedVersionID: result.FeedVersionID + 999, PatternID: candidates[0].PatternID, RouteID: candidates[0].RouteID, LineID: 1, Confidence: 1}}); e == nil {
		t.Fatal("cross-feed match accepted")
	}
}
func TestInvalidFixtureAndCancelledContextDoNotWrite(t *testing.T) {
	db := open(t)
	dir := fixture(t)
	os.WriteFile(filepath.Join(dir, "frequencies.txt"), []byte("trip_id,start_time,end_time,headway_secs\nT-A,25:00:00,26:00:00,0\n"), 0600)
	opt := options(t)
	if _, e := gtfs.NewImporter(db).Run(context.Background(), dir, opt); e == nil {
		t.Fatal("malformed fixture accepted")
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM ingest.feed_sources WHERE provider=$1 AND name=$2`, opt.Provider, opt.SourceName).Scan(&n)
	if n != 0 {
		t.Fatal("invalid input wrote metadata")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := gtfs.NewImporter(db).Run(ctx, fixture(t), options(t)); e == nil {
		t.Fatal("cancelled import succeeded")
	}
}
func TestStagingUpsertsAreIdempotentAndRollbackWithCallerTransaction(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	data, e := parser.Load(fixture(t), 100)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	opt := options(t)
	var source, feed int64
	if e = tx.QueryRow(`INSERT INTO ingest.feed_sources(provider,name,source_kind,profile_version) VALUES($1,$2,'gtfs_schedule',$3) RETURNING id`, opt.Provider, opt.SourceName, gtfs.ProfileVersion).Scan(&source); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(`INSERT INTO ingest.feed_versions(feed_source_id,checksum_sha256,normalizer_version,profile_version) VALUES($1,$2,$3,$4) RETURNING id`, source, data.Checksum, gtfs.NormalizerVersion, gtfs.ProfileVersion).Scan(&feed); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = gtfs.NewRepository(db).WriteSnapshot(ctx, tx, feed, data); e != nil {
			t.Fatal(e)
		}
	}
	for table, want := range map[string]int{"gtfs.shape_points": 4, "gtfs.frequencies": 3, "gtfs.journey_patterns": 3, "gtfs.stop_times": 11} {
		var n int
		if e = tx.QueryRow(`SELECT count(*) FROM `+table+` WHERE feed_version_id=$1`, feed).Scan(&n); e != nil || n != want {
			t.Fatal(table, n, e)
		}
	}
}

func TestDatabaseFailureRollsBackSnapshotAndRecordsFailedRun(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	dir := fixture(t)
	p := filepath.Join(dir, "frequencies.txt")
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, []byte(strings.Replace(string(b), "600,0", "17,0", 1)), 0600); e != nil {
		t.Fatal(e)
	}
	name := fmt.Sprintf("fixture_failure_%d", time.Now().UnixNano())
	if _, e = db.Exec(`CREATE FUNCTION public.` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.headway_seconds=17 THEN RAISE EXCEPTION 'injected fixture failure'; END IF; RETURN NEW; END $$`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`CREATE TRIGGER ` + name + ` BEFORE INSERT ON gtfs.frequencies FOR EACH ROW EXECUTE FUNCTION public.` + name + `() `); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Exec(`DROP TRIGGER IF EXISTS ` + name + ` ON gtfs.frequencies`)
		db.Exec(`DROP FUNCTION IF EXISTS public.` + name + `()`)
	})
	opt := options(t)
	result, e := gtfs.NewImporter(db).Run(ctx, dir, opt)
	if e == nil {
		t.Fatal("injected DB failure was ignored")
	}
	for _, table := range []string{"gtfs.agencies", "gtfs.routes", "gtfs.stops", "gtfs.shapes", "gtfs.trips", "gtfs.stop_times", "gtfs.frequencies"} {
		if n := count(t, db, table, result.FeedVersionID); n != 0 {
			t.Fatal("partial snapshot committed", table, n)
		}
	}
	var state, runState string
	db.QueryRow(`SELECT state FROM ingest.feed_versions WHERE id=$1`, result.FeedVersionID).Scan(&state)
	db.QueryRow(`SELECT state FROM ingest.import_runs WHERE id=$1`, result.RunID).Scan(&runState)
	if state != "failed" || runState != "failed" {
		t.Fatal(state, runState)
	}
	if _, e = gtfs.NewImporter(db).Run(ctx, dir, opt); e == nil {
		t.Fatal("terminal failed feed overwritten")
	}
	if _, e = db.Exec(`DROP TRIGGER ` + name + ` ON gtfs.frequencies`); e != nil {
		t.Fatal(e)
	}
	opt.NormalizerVersion = "fixture-recovery-v2"
	if _, e = gtfs.NewImporter(db).Run(ctx, dir, opt); e != nil {
		t.Fatal(e)
	}
}
func TestCancelledImportLockWaitReleasesSession(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	dir := fixture(t)
	data, e := parser.Load(dir, 100)
	if e != nil {
		t.Fatal(e)
	}
	opt := options(t)
	keyBytes, _ := json.Marshal([]string{"gtfs", opt.Provider, opt.SourceName, data.Checksum, gtfs.NormalizerVersion, gtfs.ProfileVersion})
	blocker, e := db.Conn(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Close()
	if _, e = blocker.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, string(keyBytes)); e != nil {
		t.Fatal(e)
	}
	wait, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	_, e = gtfs.NewImporter(db).Run(wait, dir, opt)
	cancel()
	if e == nil {
		t.Fatal("lock wait ignored deadline")
	}
	if _, e = blocker.ExecContext(ctx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, string(keyBytes)); e != nil {
		t.Fatal(e)
	}
	retry, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, e = gtfs.NewImporter(db).Run(retry, dir, opt); e != nil {
		t.Fatal("cancelled session leaked lock", e)
	}
}

func TestChangedArtifactCreatesNewSnapshotWithoutDeletingPreviousShapes(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	dir := fixture(t)
	opt := options(t)
	im := gtfs.NewImporter(db)
	one, e := im.Run(ctx, dir, opt)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "shapes.txt")
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, []byte(strings.Replace(string(b), "SH-0,-23.551", "SH-0,-23.552", 1)), 0600); e != nil {
		t.Fatal(e)
	}
	two, e := im.Run(ctx, dir, opt)
	if e != nil || two.FeedVersionID == one.FeedVersionID || two.SourceID != one.SourceID {
		t.Fatal(two, e)
	}
	var oldWKT, newWKT string
	if e = db.QueryRow(`SELECT ST_AsText(path_geom) FROM gtfs.shapes WHERE feed_version_id=$1 AND gtfs_shape_id='SH-0'`, one.FeedVersionID).Scan(&oldWKT); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`SELECT ST_AsText(path_geom) FROM gtfs.shapes WHERE feed_version_id=$1 AND gtfs_shape_id='SH-0'`, two.FeedVersionID).Scan(&newWKT); e != nil {
		t.Fatal(e)
	}
	if oldWKT == newWKT || !strings.Contains(oldWKT, "-23.551") || !strings.Contains(newWKT, "-23.552") {
		t.Fatal(oldWKT, newWKT)
	}
}

func TestImporterAndCatalogOperateWithRestrictedRoles(t *testing.T) {
	owner := open(t)
	ctx := context.Background()
	t.Setenv("PGSQL_ROLE", "mobility_importer")
	importerDB := open(t)
	var role string
	if e := importerDB.QueryRow(`SELECT current_user`).Scan(&role); e != nil || role != "mobility_importer" {
		t.Fatal(role, e)
	}
	r, e := gtfs.NewImporter(importerDB).Run(ctx, fixture(t), options(t))
	if e != nil {
		t.Fatal("importer permissions", e)
	}
	if _, e = importerDB.Exec(`SELECT * FROM public.users`); e == nil {
		t.Fatal("importer can read users")
	}
	if _, e = gtfs.NewRepository(importerDB).Promote(ctx, r.SourceID, r.FeedVersionID, 0, "fixture", "restricted role"); e != nil {
		t.Fatal("promotion permissions", e)
	}
	t.Setenv("PGSQL_ROLE", "mobility_processor_catalog")
	catalogDB := open(t)
	cl := int(time.Now().UnixNano()%900000000) + 1000000000
	if e = lines.NewRepository(catalogDB).UpsertFromSPTrans(ctx, sptrans.RawLine{CL: cl, LT: "role-fixture", TL: 10, SL: 1, TP: "Primary", TS: "Secondary"}); e != nil {
		t.Fatal("catalog permissions", e)
	}
	if e = stops.NewRepository(catalogDB).UpsertStop(ctx, sptrans.Stop{CP: cl, NP: "Restricted fixture", Lat: 0, Lng: 0}); e != nil {
		t.Fatal("catalog stop permissions", e)
	}
	if _, e = stops.NewRepository(catalogDB).UpsertLineStop(ctx, sptrans.LineStop{LineID: cl, StopCode: cl}); e != nil {
		t.Fatal("catalog relationship permissions", e)
	}
	if _, e = matching.NewMatcher(matching.NewRepository(catalogDB, r.FeedVersionID)).RunAll(ctx); e != nil {
		t.Fatal("catalog matching permissions", e)
	}
	if _, e = catalogDB.Exec(`UPDATE ingest.feed_publications SET generation=999`); e == nil {
		t.Fatal("catalog can publish feeds")
	}
	if _, e = catalogDB.Exec(`SELECT * FROM public.users`); e == nil {
		t.Fatal("catalog can read users")
	}
	var exists bool
	if e = owner.QueryRow(`SELECT EXISTS(SELECT 1 FROM public.lines WHERE sptrans_line_id=$1)`, cl).Scan(&exists); e != nil || !exists {
		t.Fatal("role write missing", e)
	}
}
