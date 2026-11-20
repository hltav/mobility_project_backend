<?php

namespace App\Repositories;

use App\Services\StopReferenceCandidateReview;
use Illuminate\Support\Facades\DB;
use InvalidArgumentException;

class StopReferenceCandidateRepository
{
    // Explicit feed/pattern selection is diagnostic, never an active-feed fallback.
    public function candidates(int $feedId, int $patternId, int $lineId, float $radiusM = 50): array
    {
        if ($feedId <= 0 || $patternId <= 0 || $lineId <= 0 || !is_finite($radiusM) || $radiusM <= 0 || $radiusM > 200) {
            throw new InvalidArgumentException('Explicit feed, pattern, line and radius (0,200] metres required.');
        }
        $sql = file_get_contents(database_path('postgresql/diagnostics/stop_reference_candidates.sql'));
        $rows = DB::select($sql, ['feed_version_id' => $feedId, 'pattern_id' => $patternId,
            'line_id' => $lineId, 'radius_m' => $radiusM]);
        return array_map(function ($row) {
            $data = (array) $row;
            $data['evidence'] = json_decode($row->evidence, true, 512, JSON_THROW_ON_ERROR);
            $data['review'] = StopReferenceCandidateReview::classify($data['evidence']);
            return $data;
        }, $rows);
    }
}
