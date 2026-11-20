<?php

namespace App\Services;

// Diagnostic classification only. This class never creates or confirms records.
class StopReferenceCandidateReview
{
    public static function classify(array $e): array
    {
        $stop = $e['canonical_stop_id'] ?? null;
        $existing = $e['existing_canonical_stop_id'] ?? null;
        $status = $e['existing_reference_status'] ?? null;
        $same = $stop !== null && $existing !== null && (string) $stop === (string) $existing;
        if ($stop === null) {
            return ['status' => 'unavailable', 'reason' => 'no_confirmed_sptrans_candidate'];
        }
        if ($status === 'confirmed') {
            if (($e['existing_provider_matches'] ?? false) !== true) {
                return ['status' => 'ambiguous', 'reason' => 'existing_reference_provider_requires_review'];
            }
            return $same
                ? ['status' => 'confirmed', 'reason' => 'existing_confirmed_source_reference']
                : ['status' => 'rejected', 'reason' => 'conflicts_with_confirmed_reference'];
        }
        if ($status === 'rejected' && $same) {
            return ['status' => 'rejected', 'reason' => 'existing_rejection'];
        }
        if (($e['location_type'] ?? null) !== 0) {
            return ['status' => 'rejected', 'reason' => 'not_a_boarding_stop'];
        }
        if (($e['line_compatible'] ?? false) !== true) {
            return ['status' => 'rejected', 'reason' => 'no_confirmed_membership_for_selected_cl'];
        }
        if (($e['within_radius'] ?? false) !== true) {
            return ['status' => 'rejected', 'reason' => 'outside_review_radius'];
        }
        if (($e['pattern_mapping_confirmed'] ?? false) !== true) {
            return ['status' => 'ambiguous', 'reason' => 'operational_pattern_not_confirmed'];
        }
        if (($e['eligible_count'] ?? 0) > 1 || ($e['opposite_direction_served'] ?? false) === true
            || ($existing !== null && !$same)) {
            return ['status' => 'ambiguous', 'reason' => 'multiple_candidates_or_direction_or_existing_reference_conflict'];
        }
        return ['status' => 'candidate', 'reason' => 'requires_independent_identity_evidence_and_explicit_review'];
    }
}
