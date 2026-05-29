<?php

namespace App\Http\Controllers;

use App\Http\Requests\Line\SearchLineRequest;
use App\Services\LineService;
use Illuminate\Http\JsonResponse;
use App\Models\Line;
use Illuminate\Http\Request;

class LineController extends Controller
{
    public function __construct(
        private readonly LineService $lineService
    ) {}

    /**
     * Lista todas as linhas do banco local (paginado).
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
     * Busca linhas diretamente na SPTrans em tempo real.
     * GET /api/lines/search?term=8000
     */
    public function search(SearchLineRequest $request): JsonResponse
    {
        $term  = $request->validated('term', '');
        $lines = $this->lineService->searchFromApi($term);

        return response()->json([
            'message' => 'Linhas recuperadas com sucesso.',
            'data'    => $lines,
        ]);
    }

    /**
     * Exibe detalhes de uma linha específica + ônibus em tempo real.
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

    public function stops(int $id, Request $request): JsonResponse
    {
        $validated = $request->validate([
            'direction' => ['nullable', 'integer', 'in:1,2'],
        ]);

        $result = $this->lineService->stopsForLine(
            $id,
            isset($validated['direction']) ? (int) $validated['direction'] : null,
        );

        if (!$result) {
            return response()->json([
                'message' => 'Linha não encontrada.',
            ], 404);
        }

        return response()->json([
            'message' => 'Paradas da linha recuperadas com sucesso.',
            'data' => $result,
        ]);
    }

    /**
     * Retorna linhas próximas ao endereço informado.
     * GET /api/lines/nearby?address=Av+Paulista&radius_km=1.0
     */
    public function nearby(SearchLineRequest $request): JsonResponse
    {
        $address  = $request->validated('address', '');
        $radius   = (float) $request->validated('radius_km', 1.0);

        if (empty($address)) {
            return response()->json([
                'message' => 'O campo address é obrigatório.',
            ], 422);
        }

        $result = $this->lineService->findNearby($address, $radius);

        if (is_null($result['origin'])) {
            return response()->json([
                'message' => 'Endereço não encontrado.',
                'data'    => ['origin' => null, 'lines' => []],
            ], 404);
        }

        return response()->json([
            'message' => 'Linhas próximas recuperadas com sucesso.',
            'data'    => $result,
        ]);
    }

    public function withoutCoordinates(): JsonResponse
    {
        $lines = Line::whereNull('origin_lat')
            ->select('id', 'origin')
            ->orderBy('id')
            ->get();

        return response()->json($lines);
    }

    /**
     * Recebe coordenadas geocodificadas do Go e salva no banco.
     * POST /api/internal/lines/update-coordinates
     * Body: {"results": [{"id": 1, "lat": -23.5, "lng": -46.6}, ...]}
     */
    public function updateCoordinates(Request $request): JsonResponse
    {
        $request->validate([
            'results'           => ['required', 'array'],
            'results.*.id'      => ['required', 'integer'],
            'results.*.lat'     => ['required', 'numeric'],
            'results.*.lng'     => ['required', 'numeric'],
        ]);

        $updated = 0;
        foreach ($request->input('results') as $item) {
            $rows = Line::where('id', $item['id'])->update([
                'origin_lat' => $item['lat'],
                'origin_lng' => $item['lng'],
            ]);
            $updated += $rows;
        }

        return response()->json(['updated' => $updated]);
    }
}
