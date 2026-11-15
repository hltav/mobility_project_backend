<?php

namespace App\Services;

use Illuminate\Support\Facades\Redis;
use Illuminate\Support\Facades\Log;

/**
 * Lê dados diretamente do Valkey (cache compartilhado com o Go).
 *
 * Chaves relevantes:
 *   lines:id:<sptrans_id>            → CachedLine (JSON)
 *   lines:code:<code>:dir:<dir>      → sptrans_id (string)
 *   stops:line:<sptrans_id>:dir:<dir> → array de CachedStop (JSON)
 *   stops:gtfs:<stop_id>             → CachedStop do GTFS (JSON)
 *   stops:cp:<sptrans_code>          → CachedStop da SPTrans (JSON)
 */
class ValkeyService
{
    /**
     * Retorna as paradas de uma linha em uma direção específica.
     * Lê stops:line:<sptrans_id>:dir:<dir> gravado pelo importgtfslinestops.
     *
     * @return array[]|null  Array de CachedStop ou null se não encontrado
     */
    public function getStopsForLine(int $sptransId, int $direction = 0): ?array
    {
        $key = "stops:line:{$sptransId}:dir:{$direction}";

        try {
            $raw = Redis::get($key);

            if (!$raw) {
                return null;
            }

            return json_decode($raw, true);
        } catch (\Throwable $e) {
            Log::error('[Valkey] getStopsForLine falhou', [
                'key'   => $key,
                'error' => $e->getMessage(),
            ]);
            return null;
        }
    }

    /**
     * Resolve o sptrans_id de uma linha pelo código + direção.
     * Lê lines:code:<code>:dir:<dir>.
     */
    public function resolveSptransId(string $lineCode, int $direction = 0): ?int
    {
        $key = "lines:code:{$lineCode}:dir:{$direction}";

        try {
            $raw = Redis::get($key);

            if (!$raw) {
                return null;
            }

            $id = (int) json_decode($raw, true);
            return $id > 0 ? $id : null;
        } catch (\Throwable $e) {
            Log::error('[Valkey] resolveSptransId falhou', [
                'key'   => $key,
                'error' => $e->getMessage(),
            ]);
            return null;
        }
    }

    /**
     * Retorna os dados de uma linha pelo sptrans_id.
     * Lê lines:id:<sptrans_id>.
     */
    public function getLine(int $sptransId): ?array
    {
        $key = "lines:id:{$sptransId}";

        try {
            $raw = Redis::get($key);
            return $raw ? json_decode($raw, true) : null;
        } catch (\Throwable $e) {
            Log::error('[Valkey] getLine falhou', [
                'key'   => $key,
                'error' => $e->getMessage(),
            ]);
            return null;
        }
    }

    /**
     * Retorna os dados de uma parada pelo sptrans_code (CP).
     * Lê stops:cp:<sptrans_code>.
     */
    public function getStop(int $sptransCode): ?array
    {
        $key = "stops:cp:{$sptransCode}";

        try {
            $raw = Redis::get($key);
            return $raw ? json_decode($raw, true) : null;
        } catch (\Throwable $e) {
            Log::error('[Valkey] getStop falhou', [
                'key'   => $key,
                'error' => $e->getMessage(),
            ]);
            return null;
        }
    }
}
