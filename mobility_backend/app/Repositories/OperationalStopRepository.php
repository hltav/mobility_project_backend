<?php

namespace App\Repositories;

use Illuminate\Support\Facades\DB;

class OperationalStopRepository
{
    /** Resolve existing confirmed identities only; no candidate matching or writes. */
    public function confirmedPatternCodes(int $lineId, int $cl, int $direction, int $feed, int $pattern): array
    {
        $rows = DB::select("WITH linked AS (
            SELECT DISTINCT ps.pattern_sequence,st.id AS gtfs_stop_pk,st.gtfs_stop_id,
                cp.sptrans_stop_code AS cp
            FROM public.lines l
            JOIN public.line_route_mappings m ON m.line_id=l.id AND m.status='confirmed'
              AND (m.valid_from IS NULL OR m.valid_from<=CURRENT_TIMESTAMP)
              AND (m.valid_until IS NULL OR m.valid_until>CURRENT_TIMESTAMP)
            JOIN ingest.feed_publications pub ON pub.active_feed_version_id=m.feed_version_id
            JOIN ingest.feed_versions v ON v.id=m.feed_version_id AND v.state='ready'
            JOIN ingest.feed_sources src ON src.id=v.feed_source_id AND src.source_kind='gtfs_schedule'
            JOIN gtfs.journey_patterns p ON p.feed_version_id=m.feed_version_id AND p.id=m.pattern_id
              AND p.route_id=m.route_id AND p.gtfs_direction_id IS NOT DISTINCT FROM m.gtfs_direction_id
            JOIN gtfs.journey_pattern_stops ps ON ps.feed_version_id=p.feed_version_id AND ps.pattern_id=p.id
            JOIN gtfs.stops st ON st.feed_version_id=ps.feed_version_id AND st.id=ps.gtfs_stop_pk
            JOIN public.stop_source_references g ON g.feed_version_id=st.feed_version_id AND g.gtfs_stop_pk=st.id
              AND g.external_stop_id=st.gtfs_stop_id AND g.source_kind='gtfs' AND g.status='confirmed' AND g.provider=src.provider
            JOIN public.stop_source_references cp ON cp.stop_id=g.stop_id AND cp.source_kind='sptrans'
              AND cp.provider='sptrans' AND cp.status='confirmed'
            JOIN public.line_stop_sources ls ON ls.line_id=l.id AND ls.stop_id=cp.stop_id
              AND ls.source='sptrans' AND ls.status='confirmed' AND ls.feed_version_id IS NULL
              AND ls.external_line_id=l.sptrans_line_id::text AND ls.external_stop_id=cp.external_stop_id
            WHERE l.id=? AND l.provider='sptrans' AND l.sptrans_line_id=? AND l.sptrans_direction=?
              AND p.feed_version_id=? AND p.id=?
        ) SELECT *,count(*) OVER (PARTITION BY pattern_sequence) AS codes,
            count(*) OVER (PARTITION BY cp) AS occurrences FROM linked", [$lineId,$cl,$direction,$feed,$pattern]);
        $out = [];
        foreach ($rows as $r) {
            // The provider has no stop-occurrence ID: repeated CPs stay unavailable.
            if ((int) $r->codes === 1 && (int) $r->occurrences === 1) {
                $out[(int) $r->pattern_sequence] = ['stop_id' => $r->gtfs_stop_id, 'cp' => (int) $r->cp];
            }
        }
        return $out;
    }

    public function select(int $lineId, int $cl, ?float $lat, ?float $lng, ?int $stopId, float $radiusKm, int $limit): array
    {
        // Both the official CP and its association with this exact CL must be confirmed.
        $query = DB::table('public.bus_stops as b')
            ->join('public.stop_source_references as s', 's.stop_id', '=', 'b.id')
            ->where('s.provider', 'sptrans')->where('s.source_kind', 'sptrans')->where('s.status', 'confirmed')
            ->whereNotNull('b.position_geom')
            ->whereExists(function ($q) use ($lineId, $cl) {
                $q->selectRaw('1')->from('public.line_stop_sources as ls')
                    ->whereColumn('ls.stop_id', 'b.id')->where('ls.line_id', $lineId)
                    ->where('ls.source', 'sptrans')->where('ls.status', 'confirmed')
                    ->where('ls.external_line_id', (string) $cl)->whereColumn('ls.external_stop_id', 's.external_stop_id');
            });
        if (!(clone $query)->exists()) {
            return ['status' => 'unavailable', 'reason' => 'no_confirmed_stop_association', 'stops' => []];
        }
        $query->selectRaw('b.id AS stop_id,s.sptrans_stop_code,b.name AS stop_name,
            public.ST_Y(b.position_geom) AS lat,public.ST_X(b.position_geom) AS lng');
        if ($lat !== null && $lng !== null) {
            $point = 'public.ST_SetSRID(public.ST_MakePoint(?::float8,?::float8),4326)::public.geography';
            $query->selectRaw("public.ST_Distance(b.position_geog,{$point}) AS passenger_distance_m", [$lng, $lat]);
            if ($stopId === null) {
                $query->whereRaw("public.ST_DWithin(b.position_geog,{$point},?)", [$lng, $lat, $radiusKm * 1000]);
            }
            $query->orderBy('passenger_distance_m');
        } else {
            $query->selectRaw('NULL::float8 AS passenger_distance_m');
        }
        if ($stopId !== null) {
            $query->where('b.id', $stopId);
        }
        $rows = $query->orderBy('b.id')->orderBy('s.sptrans_stop_code')->limit($limit)->get();
        $stops = $rows->map(fn($row) => [
            'stop_id' => (int) $row->stop_id, 'sptrans_stop_code' => (int) $row->sptrans_stop_code,
            'stop_name' => $row->stop_name, 'lat' => (float) $row->lat, 'lng' => (float) $row->lng,
            'passenger_distance_m' => $row->passenger_distance_m === null ? null : (float) $row->passenger_distance_m,
        ])->all();
        return ['status' => $stops ? 'available' : 'unavailable',
            'reason' => $stops ? null : ($stopId === null ? 'no_stops_in_radius' : 'stop_not_confirmed_for_line'), 'stops' => $stops];
    }
}
