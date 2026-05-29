<?php

namespace App\Services;

use App\Models\BusStop;
use Illuminate\Support\Collection;

class BusStopService
{
    public function __construct(
        private readonly ProcessorService $processorService,
    ) {}

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
