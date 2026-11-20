package matching

import (
	"math"
	"mobility_processor/internal/haversine"
	"sort"
	"strings"
)

// Reuse the matcher's existing 500 m spatial tolerance. Shapes can extend beyond
// their first/last served stops; this is review evidence, never confirmation.
const terminalToleranceKm = spatialMaxDistanceKm

// Assessment is evidence for review, never authorization for operational use.
type Assessment struct {
	ConflictingLineIDs []int64 `json:"conflicting_operational_line_ids,omitempty"`
	AgencyID           int64   `json:"gtfs_agency_id"`

	OperationalCode    string       `json:"operational_code"`
	GTFSCode           string       `json:"gtfs_code"`
	Provider           string       `json:"provider"`
	CandidateSelection string       `json:"candidate_selection"`
	IdentityScore      float64      `json:"identity_score"`
	OrientationScore   float64      `json:"orientation_score"`
	AvailableEvidence  []string     `json:"available_evidence"`
	MissingEvidence    []string     `json:"missing_evidence"`
	Reasons            []string     `json:"reasons"`
	VariantStatus      string       `json:"variant_status"`
	StopEvidence       StopEvidence `json:"stop_evidence"`

	PrimaryLabel             string        `json:"sptrans_tp_label"`
	SecondaryLabel           string        `json:"sptrans_ts_label"`
	OperationalStartTerminal Terminal      `json:"operational_start_terminal"`
	OperationalEndTerminal   Terminal      `json:"operational_end_terminal"`
	ShapeStart               ShapeEndpoint `json:"shape_start"`
	ShapeEnd                 ShapeEndpoint `json:"shape_end"`

	LineID              int64    `json:"line_id"`
	SptransLineID       int64    `json:"sptrans_line_id"`
	SptransDirection    *int64   `json:"sptrans_direction"`
	FeedVersionID       int64    `json:"feed_version_id"`
	RouteID             int64    `json:"route_id"`
	PatternID           int64    `json:"pattern_id"`
	GTFSDirectionID     *int64   `json:"gtfs_direction_id"`
	Status              string   `json:"status"`
	EvidenceStatus      string   `json:"evidence_status"`
	MatchingMethod      string   `json:"matching_method"`
	Confidence          float64  `json:"confidence_score"`
	OperationalHeadSign string   `json:"operational_headsign"`
	GTFSHeadSign        string   `json:"gtfs_headsign"`
	StartStop           Terminal `json:"gtfs_start_stop"`
	EndStop             Terminal `json:"gtfs_end_stop"`
	HeadSignMatches     bool     `json:"headsign_matches"`
	GeometryAligned     bool     `json:"gtfs_geometry_aligned"`
	StartDistanceM      *float64 `json:"operational_start_distance_m"`
	EndDistanceM        *float64 `json:"operational_end_distance_m"`
}

// AssessBatch reuses Assess and annotates competing operational directions.
// It never chooses a winner or promotes a suggestion to confirmed.
func (m *Matcher) AssessBatch(lines []LineToMatch, candidates []Candidate) []Assessment {
	out := []Assessment{}
	for _, line := range lines {
		out = append(out, m.Assess(line, candidates)...)
	}
	type key struct {
		feed, route, pattern int64
		provider, code       string
	}
	groups := map[key][]int{}
	for i, a := range out {
		if (a.EvidenceStatus != "supported" && a.EvidenceStatus != "ambiguous") || a.SptransLineID <= 0 ||
			a.SptransDirection == nil || (*a.SptransDirection != 1 && *a.SptransDirection != 2) {
			continue
		}
		k := key{a.FeedVersionID, a.RouteID, a.PatternID, a.Provider, normalizeCode(a.OperationalCode)}
		groups[k] = append(groups[k], i)
	}
	for _, indices := range groups {
		for _, i := range indices {
			a := &out[i]
			for _, j := range indices {
				b := out[j]
				if a.LineID != b.LineID && a.SptransLineID != b.SptransLineID && *a.SptransDirection != *b.SptransDirection {
					a.ConflictingLineIDs = append(a.ConflictingLineIDs, b.LineID)
				}
			}
			if len(a.ConflictingLineIDs) > 0 {
				sort.Slice(a.ConflictingLineIDs, func(x, y int) bool { return a.ConflictingLineIDs[x] < a.ConflictingLineIDs[y] })
				a.MatchingMethod = strings.TrimSuffix(a.MatchingMethod, a.EvidenceStatus) + "ambiguous"
				a.EvidenceStatus = "ambiguous"
				a.VariantStatus = "ambiguous"
				a.Confidence = 0
				a.Reasons = append(a.Reasons, "same_pattern_supports_opposite_operational_directions_requires_review")
			}
		}
	}
	return out
}

func terminalDistance(a, b ShapeEndpoint) float64 {
	return haversine.DistanceKm(a.Lat, a.Lng, b.Lat, b.Lng)
}

func terminalName(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// Assess keeps every variant and compares oriented terminals. SL and direction_id
// are identifiers only; there is deliberately no arithmetic relationship here.
func (m *Matcher) Assess(line LineToMatch, candidates []Candidate) []Assessment {
	out := []Assessment{}
	supported := 0
	selected, fallback := m.selectCandidates(line, candidates)
	for _, c := range selected {
		best, method, score := m.bestMatch(line, []Candidate{c})
		if best == nil {
			continue
		}
		a := Assessment{PrimaryLabel: line.Origin, SecondaryLabel: line.SecondaryLabel, OperationalStartTerminal: line.StartTerminal, OperationalEndTerminal: line.EndTerminal, ShapeStart: c.ShapeStart, ShapeEnd: c.ShapeEnd, LineID: line.LineID, SptransLineID: line.SptransLineID,
			FeedVersionID: c.FeedVersionID, RouteID: c.RouteID, PatternID: c.PatternID,
			Status: "pending", EvidenceStatus: "insufficient", IdentityScore: score,
			OperationalCode: line.Code, GTFSCode: c.ShortName, Provider: c.Provider, AgencyID: c.AgencyID,
			CandidateSelection: "exact_code", VariantStatus: "unverified",
			AvailableEvidence: []string{}, MissingEvidence: []string{}, Reasons: []string{},
			OperationalHeadSign: line.HeadSign, GTFSHeadSign: c.HeadSign, StartStop: c.StartStop, EndStop: c.EndStop}
		if fallback {
			a.CandidateSelection = "fallback_similarity"
			a.Reasons = append(a.Reasons, "no_exact_full_code_in_selected_feed_and_provider")
		}
		a.AvailableEvidence = append(a.AvailableEvidence, a.CandidateSelection)
		a.MissingEvidence = append(a.MissingEvidence, "operational_agency_identity")
		if c.Provider == "" {
			a.MissingEvidence = append(a.MissingEvidence, "feed_provider")
		}
		if c.HasGeometry {
			a.AvailableEvidence = append(a.AvailableEvidence, "valid_gtfs_shape")
		} else {
			a.MissingEvidence = append(a.MissingEvidence, "valid_gtfs_shape")
		}
		if line.HeadSign != "" {
			a.AvailableEvidence = append(a.AvailableEvidence, "explicit_operational_headsign")
		} else {
			a.MissingEvidence = append(a.MissingEvidence, "oriented_operational_headsign")
			a.Reasons = append(a.Reasons, "tp_ts_and_legacy_origin_destination_are_not_oriented_headsigns")
		}
		if !line.StartTerminal.HasPosition {
			a.MissingEvidence = append(a.MissingEvidence, "operational_origin_coordinates")
		}
		if !line.EndTerminal.HasPosition {
			a.MissingEvidence = append(a.MissingEvidence, "operational_destination_coordinates")
		}
		a.StopEvidence = compareStops(line, c)
		if a.StopEvidence.Available > 0 {
			a.AvailableEvidence = append(a.AvailableEvidence, "confirmed_sptrans_stops")
		} else {
			a.MissingEvidence = append(a.MissingEvidence, "operational_stops")
		}
		if !a.StopEvidence.Ordered {
			a.MissingEvidence = append(a.MissingEvidence, "official_operational_stop_sequence")
		}
		if line.Direction.Valid {
			d := line.Direction.Int64
			a.SptransDirection = &d
		}
		if c.DirectionID.Valid {
			d := c.DirectionID.Int64
			a.GTFSDirectionID = &d
		}
		name := terminalName(line.HeadSign)
		a.HeadSignMatches = name != "" && (name == terminalName(c.HeadSign) || name == terminalName(c.EndStop.Name))
		a.GeometryAligned = c.HasGeometry && c.StartStop.HasPosition && c.EndStop.HasPosition &&
			terminalDistance(c.ShapeStart, c.StartStop.Position) <= terminalToleranceKm &&
			terminalDistance(c.ShapeEnd, c.EndStop.Position) <= terminalToleranceKm
		if line.StartTerminal.HasPosition && c.StartStop.HasPosition {
			d := terminalDistance(line.StartTerminal.Position, c.StartStop.Position) * 1000
			a.StartDistanceM = &d
		}
		if line.EndTerminal.HasPosition && c.EndStop.HasPosition {
			d := terminalDistance(line.EndTerminal.Position, c.EndStop.Position) * 1000
			a.EndDistanceM = &d
		}
		contradicted := (a.StartDistanceM != nil && *a.StartDistanceM > terminalToleranceKm*1000) ||
			(a.EndDistanceM != nil && *a.EndDistanceM > terminalToleranceKm*1000)
		if contradicted || a.StopEvidence.Progression == "contradicted" {
			a.Reasons = append(a.Reasons, "oriented_operational_evidence_conflicts_with_pattern")
			a.EvidenceStatus = "contradicted"
		} else if a.GeometryAligned && (a.HeadSignMatches || (a.StartDistanceM != nil && a.EndDistanceM != nil) || a.StopEvidence.Progression == "forward") &&
			(!line.Circular || a.StopEvidence.Progression == "forward") {
			a.EvidenceStatus = "supported"
			if a.HeadSignMatches {
				a.OrientationScore = 0.6
				a.AvailableEvidence = append(a.AvailableEvidence, "operational_destination_matches_gtfs")
			}
			if a.StartDistanceM != nil && a.EndDistanceM != nil {
				a.OrientationScore = 0.8
				a.AvailableEvidence = append(a.AvailableEvidence, "oriented_terminal_pair")
			}
			if a.StopEvidence.Progression == "forward" {
				a.OrientationScore = 0.9
				a.AvailableEvidence = append(a.AvailableEvidence, "ordered_stop_progression")
			}
			a.Reasons = append(a.Reasons, "oriented_evidence_supports_pattern_requires_human_review")
			supported++
		}
		if a.EvidenceStatus == "insufficient" {
			a.Reasons = append(a.Reasons, "identity_does_not_determine_orientation")
		}
		if !a.GeometryAligned {
			a.Reasons = append(a.Reasons, "gtfs_endpoints_not_available_or_not_aligned_with_served_stops")
		}
		if line.Circular && a.StopEvidence.Progression != "forward" {
			a.Reasons = append(a.Reasons, "circular_route_requires_ordered_operational_evidence")
		}
		if name != "" && !a.HeadSignMatches {
			a.Reasons = append(a.Reasons, "headsign_not_equal_no_fuzzy_direction_inference")
		}
		a.Confidence = math.Min(a.IdentityScore, a.OrientationScore)
		a.MatchingMethod = method + "_direction_" + a.EvidenceStatus
		out = append(out, a)
	}
	// Identical terminals can describe different variants or circular routes.
	// Retain all candidates; never resolve a tie by PK, score or numeric direction.
	if supported > 1 {
		for i := range out {
			if out[i].EvidenceStatus == "supported" {
				out[i].EvidenceStatus = "ambiguous"
				out[i].VariantStatus = "ambiguous"
				out[i].Reasons = append(out[i].Reasons, "multiple_patterns_supported_no_unique_variant")
				out[i].MatchingMethod = strings.TrimSuffix(out[i].MatchingMethod, "supported") + "ambiguous"
			}
		}
	}
	return out
}
