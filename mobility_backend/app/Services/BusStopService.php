<?php

namespace App\Services;

use App\Models\BusStop;
use App\Repositories\LineRepository;
use App\Repositories\OperationalStopRepository;
use Illuminate\Support\Collection;

class BusStopService
{
    public function __construct(
        private readonly ProcessorService $processorService,
        private readonly LineRepository $lines,
        private readonly OperationalStopRepository $operationalStops,
    ) {}

    public function selectOperational(array $input): array
    {
        $result = ['contract_version' => 1, 'status' => 'unavailable', 'reason' => 'operational_line_not_found',
            'selection_mode' => isset($input['stop_id']) ? 'manual' : 'gps', 'line' => null, 'stops' => []];
        $line = $this->lines->findById((int) $input['operational_line_id']);
        if (!$line) {
            return $result;
        }
        if ($line->provider !== 'sptrans' || !$line->sptrans_id || !in_array((int) $line->sptrans_direction, [1, 2], true)) {
            return array_replace($result, ['reason' => 'operational_identity_unconfirmed']);
        }
        if ((isset($input['sptrans_line_id']) && (int) $input['sptrans_line_id'] !== (int) $line->sptrans_id)
            || (isset($input['sptrans_direction']) && (int) $input['sptrans_direction'] !== (int) $line->sptrans_direction)) {
            return array_replace($result, ['reason' => 'operational_identity_mismatch']);
        }
        $identity = ['operational_line_id' => (int) $line->id, 'sptrans_line_id' => (int) $line->sptrans_id,
            'sptrans_direction' => (int) $line->sptrans_direction];
        $selection = $this->operationalStops->select((int) $line->id, (int) $line->sptrans_id,
            isset($input['lat']) ? (float) $input['lat'] : null, isset($input['lng']) ? (float) $input['lng'] : null,
            isset($input['stop_id']) ? (int) $input['stop_id'] : null, (float) ($input['radius_km'] ?? 2), (int) ($input['limit'] ?? 5));
        $selection['stops'] = array_map(fn($stop) => $stop + $identity, $selection['stops']);
        return array_replace($result, $selection, ['line' => $identity]);
    }

    public function nearestFromCoordinates(
        float $lat,
        float $lng,
        int $limit = 1,
        float $radiusKm = 2.0,
        bool $onlyWithLines = false,
    ): Collection {
        $stopsData = $this->processorService->getNearestStops(
            $lat,
            $lng,
            $radiusKm,
            $limit,
            $onlyWithLines
        );

        return collect($stopsData);
    }

    public function nearestFromAddress(
        string $address,
        int $limit = 1,
        float $radiusKm = 2.0,
        bool $onlyWithLines = false,
    ): array {
        $coords = $this->processorService->geocode($address);

        if (!$coords) {
            return ['point' => null, 'stops' => collect()];
        }

        return [
            'point' => array_merge(['address' => $address], $coords),
            'stops' => $this->nearestFromCoordinates($coords['lat'], $coords['lng'], $limit, $radiusKm, $onlyWithLines),
        ];
    }
}
