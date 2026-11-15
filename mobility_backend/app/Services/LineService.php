<?php

// namespace App\Services;

// use App\DTOs\BusDTO;
// use App\DTOs\LineDTO;
// use App\Models\Line;
// use App\Repositories\LineRepository;
// use Illuminate\Pagination\LengthAwarePaginator;
// use App\Services\ProcessorService;
// use App\Services\ValkeyService;

// class LineService
// {
//     public function __construct(
//         private readonly LineRepository   $lineRepository,
//         private readonly ProcessorService $processorService,
//         private readonly ValkeyService    $valkeyService,
//     ) {}

//     /**
//      * Busca linhas no banco local com suporte a filtro por termo.
//      */
//     public function search(array $filters): LengthAwarePaginator
//     {
//         $term    = $filters['term']     ?? null;
//         $perPage = $filters['per_page'] ?? 15;

//         return $this->lineRepository->search($term, $perPage);
//     }

//     /**
//      * Retorna detalhes de uma linha com posição atual dos ônibus via processor.
//      */
//     public function findWithBuses(int $id): ?array
//     {
//         $line = $this->lineRepository->findById($id);

//         if (!$line) {
//             return null;
//         }

//         $buses = $this->getBusPositions($id);

//         return [
//             'line'  => LineDTO::fromModel($line)->toArray(),
//             'buses' => array_map(fn(BusDTO $dto) => $dto->toArray(), $buses),
//         ];
//     }

//     /** @return BusDTO[] */
//     public function getBusPositions(int $lineId): array
//     {
//         $line = $this->lineRepository->findById($lineId);

//         if (!$line) {
//             return [];
//         }

//         $rawBuses = $this->processorService->getBusPositions($line->sptrans_id);

//         return array_map(fn($bus) => BusDTO::fromArray((array)$bus), $rawBuses);
//     }

//     // public function stopsForLine(int $lineId, ?int $direction = null): ?array
//     // {
//     //     $line = $this->lineRepository->findById($lineId);

//     //     if (!$line) {
//     //         return null;
//     //     }

//     //     $sptransLineId = $this->resolveSptransLineId($line, $direction);

//     //     // O Go agora é responsável por sincronizar as paradas desta linha.
//     //     $this->processorService->syncStopsForLine($sptransLineId);

//     //     // O Laravel agora apenas consulta o banco local (já atualizado pelo Go).
//     //     $stops = $line->busStops()
//     //         ->get()
//     //         ->map(fn($stop, $index) => [
//     //             'id'         => $stop->id,
//     //             'sptrans_id' => $stop->sptrans_code,
//     //             'name'       => $stop->name,
//     //             'address'    => $stop->address,
//     //             'lat'        => $stop->latitude,
//     //             'lng'        => $stop->longitude,
//     //             'order'      => $index + 1,
//     //         ])->toArray();

//     //     return [
//     //         'line' => [
//     //             'id' => $line->id,
//     //             'code' => $line->code,
//     //             'name' => $line->name,
//     //             'sptrans_id' => $sptransLineId,
//     //             'direction' => $direction,
//     //         ],
//     //         'stops' => $stops,
//     //     ];
//     // }

//     // private function resolveSptransLineId(Line $line, ?int $direction): int
//     // {
//     //     if ($direction === null) {
//     //         return $line->sptrans_id;
//     //     }

//     //     $resolvedId = $this->processorService->resolveLineId($line->code, $direction);
//     //     return $resolvedId > 0 ? $resolvedId : $line->sptrans_id;
//     // }

//     // public function syncByTerm(string $term): void
//     // {
//     //     // Notifica o Go para realizar a sincronização em batch.
//     //     $this->processorService->syncLines([$term]);
//     // }

//     // public function searchFromApi(string $term): array
//     // {
//     //     // Busca direta na API (Proxy para o Go ou consulta direta se for simples)
//     //     $raw = $this->processorService->syncLines([$term]);

//     //     return array_map(fn($line) => LineDTO::fromSptrans($line)->toArray(), $raw);
//     // }

//     // /** @return array{origin: array{address: string, lat: float, lng: float}|null, lines: array} */
//     // public function findNearby(string $address, float $radiusKm = 1.0): array
//     // {
//     //     return $this->processorService->findNearbyInGo($address, $radiusKm);
//     // }
//     public function stopsForLine(int $lineId, ?int $direction = null): ?array
//     {
//         $line = $this->lineRepository->findById($lineId);

//         if (!$line) {
//             return null;
//         }

//         $dir = $direction ?? 0;

//         // Resolver sptrans_id correto para a direção
//         $sptransId = $this->valkeyService->resolveSptransId($line->code, $dir)
//             ?? $this->resolveSptransLineId($line, $direction);

//         // Tentar Valkey primeiro
//         $stops = $this->valkeyService->getStopsForLine($sptransId, $dir);

//         if ($stops !== null) {
//             return [
//                 'line' => [
//                     'id'         => $line->id,
//                     'code'       => $line->code,
//                     'name'       => $line->name,
//                     'sptrans_id' => $sptransId,
//                     'direction'  => $dir,
//                 ],
//                 'stops'  => $stops,
//                 'source' => 'valkey',
//             ];
//         }

//         // Fallback: Go + banco local
//         $this->processorService->syncStopsForLine($sptransId);

//         $stops = $line->busStops()
//             ->get()
//             ->map(fn($stop, $index) => [
//                 'sptrans_code' => $stop->sptrans_code,
//                 'name'         => $stop->name,
//                 'address'      => $stop->address,
//                 'lat'          => $stop->latitude,
//                 'lng'          => $stop->longitude,
//                 'updated_at'   => $stop->updated_at,
//             ])->toArray();

//         return [
//             'line' => [
//                 'id'         => $line->id,
//                 'code'       => $line->code,
//                 'name'       => $line->name,
//                 'sptrans_id' => $sptransId,
//                 'direction'  => $dir,
//             ],
//             'stops'  => $stops,
//             'source' => 'database',
//         ];
//     }

//     private function resolveSptransLineId(Line $line, ?int $direction): int
//     {
//         if ($direction === null) {
//             return $line->sptrans_id;
//         }

//         $resolvedId = $this->processorService->resolveLineId($line->code, $direction);
//         return $resolvedId > 0 ? $resolvedId : $line->sptrans_id;
//     }
// }


namespace App\Services;

use App\DTOs\BusDTO;
use App\DTOs\LineDTO;
use App\Models\Line;
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

        $dir = $direction ?? 0;

        $sptransId = $this->valkeyService->resolveSptransId($line->code, $dir)
            ?? $this->resolveSptransLineId($line, $direction);

        $stops = $this->valkeyService->getStopsForLine($sptransId, $dir);

        if ($stops !== null) {
            return [
                'line' => [
                    'id'         => $line->id,
                    'code'       => $line->code,
                    'name'       => $line->name,
                    'sptrans_id' => $sptransId,
                    'direction'  => $dir,
                ],
                'stops'  => $stops,
                'source' => 'valkey',
            ];
        }

        // Fallback: aciona o Go para sincronizar e lê do banco local
        $this->processorService->syncStopsForLine($sptransId);

        $stops = $line->busStops()
            ->get()
            ->map(fn($stop) => [
                'sptrans_code' => $stop->sptrans_code,
                'name'         => $stop->name,
                'address'      => $stop->address,
                'lat'          => $stop->latitude,
                'lng'          => $stop->longitude,
                'updated_at'   => $stop->updated_at,
            ])->toArray();

        return [
            'line' => [
                'id'         => $line->id,
                'code'       => $line->code,
                'name'       => $line->name,
                'sptrans_id' => $sptransId,
                'direction'  => $dir,
            ],
            'stops'  => $stops,
            'source' => 'database',
        ];
    }

    private function resolveSptransLineId(Line $line, ?int $direction): int
    {
        if ($direction === null) {
            return $line->sptrans_id;
        }

        $resolvedId = $this->processorService->resolveLineId($line->code, $direction);
        return $resolvedId > 0 ? $resolvedId : $line->sptrans_id;
    }
}
