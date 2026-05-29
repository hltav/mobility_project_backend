<?php

namespace App\Services;

use App\DTOs\BusDTO;
use App\DTOs\LineDTO;
use App\Models\Line;
use App\Repositories\LineRepository;
use Illuminate\Pagination\LengthAwarePaginator;
use App\Services\ProcessorService;

class LineService
{
    public function __construct(
        private readonly LineRepository   $lineRepository,
        private readonly ProcessorService $processorService,
    ) {}

    /**
     * Busca linhas no banco local com suporte a filtro por termo.
     */
    public function search(array $filters): LengthAwarePaginator
    {
        $term    = $filters['term']     ?? null;
        $perPage = $filters['per_page'] ?? 15;

        return $this->lineRepository->search($term, $perPage);
    }

    /**
     * Retorna detalhes de uma linha com posição atual dos ônibus
     * buscada em tempo real na API da SPTrans.
     */
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

    /**
     * Busca posições em tempo real via SPTrans.
     * Autentica, consulta posição por código da linha.
     *
     * @todo Migrar para o serviço em Go para melhor performance em
     * requisições simultâneas e pooling de autenticação.
     *
     * @return BusDTO[]
     */
    public function getBusPositions(int $lineId): array
    {
        $line = $this->lineRepository->findById($lineId);

        if (!$line) {
            return [];
        }

        // Atualmente chama o ProcessorService (PHP), será substituído por chamada ao Go.
        $rawBuses = $this->processorService->getBusPositions($line->sptrans_id);

        return array_map(fn($bus) => BusDTO::fromArray((array)$bus), $rawBuses);
    }

    public function stopsForLine(int $lineId, ?int $direction = null): ?array
    {
        $line = $this->lineRepository->findById($lineId);

        if (!$line) {
            return null;
        }

        $sptransLineId = $this->resolveSptransLineId($line, $direction);

        // O Go agora é responsável por sincronizar as paradas desta linha.
        $this->processorService->syncStopsForLine($sptransLineId);

        // O Laravel agora apenas consulta o banco local (já atualizado pelo Go).
        $stops = $line->busStops()
            ->get()
            ->map(fn($stop, $index) => [
                'id'         => $stop->id,
                'sptrans_id' => $stop->sptrans_code,
                'name'       => $stop->name,
                'address'    => $stop->address,
                'lat'        => $stop->latitude,
                'lng'        => $stop->longitude,
                'order'      => $index + 1,
            ])->toArray();

        return [
            'line' => [
                'id' => $line->id,
                'code' => $line->code,
                'name' => $line->name,
                'sptrans_id' => $sptransLineId,
                'direction' => $direction,
            ],
            'stops' => $stops,
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

    public function syncByTerm(string $term): void
    {
        // Notifica o Go para realizar a sincronização em batch.
        $this->processorService->syncLines([$term]);
    }

    public function searchFromApi(string $term): array
    {
        // Busca direta na API (Proxy para o Go ou consulta direta se for simples)
        $raw = $this->processorService->syncLines([$term]);

        return array_map(fn($line) => LineDTO::fromSptrans($line)->toArray(), $raw);
    }

    /**
     * DEPRECATED: Migrando para Go.
     *
     * @return array{
     *   origin: array{address: string, lat: float, lng: float}|null,
     *   lines: array
     * }
     */
    public function findNearby(string $address, float $radiusKm = 1.0): array
    {
        /**
         * O cálculo de Haversine em PHP com Collection::get() é lento.
         * O Go processará isso usando PostGIS ou cálculos nativos
         * muito mais rápidos.
         */
        return $this->processorService->findNearbyInGo($address, $radiusKm);
    }
}
