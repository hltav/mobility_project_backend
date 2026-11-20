<?php

namespace Tests\Unit;

use App\Services\StopReferenceCandidateReview;
use PHPUnit\Framework\TestCase;

class StopReferenceCandidateReviewTest extends TestCase
{
    private function evidence(array $changes = []): array
    {
        return array_replace(['canonical_stop_id' => 10, 'existing_canonical_stop_id' => null,
            'existing_reference_status' => null, 'existing_provider_matches' => null,
            'id_equal' => false, 'code_equal' => false, 'names_equal' => true,
            'within_radius' => true, 'line_compatible' => true, 'opposite_direction_served' => false,
            'location_type' => 0, 'eligible_count' => 1, 'pattern_mapping_confirmed' => true,
            'feed_published' => false], $changes);
    }

    public function test_only_an_existing_verified_source_reference_is_reported_confirmed(): void
    {
        $e = $this->evidence(['existing_canonical_stop_id' => 10, 'existing_reference_status' => 'confirmed',
            'existing_provider_matches' => true]);
        $this->assertSame('confirmed', StopReferenceCandidateReview::classify($e)['status']);
        $e['existing_provider_matches'] = false;
        $this->assertSame('ambiguous', StopReferenceCandidateReview::classify($e)['status']);
    }

    public function test_equal_numeric_identifier_or_name_never_confirms(): void
    {
        $e = $this->evidence(['id_equal' => true, 'code_equal' => true]);
        $this->assertSame('candidate', StopReferenceCandidateReview::classify($e)['status']);
        $e['within_radius'] = false;
        $this->assertSame('rejected', StopReferenceCandidateReview::classify($e)['status']);
    }

    public function test_nearby_different_code_is_only_a_candidate(): void
    {
        $this->assertSame('candidate', StopReferenceCandidateReview::classify($this->evidence())['status']);
    }

    public function test_unconfirmed_pattern_never_proves_operational_direction(): void
    {
        $out = StopReferenceCandidateReview::classify($this->evidence(['pattern_mapping_confirmed' => false]));
        $this->assertSame('ambiguous', $out['status']);
        $this->assertSame('operational_pattern_not_confirmed', $out['reason']);
    }

    public function test_opposite_platforms_and_shared_directions_remain_unproved(): void
    {
        $this->assertSame('rejected', StopReferenceCandidateReview::classify($this->evidence(['line_compatible' => false]))['status']);
        $this->assertSame('ambiguous', StopReferenceCandidateReview::classify($this->evidence(['opposite_direction_served' => true]))['status']);
        $this->assertSame('rejected', StopReferenceCandidateReview::classify($this->evidence(['location_type' => 1]))['status']);
    }

    public function test_no_candidate_and_multiple_candidates_are_explicit(): void
    {
        $this->assertSame('unavailable', StopReferenceCandidateReview::classify($this->evidence(['canonical_stop_id' => null]))['status']);
        $this->assertSame('ambiguous', StopReferenceCandidateReview::classify($this->evidence(['eligible_count' => 2]))['status']);
    }

    public function test_conflicts_and_rejections_are_preserved(): void
    {
        $e = $this->evidence(['existing_canonical_stop_id' => 11, 'existing_reference_status' => 'pending']);
        $this->assertSame('ambiguous', StopReferenceCandidateReview::classify($e)['status']);
        $e['existing_reference_status'] = 'confirmed'; $e['existing_provider_matches'] = true;
        $this->assertSame('rejected', StopReferenceCandidateReview::classify($e)['status']);
        $e['existing_reference_status'] = 'rejected'; $e['existing_canonical_stop_id'] = 10;
        $this->assertSame('rejected', StopReferenceCandidateReview::classify($e)['status']);
    }

    public function test_repeated_review_is_idempotent_and_never_promotes_pending(): void
    {
        $e = $this->evidence(['existing_canonical_stop_id' => 10, 'existing_reference_status' => 'pending']);
        $this->assertSame(StopReferenceCandidateReview::classify($e), StopReferenceCandidateReview::classify($e));
        $this->assertSame('candidate', StopReferenceCandidateReview::classify($e)['status']);
        $this->assertSame('pending', $e['existing_reference_status']);
    }
}
