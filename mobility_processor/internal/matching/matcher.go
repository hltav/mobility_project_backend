package matching

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type Terminal struct {
	Name        string        `json:"name"`
	Position    ShapeEndpoint `json:"position"`
	HasPosition bool          `json:"has_position"`
}

type PatternStop struct {
	Sequence int           `json:"sequence"`
	Position ShapeEndpoint `json:"position"`
}

type OperationalStop struct {
	Position ShapeEndpoint `json:"position"`
	// Nil means an unordered association, never an inferred official sequence.
	Sequence *int `json:"sequence"`
}

type Candidate struct {
	AgencyID int64
	Provider string
	Stops    []PatternStop

	FeedVersionID int64
	PatternID     int64
	DirectionID   sql.NullInt64
	HasGeometry   bool
	RouteID       int64
	ShortName     string
	LongName      string
	ShapeStart    ShapeEndpoint
	ShapeEnd      ShapeEndpoint
	HeadSign      string
	StartStop     Terminal
	EndStop       Terminal
}

type LineToMatch struct {
	Provider              string
	SelectedFeedVersionID int64
	Circular              bool
	Stops                 []OperationalStop

	LineID         int64
	SptransLineID  int64
	Direction      sql.NullInt64
	HeadSign       string
	StartTerminal  Terminal
	EndTerminal    Terminal
	Code           string
	Origin         string
	SecondaryLabel string
	OriginLat      sql.NullFloat64
	OriginLng      sql.NullFloat64
}

type Match struct {
	FeedVersionID  int64
	PatternID      int64
	RouteID        int64
	LineID         int64
	Confidence     float64
	MatchingMethod string
}

const spatialMaxDistanceKm = 0.5

// Matcher separates full-code identity selection from oriented pattern evidence.
type Matcher struct {
	repo *Repository
}

func NewMatcher(repo *Repository) *Matcher {
	return &Matcher{repo: repo}
}

func (m *Matcher) RunAll(ctx context.Context) (int, error) {
	lines, err := m.repo.LoadLinesToMatch(ctx)
	if err != nil {
		return 0, fmt.Errorf("load lines: %w", err)
	}
	candidates, err := m.repo.LoadCandidates(ctx)
	if err != nil {
		return 0, fmt.Errorf("load candidates: %w", err)
	}

	var matches []Match
	for _, assessment := range m.AssessBatch(lines, candidates) {
		if assessment.EvidenceStatus == "contradicted" {
			continue
		}
		matches = append(matches, Match{FeedVersionID: assessment.FeedVersionID, PatternID: assessment.PatternID,
			RouteID: assessment.RouteID, LineID: assessment.LineID, Confidence: assessment.Confidence,
			MatchingMethod: assessment.MatchingMethod})
	}

	if err := m.repo.SaveMatches(ctx, matches); err != nil {
		return 0, fmt.Errorf("save matches: %w", err)
	}
	return len(matches), nil
}

func (m *Matcher) bestMatch(line LineToMatch, candidates []Candidate) (*Candidate, string, float64) {
	var best *Candidate
	bestScore := 0.0
	bestMethod := ""

	normalizedCode := normalizeCode(line.Code)

	for i := range candidates {
		c := &candidates[i]

		// 1. Match exato por código
		if normalizeCode(c.ShortName) == normalizedCode && normalizedCode != "" {
			return c, "exact_code", 1.0
		}

		// Similarity is an explicit identity fallback, never orientation evidence.
		combined := textSimilarity(c.LongName, strings.TrimSpace(line.Origin+" "+line.SecondaryLabel))

		if combined > bestScore {
			bestScore = combined
			best = c
			bestMethod = "fallback_text"
		}
	}

	if best == nil || bestScore < 0.6 {
		return nil, "", 0
	}
	return best, bestMethod, bestScore
}

func normalizeCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, " ", "")
	return code
}

// MatchStatus deliberately never promotes an algorithmic suggestion to confirmed.
func MatchStatus(confidence float64) string { return "pending" }

// Candidate selection is global for this operational identity, before scoring
// individual patterns. Exact full codes suppress all fuzzy alternatives.
func (m *Matcher) selectCandidates(line LineToMatch, candidates []Candidate) ([]Candidate, bool) {
	eligible := []Candidate{}
	exact := []Candidate{}
	for _, c := range candidates {
		if line.SelectedFeedVersionID > 0 && c.FeedVersionID != line.SelectedFeedVersionID {
			continue
		}
		if line.Provider != "" && c.Provider != "" && line.Provider != c.Provider {
			continue
		}
		eligible = append(eligible, c)
		if normalizeCode(line.Code) != "" && normalizeCode(line.Code) == normalizeCode(c.ShortName) {
			exact = append(exact, c)
		}
	}
	if len(exact) > 0 {
		return exact, false
	}
	return eligible, true
}
