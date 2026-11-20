<?php

namespace App\Services;

use App\DTOs\BusDTO;
use App\DTOs\LineDTO;
use App\Repositories\LineRepository;
use Illuminate\Pagination\LengthAwarePaginator;

class LineService
{
    public function __construct(
        private readonly LineRepository   $lineRepository,
        private readonly ProcessorService $processorService,
        private readonly ValkeyService    $valkeyService,
    ) {}

    public function search(array $filters): LengthAwarePaginator
    {
        $term    = $filters['term']     ?? null;
        $perPage = $filters['per_page'] ?? 15;

        return $this->lineRepository->search($term, $perPage);
    }

    public function findWithBuses(int $id): ?array
    {
        $line = $this->lineRepository->findById($id);

        if (!$line) {
            return null;
        }

        $buses = $this->getBusPositions($id);

        return [
            'line'  => LineDTO::fromModel($line)->toArray(),
            'buses' => array_map(fn(BusDTO $dto) => $dto->toArray(), $buses),
        ];
    }

    /** @return BusDTO[] */
    public function getBusPositions(int $lineId): array
    {
        $line = $this->lineRepository->findById($lineId);

        if (!$line) {
            return [];
        }

        $rawBuses = $this->processorService->getBusPositions($line->sptrans_id);

        return array_map(fn($bus) => BusDTO::fromArray((array) $bus), $rawBuses);
    }

    public function stopsForLine(int $lineId, ?int $direction = null): ?array
    {
        $line = $this->lineRepository->findById($lineId);

        if (!$line) {
            return null;
        }

        // Resolve the operational record locally; a read must never synchronize SPTrans.
        $requestedLine = $line;
        $dir = $direction ?? ($line->sptrans_direction === null ? 0 : (int) $line->sptrans_direction);
        if ($direction !== null && (int) $line->sptrans_direction !== $direction) {
            $line = $this->lineRepository->findByCodeAndDirection($line->code, $direction);
        }

        // Old Valkey entries have neither publication nor pattern provenance.
        // Keep the legacy payload, but never present this unordered association as GTFS.
        $stops = $line ? $line->busStops()->get()->map(fn($stop) => [
            'sptrans_code' => $stop->sptrans_code,
            'name' => $stop->name,
            'address' => $stop->address,
            'lat' => $stop->latitude,
            'lng' => $stop->longitude,
            'updated_at' => $stop->updated_at,
        ])->toArray() : [];

        return [
            'line' => [
                'id' => $requestedLine->id,
                'code' => $requestedLine->code,
                'name' => $requestedLine->name,
                'sptrans_id' => $line?->sptrans_id,
                'resolved_line_id' => $line?->id,
                'direction' => $dir,
            ],
            'stops' => $stops,
            'source' => 'database',
            'status' => $line ? 'legacy_unordered' : 'direction_unavailable',
            'official' => false,
            'ordered' => false,
        ];
    }
}
