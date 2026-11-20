<?php

namespace Tests\PostgreSQL;

use App\Models\User;
use App\Repositories\JourneyPatternRepository;
use App\Services\LineService;
use App\Services\ProcessorService;
use App\Services\ValkeyService;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Http;
use Laravel\Sanctum\Sanctum;
use Tests\TestCase;

class JourneyPatternContractTest extends TestCase
{
    protected function setUp(): void
    {
        parent::setUp();
        if (getenv('POSTGRES_STAGE1_TESTS') !== '1' || DB::connection()->getDriverName() !== 'pgsql'
            || !in_array(DB::connection()->getDatabaseName(), ['open_transit_test', 'open_transit_schema_test'], true)) {
            $this->markTestSkipped('Requires the existing isolated PostgreSQL executor.');
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

    private function fixture(bool $ready = true): array
    {
        $source = DB::table('ingest.feed_sources')->insertGetId([
            'provider' => 'sptrans', 'name' => 'ope38-fixture', 'source_kind' => 'gtfs_schedule', 'profile_version' => 'stage1',
        ]);
        $feed = DB::table('ingest.feed_versions')->insertGetId([
            'feed_source_id' => $source, 'checksum_sha256' => hash('sha256', 'ope38-fixture'),
            'normalizer_version' => 'fixture', 'profile_version' => 'stage1',
        ]);
        $agency = DB::table('gtfs.agencies')->insertGetId(['feed_version_id' => $feed, 'name' => 'Fixture', 'timezone' => 'America/Sao_Paulo']);
        $route = DB::table('gtfs.routes')->insertGetId(['feed_version_id' => $feed, 'agency_id' => $agency, 'gtfs_route_id' => 'fixture-route', 'short_name' => 'Fixture', 'route_type' => 3]);
        $service = DB::table('gtfs.services')->insertGetId(['feed_version_id' => $feed, 'gtfs_service_id' => 'WK', 'has_weekly_calendar' => false]);
        DB::table('gtfs.service_exceptions')->insert(['feed_version_id' => $feed, 'service_id' => $service, 'service_date' => '2026-11-15', 'exception_type' => 1]);
        $stops = [];
        foreach (['A', 'B'] as $name) {
            $stops[] = DB::selectOne("INSERT INTO gtfs.stops(feed_version_id,gtfs_stop_id,name,position_geom)
                VALUES (?,?,?,public.ST_GeomFromText('POINT(-46.63 -23.55)',4326)) RETURNING id", [$feed, $name, $name])->id;
        }
        $patterns = $lines = [];
        foreach ([0, 1] as $direction) {
            $shape = DB::selectOne("INSERT INTO gtfs.shapes(feed_version_id,gtfs_shape_id,path_geom,geometry_status)
                VALUES (?,?,public.ST_GeomFromText('LINESTRING(-46.63 -23.55,-46.64 -23.54,-46.65 -23.56)',4326),'valid') RETURNING id", [$feed, 'shape-'.$direction])->id;
            $pattern = DB::table('gtfs.journey_patterns')->insertGetId(['feed_version_id' => $feed, 'route_id' => $route, 'shape_id' => $shape, 'gtfs_direction_id' => $direction, 'fingerprint_sha256' => hash('sha256', 'pattern-'.$direction)]);
            $trip = DB::table('gtfs.trips')->insertGetId(['feed_version_id' => $feed, 'route_id' => $route, 'service_id' => $service, 'pattern_id' => $pattern, 'shape_id' => $shape, 'gtfs_direction_id' => $direction, 'gtfs_trip_id' => 'trip-'.$direction]);
            // Insert out of order, including a repeated occurrence of the same stop.
            foreach ([20, 0, 10] as $sequence) {
                $stop = $stops[($sequence === 0 ? 1 : 0) ^ $direction];
                DB::table('gtfs.journey_pattern_stops')->insert(['feed_version_id' => $feed, 'pattern_id' => $pattern, 'pattern_sequence' => $sequence, 'gtfs_stop_pk' => $stop]);
                DB::table('gtfs.stop_times')->insert(['feed_version_id' => $feed, 'trip_id' => $trip, 'stop_sequence' => $sequence, 'gtfs_stop_pk' => $stop]);
            }
            $line = DB::table('public.lines')->insertGetId(['sptrans_line_id' => 91000 + $direction, 'display_code' => 'Fixture', 'display_name' => 'Fixture', 'sptrans_direction' => $direction + 1]);
            $patterns[] = $pattern;
            $lines[] = $line;
        }
        if ($ready) {
            DB::table('ingest.import_runs')->insert(['feed_version_id' => $feed, 'state' => 'succeeded']);
            DB::table('ingest.feed_versions')->where('id', $feed)->update(['state' => 'validating']);
            DB::table('ingest.validation_results')->insert(['feed_version_id' => $feed, 'validator_version' => 'fixture-only', 'rule_code' => 'profile.complete', 'severity' => 'info', 'passed' => true]);
            DB::table('ingest.feed_versions')->where('id', $feed)->update(['state' => 'ready']);
        }

        return compact('source', 'feed', 'route', 'patterns', 'lines');
    }

    private function mapping(array $f, string $status = 'confirmed', int $direction = 0): void
    {
        DB::table('public.line_route_mappings')->insert(['feed_version_id' => $f['feed'], 'line_id' => $f['lines'][0],
            'route_id' => $f['route'], 'pattern_id' => $f['patterns'][0], 'gtfs_direction_id' => $direction,
            'confidence_score' => 1, 'matching_method' => 'fixture_manual', 'status' => $status]);
    }

    private function publish(array $f): void
    {
        DB::selectOne("SELECT ingest.promote_feed(?,?,0,'fixture','contract test')", [$f['source'], $f['feed']]);
    }

    public function test_preview_preserves_shape_sequence_repeated_stops_and_feed_boundaries(): void
    {
        $f = $this->fixture();
        $repo = app(JourneyPatternRepository::class);
        $patterns = $repo->preview($f['feed'], $f['route']);
        $this->assertCount(2, $patterns);
        $this->assertSame([0, 10, 20], array_column($patterns[0]['stops'], 'pattern_sequence'));
        $this->assertSame(['B', 'A', 'A'], array_column($patterns[0]['stops'], 'gtfs_stop_id'));
        $this->assertSame(['A', 'B', 'B'], array_column($patterns[1]['stops'], 'gtfs_stop_id'));
        $this->assertSame([0, 1], array_column($patterns, 'gtfs_direction_id'));
        $this->assertSame('LineString', $patterns[0]['geometry']['type']);
        $this->assertSame([-46.64, -23.54], $patterns[0]['geometry']['coordinates'][1]);
        $this->assertSame([-46.65, -23.56, -46.63, -23.54], $patterns[0]['bbox']);
        $this->assertSame('unverified', $patterns[0]['matching_status']);
        $this->assertSame([], $repo->preview($f['feed'] + 100000, $f['route']));
        $this->assertSame([], $repo->preview($f['feed'], $f['route'] + 100000));
    }

    public function test_ready_and_confirmed_do_not_bypass_publication_and_app_role_can_read_active_contract(): void
    {
        $f = $this->fixture();
        $this->mapping($f);
        $repo = app(JourneyPatternRepository::class);
        $this->assertSame([], $repo->forLine($f['lines'][0]));
        $this->publish($f);
        DB::statement('SET LOCAL ROLE mobility_app');
        try {
            $patterns = $repo->forLine($f['lines'][0]);
            $this->assertCount(1, $patterns);
            $this->assertSame($f['patterns'][0], $patterns[0]['pattern_id']);
            $this->assertSame('active', $patterns[0]['publication_status']);
            $this->assertSame([], $repo->forLine($f['lines'][1]));
        } finally {
            DB::statement('RESET ROLE');
        }
    }

    public function test_pending_mismatched_and_expired_mappings_are_not_official(): void
    {
        $f = $this->fixture();
        $this->publish($f);
        $this->mapping($f, 'pending');
        $repo = app(JourneyPatternRepository::class);
        $this->assertSame([], $repo->forLine($f['lines'][0]));
        DB::table('public.line_route_mappings')->where('line_id', $f['lines'][0])->update(['status' => 'confirmed', 'gtfs_direction_id' => 1]);
        $this->assertSame([], $repo->forLine($f['lines'][0]));
        DB::table('public.line_route_mappings')->where('line_id', $f['lines'][0])->update(['gtfs_direction_id' => 0, 'valid_until' => '2000-01-01']);
        $this->assertSame([], $repo->forLine($f['lines'][0]));
    }

    public function test_confirmed_operational_directions_preserve_their_patterns_and_feed(): void
    {
        $f = $this->fixture();
        $this->mapping($f);
        DB::table('public.line_route_mappings')->insert(['feed_version_id' => $f['feed'], 'line_id' => $f['lines'][1],
            'route_id' => $f['route'], 'pattern_id' => $f['patterns'][1], 'gtfs_direction_id' => 1,
            'confidence_score' => 1, 'matching_method' => 'fixture_manual', 'status' => 'confirmed']);
        $this->publish($f);
        $repo = app(JourneyPatternRepository::class);
        foreach ([0, 1] as $direction) {
            $patterns = $repo->forLine($f['lines'][$direction]);
            $this->assertCount(1, $patterns);
            $this->assertSame($f['patterns'][$direction], $patterns[0]['pattern_id']);
            $this->assertSame($f['feed'], $patterns[0]['feed_version_id']);
            $this->assertSame($direction, $patterns[0]['gtfs_direction_id']);
        }
        // A reviewed association may legitimately use SL=1 and GTFS direction=1.
        DB::table('public.line_route_mappings')->where('line_id', $f['lines'][0])
            ->update(['pattern_id' => $f['patterns'][1], 'gtfs_direction_id' => 1]);
        $this->assertSame($f['patterns'][1], $repo->forLine($f['lines'][0])[0]['pattern_id']);
    }

    public function test_staging_feed_is_not_previewed(): void
    {
        $f = $this->fixture(false);
        $this->assertSame([], app(JourneyPatternRepository::class)->preview($f['feed'], $f['route']));
    }

    public function test_legacy_read_resolves_internal_direction_without_sync_or_cache(): void
    {
        $f = $this->fixture();
        foreach ($f['lines'] as $index => $line) {
            $stop = DB::table('public.bus_stops')->insertGetId(['name' => 'Legacy-'.$index, 'legacy_visible' => true]);
            DB::table('public.bus_stop_line')->insert(['line_id' => $line, 'bus_stop_id' => $stop]);
        }
        $this->mock(ProcessorService::class, function ($mock) {
            $mock->shouldNotReceive('syncStopsForLine');
            $mock->shouldNotReceive('resolveLineId');
        });
        $this->mock(ValkeyService::class, function ($mock) {
            $mock->shouldNotReceive('getStopsForLine');
            $mock->shouldNotReceive('resolveSptransId');
        });
        $result = app(LineService::class)->stopsForLine($f['lines'][0], 2);
        $this->assertSame('Legacy-1', $result['stops'][0]['name']);
        $this->assertSame(91001, $result['line']['sptrans_id']);
        $this->assertFalse($result['official']);
        $this->assertFalse($result['ordered']);
        DB::table('public.lines')->insert(['sptrans_line_id' => 92000, 'display_code' => 'Fixture',
            'display_name' => 'Ambiguous', 'sptrans_direction' => 2]);
        $ambiguous = app(LineService::class)->stopsForLine($f['lines'][0], 2);
        $this->assertSame('direction_unavailable', $ambiguous['status']);
        $this->assertSame([], $ambiguous['stops']);
        $this->assertNull($ambiguous['line']['sptrans_id']);
    }

    public function test_vehicle_projection_uses_geographic_lengths_and_selected_feed_pattern(): void
    {
        $f = $this->fixture();
        $this->publish($f);
        $rows = app(\App\Repositories\VehicleProgressRepository::class)->project(
            [['feed_version_id' => $f['feed'], 'pattern_id' => $f['patterns'][0]]],
            [['n' => 0, 'lat' => -23.54, 'lng' => -46.64]], false);
        $this->assertCount(1, $rows);
        $this->assertSame($f['patterns'][0], (int) $rows[0]['pattern_id']);
        $this->assertSame($f['feed'], (int) $rows[0]['feed_version_id']);
        $this->assertGreaterThan(1000, (float) $rows[0]['total_m']);
        $this->assertGreaterThan(1000, (float) $rows[0]['along_m']);
        $this->assertLessThan(1, (float) $rows[0]['lateral_m']);
        $this->assertFalse($rows[0]['ambiguous']);
        $this->assertSame([0, 10, 20], array_column($rows[0]['stops'], 'pattern_sequence'));
        $this->assertSame([], app(\App\Repositories\VehicleProgressRepository::class)->project(
            [['feed_version_id' => $f['feed'] + 100000, 'pattern_id' => $f['patterns'][0]]],
            [['n' => 0, 'lat' => -23.54, 'lng' => -46.64]], false));
    }

    public function test_routes_require_auth_and_preview_is_explicitly_gated(): void
    {
        $f = $this->fixture();
        $url = '/api/v1/gtfs/preview/feeds/'.$f['feed'].'/routes/'.$f['route'].'/journey-patterns';
        $this->getJson('/api/v1/lines/'.$f['lines'][0].'/journey-patterns')->assertUnauthorized();
        $this->getJson($url)->assertUnauthorized();
        Sanctum::actingAs(new User);
        config(['gtfs.preview_enabled' => false]);
        $this->getJson($url)->assertNotFound();
        config(['gtfs.preview_enabled' => true]);
        $this->getJson($url)->assertOk()->assertJsonPath('data.mode', 'preview')->assertJsonPath('data.operational_line_id', null);
        $this->getJson('/api/v1/lines/'.$f['lines'][0].'/journey-patterns')
            ->assertOk()->assertJsonPath('data.status', 'unavailable')->assertJsonPath('data.patterns', []);
        $this->getJson('/api/v1/lines/'.$f['lines'][0].'/journey-patterns?direction=2')->assertUnprocessable();
        DB::statement('SET LOCAL ROLE mobility_app');
        try {
            $this->getJson($url)->assertStatus(503);
        } finally {
            DB::statement('RESET ROLE');
        }
    }
}
