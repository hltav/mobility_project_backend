<?php

namespace App\Repositories;

use Illuminate\Support\Facades\DB;

class VehicleProgressRepository
{
    /** Batch only selected patterns/observations; all reported distances are geographic metres. */
    public function project(array $patterns, array $observations, bool $preview): array
    {
        if (!$patterns || !$observations) {
            return [];
        }
        $prefix = $preview ? 'gtfs.' : 'gtfs.active_';
        $ready = $preview ? "JOIN ingest.feed_versions v ON v.id=p.feed_version_id AND v.state='ready'" : '';
        $sql = "WITH requested AS (
            SELECT * FROM jsonb_to_recordset(?::jsonb) AS x(feed_version_id bigint,pattern_id bigint)
        ), paths AS (
            SELECT p.feed_version_id,p.id AS pattern_id,p.gtfs_direction_id,s.path_geom
            FROM requested r JOIN {$prefix}journey_patterns p ON p.feed_version_id=r.feed_version_id AND p.id=r.pattern_id
            JOIN {$prefix}shapes s ON s.feed_version_id=p.feed_version_id AND s.id=p.shape_id AND s.geometry_status='valid'
            {$ready}
        ), raw_segments AS (
            SELECT p.*,d.path[1] AS segment_index,d.geom AS segment,
                public.ST_Length(d.geom) AS length_2d,public.ST_Length(d.geom::public.geography) AS length_m
            FROM paths p CROSS JOIN LATERAL public.ST_DumpSegments(p.path_geom) d
            WHERE public.ST_Length(d.geom)>0
        ), segments AS (
            SELECT *,coalesce(sum(length_m) OVER w,0) AS prefix_m
            FROM raw_segments WINDOW w AS (PARTITION BY feed_version_id,pattern_id ORDER BY segment_index
                ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING)
        ), points AS (
            SELECT n,lat,lng FROM jsonb_to_recordset(?::jsonb) AS x(n integer,lat float8,lng float8)
        ), targets AS (
            SELECT p.feed_version_id,p.pattern_id,'vehicle'::text AS kind,o.n::bigint AS target_id,
                null::integer AS pattern_sequence,null::text AS gtfs_stop_id,null::text AS name,
                public.ST_SetSRID(public.ST_MakePoint(o.lng,o.lat),4326) AS point
            FROM paths p CROSS JOIN points o
            UNION ALL
            SELECT p.feed_version_id,p.pattern_id,'stop',st.id,ps.pattern_sequence,st.gtfs_stop_id,st.name,st.position_geom
            FROM paths p JOIN {$prefix}journey_pattern_stops ps ON ps.feed_version_id=p.feed_version_id AND ps.pattern_id=p.pattern_id
            JOIN {$prefix}stops st ON st.feed_version_id=ps.feed_version_id AND st.id=ps.gtfs_stop_pk
            WHERE st.position_geom IS NOT NULL
        ), feet AS (
            SELECT t.*,s.segment_index,s.prefix_m,s.segment,
                public.ST_LineLocatePoint(s.segment,t.point) AS local_fraction
            FROM targets t JOIN segments s USING(feed_version_id,pattern_id)
        ), distances AS (
            SELECT *,public.ST_LineInterpolatePoint(segment,local_fraction) AS projected,
                prefix_m+public.ST_Length(public.ST_LineSubstring(segment,0,local_fraction)::public.geography) AS along_m,
                public.ST_Distance(point::public.geography,
                    public.ST_LineInterpolatePoint(segment,local_fraction)::public.geography) AS lateral_m
            FROM feet
        ), ranked AS (
            SELECT *,min(lateral_m) OVER (PARTITION BY feed_version_id,pattern_id,kind,target_id,pattern_sequence) AS best_m
            FROM distances
        ), alternatives AS (
            SELECT feed_version_id,pattern_id,kind,target_id,pattern_sequence,
                max(along_m)-min(along_m)>25 AS ambiguous
            FROM ranked WHERE lateral_m<=best_m+5
            GROUP BY feed_version_id,pattern_id,kind,target_id,pattern_sequence
        ), located AS (
            SELECT DISTINCT ON (r.feed_version_id,r.pattern_id,r.kind,r.target_id,r.pattern_sequence) r.*,a.ambiguous
            FROM ranked r JOIN alternatives a ON a.feed_version_id=r.feed_version_id AND a.pattern_id=r.pattern_id
                AND a.kind=r.kind AND a.target_id=r.target_id AND a.pattern_sequence IS NOT DISTINCT FROM r.pattern_sequence
            ORDER BY r.feed_version_id,r.pattern_id,r.kind,r.target_id,r.pattern_sequence,r.lateral_m,r.segment_index
        )
        SELECT l.target_id AS n,l.feed_version_id,l.pattern_id,p.gtfs_direction_id,l.along_m,l.lateral_m,l.ambiguous,l.segment_index,
            public.ST_Y(l.projected) AS projected_lat,public.ST_X(l.projected) AS projected_lng,
            (SELECT sum(length_m) FROM segments s WHERE s.feed_version_id=l.feed_version_id AND s.pattern_id=l.pattern_id) AS total_m,
            (SELECT count(*) FROM {$prefix}journey_pattern_stops ps WHERE ps.feed_version_id=l.feed_version_id AND ps.pattern_id=l.pattern_id) AS expected_stops,
            coalesce((SELECT jsonb_agg(jsonb_build_object('id',s.target_id,'gtfs_stop_id',s.gtfs_stop_id,'name',s.name,
                'pattern_sequence',s.pattern_sequence,'along_m',s.along_m,'lateral_distance_m',s.lateral_m,'ambiguous',s.ambiguous)
                ORDER BY s.pattern_sequence) FROM located s WHERE s.feed_version_id=l.feed_version_id
                AND s.pattern_id=l.pattern_id AND s.kind='stop'),'[]'::jsonb) AS stops
        FROM located l JOIN paths p USING(feed_version_id,pattern_id) WHERE l.kind='vehicle'
        ORDER BY l.target_id,l.feed_version_id,l.pattern_id";

        return array_map(function ($row) {
            $data = (array) $row;
            $data['stops'] = json_decode($row->stops, true, 512, JSON_THROW_ON_ERROR);
            return $data;
        }, DB::select($sql, [json_encode($patterns, JSON_THROW_ON_ERROR), json_encode($observations, JSON_THROW_ON_ERROR)]));
    }
}
