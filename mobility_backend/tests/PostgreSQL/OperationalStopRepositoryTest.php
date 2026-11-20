<?php

namespace Tests\PostgreSQL;

use App\Repositories\OperationalStopRepository;
use Illuminate\Support\Facades\DB;
use Tests\TestCase;

class OperationalStopRepositoryTest extends TestCase
{
    protected function setUp(): void
    {
        parent::setUp();
        if (getenv('POSTGRES_STAGE1_TESTS') !== '1' || DB::connection()->getDriverName() !== 'pgsql'
            || !in_array(DB::connection()->getDatabaseName(), ['open_transit_test', 'open_transit_schema_test'], true)) {
            $this->markTestSkipped('Requires the existing isolated PostgreSQL executor.');
        }
        DB::beginTransaction();
    }

    protected function tearDown(): void
    {
        while (DB::transactionLevel() > 0) { DB::rollBack(); }
        parent::tearDown();
    }

    private function fixture(): array
    {
        $lines = [];
        foreach ([1, 2, 1] as $index => $direction) {
            $lines[] = DB::table('public.lines')->insertGetId(['sptrans_line_id' => 99001 + $index,
                'display_code' => 'Fixture', 'display_name' => 'Fixture', 'sptrans_direction' => $direction]);
        }
        $stops = [];
        foreach (range(0, 5) as $i) {
            $id = DB::selectOne("INSERT INTO public.bus_stops(name,position_geom)
                VALUES (?,public.ST_SetSRID(public.ST_MakePoint(-46.63,?),4326)) RETURNING id", ['Fixture '.$i, -23.55 - 0.0001 * ($i + 1)])->id;
            DB::table('public.stop_source_references')->insert(['stop_id' => $id, 'provider' => 'sptrans',
                'source_kind' => 'sptrans', 'sptrans_stop_code' => 880001 + $i, 'external_stop_id' => (string) (880001 + $i),
                'status' => $i === 4 ? 'pending' : 'confirmed', 'method' => 'fixture']);
            $members = $i === 1 ? [1] : ($i === 2 ? [0, 1] : [0]);
            foreach ($members as $member) {
                DB::table('public.line_stop_sources')->insert(['line_id' => $lines[$member], 'stop_id' => $id,
                    'source' => 'sptrans', 'external_line_id' => (string) ($i === 5 ? 99002 : 99001 + $member),
                    'external_stop_id' => (string) (880001 + $i), 'status' => $i === 3 ? 'pending' : 'confirmed']);
            }
            $stops[] = $id;
        }
        return [$lines, $stops];
    }

    public function test_correct_direction_shared_stops_and_unconfirmed_associations(): void
    {
        [$lines, $stops] = $this->fixture();
        $repo = app(OperationalStopRepository::class);
        $out = $repo->select($lines[0], 99001, -23.55, -46.63, null, 1, 20);
        $back = $repo->select($lines[1], 99002, -23.55, -46.63, null, 1, 20);
        $this->assertSame([$stops[0], $stops[2]], array_column($out['stops'], 'stop_id'));
        $this->assertSame([$stops[1], $stops[2]], array_column($back['stops'], 'stop_id'));
        $this->assertSame([880001, 880003], array_column($out['stops'], 'sptrans_stop_code'));
        $this->assertGreaterThan(1, $out['stops'][0]['passenger_distance_m']);
        $this->assertLessThan(100, $out['stops'][0]['passenger_distance_m']);
        $this->assertSame('no_confirmed_stop_association', $repo->select($lines[2], 99003, -23.55, -46.63, null, 1, 20)['reason']);
    }

    public function test_manual_selection_without_gps_does_not_accept_an_opposite_stop(): void
    {
        [$lines, $stops] = $this->fixture();
        $repo = app(OperationalStopRepository::class);
        $manual = $repo->select($lines[0], 99001, null, null, $stops[0], 1, 20);
        $this->assertSame('available', $manual['status']);
        $this->assertNull($manual['stops'][0]['passenger_distance_m']);
        $this->assertSame('stop_not_confirmed_for_line', $repo->select($lines[0], 99001, null, null, $stops[1], 1, 20)['reason']);
        $this->assertSame('no_stops_in_radius', $repo->select($lines[0], 99001, 0, 0, null, 1, 20)['reason']);
    }
}
