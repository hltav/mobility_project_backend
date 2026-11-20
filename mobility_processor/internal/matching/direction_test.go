package matching

import (
	"database/sql"
	"testing"
)

func orientedCandidate(pattern, direction int64, start, end ShapeEndpoint) Candidate {
	return Candidate{FeedVersionID: 1, RouteID: 971, PatternID: pattern,
		DirectionID: sql.NullInt64{Int64: direction, Valid: true}, ShortName: "8000-1", HasGeometry: true,
		ShapeStart: start, ShapeEnd: end, StartStop: Terminal{Name: "Start", Position: start, HasPosition: true},
		EndStop: Terminal{Name: "End", Position: end, HasPosition: true}}
}

func TestReference8000DirectionsRemainPendingWithOrientedEvidence(t *testing.T) {
	lapa := ShapeEndpoint{Lat: -23.520440, Lng: -46.699604}
	centro := ShapeEndpoint{Lat: -23.546953, Lng: -46.642793}
	out := orientedCandidate(1655, 0, lapa, centro)
	back := orientedCandidate(1656, 1, centro, lapa)
	// Operational terminal coordinates below are fixture evidence, not a claim
	// that these references are populated in the real operational database.
	for _, tc := range []struct {
		lineID, cl, sl, want int64
		start, end           ShapeEndpoint
	}{
		{1, 2506, 1, 1655, lapa, centro}, {2, 35274, 2, 1656, centro, lapa},
	} {
		line := LineToMatch{LineID: tc.lineID, SptransLineID: tc.cl, Direction: sql.NullInt64{Int64: tc.sl, Valid: true}, Code: "8000-1",
			StartTerminal: Terminal{Position: tc.start, HasPosition: true}, EndTerminal: Terminal{Position: tc.end, HasPosition: true}}
		assessments := NewMatcher(nil).Assess(line, []Candidate{out, back})
		if len(assessments) != 2 {
			t.Fatal("lost direction candidates", assessments)
		}
		for _, a := range assessments {
			want := "contradicted"
			if a.PatternID == tc.want {
				want = "supported"
			}
			if a.EvidenceStatus != want || a.Status != "pending" || a.FeedVersionID != 1 || a.RouteID != 971 ||
				a.SptransLineID != tc.cl || a.SptransDirection == nil || *a.SptransDirection != tc.sl {
				t.Fatalf("unexpected assessment: %+v", a)
			}
		}
	}
}

func TestNumericDirectionAloneIsInsufficientAndReversedConventionIsAllowed(t *testing.T) {
	start, end := ShapeEndpoint{Lat: 0, Lng: 0}, ShapeEndpoint{Lat: 0, Lng: 0.02}
	c := orientedCandidate(80, 1, start, end)
	line := LineToMatch{Code: "8000-1", Direction: sql.NullInt64{Int64: 1, Valid: true}}
	m := NewMatcher(nil)
	if a := m.Assess(line, []Candidate{c})[0]; a.EvidenceStatus != "insufficient" {
		t.Fatal(a)
	}
	line.StartTerminal = Terminal{Position: start, HasPosition: true}
	line.EndTerminal = Terminal{Position: end, HasPosition: true}
	a := m.Assess(line, []Candidate{c})[0]
	if a.EvidenceStatus != "supported" || *a.GTFSDirectionID != 1 || a.Status != "pending" {
		t.Fatal(a)
	}
}

func TestVariantsAreAmbiguousAndNeverCollapsed(t *testing.T) {
	start, end := ShapeEndpoint{}, ShapeEndpoint{Lng: 0.02}
	a, b := orientedCandidate(80, 0, start, end), orientedCandidate(81, 0, start, end)
	line := LineToMatch{Code: "8000-1", StartTerminal: a.StartStop, EndTerminal: a.EndStop}
	report := NewMatcher(nil).Assess(line, []Candidate{a, b})
	if len(report) != 2 {
		t.Fatal(report)
	}
	for i, r := range report {
		if r.PatternID != int64(80+i) || r.EvidenceStatus != "ambiguous" || r.Status != "pending" {
			t.Fatal(r)
		}
	}
}

func TestNoMatchAndUnalignedGeometry(t *testing.T) {
	m := NewMatcher(nil)
	a := orientedCandidate(80, 0, ShapeEndpoint{}, ShapeEndpoint{Lng: 0.02})
	if len(m.Assess(LineToMatch{Code: "different"}, []Candidate{a})) != 0 {
		t.Fatal("invented correspondence")
	}
	a.HasGeometry = false
	line := LineToMatch{Code: "8000-1", StartTerminal: a.StartStop, EndTerminal: a.EndStop}
	if r := m.Assess(line, []Candidate{a})[0]; r.EvidenceStatus != "insufficient" {
		t.Fatal(r)
	}
	a.HasGeometry = true
	a.ShapeStart = ShapeEndpoint{Lat: 10}
	if r := m.Assess(line, []Candidate{a})[0]; r.EvidenceStatus != "insufficient" {
		t.Fatal(r)
	}
}

func TestHeadSignSupportsButDoesNotConfirmAndConflictingTerminalsTakePrecedence(t *testing.T) {
	c := orientedCandidate(80, 0, ShapeEndpoint{}, ShapeEndpoint{Lng: 0.02})
	c.HeadSign = "Term. Lapa"
	line := LineToMatch{Code: "8000-1", HeadSign: " TERM.  LAPA "}
	m := NewMatcher(nil)
	if a := m.Assess(line, []Candidate{c})[0]; a.EvidenceStatus != "supported" || a.Status != "pending" {
		t.Fatal(a)
	}
	line.EndTerminal = Terminal{Position: ShapeEndpoint{Lng: 10}, HasPosition: true}
	if a := m.Assess(line, []Candidate{c})[0]; a.EvidenceStatus != "contradicted" {
		t.Fatal(a)
	}
}

func TestAssessmentPreservesFeedPatternAndNullGTFSDirection(t *testing.T) {
	c := orientedCandidate(80, 0, ShapeEndpoint{}, ShapeEndpoint{Lng: 0.02})
	c.FeedVersionID = 90
	c.DirectionID = sql.NullInt64{}
	a := NewMatcher(nil).Assess(LineToMatch{Code: "8000-1"}, []Candidate{c})[0]
	if a.FeedVersionID != 90 || a.PatternID != 80 || a.GTFSDirectionID != nil || a.SptransDirection != nil {
		t.Fatal(a)
	}
}
