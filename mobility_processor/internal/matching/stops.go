package matching

import (
	"math"
	"sort"
)

// 100 m tolerates differences between stop coordinates, while avoiding using
// the broader 500 m terminal tolerance to associate neighboring stops.
const stopToleranceKm = 0.1

type StopEvidence struct {
	Available   int     `json:"available"`
	Matched     int     `json:"matched"`
	Coverage    float64 `json:"coverage"`
	Ordered     bool    `json:"ordered"`
	Progression string  `json:"progression"`
}

func compareStops(line LineToMatch, c Candidate) StopEvidence {
	e := StopEvidence{Available: len(line.Stops), Progression: "unavailable"}
	if len(line.Stops) == 0 || len(c.Stops) == 0 {
		return e
	}
	stops := append([]OperationalStop(nil), line.Stops...)
	e.Ordered = true
	for _, s := range stops {
		if s.Sequence == nil {
			e.Ordered = false
		}
	}
	if e.Ordered {
		sort.SliceStable(stops, func(i, j int) bool { return *stops[i].Sequence < *stops[j].Sequence })
	}
	located := []int{}
	unique := true
	for i, s := range stops {
		if e.Ordered && i > 0 && *s.Sequence == *stops[i-1].Sequence {
			unique = false
		}
		nearest, second := math.Inf(1), math.Inf(1)
		sequence := 0
		for _, g := range c.Stops {
			d := terminalDistance(s.Position, g.Position)
			if d < nearest {
				second = nearest
				nearest = d
				sequence = g.Sequence
			} else if d < second {
				second = d
			}
		}
		if nearest <= stopToleranceKm {
			e.Matched++
			located = append(located, sequence)
			// Repeated GTFS stops and indistinguishable platforms cannot establish order.
			if second-nearest < 0.01 {
				unique = false
			}
		}
	}
	e.Coverage = float64(e.Matched) / float64(e.Available)
	if !e.Ordered {
		e.Progression = "unordered"
		return e
	}
	if !unique {
		e.Progression = "ambiguous"
		return e
	}
	if len(located) < 3 || e.Coverage < 0.8 {
		e.Progression = "insufficient"
		return e
	}
	decreases := 0
	for i := 1; i < len(located); i++ {
		if located[i] == located[i-1] {
			e.Progression = "ambiguous"
			return e
		}
		if located[i] < located[i-1] {
			decreases++
		}
	}
	if decreases == 0 || (line.Circular && decreases == 1) {
		e.Progression = "forward"
	} else {
		e.Progression = "contradicted"
	}
	return e
}
