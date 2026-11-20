<?php

namespace Tests\PostgreSQL;

use Illuminate\Support\Facades\DB;
use Tests\TestCase;

// Synthetic fixtures only, explicitly gated to the existing isolated executor.
class StopReferenceIdentityTest extends TestCase
{
    protected function setUp(): void
    {
        parent::setUp();
        if (getenv('POSTGRES_STAGE1_TESTS') !== '1' || DB::connection()->getDriverName() !== 'pgsql'
            || !in_array(DB::connection()->getDatabaseName(), ['open_transit_test', 'open_transit_schema_test'], true)) {
            $this->markTestSkipped('Requires isolated PostgreSQL executor.');
        }
        DB::beginTransaction();
    }

    protected function tearDown(): void
    {
        while (DB::transactionLevel() > 0) { DB::rollBack(); }
        parent::tearDown();
    }

    public function test_same_external_id_is_feed_scoped_and_repeated_registration_preserves_confirmation(): void
    {
        $tag = 'stop-reference-'.bin2hex(random_bytes(8));
        $source = DB::table('ingest.feed_sources')->insertGetId(['provider' => 'fixture', 'name' => $tag,
            'source_kind' => 'gtfs_schedule', 'profile_version' => 'fixture']);
        $canonical = DB::table('public.bus_stops')->insertGetId(['name' => 'Synthetic stop']);
        $ids = [];
        foreach ([1, 2] as $version) {
            $feed = DB::table('ingest.feed_versions')->insertGetId(['feed_source_id' => $source,
                'checksum_sha256' => hash('sha256', $tag.$version), 'normalizer_version' => 'fixture', 'profile_version' => 'fixture']);
            $stop = DB::selectOne("INSERT INTO gtfs.stops(feed_version_id,gtfs_stop_id,name,position_geom)
                VALUES (?,'000CP','Synthetic',public.ST_SetSRID(public.ST_MakePoint(-46.63,-23.55),4326)) RETURNING id", [$feed])->id;
            $reference = ['stop_id' => $canonical, 'provider' => 'fixture', 'source_kind' => 'gtfs',
                'feed_version_id' => $feed, 'gtfs_stop_pk' => $stop, 'external_stop_id' => '000CP',
                'method' => 'synthetic_explicit_review', 'status' => 'confirmed'];
            $ids[] = DB::table('public.stop_source_references')->insertGetId($reference);
            // Foundation for an idempotent future writer; not a confirmation API.
            $reference['status'] = 'pending';
            $this->assertSame(0, DB::table('public.stop_source_references')->insertOrIgnore($reference));
            $this->assertSame('confirmed', DB::table('public.stop_source_references')->where('id', end($ids))->value('status'));
        }
        $this->assertNotSame($ids[0], $ids[1]);
    }
}
