<?php

namespace App\Http\Controllers;

use App\Services\LineService;
use Illuminate\Http\JsonResponse;

class BusController extends Controller
{
    public function __construct(
        private readonly LineService $lineService
    ) {}

    /**
     * Retorna a posição em tempo real dos ônibus de uma linha.
     * Consome diretamente a SPTrans via ExternalApiService.
     */
    public function positions(int $lineId): JsonResponse
    {
        $buses = $this->lineService->getBusPositions($lineId);

        return response()->json([
            'message' => 'Posições recuperadas com sucesso.',
            'data'    => $buses,
        ]);
    }
}
