<?php

namespace App\Http\Controllers;

use App\Repositories\JourneyPatternRepository;
use App\Repositories\LineRepository;
use App\Services\VehicleProgressService;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\DB;

class VehicleProgressController extends Controller
{
    public function __construct(private readonly LineRepository $lines,
        private readonly JourneyPatternRepository $patterns, private readonly VehicleProgressService $progress) {}

    public function index(int $id, Request $request): JsonResponse
    {
        $request->validate(['feed_version_id' => ['prohibited'], 'pattern_id' => ['prohibited'], 'direction' => ['prohibited']]);
        $line = $this->lines->findById($id);
        abort_unless($line, 404);
        return response()->json(['data' => $this->progress->forLine($line, $this->patterns->forLine($id))]);
    }

    public function preview(int $feedId, int $patternId, int $id): JsonResponse
    {
        abort_unless(config('gtfs.preview_enabled') && app()->environment(['local', 'testing']), 404);
        $allowed = DB::selectOne("SELECT bool_and(has_table_privilege(current_user,t,'SELECT')) AS allowed
            FROM unnest(ARRAY['gtfs.journey_patterns','gtfs.journey_pattern_stops','gtfs.routes',
                'gtfs.stops','gtfs.shapes','ingest.feed_versions']) AS t");
        abort_unless($allowed->allowed, 503, 'Prévia indisponível para o papel PostgreSQL atual.');
        $line = $this->lines->findById($id);
        abort_unless($line, 404);
        $pattern = DB::table('gtfs.journey_patterns as p')->join('ingest.feed_versions as v', 'v.id', '=', 'p.feed_version_id')
            ->where('v.state', 'ready')->where('p.feed_version_id', $feedId)->where('p.id', $patternId)->first(['p.route_id']);
        $patterns = $pattern ? array_values(array_filter($this->patterns->preview($feedId, (int) $pattern->route_id),
            fn($p) => $p['pattern_id'] === $patternId)) : [];
        return response()->json(['data' => $this->progress->forLine($line, $patterns, true)]);
    }
}
