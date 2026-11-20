package matching

import (
	"database/sql"
	"testing"
)

func reference1012Fixture() ([]LineToMatch, Candidate) {
	// IDs/names/count describe the supplied reference case. All coordinates are
	// deliberately synthetic; they are NOT operational evidence for the real line.
	start, end := ShapeEndpoint{}, ShapeEndpoint{Lng: 0.08}
	c := orientedCandidate(1, 0, start, end)
	c.RouteID = 1
	c.ShortName = "1012-10"
	c.Provider = "sptrans"
	c.HeadSign = "Jd. Monte Belo"
	c.StartStop.Name = "Terminal Jardim Britânia"
	c.EndStop.Name = "R. Domênico Aspari, 1"
	for i := 0; i < 41; i++ {
		c.Stops = append(c.Stops, PatternStop{Sequence: i, Position: ShapeEndpoint{Lng: float64(i) * 0.002}})
	}
	lines := []LineToMatch{
		{LineID: 9, SptransLineID: 35254, Direction: sql.NullInt64{Int64: 2, Valid: true}, Code: "1012-10", Provider: "sptrans", SelectedFeedVersionID: 1},
		{LineID: 10, SptransLineID: 2486, Direction: sql.NullInt64{Int64: 1, Valid: true}, Code: "1012-10", Provider: "sptrans", SelectedFeedVersionID: 1},
	}
	return lines, c
}

func TestReference1012KnownMetadataDoesNotChooseAnOperationalDirection(t *testing.T) {
	lines, c := reference1012Fixture()
	report := NewMatcher(nil).AssessBatch(lines, []Candidate{c})
	if len(report) != 2 || len(c.Stops) != 41 {
		t.Fatal("lost reference identities")
	}
	for i, a := range report {
		if a.LineID != lines[i].LineID || a.SptransLineID != lines[i].SptransLineID || a.PatternID != 1 || a.RouteID != 1 || a.FeedVersionID != 1 ||
			a.GTFSDirectionID == nil || *a.GTFSDirectionID != 0 || a.EvidenceStatus != "insufficient" || a.Status != "pending" || a.Confidence != 0 {
			t.Fatal("numeric direction or GTFS-only labels selected a CL", a)
		}
	}
}

func TestReference1012SyntheticForwardEvidenceSupportsCL35254Only(t *testing.T) {
	lines, c := reference1012Fixture()
	for i := range c.Stops {
		sequence := i
		lines[0].Stops = append(lines[0].Stops, OperationalStop{Sequence: &sequence, Position: c.Stops[i].Position})
		lines[1].Stops = append(lines[1].Stops, OperationalStop{Sequence: &sequence, Position: c.Stops[len(c.Stops)-1-i].Position})
	}
	report := NewMatcher(nil).AssessBatch(lines, []Candidate{c})
	if report[0].SptransLineID != 35254 || *report[0].SptransDirection != 2 || *report[0].GTFSDirectionID != 0 || report[0].EvidenceStatus != "supported" ||
		report[0].StopEvidence.Progression != "forward" || report[1].EvidenceStatus != "contradicted" {
		t.Fatal(report)
	}
	for _, a := range report {
		if a.Status != "pending" || MatchStatus(a.Confidence) != "pending" {
			t.Fatal("suggestion confirmed", a)
		}
	}
}

func TestReference1012SamePatternSupportedForBothDirectionsIsAmbiguous(t *testing.T) {
	lines, c := reference1012Fixture()
	for i := range lines {
		lines[i].HeadSign = c.HeadSign
	}
	m := NewMatcher(nil)
	// Local evidence alone does not reveal a conflicting operational record.
	for _, line := range lines {
		if m.Assess(line, []Candidate{c})[0].EvidenceStatus != "supported" {
			t.Fatal("invalid conflict fixture")
		}
	}
	for _, order := range [][]LineToMatch{lines, {lines[1], lines[0]}} {
		report := m.AssessBatch(order, []Candidate{c})
		for i, a := range report {
			if a.EvidenceStatus != "ambiguous" || a.Status != "pending" || a.Confidence != 0 ||
				len(a.ConflictingLineIDs) != 1 || a.ConflictingLineIDs[0] != order[1-i].LineID {
				t.Fatal("opposite directions not flagged", a)
			}
		}
	}
}

func TestBatchDoesNotInventConflictWithInsufficientPeerOrOtherFeed(t *testing.T) {
	lines, c := reference1012Fixture()
	lines[0].HeadSign = c.HeadSign
	report := NewMatcher(nil).AssessBatch(lines, []Candidate{c})
	if report[0].EvidenceStatus != "supported" || report[1].EvidenceStatus != "insufficient" {
		t.Fatal(report)
	}
	other := c
	other.FeedVersionID = 2
	lines[1].SelectedFeedVersionID = 2
	lines[1].HeadSign = c.HeadSign
	report = NewMatcher(nil).AssessBatch(lines, []Candidate{c, other})
	for _, a := range report {
		if a.EvidenceStatus != "supported" || len(a.ConflictingLineIDs) != 0 {
			t.Fatal("cross-feed conflict", a)
		}
	}
}
