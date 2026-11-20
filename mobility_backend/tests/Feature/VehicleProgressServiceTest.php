<?php

namespace Tests\Feature;

use App\Models\Line;
use App\Repositories\VehicleProgressRepository;
use App\Services\ValkeyService;
use App\Services\VehicleProgressService;
use Carbon\CarbonImmutable;
use Tests\TestCase;

class VehicleProgressServiceTest extends TestCase
{
    public function test_missing_confirmed_stop_link_never_reads_prediction_cache(): void
    {
        CarbonImmutable::setTestNow('2026-11-18T12:00:00Z');
        try {
            $cache=$this->createMock(ValkeyService::class);
            $cache->expects($this->never())->method('getPredictionSnapshot');
            $cache->method('getVehicleSnapshot')->willReturn([
                ['prefix'=>'fixture-current','lat'=>-23.55,'lng'=>-46.63,'captured_at'=>'2026-11-18T12:00:00Z']]);
            $codes=$this->createMock(\App\Repositories\OperationalStopRepository::class);
            $codes->method('confirmedPatternCodes')->willReturn([]);
            $this->instance(\App\Repositories\OperationalStopRepository::class,$codes);
            $repo=$this->createMock(VehicleProgressRepository::class);
            $repo->method('project')->willReturn([$this->row()]);
            $line=new Line; $line->setRawAttributes(['id'=>7,'provider'=>'sptrans','sptrans_id'=>99,'sptrans_direction'=>1]);
            $out=(new VehicleProgressService($cache,$repo))->forLine($line,[['feed_version_id'=>90,'pattern_id'=>901,
                'matching_status'=>'confirmed','publication_status'=>'active']]);
            $this->assertSame('unavailable',$out['vehicles'][0]['eta']['status']);
            $this->assertNull($out['vehicles'][0]['eta_minutes']);
            $this->assertSame(350.0,$out['vehicles'][0]['remaining_distance_m']);
        } finally { CarbonImmutable::setTestNow(); }
    }

    public function test_official_eta_reads_once_and_cache_failure_preserves_progress(): void
    {
        CarbonImmutable::setTestNow('2026-11-18T12:00:00Z');
        try {
            foreach ([false,true] as $cacheFailure) {
                $cache=$this->createMock(ValkeyService::class);
                $cache->method('getVehicleSnapshot')->willReturn([
                    ['prefix'=>'fixture-current','lat'=>-23.55,'lng'=>-46.63,'captured_at'=>'2026-11-18T12:00:00Z'],
                    ['prefix'=>'fixture-other','lat'=>-23.55,'lng'=>-46.63,'captured_at'=>'2026-11-18T12:00:00Z'],
                ]);
                $read=$cache->expects($this->once())->method('getPredictionSnapshot')->with(99,1);
                if ($cacheFailure) { $read->willThrowException(new \RuntimeException('synthetic cache failure')); }
                else { $read->willReturn(['schema_version'=>1,'source'=>'sptrans','sptrans_line_id'=>99,'sptrans_direction'=>1,
                    'status'=>'available','fetched_at'=>'2026-11-18T12:00:00Z','valid_until'=>'2026-11-18T12:00:20Z',
                    'predictions'=>['hr'=>'09:00','ps'=>[['cp'=>700016623,'vs'=>[
                        ['p'=>'fixture-current','t'=>'09:02','ta'=>'2026-11-18T12:00:00Z'],
                        ['p'=>'fixture-other','t'=>'09:03','ta'=>'2026-11-18T12:00:00Z']]]]]]); }
                $codes=$this->createMock(\App\Repositories\OperationalStopRepository::class);
                $codes->expects($this->once())->method('confirmedPatternCodes')->with(7,99,1,90,901)
                    ->willReturn([10=>['stop_id'=>'000B','cp'=>700016623]]);
                $this->instance(\App\Repositories\OperationalStopRepository::class,$codes);
                $repo=$this->createMock(VehicleProgressRepository::class);
                $second=$this->row(); $second['n']=1;
                $repo->method('project')->willReturn([$this->row(),$second]);
                $line=new Line; $line->setRawAttributes(['id'=>7,'provider'=>'sptrans','sptrans_id'=>99,'sptrans_direction'=>1]);
                $out=(new VehicleProgressService($cache,$repo))->forLine($line,[['feed_version_id'=>90,'pattern_id'=>901,
                    'matching_status'=>'confirmed','publication_status'=>'active']]);
                foreach ($out['vehicles'] as $i=>$v) {
                    $this->assertSame('matched',$v['match_status']);
                    $this->assertSame(350.0,$v['remaining_distance_m']);
                    $this->assertSame(0.25,$v['progress_fraction']);
                    $this->assertSame('not_observed',$v['passage_status']);
                    $this->assertSame($cacheFailure?null:2+$i,$v['eta_minutes']);
                }
            }
        } finally { CarbonImmutable::setTestNow(); }
    }

    private function service(): VehicleProgressService
    {
        return new VehicleProgressService($this->createMock(ValkeyService::class), $this->createMock(VehicleProgressRepository::class));
    }

    private function row(): array
    {
        return ['n' => 0, 'feed_version_id' => 90, 'pattern_id' => 901, 'gtfs_direction_id' => 0,
            'along_m' => 250, 'lateral_m' => 3, 'total_m' => 1000, 'ambiguous' => false,
            'projected_lat' => -23.55, 'projected_lng' => -46.63,
            'stops' => [
                ['id' => 10, 'gtfs_stop_id' => '000A', 'name' => 'Parada A', 'pattern_sequence' => 0, 'along_m' => 100, 'lateral_distance_m' => 2, 'ambiguous' => false],
                ['id' => 11, 'gtfs_stop_id' => '000B', 'name' => 'Parada B', 'pattern_sequence' => 10, 'along_m' => 600, 'lateral_distance_m' => 2, 'ambiguous' => false],
                ['id' => 12, 'gtfs_stop_id' => '000C', 'name' => 'Parada C', 'pattern_sequence' => 30, 'along_m' => 900, 'lateral_distance_m' => 2, 'ambiguous' => false],
            ]];
    }

    public function test_single_observation_has_spatial_candidates_and_along_shape_distance(): void
    {
        $result = $this->service()->interpret($this->row(), true);
        $this->assertSame('provisional', $result['match_status']);
        $this->assertSame(0.25, $result['progress_fraction']);
        $this->assertSame(350.0, $result['remaining_distance_m']);
        $this->assertSame(11, $result['next_stop']['id']);
        $this->assertSame(0.1, $result['previous_stop']['progress_fraction']);
        $this->assertSame(0.6, $result['next_stop']['progress_fraction']);
        $this->assertSame($result['next_stop']['progress_fraction'], $result['upcoming_stops'][0]['progress_fraction']);
        $this->assertSame($this->row()['stops'][0], array_diff_key($result['previous_stop'], ['progress_fraction' => true]));
        $this->assertSame($this->row()['stops'][1], array_diff_key($result['next_stop'], ['progress_fraction' => true]));
        $this->assertSame(['000B', '000C'], array_column($result['upcoming_stops'], 'stop_id'));
        $this->assertSame([10, 30], array_column($result['upcoming_stops'], 'stop_sequence'));
        $this->assertSame([350.0, 650.0], array_column($result['upcoming_stops'], 'distance_remaining_m'));
        $this->assertSame([0.6, 0.9], array_column($result['upcoming_stops'], 'progress_fraction'));
        $this->assertSame($result['next_stop']['gtfs_stop_id'], $result['upcoming_stops'][0]['stop_id']);
        $this->assertSame($result['remaining_distance_m'], $result['upcoming_stops'][0]['distance_remaining_m']);
        $this->assertSame('matched', $this->service()->interpret($this->row(), false)['match_status']);
    }

    public function test_crossing_and_non_monotonic_stop_projection_do_not_select_a_next_stop(): void
    {
        $row = $this->row(); $row['ambiguous'] = true;
        $result = $this->service()->interpret($row, true);
        $this->assertSame('ambiguous', $result['match_status']);
        $this->assertArrayNotHasKey('next_stop', $result);
        $this->assertNull($result['upcoming_stops']);
        $row['ambiguous'] = false; $row['stops'][1]['along_m'] = 50;
        $this->assertSame('ambiguous', $this->service()->interpret($row, true)['match_status']);
        $this->assertNull($this->service()->interpret($row, true)['upcoming_stops']);
        $row = $this->row(); $row['stops'][0]['ambiguous'] = true;
        $this->assertSame('ambiguous', $this->service()->interpret($row, true)['match_status']);
        $row = $this->row(); $row['stops'][1]['pattern_sequence'] = 40;
        $this->assertNull($this->service()->interpret($row, true)['upcoming_stops']);
    }

    public function test_off_route_missing_stops_and_end_of_pattern(): void
    {
        $row = $this->row(); $row['lateral_m'] = 101;
        $this->assertSame('unavailable', $this->service()->interpret($row, true)['match_status']);
        $this->assertNull($this->service()->interpret($row, true)['upcoming_stops']);
        $row = $this->row(); $row['stops'] = [];
        $this->assertSame('unavailable', $this->service()->interpret($row, true)['match_status']);
        $row = $this->row(); $row['along_m'] = 1000;
        $this->assertNull($this->service()->interpret($row, true)['next_stop']);
        $this->assertSame([], $this->service()->interpret($row, true)['upcoming_stops']);
    }

    public function test_stale_missing_time_and_unconfirmed_patterns_never_call_external_services(): void
    {
        CarbonImmutable::setTestNow('2026-11-18T12:00:00Z');
        try {
            $cache = $this->createMock(ValkeyService::class);
            $cache->method('getVehicleSnapshot')->willReturn([
                ['prefix' => 'fixture-old', 'lat' => -23.55, 'lng' => -46.63, 'captured_at' => '2026-11-18T11:50:00Z'],
                ['prefix' => 'fixture-unknown', 'lat' => -23.55, 'lng' => -46.63],
                ['prefix' => 'fixture-current', 'lat' => -23.55, 'lng' => -46.63, 'captured_at' => '2026-11-18T12:00:00Z'],
            ]);
            $repository = $this->createMock(VehicleProgressRepository::class);
            $repository->expects($this->once())->method('project')->with([], [], false)->willReturn([]);
            $line = new Line; $line->setRawAttributes(['id' => 7, 'sptrans_id' => 99, 'sptrans_direction' => 1]);
            $result = (new VehicleProgressService($cache, $repository))->forLine($line, []);
            $this->assertSame(['stale', 'unavailable', 'unavailable'], array_column($result['vehicles'], 'match_status'));
            foreach ($result['vehicles'] as $vehicle) {
                $this->assertNull($vehicle['previous_stop']);
                $this->assertNull($vehicle['progress_fraction']);
                $this->assertSame('candidate_only', $vehicle['stop_status']);
                $this->assertNull($vehicle['next_stop']);
                $this->assertNull($vehicle['upcoming_stops']);
                $this->assertSame('not_observed', $vehicle['passage_status']);
            }
        } finally {
            CarbonImmutable::setTestNow();
        }
    }

    public function test_near_last_stop_and_after_shape_end_keep_next_stop_consistent(): void
    {
        $row = $this->row(); $row['along_m'] = 899.5;
        $result = $this->service()->interpret($row, true);
        $this->assertCount(1, $result['upcoming_stops']);
        $this->assertSame('000C', $result['upcoming_stops'][0]['stop_id']);
        $this->assertSame(0.5, $result['upcoming_stops'][0]['distance_remaining_m']);
        $this->assertSame($result['next_stop']['gtfs_stop_id'], $result['upcoming_stops'][0]['stop_id']);
        $this->assertSame($result['remaining_distance_m'], $result['upcoming_stops'][0]['distance_remaining_m']);

        // Preserve the existing numerical tolerance without inferring passage.
        $row['along_m'] = 900.5;
        $result = $this->service()->interpret($row, true);
        $this->assertSame(0, $result['upcoming_stops'][0]['distance_remaining_m']);
        $this->assertSame($result['remaining_distance_m'], $result['upcoming_stops'][0]['distance_remaining_m']);
        $row['along_m'] = 1100;
        $result = $this->service()->interpret($row, true);
        $this->assertSame([], $result['upcoming_stops']);
        $this->assertNull($result['next_stop']);
        $this->assertNull($result['remaining_distance_m']);
    }

    public function test_equal_and_nearby_positions_preserve_gtfs_stop_occurrences(): void
    {
        $row = $this->row();
        $row['stops'][2]['gtfs_stop_id'] = '000B';
        $row['stops'][2]['along_m'] = 600;
        $stop = $row['stops'][2];
        $stop['id'] = 13; $stop['gtfs_stop_id'] = '000D'; $stop['pattern_sequence'] = 50; $stop['along_m'] = 600.2;
        $row['stops'][] = $stop;
        $result = $this->service()->interpret($row, true);
        $this->assertSame(['000B', '000B', '000D'], array_column($result['upcoming_stops'], 'stop_id'));
        $this->assertSame([10, 30, 50], array_column($result['upcoming_stops'], 'stop_sequence'));
        $this->assertSame([350.0, 350.0], array_slice(array_column($result['upcoming_stops'], 'distance_remaining_m'), 0, 2));
        $this->assertEqualsWithDelta(350.2, $result['upcoming_stops'][2]['distance_remaining_m'], 0.000001);
        $this->assertSame(10, $result['next_stop']['pattern_sequence']);

        // Close projections on opposite sides of eligibility cannot skip official occurrences.
        $row = $this->row();
        $row['stops'][1]['along_m'] = 249.1; $row['stops'][2]['along_m'] = 248.9;
        $result = $this->service()->interpret($row, true);
        $this->assertSame('ambiguous', $result['match_status']);
        $this->assertNull($result['upcoming_stops']);
    }

    public function test_expired_snapshot_returns_no_current_upcoming_stops(): void
    {
        $cache = $this->createMock(ValkeyService::class);
        $cache->method('getVehicleSnapshot')->willReturn(null);
        $repository = $this->createMock(VehicleProgressRepository::class);
        $repository->expects($this->once())->method('project')
            ->with([['feed_version_id' => 90, 'pattern_id' => 901]], [], true)->willReturn([]);
        $line = new Line; $line->setRawAttributes(['id' => 7, 'sptrans_id' => 99, 'sptrans_direction' => 1]);
        $result = (new VehicleProgressService($cache, $repository))->forLine($line,
            [['feed_version_id' => 90, 'pattern_id' => 901]], true);
        $this->assertSame('vehicle_snapshot_unavailable', $result['reason']);
        $this->assertSame([], $result['vehicles']);
    }

    public function test_stop_fractions_share_the_vehicle_pattern_reference(): void
    {
        $row = $this->row();
        $first = $this->service()->interpret($row, true);
        $row['pattern_id'] = 902;
        $row['total_m'] = 2000;
        $second = $this->service()->interpret($row, true);
        $this->assertSame(901, $first['pattern_id']);
        $this->assertSame(902, $second['pattern_id']);
        $this->assertSame(90, $second['feed_version_id']);
        $this->assertSame(0.125, $second['progress_fraction']);
        $this->assertSame(0.05, $second['previous_stop']['progress_fraction']);
        $this->assertSame(0.3, $second['next_stop']['progress_fraction']);
        $this->assertSame($second['next_stop']['progress_fraction'], $second['upcoming_stops'][0]['progress_fraction']);
    }

    public function test_repeated_stop_identity_preserves_each_occurrence_fraction(): void
    {
        $row = $this->row();
        $row['along_m'] = 700;
        $row['stops'][2]['id'] = $row['stops'][1]['id'];
        $row['stops'][2]['gtfs_stop_id'] = $row['stops'][1]['gtfs_stop_id'];
        $result = $this->service()->interpret($row, true);
        $this->assertSame('000B', $result['previous_stop']['gtfs_stop_id']);
        $this->assertSame('000B', $result['next_stop']['gtfs_stop_id']);
        $this->assertSame(10, $result['previous_stop']['pattern_sequence']);
        $this->assertSame(30, $result['next_stop']['pattern_sequence']);
        $this->assertSame(0.6, $result['previous_stop']['progress_fraction']);
        $this->assertSame(0.9, $result['next_stop']['progress_fraction']);
    }

    public function test_fraction_references_cover_segment_start_end_and_null_boundaries(): void
    {
        foreach ([[101.5, 0.003], [600, 1.0]] as [$along, $expected]) {
            $row = $this->row();
            $row['along_m'] = $along;
            $result = $this->service()->interpret($row, true);
            $start = $result['previous_stop']['progress_fraction'];
            $end = $result['next_stop']['progress_fraction'];
            $position = ($result['progress_fraction'] - $start) / ($end - $start);
            $this->assertEqualsWithDelta($expected, $position, 0.000001);
        }
        $row = $this->row(); $row['along_m'] = 0;
        $start = $this->service()->interpret($row, true);
        $this->assertNull($start['previous_stop']);
        $this->assertSame(0.1, $start['next_stop']['progress_fraction']);
        $row['along_m'] = 1000;
        $end = $this->service()->interpret($row, true);
        $this->assertNull($end['next_stop']);
        $this->assertSame(0.9, $end['previous_stop']['progress_fraction']);
    }

    public function test_invalid_or_missing_projection_cannot_fabricate_stop_fractions(): void
    {
        $invalid = [];
        foreach ([null, 0, INF, NAN] as $total) {
            $row = $this->row(); $row['total_m'] = $total; $invalid[] = $row;
        }
        foreach ([null, INF, NAN] as $along) {
            $row = $this->row(); $row['along_m'] = $along; $invalid[] = $row;
            $row = $this->row(); $row['stops'][0]['along_m'] = $along; $invalid[] = $row;
        }
        foreach ($invalid as $row) {
            $result = $this->service()->interpret($row, true);
            $this->assertSame('unavailable', $result['match_status']);
            $this->assertNull($result['upcoming_stops']);
            $this->assertArrayNotHasKey('previous_stop', $result);
            $this->assertArrayNotHasKey('next_stop', $result);
            $this->assertArrayNotHasKey('progress_fraction', $result);
        }
    }

    public function test_preview_envelope_keeps_spatial_candidates_without_claiming_passage(): void
    {
        CarbonImmutable::setTestNow('2026-11-18T12:00:00Z');
        try {
            $cache = $this->createMock(ValkeyService::class);
            $cache->method('getVehicleSnapshot')->willReturn([
                ['prefix' => 'fixture-current', 'lat' => -23.55, 'lng' => -46.63, 'captured_at' => '2026-11-18T12:00:00Z'],
            ]);
            $repository = $this->createMock(VehicleProgressRepository::class);
            $repository->expects($this->once())->method('project')->with(
                [['feed_version_id' => 90, 'pattern_id' => 901]],
                [['n' => 0, 'lat' => -23.55, 'lng' => -46.63]], true
            )->willReturn([$this->row()]);
            $line = new Line; $line->setRawAttributes(['id' => 7, 'sptrans_id' => 99, 'sptrans_direction' => 1]);
            $result = (new VehicleProgressService($cache, $repository))->forLine($line,
                [['feed_version_id' => 90, 'pattern_id' => 901]], true);
            $vehicle = $result['vehicles'][0];
            $this->assertSame('available', $result['status']);
            $this->assertSame('provisional', $vehicle['match_status']);
            $this->assertSame('unavailable', $vehicle['eta']['status']);
            $this->assertSame('preview_pattern_operational_link_unconfirmed', $vehicle['eta']['reason']);
            $this->assertNull($vehicle['eta_minutes']);
            $this->assertSame('not_observed', $vehicle['passage_status']);
            $this->assertSame('candidate_only', $vehicle['stop_status']);
            $this->assertSame(7, $vehicle['operational_line_id']);
            $this->assertSame(0.1, $vehicle['previous_stop']['progress_fraction']);
            $this->assertSame(0.6, $vehicle['next_stop']['progress_fraction']);
        } finally {
            CarbonImmutable::setTestNow();
        }
    }

    public function test_progress_routes_require_authentication_and_preview_is_disabled_by_default(): void
    {
        $this->getJson('/api/v1/lines/1/vehicles/stop-progress')->assertUnauthorized();
        $this->getJson('/api/v1/gtfs/preview/feeds/1/patterns/1655/lines/1/vehicles/stop-progress')->assertUnauthorized();
        \Laravel\Sanctum\Sanctum::actingAs(new \App\Models\User);
        config(['gtfs.preview_enabled' => false]);
        $this->getJson('/api/v1/gtfs/preview/feeds/1/patterns/1655/lines/1/vehicles/stop-progress')->assertNotFound();
    }
}
