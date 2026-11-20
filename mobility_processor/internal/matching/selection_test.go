package matching

import "testing"

func TestExactFullCodeSuppressesSimilarRoutesAndKeepsEveryVariant(t *testing.T) {
	m := NewMatcher(nil)
	a := orientedCandidate(1655, 0, ShapeEndpoint{}, ShapeEndpoint{Lng: 0.02})
	b := a
	b.PatternID = 1656
	b.DirectionID.Int64 = 1
	unrelated := a
	unrelated.PatternID = 2000
	unrelated.ShortName = "9000-1"
	unrelated.LongName = "Lapa Centro"
	suffix := a
	suffix.PatternID = 2001
	suffix.ShortName = "8000-10"
	suffix.LongName = "Lapa Centro"
	line := LineToMatch{Code: "8000-1", Origin: "Lapa", SecondaryLabel: "Centro"}
	report := m.Assess(line, []Candidate{unrelated, suffix, a, b})
	if len(report) != 2 {
		t.Fatal("fallback leaked into exact set", report)
	}
	for _, r := range report {
		if r.GTFSCode != "8000-1" || r.IdentityScore != 1 || r.OrientationScore != 0 || r.Confidence != 0 || r.CandidateSelection != "exact_code" {
			t.Fatal(r)
		}
	}
}

func TestCandidateSelectionRespectsProviderAndSelectedFeed(t *testing.T) {
	c := orientedCandidate(10, 0, ShapeEndpoint{}, ShapeEndpoint{Lng: 0.02})
	c.Provider = "sptrans"
	otherProvider := c
	otherProvider.Provider = "other"
	otherProvider.PatternID = 20
	otherFeed := c
	otherFeed.FeedVersionID = 2
	otherFeed.PatternID = 30
	line := LineToMatch{Code: "8000-1", Provider: "sptrans", SelectedFeedVersionID: 1}
	report := NewMatcher(nil).Assess(line, []Candidate{otherProvider, otherFeed, c})
	if len(report) != 1 || report[0].PatternID != 10 || report[0].FeedVersionID != 1 {
		t.Fatal(report)
	}
}

func TestFallbackOnlyWithoutExactCodeAndLeadingZerosRemainSignificant(t *testing.T) {
	m := NewMatcher(nil)
	c := orientedCandidate(10, 0, ShapeEndpoint{}, ShapeEndpoint{Lng: 0.02})
	c.ShortName = "other"
	c.LongName = "Lapa Centro"
	line := LineToMatch{Code: "8000-1", Origin: "Lapa", SecondaryLabel: "Centro"}
	report := m.Assess(line, []Candidate{c})
	if len(report) != 1 || report[0].CandidateSelection != "fallback_similarity" {
		t.Fatal(report)
	}
	c.ShortName = "0008-1"
	c.LongName = ""
	if len(m.Assess(LineToMatch{Code: "8-1"}, []Candidate{c})) != 0 {
		t.Fatal("leading zeros collapsed")
	}
}

func TestTPAndTSAliasesDoNotBecomeOrientedHeadSign(t *testing.T) {
	c := orientedCandidate(10, 0, ShapeEndpoint{}, ShapeEndpoint{Lng: 0.02})
	c.HeadSign = "Centro"
	a := NewMatcher(nil).Assess(LineToMatch{Code: "8000-1", Origin: "Lapa", SecondaryLabel: "Centro"}, []Candidate{c})[0]
	if a.HeadSignMatches || a.OrientationScore != 0 || a.EvidenceStatus != "insufficient" || len(a.MissingEvidence) == 0 || len(a.Reasons) == 0 {
		t.Fatal(a)
	}
}

func stopFixture() (Candidate, []OperationalStop) {
	c := orientedCandidate(10, 0, ShapeEndpoint{}, ShapeEndpoint{Lng: 0.03})
	stops := []OperationalStop{}
	for i := 0; i < 4; i++ {
		seq := i * 10
		point := ShapeEndpoint{Lng: float64(i) * 0.01}
		c.Stops = append(c.Stops, PatternStop{Sequence: seq, Position: point})
		stops = append(stops, OperationalStop{Sequence: &seq, Position: point})
	}
	return c, stops
}

func TestUnorderedStopMembershipDoesNotProveDirection(t *testing.T) {
	c, stops := stopFixture()
	for i := range stops {
		stops[i].Sequence = nil
	}
	a := NewMatcher(nil).Assess(LineToMatch{Code: "8000-1", Stops: stops}, []Candidate{c})[0]
	if a.StopEvidence.Coverage != 1 || a.StopEvidence.Progression != "unordered" || a.OrientationScore != 0 {
		t.Fatal(a)
	}
}

func TestOrderedStopsSupportProgressionAndReverseConflicts(t *testing.T) {
	c, stops := stopFixture()
	line := LineToMatch{Code: "8000-1", Stops: stops}
	a := NewMatcher(nil).Assess(line, []Candidate{c})[0]
	if a.StopEvidence.Progression != "forward" || a.OrientationScore != 0.9 || a.EvidenceStatus != "supported" || a.Status != "pending" {
		t.Fatal(a)
	}
	for i := range c.Stops {
		c.Stops[i].Sequence = 30 - c.Stops[i].Sequence
	}
	a = NewMatcher(nil).Assess(line, []Candidate{c})[0]
	if a.EvidenceStatus != "contradicted" || a.Confidence != 0 {
		t.Fatal(a)
	}
}

func TestRepeatedStopsAndIdenticalVariantsRemainAmbiguous(t *testing.T) {
	c, stops := stopFixture()
	c.Stops = append(c.Stops, PatternStop{Sequence: 100, Position: c.Stops[0].Position})
	e := compareStops(LineToMatch{Stops: stops}, c)
	if e.Progression != "ambiguous" {
		t.Fatal(e)
	}
}

func TestCircularRoutesRequireSequenceAndAllowOneWrap(t *testing.T) {
	c, stops := stopFixture()
	line := LineToMatch{Code: "8000-1", Circular: true, HeadSign: "End"}
	a := NewMatcher(nil).Assess(line, []Candidate{c})[0]
	if a.EvidenceStatus != "insufficient" {
		t.Fatal(a)
	}
	// Official observed order starts after the GTFS pattern's starting point.
	order := []int{2, 3, 0, 1}
	line.Stops = []OperationalStop{}
	for i, j := range order {
		seq := i
		line.Stops = append(line.Stops, OperationalStop{Sequence: &seq, Position: stops[j].Position})
	}
	if e := compareStops(line, c); e.Progression != "forward" {
		t.Fatal(e)
	}
	line.Circular = false
	if e := compareStops(line, c); e.Progression != "contradicted" {
		t.Fatal(e)
	}
}

func TestStopToleranceAndLowCoverageDoNotInventProgression(t *testing.T) {
	c, stops := stopFixture()
	stops[0].Position.Lat = 1
	stops[1].Position.Lat = 1
	e := compareStops(LineToMatch{Stops: stops}, c)
	if e.Coverage != 0.5 || e.Progression != "insufficient" {
		t.Fatal(e)
	}
}
