<?php

namespace App\Services;

use GuzzleHttp\Client;
use GuzzleHttp\Cookie\CookieJar;
use GuzzleHttp\Cookie\SetCookie;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\Log;

class ExternalApiService
{
    private const BASE_URL    = 'http://api.olhovivo.sptrans.com.br/v2.1';
    private const SESSION_TTL = 3600;
    private const CACHE_TTL   = 30;

    private Client    $client;
    private CookieJar $jar;

    public function __construct()
    {
        $this->jar    = new CookieJar();
        $this->client = new Client(['cookies' => $this->jar]);
    }

    public function authenticate(): bool
    {
        $token = config('services.sptrans.token');

        try {
            $response = $this->client->post(self::BASE_URL . '/Login/Autenticar?token=' . $token, [
                'body'    => '',
                'headers' => ['Content-Length' => '0'],
            ]);

            $body = trim((string) $response->getBody());

            Log::info('[SPTrans] Auth response', [
                'status'  => $response->getStatusCode(),
                'body'    => $body,
                'cookies' => $this->jar->toArray(),
            ]);

            if ($body === 'true') {
                Cache::put('sptrans_cookie_jar', $this->jar->toArray(), self::SESSION_TTL);
                return true;
            }

            Log::error('[SPTrans] Auth retornou false');
        } catch (\Throwable $e) {
            Log::error('[SPTrans] Erro na autenticação', ['error' => $e->getMessage()]);
        }

        return false;
    }

    private function ensureAuthenticated(): void
    {
        $cached = Cache::get('sptrans_cookie_jar', []);

        if (empty($cached)) {
            $this->authenticate();
            return;
        }

        foreach ($cached as $cookieData) {
            $this->jar->setCookie(new SetCookie($cookieData));
        }
    }

    private function authenticatedGet(string $endpoint, array $query = []): array
    {
        $response = $this->client->get(self::BASE_URL . $endpoint, ['query' => $query]);
        return json_decode((string) $response->getBody(), true) ?? [];
    }

    public function getBusPositionsByLine(int $sptransLineId): array
    {
        $this->ensureAuthenticated();

        return Cache::remember("sptrans_positions_{$sptransLineId}", self::CACHE_TTL, function () use ($sptransLineId) {
            try {
                $data = $this->authenticatedGet('/Posicao/Linha', ['codigoLinha' => $sptransLineId]);
                return $this->mapBusPositions($data['vs'] ?? []);
            } catch (\Throwable $e) {
                Log::error('[SPTrans] Erro ao buscar posições', ['error' => $e->getMessage()]);
                return [];
            }
        });
    }

    public function searchLines(string $term): array
    {
        if (empty(trim($term))) return [];

        $this->ensureAuthenticated();

        return Cache::remember('sptrans_lines_' . md5($term), 600, function () use ($term) {
            try {
                return $this->authenticatedGet('/Linha/Buscar', ['termosBusca' => $term]);
            } catch (\Throwable $e) {
                Log::error('[SPTrans] Erro ao buscar linhas', ['error' => $e->getMessage()]);
                return [];
            }
        });
    }

    public function searchLinesBySentido(string $term, int $sentido = 1): array
    {
        if (empty(trim($term)) || !in_array($sentido, [1, 2])) return [];

        $this->ensureAuthenticated();

        return Cache::remember('sptrans_lines_sentido_' . md5($term . $sentido), 600, function () use ($term, $sentido) {
            try {
                return $this->authenticatedGet('/Linha/BuscarLinhaSentido', [
                    'termosBusca' => $term,
                    'sentido'     => $sentido,
                ]);
            } catch (\Throwable $e) {
                Log::error('[SPTrans] Erro ao buscar linhas por sentido', ['error' => $e->getMessage()]);
                return [];
            }
        });
    }

    public function getArrivalPrediction(int $codigoParada, int $codigoLinha): array
    {
        $this->ensureAuthenticated();

        try {
            return $this->authenticatedGet('/Previsao', [
                'codigoParada' => $codigoParada,
                'codigoLinha'  => $codigoLinha,
            ]);
        } catch (\Throwable $e) {
            Log::error('[SPTrans] Erro ao buscar previsão', ['error' => $e->getMessage()]);
            return [];
        }
    }

    public function getStopsByLine(int $codigoLinha): array
    {
        $this->ensureAuthenticated();

        return Cache::remember('sptrans_stops_' . $codigoLinha, 1800, function () use ($codigoLinha) {
            try {
                return $this->authenticatedGet('/Parada/BuscarParadasPorLinha', [
                    'codigoLinha' => $codigoLinha,
                ]);
            } catch (\Throwable $e) {
                Log::error('[SPTrans] Erro ao buscar paradas', ['error' => $e->getMessage()]);
                return [];
            }
        });
    }

    private function mapBusPositions(array $vehicles): array
    {
        return array_map(fn($v) => [
            'prefix'      => (string) ($v['p']  ?? ''),
            'accessible'  => (bool)   ($v['a']  ?? false),
            'captured_at' => $v['ta'] ?? null,
            'latitude'    => (float)  ($v['py'] ?? 0),
            'longitude'   => (float)  ($v['px'] ?? 0),
        ], $vehicles);
    }
}
