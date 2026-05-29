<?php

namespace App\Http\Controllers;

use App\Services\BusStopService;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;

class BusStopController extends Controller
{
    public function __construct(
        private readonly BusStopService $busStopService,
    ) {}

    public function nearest(Request $request): JsonResponse
    {
        $validated = $request->validate([
            'address' => ['nullable', 'string', 'max:200'],
            'lat' => ['nullable', 'numeric', 'between:-90,90'],
            'lng' => ['nullable', 'numeric', 'between:-180,180'],
            'limit' => ['nullable', 'integer', 'min:1', 'max:20'],
            'radius_km' => ['nullable', 'numeric', 'min:0.1', 'max:10'],
            'mode' => ['nullable', 'in:any,with_lines'],
        ]);

        $limit = (int) ($validated['limit'] ?? 1);
        $radiusKm = (float) ($validated['radius_km'] ?? 2.0);
        $onlyWithLines = ($validated['mode'] ?? 'any') === 'with_lines';

        if (!empty($validated['address'])) {
            $result = $this->busStopService->nearestFromAddress($validated['address'], $limit, $radiusKm, $onlyWithLines);

            return response()->json([
                'message' => 'Pontos próximos recuperados com sucesso.',
                'data' => $result,
            ]);
        }

        if (!isset($validated['lat'], $validated['lng'])) {
            return response()->json([
                'message' => 'Informe address ou lat/lng.',
            ], 422);
        }

        $stops = $this->busStopService->nearestFromCoordinates(
            (float) $validated['lat'],
            (float) $validated['lng'],
            $limit,
            $radiusKm,
            $onlyWithLines,
        );

        return response()->json([
            'message' => 'Pontos próximos recuperados com sucesso.',
            'data' => [
                'point' => [
                    'lat' => (float) $validated['lat'],
                    'lng' => (float) $validated['lng'],
                ],
                'stops' => $stops,
            ],
        ]);
    }

    public function tripNearest(Request $request): JsonResponse
    {
        $validated = $request->validate([
            'origin_address' => ['nullable', 'string', 'max:200'],
            'origin_lat' => ['nullable', 'numeric', 'between:-90,90'],
            'origin_lng' => ['nullable', 'numeric', 'between:-180,180'],
            'destination_address' => ['nullable', 'string', 'max:200'],
            'destination_lat' => ['nullable', 'numeric', 'between:-90,90'],
            'destination_lng' => ['nullable', 'numeric', 'between:-180,180'],
            'limit' => ['nullable', 'integer', 'min:1', 'max:20'],
            'radius_km' => ['nullable', 'numeric', 'min:0.1', 'max:10'],
            'mode' => ['nullable', 'in:any,with_lines'],
        ]);

        $limit = (int) ($validated['limit'] ?? 1);
        $radiusKm = (float) ($validated['radius_km'] ?? 2.0);
        $onlyWithLines = ($validated['mode'] ?? 'any') === 'with_lines';

        return response()->json([
            'message' => 'Pontos de partida e destino recuperados com sucesso.',
            'data' => [
                'origin' => $this->resolveNearest(
                    $validated['origin_address'] ?? null,
                    $validated['origin_lat'] ?? null,
                    $validated['origin_lng'] ?? null,
                    $limit,
                    $radiusKm,
                    $onlyWithLines,
                ),
                'destination' => $this->resolveNearest(
                    $validated['destination_address'] ?? null,
                    $validated['destination_lat'] ?? null,
                    $validated['destination_lng'] ?? null,
                    $limit,
                    $radiusKm,
                    $onlyWithLines,
                ),
            ],
        ]);
    }

    private function resolveNearest(
        ?string $address,
        mixed $lat,
        mixed $lng,
        int $limit,
        float $radiusKm,
        bool $onlyWithLines,
    ): array
    {
        if (!empty($address)) {
            return $this->busStopService->nearestFromAddress($address, $limit, $radiusKm, $onlyWithLines);
        }

        if ($lat === null || $lng === null) {
            return ['point' => null, 'stops' => []];
        }

        return [
            'point' => ['lat' => (float) $lat, 'lng' => (float) $lng],
            'stops' => $this->busStopService->nearestFromCoordinates(
                (float) $lat,
                (float) $lng,
                $limit,
                $radiusKm,
                $onlyWithLines,
            ),
        ];
    }
}
