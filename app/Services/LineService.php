<?php

namespace App\Services;

use App\DTOs\BusDTO;
use App\DTOs\LineDTO;
use App\Repositories\LineRepository;
use App\Services\ExternalApiService;
use Illuminate\Pagination\LengthAwarePaginator;

class LineService
{
    public function __construct(
        private readonly LineRepository     $lineRepository,
        private readonly ExternalApiService $externalApiService,
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
     * @return BusDTO[]
     */
    public function getBusPositions(int $lineId): array
    {
        $line = $this->lineRepository->findById($lineId);

        if (!$line) {
            return [];
        }

        $rawBuses = $this->externalApiService->getBusPositionsByLine($line->sptrans_id);

        return array_map(fn($bus) => BusDTO::fromArray($bus), $rawBuses);
    }

    /**
     * Sincroniza as linhas do banco local com os dados mais recentes
     * da SPTrans. Busca os dados brutos, transforma em DTO e persiste.
     */

    public function syncLinesWithSptrans(): void
    {
        $this->externalApiService->authenticate();

        // Tente buscar por 'a' em vez de vazio para forçar o retorno de dados
        $rawLines = $this->externalApiService->searchLines('8000');

        if (empty($rawLines)) {
            \Illuminate\Support\Facades\Log::warning('[Sync] Nenhuma linha retornada da SPTrans.');
            return;
        }

        foreach ($rawLines as $raw) {
            $dto = LineDTO::fromSptrans($raw);
            $this->lineRepository->upsert($dto->toArray());
        }
    }

    public function syncByTerm(string $term): void
    {
        $rawLines = $this->externalApiService->searchLines($term);

        foreach ($rawLines as $raw) {
            // LineDTO::fromSptrans mapeia os campos 'cl', 'lt', 'tp', 'ts' corretamente
            $dto = LineDTO::fromSptrans($raw);

            // O Repository usa updateOrCreate pelo 'sptrans_id',
            // então não haverá duplicatas mesmo buscando '1', depois '10', etc.
            $this->lineRepository->upsert($dto->toArray());
        }
    }

    public function searchFromApi(string $term): array
    {
        $raw = $this->externalApiService->searchLines($term);

        return array_map(fn($line) => LineDTO::fromSptrans($line)->toArray(), $raw);
    }
}
