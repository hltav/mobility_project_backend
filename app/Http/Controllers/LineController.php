<?php

namespace App\Http\Controllers;

use App\Http\Requests\Line\SearchLineRequest;
use App\Services\LineService;
use Illuminate\Http\JsonResponse;

class LineController extends Controller
{
    public function __construct(
        private readonly LineService $lineService
    ) {}

    /**
     * Lista todas as linhas, com suporte a busca por termo.
     */
    public function index(SearchLineRequest $request): JsonResponse
    {
        $lines = $this->lineService->search($request->validated());

        return response()->json([
            'message' => 'Linhas recuperadas com sucesso.',
            'data'    => $lines,
        ]);
    }

    /**
     * Exibe detalhes de uma linha específica, incluindo
     * posição atual dos ônibus via SPTrans.
     */
    public function show(int $id): JsonResponse
    {
        $line = $this->lineService->findWithBuses($id);

        if (!$line) {
            return response()->json([
                'message' => 'Linha não encontrada.',
            ], 404);
        }

        return response()->json([
            'message' => 'Linha recuperada com sucesso.',
            'data'    => $line,
        ]);
    }
}
