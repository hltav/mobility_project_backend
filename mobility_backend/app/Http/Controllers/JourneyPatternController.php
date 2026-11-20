<?php

namespace App\Http\Controllers;

use App\Repositories\JourneyPatternRepository;
use App\Repositories\LineRepository;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\DB;

class JourneyPatternController extends Controller
{
    public function __construct(
        private readonly JourneyPatternRepository $patterns,
        private readonly LineRepository $lines,
    ) {}

    public function index(int $id, Request $request): JsonResponse
    {
        // This contract uses an exact operational identity, not a code/direction guess.
        $request->validate(['direction' => ['prohibited'], 'feed_version_id' => ['prohibited']]);
        $line = $this->lines->findById($id);
        if (!$line) {
            return response()->json(['message' => 'Linha não encontrada.'], 404);
        }

        $patterns = $this->patterns->forLine($id);

        return response()->json(['data' => [
            'contract_version' => 1,
            'line' => ['id' => $line->id, 'code' => $line->code,
                'sptrans_line_id' => $line->sptrans_id,
                'sptrans_direction' => $line->sptrans_direction === null ? null : (int) $line->sptrans_direction],
            'status' => $patterns ? 'available' : 'unavailable',
            'reason' => $patterns ? null : 'no_active_confirmed_pattern',
            'patterns' => $patterns,
        ]]);
    }

    public function preview(int $feedId, int $routeId): JsonResponse
    {
        abort_unless(config('gtfs.preview_enabled') && app()->environment(['local', 'testing']), 404);
        // Never widen mobility_app privileges or fall back to the importer account.
        $allowed = DB::selectOne("SELECT bool_and(has_table_privilege(current_user,t,'SELECT')) AS allowed
            FROM unnest(ARRAY['gtfs.journey_patterns','gtfs.journey_pattern_stops',
                'gtfs.routes','gtfs.stops','gtfs.shapes','ingest.feed_versions']) AS t");
        if (!$allowed->allowed) {
            return response()->json(['message' => 'Prévia indisponível para o papel PostgreSQL atual.'], 503);
        }
        $patterns = $this->patterns->preview($feedId, $routeId);

        return response()->json(['data' => [
            'contract_version' => 1, 'mode' => 'preview', 'operational_line_id' => null,
            'sptrans_direction' => null, 'feed_version_id' => $feedId, 'route_id' => $routeId,
            'status' => $patterns ? 'available' : 'unavailable',
            'reason' => $patterns ? null : 'ready_feed_route_not_found',
            'patterns' => $patterns,
        ]]);
    }
}
