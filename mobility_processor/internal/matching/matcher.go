package matching

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type Candidate struct {
	FeedVersionID int64
	PatternID     int64
	DirectionID   sql.NullInt64
	HasGeometry   bool
	RouteID       int64
	ShortName     string
	LongName      string
	ShapeStart    ShapeEndpoint
	ShapeEnd      ShapeEndpoint
}

type LineToMatch struct {
	LineID    int64
	Code      string
	Origin    string
	OriginLat sql.NullFloat64
	OriginLng sql.NullFloat64
}

type Match struct {
	FeedVersionID  int64
	PatternID      int64
	RouteID        int64
	LineID         int64
	Confidence     float64
	MatchingMethod string
}

const (
	exactScoreThreshold  = 0.95
	textSimWeight        = 0.6
	spatialWeight        = 0.4
	spatialMaxDistanceKm = 0.5
	confirmThreshold     = 0.7
)

// Matcher executa o pipeline: exato -> texto -> espacial, combinando scores.
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
	for _, line := range lines {
		for _, candidate := range candidates {
			best, method, score := m.bestMatch(line, []Candidate{candidate})
			if best == nil {
				continue
			}
			matches = append(matches, Match{FeedVersionID: best.FeedVersionID, PatternID: best.PatternID, RouteID: best.RouteID, LineID: line.LineID, Confidence: score, MatchingMethod: method})
		}
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

		// 2. Similaridade de texto (long_name vs origin)
		textScore := textSimilarity(c.LongName, line.Origin)

		// 3. Score espacial, se a linha já tem coordenadas de origem
		spatial := 0.0
		if line.OriginLat.Valid && line.OriginLng.Valid && c.HasGeometry {
			spatial = spatialScore(
				ShapeEndpoint{Lat: line.OriginLat.Float64, Lng: line.OriginLng.Float64},
				c.ShapeStart, c.ShapeEnd, spatialMaxDistanceKm,
			)
		}

		combined := textScore*textSimWeight + spatial*spatialWeight
		if combined > bestScore {
			bestScore = combined
			best = c
			bestMethod = "text_spatial"
		}
	}

	if best == nil || bestScore < 0.3 {
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
