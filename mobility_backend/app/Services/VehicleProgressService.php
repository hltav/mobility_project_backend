<?php

namespace App\Services;

use App\Models\Line;
use App\Repositories\VehicleProgressRepository;
use App\Repositories\OperationalStopRepository;
use Carbon\CarbonImmutable;

class VehicleProgressService
{
    public function __construct(private readonly ValkeyService $cache, private readonly VehicleProgressRepository $progress) {}

    public function forLine(Line $line, array $patterns, bool $preview = false): array
    {
        $snapshot = $this->cache->getVehicleSnapshot((int) $line->sptrans_id);
        $vehicles = [];
        $observations = [];
        $prefixes = array_count_values(array_map(fn($v) => (string) ($v['prefix'] ?? ''), $snapshot ?? []));
        foreach ($snapshot ?? [] as $n => $raw) {
            $v = [
                'vehicle_prefix' => $raw['prefix'] ?? null, 'operational_line_id' => $line->id,
                'sptrans_line_id' => $line->sptrans_id,
                'sptrans_direction' => $line->sptrans_direction === null ? null : (int) $line->sptrans_direction,
                'observed_at' => null, 'feed_version_id' => null, 'pattern_id' => null, 'gtfs_direction_id' => null,
                'progress_fraction' => null, 'lateral_distance_m' => null, 'projected_position' => null, 'shape_segment_index' => null,
                'previous_stop' => null, 'next_stop' => null, 'remaining_distance_m' => null, 'upcoming_stops' => null,
                'match_status' => 'unavailable', 'confidence_reason' => 'observation_invalid',
                'passage_status' => 'not_observed', 'stop_status' => 'candidate_only',
                'eta_minutes' => null, 'eta' => OfficialPredictionEta::unavailable($preview
                    ? 'preview_pattern_operational_link_unconfirmed' : 'confirmed_pattern_stop_cp_link_missing'),
            ];
            $stamp = $raw['captured_at'] ?? '';
            try {
                if (!is_string($stamp) || !preg_match('/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/', $stamp)) {
                    throw new \InvalidArgumentException;
                }
                $observed = CarbonImmutable::parse($stamp);
                if ($observed->format('Y-m-d\TH:i:s') !== substr($stamp, 0, 19)) {
                    throw new \InvalidArgumentException;
                }
                $v['observed_at'] = $observed->toIso8601String();
                $age = CarbonImmutable::now('UTC')->getTimestamp() - $observed->getTimestamp();
                if ($age > config('gtfs.vehicle_max_age_seconds', 120)) {
                    $v['match_status'] = 'stale';
                    $v['confidence_reason'] = 'gps_observation_expired';
                } elseif ($age < -30) {
                    $v['confidence_reason'] = 'gps_observation_in_future';
                } elseif (empty($raw['prefix']) || !is_numeric($raw['lat'] ?? null) || !is_numeric($raw['lng'] ?? null)
                    || !is_finite((float) $raw['lat']) || !is_finite((float) $raw['lng'])
                    || abs((float) $raw['lat']) > 90 || abs((float) $raw['lng']) > 180) {
                    $v['confidence_reason'] = 'gps_coordinates_or_identity_invalid';
                } elseif (($prefixes[(string) $raw['prefix']] ?? 0) > 1) {
                    $v['match_status'] = 'ambiguous';
                    $v['confidence_reason'] = 'duplicate_vehicle_observation';
                } elseif (!$patterns) {
                    $v['confidence_reason'] = $preview ? 'ready_pattern_not_found' : 'no_active_confirmed_pattern';
                } elseif (count($patterns) > 1) {
                    $v['match_status'] = 'ambiguous';
                    $v['confidence_reason'] = 'multiple_confirmed_patterns';
                } else {
                    $v['confidence_reason'] = 'pattern_geometry_unavailable';
                    $observations[] = ['n' => $n, 'lat' => (float) $raw['lat'], 'lng' => (float) $raw['lng']];
                }
            } catch (\Throwable $e) {
                // Unknown observation time cannot be replaced by request/cache time.
            }
            $vehicles[$n] = $v;
        }
        $ids = array_map(fn($p) => ['feed_version_id' => $p['feed_version_id'], 'pattern_id' => $p['pattern_id']], $patterns);
        foreach ($this->progress->project($ids, $observations, $preview) as $row) {
            $n = (int) $row['n'];
            $vehicles[$n] = array_merge($vehicles[$n], $this->interpret($row, $preview));
        }
        $vehicles = $this->withOfficialEta($line, $patterns, $vehicles, $preview);

        return ['contract_version' => 1, 'mode' => $preview ? 'preview' : 'operational', 'source' => 'processor_snapshot',
            'status' => $snapshot !== null && $patterns ? 'available' : 'unavailable',
            'reason' => $snapshot === null ? 'vehicle_snapshot_unavailable' : (!$patterns ? 'no_eligible_pattern' : null),
            'vehicles' => array_values($vehicles)];
    }

    private function withOfficialEta(Line $line, array $patterns, array $vehicles, bool $preview): array
    {
        // Preview spatial candidates never establish operational pattern identity.
        if ($preview || count($patterns)!==1 || $line->provider!=='sptrans'
            || (int)$line->sptrans_id<=0 || !in_array((int)$line->sptrans_direction,[1,2],true)
            || ($patterns[0]['matching_status']??null)!=='confirmed'
            || ($patterns[0]['publication_status']??null)!=='active') { return $vehicles; }
        $eligible=array_filter($vehicles,fn($v)=>$v['match_status']==='matched' && !empty($v['upcoming_stops']));
        if (!$eligible) { return $vehicles; }
        try {
            $cl=(int)$line->sptrans_id; $direction=(int)$line->sptrans_direction; $p=$patterns[0];
            $codes=app(OperationalStopRepository::class)->confirmedPatternCodes((int)$line->id,$cl,$direction,
                (int)$p['feed_version_id'],(int)$p['pattern_id']);
            if (!$codes) { return $vehicles; }
            $snapshot=$this->cache->getPredictionSnapshot($cl,$direction); // Once per request, never per stop/vehicle.
            $now=CarbonImmutable::now('UTC'); $eta=new OfficialPredictionEta;
            foreach ($eligible as $n=>$vehicle) {
                foreach ($vehicle['upcoming_stops'] as $i=>$stop) {
                    $code=$codes[$stop['stop_sequence']]??null;
                    $forecast=$code && $code['stop_id']===$stop['stop_id']
                        ? $eta->forStop($snapshot,$cl,$direction,(string)$vehicle['vehicle_prefix'],$code['cp'],$now)
                        : OfficialPredictionEta::unavailable('confirmed_stop_cp_or_occurrence_missing');
                    $vehicles[$n]['upcoming_stops'][$i]['eta']=$forecast;
                    $vehicles[$n]['upcoming_stops'][$i]['eta_minutes']=$forecast['eta_minutes'];
                }
                $first=$vehicles[$n]['upcoming_stops'][0]['eta'];
                $vehicles[$n]['eta']=$first; $vehicles[$n]['eta_minutes']=$first['eta_minutes'];
                $vehicles[$n]['next_stop']['eta']=$first;
                $vehicles[$n]['next_stop']['eta_minutes']=$first['eta_minutes'];
            }
        } catch (\Throwable $e) {
            // Reference/cache failures cannot change GPS, projection or distances.
            foreach ($eligible as $n=>$vehicle) {
                $vehicles[$n]['eta']=OfficialPredictionEta::unavailable('prediction_dependencies_unavailable');
                $vehicles[$n]['eta_minutes']=null;
            }
        }
        return $vehicles;
    }

    /** A single observation provides spatial candidates, never evidence of passage. */
    public function interpret(array $row, bool $preview): array
    {
        $result = ['feed_version_id' => (int) $row['feed_version_id'], 'pattern_id' => (int) $row['pattern_id'],
            'gtfs_direction_id' => $row['gtfs_direction_id'] === null ? null : (int) $row['gtfs_direction_id'],
            'lateral_distance_m' => (float) $row['lateral_m'],
            'match_status' => 'unavailable', 'confidence_reason' => 'gps_outside_shape_tolerance', 'upcoming_stops' => null];
        if ((float) $row['lateral_m'] > config('gtfs.vehicle_max_lateral_m', 100)) {
            return $result;
        }
        $ambiguous = (bool) $row['ambiguous'];
        $last = -INF;
        $lastSequence = null;
        foreach ($row['stops'] as $stop) {
            if (!is_numeric($stop['along_m'] ?? null) || !is_finite((float) $stop['along_m'])) {
                return array_merge($result, ['confidence_reason' => 'pattern_stops_or_geometry_unavailable']);
            }
            $sequence = (int) $stop['pattern_sequence'];
            $ambiguous = $ambiguous || $stop['ambiguous'] || (float) $stop['along_m'] < $last - 1
                || ($lastSequence !== null && $sequence <= $lastSequence);
            $last = (float) $stop['along_m'];
            $lastSequence = $sequence;
            if ((float) $stop['lateral_distance_m'] > 100) {
                return array_merge($result, ['confidence_reason' => 'gtfs_stop_outside_shape_tolerance']);
            }
        }
        if ($ambiguous) {
            return array_merge($result, ['match_status' => 'ambiguous', 'confidence_reason' => 'projection_or_stop_order_ambiguous']);
        }
        if (!$row['stops'] || count($row['stops']) !== (int) ($row['expected_stops'] ?? count($row['stops']))
            || !is_numeric($row['total_m'] ?? null) || !is_finite((float) $row['total_m']) || (float) $row['total_m'] <= 0
            || !is_numeric($row['along_m'] ?? null) || !is_finite((float) $row['along_m'])) {
            return array_merge($result, ['confidence_reason' => 'pattern_stops_or_geometry_unavailable']);
        }
        $previous = $next = null;
        $upcoming = [];
        $vehicleAlong = (float) $row['along_m'];
        $total = (float) $row['total_m'];
        foreach ($row['stops'] as $stop) {
            $stopAlong = (float) $stop['along_m'];
            // Reuse this pattern's accumulated geography metres and total; no new projection.
            $stop['progress_fraction'] = max(0, min(1, $stopAlong / $total));
            if ($stopAlong < $vehicleAlong - 1) {
                if ($next !== null) {
                    // Even sub-metre projection noise cannot reorder or skip an occurrence.
                    return array_merge($result, ['match_status' => 'ambiguous',
                        'confidence_reason' => 'stop_eligibility_conflicts_with_pattern_order']);
                }
                $previous = $stop;
            } else {
                // Keep the existing 1 m numerical tolerance and official occurrence order.
                $next ??= $stop;
                $upcoming[] = [
                    'stop_id' => $stop['gtfs_stop_id'] ?? null,
                    'stop_sequence' => (int) $stop['pattern_sequence'],
                    'stop_name' => $stop['name'] ?? null,
                    'distance_remaining_m' => max(0, $stopAlong - $vehicleAlong),
                    'progress_fraction' => $stop['progress_fraction'],
                ];
            }
        }
        return array_merge($result, [
            'progress_fraction' => max(0, min(1, (float) $row['along_m'] / (float) $row['total_m'])),
            'shape_segment_index' => isset($row['segment_index']) ? (int) $row['segment_index'] : null,
            'projected_position' => ['lat' => (float) $row['projected_lat'], 'lng' => (float) $row['projected_lng']],
            'previous_stop' => $previous, 'next_stop' => $next,
            'remaining_distance_m' => $upcoming[0]['distance_remaining_m'] ?? null,
            'upcoming_stops' => $upcoming,
            'match_status' => $preview ? 'provisional' : 'matched',
            'confidence_reason' => $preview ? 'explicit_pattern_unconfirmed_spatial_candidates_only' : 'confirmed_pattern_single_observation_candidates_only',
        ]);
    }
}
