package matching

import (
	"database/sql"
	"testing"
)

func TestMatchingSuggestionsNeverBecomeConfirmed(t *testing.T) {
	for _, score := range []float64{0, 0.5, 0.7, 1} {
		if MatchStatus(score) != "pending" {
			t.Fatal("automatic confirmation")
		}
	}
	if normalizeCode("0008-10") == normalizeCode("8-10") {
		t.Fatal("leading zeros lost")
	}
}
func TestMatcherKeepsEveryPatternCandidate(t *testing.T) {
	m := NewMatcher(nil)
	for _, pattern := range []int64{10, 20} {
		best, _, score := m.bestMatch(LineToMatch{Code: "8000-10"}, []Candidate{{RouteID: 3, PatternID: pattern, DirectionID: sql.NullInt64{Int64: 0, Valid: true}, ShortName: "8000-10"}})
		if best == nil || best.PatternID != pattern || score != 1 {
			t.Fatal("pattern lost")
		}
	}
}
