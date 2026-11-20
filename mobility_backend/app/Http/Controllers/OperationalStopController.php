<?php

namespace App\Http\Controllers;

use App\Services\BusStopService;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Log;
use Illuminate\Support\Facades\Validator;

class OperationalStopController extends Controller
{
    public function __construct(private readonly BusStopService $stops) {}

    public function select(Request $request): JsonResponse
    {
        // Use only the JSON body. Query parameters can expose personal coordinates in URLs.
        if ($request->query->count() || !$request->isJson()) {
            return $this->respond(['message' => 'Envie os parâmetros somente no corpo JSON.'], 422);
        }
        $validator = Validator::make($request->json()->all(), [
            'operational_line_id' => ['required', 'integer', 'min:1'],
            'sptrans_line_id' => ['nullable', 'integer', 'min:1'],
            'sptrans_direction' => ['nullable', 'integer', 'in:1,2'],
            'stop_id' => ['nullable', 'integer', 'min:1'],
            'lat' => ['nullable', 'required_without:stop_id', 'required_with:lng', 'numeric', 'between:-90,90'],
            'lng' => ['nullable', 'required_without:stop_id', 'required_with:lat', 'numeric', 'between:-180,180'],
            'radius_km' => ['nullable', 'numeric', 'min:0.1', 'max:10'],
            'limit' => ['nullable', 'integer', 'min:1', 'max:20'],
        ]);
        if ($validator->fails()) {
            return $this->respond(['message' => 'Parâmetros inválidos.', 'errors' => $validator->errors()], 422);
        }
        try {
            return $this->respond(['data' => $this->stops->selectOperational($validator->validated())]);
        } catch (\Throwable $e) {
            // Do not log SQL bindings, request body, coordinates or exception text.
            Log::error('[Stops] operational selection failed', ['exception_type' => get_class($e)]);
            return $this->respond(['data' => ['status' => 'unavailable', 'reason' => 'stop_selection_failed', 'stops' => []]], 503);
        }
    }

    private function respond(array $body, int $status = 200): JsonResponse
    {
        return response()->json($body, $status)->header('Cache-Control', 'no-store');
    }
}
