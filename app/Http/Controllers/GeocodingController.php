<?php

namespace App\Http\Controllers;

use App\Services\ProcessorService;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;

class GeocodingController extends Controller
{
    public function __construct(
        private readonly ProcessorService $processorService
    ) {}

    public function reverse(Request $request): JsonResponse
    {
        $request->validate([
            'lat' => ['required', 'numeric', 'between:-90,90'],
            'lng' => ['required', 'numeric', 'between:-180,180'],
        ]);

        $address = $this->processorService->reverse(
            (float) $request->lat,
            (float) $request->lng,
        );

        if (!$address) {
            return response()->json(['message' => 'Endereço não encontrado.'], 404);
        }

        return response()->json([
            'data' => ['address' => $address],
        ]);
    }

    public function geocode(Request $request): JsonResponse
    {
        $request->validate([
            'address' => ['required', 'string'],
        ]);

        $coords = $this->processorService->geocode($request->input('address'));

        if (!$coords) {
            return response()->json(['message' => 'Endereço não encontrado.'], 404);
        }

        return response()->json(['data' => $coords]);
    }
}
