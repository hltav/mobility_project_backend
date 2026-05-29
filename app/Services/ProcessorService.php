<?php

namespace App\Services;

use Illuminate\Support\Facades\Http;
use Illuminate\Support\Facades\Log;

/**
 * Proxy para o microserviço Go (mobility-processor).
 * Substitui chamadas diretas ao ExternalApiService, GeocodingService e HaversineService.
 */
class ProcessorService
{
    private string $baseUrl;

    public function __construct()
    {
        $this->baseUrl = rtrim(config('services.processor.url', 'http://localhost:8080'), '/');
    }

    /**
     * Posições dos ônibus em tempo real — equivale a ExternalApiService::getBusPositionsByLine
     */
    public function getBusPositions(int $sptransId): array
    {
        try {
            $response = Http::timeout(5)->get("{$this->baseUrl}/buses/{$sptransId}");
            return $response->json('buses', []);
        } catch (\Throwable $e) {
            Log::error('[Processor] getBusPositions falhou', ['error' => $e->getMessage()]);
            return [];
        }
    }

    /**
     * Geocodificação — equivale a GeocodingService::geocode
     *
     * @return array{lat: float, lng: float}|null
     */
    public function geocode(string $address): ?array
    {
        try {
            $response = Http::timeout(6)->post("{$this->baseUrl}/geocode", [
                'address' => $address,
            ]);

            return $response->json('coords');
        } catch (\Throwable $e) {
            Log::error('[Processor] geocode falhou', ['error' => $e->getMessage()]);
            return null;
        }
    }

    /**
     * Reverse geocoding — equivale a GeocodingService::reverse
     */
    public function reverse(float $lat, float $lng): ?string
    {
        try {
            $response = Http::timeout(6)->post("{$this->baseUrl}/reverse", [
                'lat' => $lat,
                'lng' => $lng,
            ]);

            return $response->json('address');
        } catch (\Throwable $e) {
            Log::error('[Processor] reverse falhou', ['error' => $e->getMessage()]);
            return null;
        }
    }

    /**
     * Linhas próximas — equivale a GeocodingService::geocode + HaversineService::filterByRadius
     */
    public function findNearby(string $address, array $lines, float $radiusKm = 1.0): array
    {
        try {
            $response = Http::timeout(10)->post("{$this->baseUrl}/nearby", [
                'address'   => $address,
                'radius_km' => $radiusKm,
                'lines'     => $lines,
            ]);

            return $response->json() ?? ['origin' => null, 'lines' => []];
        } catch (\Throwable $e) {
            Log::error('[Processor] findNearby falhou', ['error' => $e->getMessage()]);
            return ['origin' => null, 'lines' => []];
        }
    }

    /**
     * Sincroniza linhas com a SPTrans em paralelo via Go.
     * Retorna os dados brutos para o Laravel persistir.
     *
     * @param  string[] $terms
     * @return array[]
     */
    public function syncLines(array $terms): array
    {
        try {
            $response = Http::timeout(60)->post("{$this->baseUrl}/sync", [
                'terms' => $terms,
            ]);

            return $response->json('lines', []);
        } catch (\Throwable $e) {
            Log::error('[Processor] syncLines falhou', ['error' => $e->getMessage()]);
            return [];
        }
    }

    /**
     * Ordena e filtra pontos por distância — substitui a lógica local do HaversineService.
     */
    public function sortByDistance(float $lat, float $lng, array $points, float $radiusKm = 2.0): array
    {
        try {
            $response = Http::timeout(10)->post("{$this->baseUrl}/distance/sort", [
                'lat'       => $lat,
                'lng'       => $lng,
                'radius_km' => $radiusKm,
                'points'    => $points,
            ]);

            return $response->json('points', []);
        } catch (\Throwable $e) {
            Log::error('[Processor] sortByDistance falhou', ['error' => $e->getMessage()]);
            return [];
        }
    }

    /**
     * Busca linhas próximas via consulta espacial realizada inteiramente no Go.
     * O Go consulta o banco/index e retorna o resultado final sem que o PHP precise processar.
     */
    public function findNearbyInGo(string $address, float $radiusKm = 1.0): array
    {
        try {
            $response = Http::timeout(15)->post("{$this->baseUrl}/nearby/spatial", [
                'address'   => $address,
                'radius_km' => $radiusKm,
            ]);

            return $response->json() ?? ['origin' => null, 'lines' => []];
        } catch (\Throwable $e) {
            Log::error('[Processor] findNearbyInGo falhou', ['error' => $e->getMessage()]);
            return ['origin' => null, 'lines' => []];
        }
    }

    /**
     * Solicita ao Go a sincronização das paradas de uma linha específica.
     * O microserviço busca na SPTrans e atualiza o banco de dados.
     */
    public function syncStopsForLine(int $sptransLineId): void
    {
        try {
            // O endpoint /sync/stops no Go deve realizar o upsert das paradas e relações.
            Http::timeout(30)->post("{$this->baseUrl}/sync/stops", [
                'sptrans_line_id' => $sptransLineId,
            ]);
        } catch (\Throwable $e) {
            Log::error('[Processor] syncStopsForLine falhou', ['error' => $e->getMessage()]);
        }
    }

    /**
     * Resolve o ID da SPTrans para uma linha e sentido específicos via Go.
     */
    public function resolveLineId(string $lineCode, ?int $direction): int
    {
        try {
            $response = Http::timeout(5)->get("{$this->baseUrl}/lines/resolve", [
                'code'      => $lineCode,
                'direction' => $direction,
            ]);

            return (int) $response->json('sptrans_id', 0);
        } catch (\Throwable $e) {
            Log::error('[Processor] resolveLineId falhou', ['error' => $e->getMessage()]);
            return 0;
        }
    }

    /**
     * Busca pontos próximos delegando toda a lógica espacial para o Go.
     */
    public function getNearestStops(float $lat, float $lng, float $radiusKm, int $limit, bool $onlyWithLines): array
    {
        try {
            $response = Http::timeout(10)->post("{$this->baseUrl}/stops/nearby", compact('lat', 'lng', 'radiusKm', 'limit', 'onlyWithLines'));
            return $response->json('stops', []);
        } catch (\Throwable $e) {
            Log::error('[Processor] getNearestStops falhou', ['error' => $e->getMessage()]);
            return [];
        }
    }
}
