<?php

namespace Tests\PostgreSQL;

use Illuminate\Database\QueryException;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class CanonicalSchemaTest extends TestCase
{
    protected function setUp(): void
    {
        parent::setUp();
        if (getenv('POSTGRES_STAGE1_TESTS') !== '1'
            || DB::connection()->getDriverName() !== 'pgsql'
            || ! in_array(DB::connection()->getDatabaseName(), ['open_transit_test', 'open_transit_schema_test'], true)) {
            $this->markTestSkipped('Use the isolated PostgreSQL Compose executor and explicit opt-in.');
        }
        Http::preventStrayRequests();
        DB::beginTransaction();
    }

    protected function tearDown(): void
    {
        while (DB::transactionLevel() > 0) {
            DB::rollBack();
        }
        parent::tearDown();
    }

    private function reject(string $sql, array $bindings, string $state): void
    {
        DB::statement('SAVEPOINT rejection');
        try {
            DB::statement($sql, $bindings);
            $this->fail('Invalid data accepted: '.$sql);
        } catch (QueryException $e) {
            $this->assertSame($state, $e->errorInfo[0]);
        } finally {
            DB::statement('ROLLBACK TO SAVEPOINT rejection');
        }
    }

    private function feed(?int $source = null, string $tag = 'one'): array
    {
        $source ??= DB::table('ingest.feed_sources')->insertGetId([
            'provider' => 'fixture', 'name' => $tag, 'source_kind' => 'gtfs_schedule', 'profile_version' => 'stage1',
        ]);
        $version = DB::table('ingest.feed_versions')->insertGetId([
            'feed_source_id' => $source, 'checksum_sha256' => hash('sha256', $tag),
            'normalizer_version' => 'fixture', 'profile_version' => 'stage1',
        ]);

        return [$source, $version];
    }

    private function schedule(int $feed): array
    {
        $agency = DB::table('gtfs.agencies')->insertGetId(['feed_version_id' => $feed, 'name' => 'Fixture', 'timezone' => 'America/Sao_Paulo']);
        $route = DB::table('gtfs.routes')->insertGetId(['feed_version_id' => $feed, 'agency_id' => $agency, 'gtfs_route_id' => '000A', 'short_name' => 'Test', 'route_type' => 3]);
        $stop = DB::selectOne("INSERT INTO gtfs.stops(feed_version_id,gtfs_stop_id,name,position_geom) VALUES (?,'00001A','Stop',ST_SetSRID(ST_MakePoint(-46.63,-23.55),4326)) RETURNING id", [$feed])->id;
        $service = DB::table('gtfs.services')->insertGetId(['feed_version_id' => $feed, 'gtfs_service_id' => 'WK', 'has_weekly_calendar' => false]);
        DB::table('gtfs.service_exceptions')->insert(['feed_version_id' => $feed, 'service_id' => $service, 'service_date' => '2026-10-25', 'exception_type' => 1]);
        $trip = DB::table('gtfs.trips')->insertGetId(['feed_version_id' => $feed, 'route_id' => $route, 'service_id' => $service, 'gtfs_trip_id' => '000A-trip']);
        DB::table('gtfs.stop_times')->insert(['feed_version_id' => $feed, 'trip_id' => $trip, 'gtfs_stop_pk' => $stop, 'stop_sequence' => 0, 'arrival_service_seconds' => 90000, 'departure_service_seconds' => 90030]);
        DB::table('gtfs.stop_times')->insert(['feed_version_id' => $feed, 'trip_id' => $trip, 'gtfs_stop_pk' => $stop, 'stop_sequence' => 10, 'arrival_service_seconds' => 90100, 'departure_service_seconds' => 90130]);

        return compact('agency', 'route', 'stop', 'service', 'trip');
    }

    private function ready(int $feed): void
    {
        $run = DB::table('ingest.import_runs')->insertGetId(['feed_version_id' => $feed, 'state' => 'succeeded', 'finished_at' => DB::raw('CURRENT_TIMESTAMP')]);
        DB::table('ingest.feed_versions')->where('id', $feed)->update(['state' => 'validating']);
        DB::table('ingest.validation_results')->insert(['feed_version_id' => $feed, 'import_run_id' => $run, 'validator_version' => 'fixture-only', 'rule_code' => 'profile.complete', 'severity' => 'info', 'passed' => true]);
        DB::table('ingest.feed_versions')->where('id', $feed)->update(['state' => 'ready']);
    }

    public function test_connection_extension_migration_history_and_primary_keys(): void
    {
        $this->assertSame('pgsql', DB::connection()->getDriverName());
        $this->assertNotContains('mysql', \PDO::getAvailableDrivers(), 'Isolated executor must not contain a MySQL driver.');
        $this->assertStringStartsWith('3.6.', DB::selectOne('SELECT postgis_lib_version() AS version')->version);
        $this->assertSame(count(glob(database_path('postgresql/*_*.php'))), DB::table('migrations')->count());
        $missing = DB::select("SELECT n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind='r' AND n.nspname IN ('public','gtfs','ingest') AND c.relname<>'spatial_ref_sys' AND NOT EXISTS(SELECT 1 FROM pg_constraint k WHERE k.conrelid=c.oid AND k.contype='p')");
        $this->assertSame([], $missing);
        $this->assertSame([], DB::select("SELECT conname FROM pg_constraint WHERE connamespace IN ('public'::regnamespace,'gtfs'::regnamespace,'ingest'::regnamespace) AND NOT convalidated"));
        $this->assertSame([], DB::select("SELECT c.conname FROM pg_constraint c WHERE c.contype='f' AND c.connamespace IN ('public'::regnamespace,'gtfs'::regnamespace,'ingest'::regnamespace) AND NOT EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid=c.conrelid AND i.indisvalid AND i.indpred IS NULL AND ARRAY(SELECT unnest(i.indkey) LIMIT cardinality(c.conkey))=c.conkey)"));
    }

    public function test_point_round_trip_and_meter_distance_with_gist(): void
    {
        $row = DB::selectOne("INSERT INTO public.bus_stops(name,position_geom) VALUES ('Fixture',ST_SetSRID(ST_MakePoint(-46.63,-23.55),4326)) RETURNING id,latitude,longitude,ST_SRID(position_geom) AS srid,ST_AsGeoJSON(position_geom)::jsonb AS geo");
        $this->assertEquals(-23.55, $row->latitude);
        $this->assertEquals(-46.63, $row->longitude);
        $this->assertSame(4326, $row->srid);
        $this->assertSame([-46.63, -23.55], json_decode($row->geo, true)['coordinates']);
        $near = DB::selectOne('SELECT ST_DWithin(position_geog,ST_SetSRID(ST_MakePoint(-46.63,-23.5505),4326)::geography,100) AS near,ST_DWithin(position_geog,ST_SetSRID(ST_MakePoint(-46.63,-23.56),4326)::geography,100) AS far FROM public.bus_stops WHERE id=?', [$row->id]);
        $this->assertTrue($near->near);
        $this->assertFalse($near->far);
        $idx = DB::selectOne("SELECT indexdef FROM pg_indexes WHERE schemaname='public' AND indexname='bus_stops_geog_gist'");
        $this->assertStringContainsString('USING gist (position_geog)', $idx->indexdef);
    }

    public function test_invalid_geometry_type_srid_range_and_empty_are_rejected(): void
    {
        foreach (["ST_GeomFromText('POINT EMPTY',4326)" => '23514', 'ST_SetSRID(ST_MakePoint(0,91),4326)' => '23514', 'ST_SetSRID(ST_MakePoint(0,0),3857)' => '22023', "ST_GeomFromText('LINESTRING(0 0,1 1)',4326)" => '22023'] as $geom => $code) {
            $this->reject("INSERT INTO public.bus_stops(name,position_geom) VALUES ('bad',{$geom})", [], $code);
        }
    }

    public function test_ordered_linestring_and_original_shape_distances(): void
    {
        [, $feed] = $this->feed();
        $shape = DB::table('gtfs.shapes')->insertGetId(['feed_version_id' => $feed, 'gtfs_shape_id' => '000-shape']);
        foreach ([[20, -23.56, -46.64], [0, -23.55, -46.63]] as [$seq,$lat,$lon]) {
            DB::table('gtfs.shape_points')->insert(['feed_version_id' => $feed, 'shape_id' => $shape, 'shape_point_sequence' => $seq, 'latitude' => $lat, 'longitude' => $lon, 'source_shape_distance' => null]);
        }
        DB::update("UPDATE gtfs.shapes SET path_geom=(SELECT ST_MakeLine(ST_SetSRID(ST_MakePoint(longitude::float8,latitude::float8),4326) ORDER BY shape_point_sequence) FROM gtfs.shape_points WHERE shape_id=?),geometry_status='valid' WHERE id=?", [$shape, $shape]);
        $row = DB::selectOne('SELECT ST_AsText(path_geom) AS wkt,ST_SRID(path_geom) AS srid FROM gtfs.shapes WHERE id=?', [$shape]);
        $this->assertSame('LINESTRING(-46.63 -23.55,-46.64 -23.56)', $row->wkt);
        $this->assertSame(4326, $row->srid);
        $this->assertSame(2, DB::table('gtfs.shape_points')->whereNull('source_shape_distance')->count());
    }

    public function test_source_version_identity_service_seconds_and_repeated_stops(): void
    {
        [$source,$a] = $this->feed();
        [, $b] = $this->feed($source, 'two');
        $one = $this->schedule($a);
        $two = $this->schedule($b);
        $this->assertSame(2, DB::table('gtfs.routes')->where('gtfs_route_id', '000A')->count());
        $this->assertSame(2, DB::table('gtfs.stop_times')->where('trip_id', $one['trip'])->where('gtfs_stop_pk', $one['stop'])->count());
        $this->assertSame(90000, DB::table('gtfs.stop_times')->where('trip_id', $one['trip'])->min('arrival_service_seconds'));
        $this->assertNull(DB::table('gtfs.trips')->where('id', $one['trip'])->value('gtfs_direction_id'));
        $this->reject("INSERT INTO gtfs.trips(feed_version_id,route_id,service_id,gtfs_trip_id) VALUES (?,?,?,'cross')", [$a, $two['route'], $one['service']], '23503');
        $this->reject("INSERT INTO gtfs.routes(feed_version_id,agency_id,gtfs_route_id,short_name,route_type) VALUES (?,?, '000A','duplicate',3)", [$a, $one['agency']], '23505');
        $this->reject('UPDATE gtfs.stop_times SET arrival_service_seconds=-1 WHERE trip_id=?', [$one['trip']], '23514');
    }

    public function test_pattern_belongs_to_route_and_multiple_patterns_are_supported(): void
    {
        [, $feed] = $this->feed();
        $s = $this->schedule($feed);
        $other = DB::table('gtfs.routes')->insertGetId(['feed_version_id' => $feed, 'agency_id' => $s['agency'], 'gtfs_route_id' => 'other', 'long_name' => 'Other', 'route_type' => 3]);
        $patterns = [];
        foreach (['a', 'b'] as $tag) {
            $patterns[] = DB::table('gtfs.journey_patterns')->insertGetId(['feed_version_id' => $feed, 'route_id' => $s['route'], 'gtfs_direction_id' => 0, 'fingerprint_sha256' => hash('sha256', $tag)]);
        }
        $this->assertCount(2, $patterns);
        $this->reject('UPDATE gtfs.trips SET route_id=?,pattern_id=? WHERE id=?', [$other, $patterns[0], $s['trip']], '23503');
    }

    public function test_numeric_coincidence_never_creates_mapping_and_directions_remain_distinct(): void
    {
        [, $feed] = $this->feed();
        $s = $this->schedule($feed);
        DB::table('public.lines')->insert(['provider' => 'sptrans', 'sptrans_line_id' => 123, 'display_code' => '8000-10', 'display_name' => 'Out', 'sptrans_direction' => 1]);
        DB::table('public.lines')->insert(['provider' => 'sptrans', 'sptrans_line_id' => 456, 'display_code' => '8000-10', 'display_name' => 'Back', 'sptrans_direction' => 2]);
        $this->assertSame(0, DB::table('public.line_route_mappings')->count());
        $this->assertSame(0, DB::table('public.stop_source_references')->count());
        $this->reject("INSERT INTO public.lines(sptrans_line_id,display_code,display_name,sptrans_direction) VALUES (789,'x','bad',0)", [], '23514');
        $this->reject("INSERT INTO public.lines(sptrans_line_id,display_code,display_name) VALUES (123,'x','dup')", [], '23505');
    }

    public function test_atomic_publication_visibility_generation_and_immutability(): void
    {
        [$source,$feed] = $this->feed();
        $s = $this->schedule($feed);
        $this->assertSame(0, DB::table('gtfs.active_routes')->count());
        $this->reject("SELECT ingest.promote_feed(?,?,0,'test','not ready')", [$source, $feed], '23514');
        $this->ready($feed);
        DB::statement('SET LOCAL ROLE mobility_importer');
        $row = DB::selectOne("SELECT ingest.promote_feed(?,?,0,'fixture','test') AS generation", [$source, $feed]);
        $this->assertSame(1, $row->generation);
        $this->assertSame(1, DB::selectOne("SELECT ingest.promote_feed(?,?,1,'fixture','no-op') AS generation", [$source, $feed])->generation);
        $this->reject("SELECT ingest.promote_feed(?,?,0,'fixture','stale')", [$source, $feed], '40001');
        $this->reject('UPDATE gtfs.routes SET long_name=? WHERE id=?', ['changed', $s['route']], '55000');
        $this->reject('UPDATE ingest.feed_publications SET generation=99 WHERE feed_source_id=?', [$source], '42501');
        DB::statement('RESET ROLE');
        $this->assertSame(1, DB::table('gtfs.active_routes')->count());
        $this->assertSame(1, DB::table('ingest.feed_promotion_events')->count());
        DB::statement('SET LOCAL ROLE mobility_app');
        $this->assertSame(1, DB::table('gtfs.active_routes')->count());
        $this->reject('SELECT * FROM gtfs.routes', [], '42501');
    }

    public function test_ready_requires_attestation_and_frequency_intervals_do_not_overlap(): void
    {
        [, $feed] = $this->feed();
        $s = $this->schedule($feed);
        $this->reject("UPDATE ingest.feed_versions SET state='ready',validated_at=now() WHERE id=?", [$feed], '55000');
        foreach ([[90000, 91000], [90500, 92000]] as [$start,$end]) {
            DB::table('gtfs.frequencies')->insert(['feed_version_id' => $feed, 'trip_id' => $s['trip'], 'start_service_seconds' => $start, 'end_service_seconds' => $end, 'headway_seconds' => 600, 'exact_times' => 0]);
        }
        $run = DB::table('ingest.import_runs')->insertGetId(['feed_version_id' => $feed, 'state' => 'succeeded']);
        DB::table('ingest.feed_versions')->where('id', $feed)->update(['state' => 'validating']);
        $this->reject("UPDATE ingest.feed_versions SET state='ready' WHERE id=?", [$feed], '23514');
        DB::table('ingest.validation_results')->insert(['feed_version_id' => $feed, 'import_run_id' => $run, 'validator_version' => 'fixture', 'rule_code' => 'profile.complete', 'severity' => 'info', 'passed' => true]);
        $this->reject("UPDATE ingest.feed_versions SET state='ready' WHERE id=?", [$feed], '23514');
    }

    public function test_calendar_dates_only_weekly_calendars_and_exception_uniqueness(): void
    {
        [, $feed] = $this->feed();
        $s = $this->schedule($feed);
        $this->reject("INSERT INTO gtfs.service_exceptions(feed_version_id,service_id,service_date,exception_type) VALUES (?,?,'2026-10-25',1)", [$feed, $s['service']], '23505');
        DB::table('gtfs.services')->where('id', $s['service'])->update(['has_weekly_calendar' => true]);
        $this->reject("INSERT INTO gtfs.calendars VALUES (?,?,true,true,true,true,true,false,false,'2026-12-01','2026-01-01',null)", [$feed, $s['service']], '23514');
        DB::table('gtfs.calendars')->insert(['feed_version_id' => $feed, 'service_id' => $s['service'], 'monday' => true, 'tuesday' => true, 'wednesday' => true, 'thursday' => true, 'friday' => true, 'saturday' => false, 'sunday' => false, 'start_date' => '2026-01-01', 'end_date' => '2026-12-31']);
        $this->ready($feed);
        $this->assertSame('ready', DB::table('ingest.feed_versions')->where('id', $feed)->value('state'));
    }

    public function test_source_references_reject_fabricated_gtfs_sptrans_identity(): void
    {
        [, $feed] = $this->feed();
        $s = $this->schedule($feed);
        $stop = DB::table('public.bus_stops')->insertGetId(['name' => 'Canonical stop']);
        $this->reject("INSERT INTO public.stop_source_references(stop_id,provider,source_kind,feed_version_id,gtfs_stop_pk,external_stop_id,method) VALUES (?,'fixture','gtfs',?,?,'wrong','manual')", [$stop, $feed, $s['stop']], '23503');
        $this->reject("INSERT INTO public.stop_source_references(stop_id,provider,source_kind,sptrans_stop_code,external_stop_id,method) VALUES (?,'sptrans','sptrans',123,'00001A','manual')", [$stop], '23514');
        DB::table('public.stop_source_references')->insert(['stop_id' => $stop, 'provider' => 'fixture', 'source_kind' => 'gtfs', 'feed_version_id' => $feed, 'gtfs_stop_pk' => $s['stop'], 'external_stop_id' => '00001A', 'method' => 'manual']);
        $this->assertFalse(DB::table('public.bus_stops')->where('id', $stop)->value('legacy_visible'));
    }

    public function test_feed_is_deduplicated_by_source_checksum_and_normalizer(): void
    {
        [$source,$feed] = $this->feed();
        $this->reject("INSERT INTO ingest.feed_versions(feed_source_id,checksum_sha256,normalizer_version,profile_version) VALUES (?,?,'fixture','stage1')", [$source, hash('sha256', 'one')], '23505');
        $this->reject('UPDATE ingest.feed_versions SET checksum_sha256=? WHERE id=?', [hash('sha256', 'changed'), $feed], '55000');
        $this->reject("INSERT INTO ingest.feed_versions(feed_source_id,checksum_sha256,normalizer_version,profile_version,state) VALUES (?,?,'other','stage1','ready')", [$source, hash('sha256', 'two')], '55000');
    }

    public function test_service_time_monotonicity_blocks_ready_promotion(): void
    {
        [, $feed] = $this->feed();
        $s = $this->schedule($feed);
        DB::table('gtfs.stop_times')->where('trip_id', $s['trip'])->where('stop_sequence', 10)->update(['arrival_service_seconds' => 80000, 'departure_service_seconds' => 80030]);
        DB::table('ingest.import_runs')->insert(['feed_version_id' => $feed, 'state' => 'succeeded']);
        DB::table('ingest.feed_versions')->where('id',$feed)->update(['state' => 'validating']);
        DB::table('ingest.validation_results')->insert(['feed_version_id' => $feed, 'validator_version' => 'fixture', 'rule_code' => 'profile.complete', 'severity' => 'info', 'passed' => true]);
        $this->reject("UPDATE ingest.feed_versions SET state='ready' WHERE id=?",[$feed],'23514');
    }
}
