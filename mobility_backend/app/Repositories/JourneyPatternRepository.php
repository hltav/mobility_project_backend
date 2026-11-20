<?php

namespace App\Repositories;

use Illuminate\Support\Facades\DB;

class JourneyPatternRepository
{
    public function forLine(int $lineId): array
    {
        // Active views enforce publication even for a restricted mobility_app role.
        return $this->patterns(false, $lineId, null);
    }

    public function preview(int $feedId, int $routeId): array
    {
        return $this->patterns(true, $feedId, $routeId);
    }

    private function patterns(bool $preview, int $id, ?int $routeId): array
    {
        // Only these internal constants choose SQL identifiers, never request values.
        $prefix = $preview ? 'gtfs.' : 'gtfs.active_';
        $scope = $preview
            ? "JOIN ingest.feed_versions v ON v.id=p.feed_version_id AND v.state='ready'
               WHERE p.feed_version_id=? AND p.route_id=?"
            : "JOIN public.line_route_mappings m ON m.feed_version_id=p.feed_version_id
                 AND m.route_id=p.route_id AND m.pattern_id=p.id
                 AND m.gtfs_direction_id IS NOT DISTINCT FROM p.gtfs_direction_id
               WHERE m.line_id=? AND m.status='confirmed'
                 AND (m.valid_from IS NULL OR m.valid_from<=CURRENT_TIMESTAMP)
                 AND (m.valid_until IS NULL OR m.valid_until>CURRENT_TIMESTAMP)";
        $sourceId = $preview ? 'v.feed_source_id' : 'pub.feed_source_id';
        $rows = DB::select("SELECT p.id AS pattern_id,p.route_id,p.feed_version_id,p.gtfs_direction_id,
            p.headsign,p.shape_id,s.gtfs_shape_id,r.gtfs_route_id,
            {$sourceId} AS feed_source_id,
            COALESCE(s.geometry_status,'unavailable') AS geometry_status,
            CASE WHEN s.geometry_status='valid' THEN public.ST_AsGeoJSON(s.path_geom,7,1) END AS geojson,
            COALESCE((SELECT jsonb_agg(jsonb_build_object(
                'id',st.id,'gtfs_stop_id',st.gtfs_stop_id,'name',st.name,
                'lat',public.ST_Y(st.position_geom),'lng',public.ST_X(st.position_geom),
                'pattern_sequence',ps.pattern_sequence) ORDER BY ps.pattern_sequence)
              FROM {$prefix}journey_pattern_stops ps
              JOIN {$prefix}stops st ON st.feed_version_id=ps.feed_version_id AND st.id=ps.gtfs_stop_pk
              WHERE ps.feed_version_id=p.feed_version_id AND ps.pattern_id=p.id),'[]'::jsonb) AS stops
            FROM {$prefix}journey_patterns p
            JOIN {$prefix}routes r ON r.feed_version_id=p.feed_version_id AND r.id=p.route_id
            LEFT JOIN {$prefix}shapes s ON s.feed_version_id=p.feed_version_id AND s.id=p.shape_id
            LEFT JOIN ingest.feed_publications pub ON pub.active_feed_version_id=p.feed_version_id
            {$scope} ORDER BY p.feed_version_id,p.route_id,p.id", $preview ? [$id, $routeId] : [$id]);

        return array_map(function ($row) use ($preview) {
            $geometry = $row->geojson === null ? null : json_decode($row->geojson, true, 512, JSON_THROW_ON_ERROR);
            $stops = json_decode($row->stops, true, 512, JSON_THROW_ON_ERROR);
            $bbox = $geometry['bbox'] ?? null;
            if ($geometry !== null) {
                unset($geometry['bbox']);
            }

            return [
                'route_id' => (int) $row->route_id,
                'gtfs_route_id' => $row->gtfs_route_id,
                'pattern_id' => (int) $row->pattern_id,
                // The pattern itself identifies the variant; no arbitrary primary trip.
                'variant_id' => (int) $row->pattern_id,
                'gtfs_direction_id' => $row->gtfs_direction_id === null ? null : (int) $row->gtfs_direction_id,
                'headsign' => $row->headsign,
                // These are GTFS endpoints, not unverified SPTrans terminal labels.
                'origin' => $stops[0] ?? null,
                'destination' => $stops ? $stops[count($stops) - 1] : null,
                'shape_id' => $row->shape_id === null ? null : (int) $row->shape_id,
                'gtfs_shape_id' => $row->gtfs_shape_id,
                'geometry_status' => $row->geometry_status,
                'geometry' => $geometry,
                'bbox' => $bbox,
                'source' => 'gtfs',
                'feed_source_id' => (int) $row->feed_source_id,
                'feed_version_id' => (int) $row->feed_version_id,
                'publication_status' => $preview ? 'preview' : 'active',
                'matching_status' => $preview ? 'unverified' : 'confirmed',
                'official' => !$preview,
                'stops' => $stops,
            ];
        }, $rows);
    }
}
