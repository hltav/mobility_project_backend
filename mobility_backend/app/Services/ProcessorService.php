<?php

namespace App\Services;

use Illuminate\Support\Facades\Http;
use Illuminate\Support\Facades\Log;

/**
 * Proxy para o microserviço Go (mobility-processor).
 * Mantém o Laravel como camada de API e delega processamento ao Go.
 */
class ProcessorService
{
    private string $baseUrl;

    public function __construct()
    {
        $this->baseUrl = rtrim(config('services.processor.url', 'http://localhost:8080'), '/');
    }

    public function getBusPositions(int $sptransId): array
    {
        try {
            $response = Http::timeout(5)->get("{$this->baseUrl}/buses/{$sptransId}");
            $this->logFailedResponse('getBusPositions', $response, ['sptrans_id' => $sptransId]);
            return $response->json('buses', []);
        } catch (\Throwable $e) {
            Log::error('[Processor] getBusPositions falhou', ['error' => $e->getMessage()]);
            return [];
        }
    }

    /** @return array{lat: float, lng: float}|null */
    public function geocode(string $address): ?array
    {
        try {
            $response = Http::timeout(6)->post("{$this->baseUrl}/geocode", [
                'address' => $address,
            ]);

            $this->logFailedResponse('geocode', $response, ['address' => $address]);
            return $response->json('coords');
        } catch (\Throwable $e) {
            Log::error('[Processor] geocode falhou', ['error' => $e->getMessage()]);
            return null;
        }
    }

    public function reverse(float $lat, float $lng): ?string
    {
        try {
            $response = Http::timeout(6)->post("{$this->baseUrl}/reverse", [
                'lat' => $lat,
                'lng' => $lng,
            ]);

            $this->logFailedResponse('reverse', $response, ['lat' => $lat, 'lng' => $lng]);
            return $response->json('address');
        } catch (\Throwable $e) {
            Log::error('[Processor] reverse falhou', ['error' => $e->getMessage()]);
            return null;
        }
    }

    public function findNearby(string $address, array $lines, float $radiusKm = 1.0): array
    {
        try {
            $response = Http::timeout(10)->post("{$this->baseUrl}/nearby", [
                'address'   => $address,
                'radius_km' => $radiusKm,
                'lines'     => $lines,
            ]);

            $this->logFailedResponse('findNearby', $response, ['address' => $address, 'radius_km' => $radiusKm]);
            return $response->json() ?? ['origin' => null, 'lines' => []];
        } catch (\Throwable $e) {
            Log::error('[Processor] findNearby falhou', ['error' => $e->getMessage()]);
            return ['origin' => null, 'lines' => []];
        }
    }

    /** @param string[] $terms */
    public function syncLines(array $terms): array
    {
        try {
            $response = Http::timeout(60)->post("{$this->baseUrl}/sync", [
                'terms' => $terms,
            ]);

            $this->logFailedResponse('syncLines', $response, ['terms' => $terms]);
            return $response->json('lines', []);
        } catch (\Throwable $e) {
            Log::error('[Processor] syncLines falhou', ['error' => $e->getMessage()]);
            return [];
        }
    }

    /**
     * Ordena e filtra pontos por distância no processor.
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

            $this->logFailedResponse('sortByDistance', $response, ['lat' => $lat, 'lng' => $lng, 'radius_km' => $radiusKm]);
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

            $this->logFailedResponse('findNearbyInGo', $response, ['address' => $address, 'radius_km' => $radiusKm]);
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
            $response = Http::timeout(30)->post("{$this->baseUrl}/sync/stops", [
                'sptrans_line_id' => $sptransLineId,
            ]);
            $this->logFailedResponse('syncStopsForLine', $response, ['sptrans_line_id' => $sptransLineId]);
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

            $this->logFailedResponse('resolveLineId', $response, ['code' => $lineCode, 'direction' => $direction]);
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
            $response = Http::timeout(10)->post("{$this->baseUrl}/stops/nearby", [
                'lat' => $lat,
                'lng' => $lng,
                'radius_km' => $radiusKm,
                'limit' => $limit,
                'only_with_lines' => $onlyWithLines,
            ]);
            $this->logFailedResponse('getNearestStops', $response, [
                'lat' => $lat,
                'lng' => $lng,
                'radius_km' => $radiusKm,
                'limit' => $limit,
                'only_with_lines' => $onlyWithLines,
            ]);
            return $response->json('stops', []);
        } catch (\Throwable $e) {
            Log::error('[Processor] getNearestStops falhou', ['error' => $e->getMessage()]);
            return [];
        }
    }

    private function logFailedResponse(string $operation, \Illuminate\Http\Client\Response $response, array $context = []): void
    {
        if (!$response->failed()) {
            return;
        }

        Log::error("[Processor] {$operation} retornou HTTP inválido", $context + [
            'status' => $response->status(),
            'body' => $response->body(),
            'base_url' => $this->baseUrl,
        ]);
    }
}
