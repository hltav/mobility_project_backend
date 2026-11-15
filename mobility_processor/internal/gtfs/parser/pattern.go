package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func PatternFingerprint(t Trip, seq []StopTime) string {
	type occurrence struct {
		Stop            string
		Sequence        int
		Pickup, DropOff *int
		Distance        any
	}
	stops := []occurrence{}
	for _, s := range seq {
		stops = append(stops, occurrence{s.GTFSStopID, s.StopSequence, s.Pickup, s.DropOff, sourceDistance(s.Raw, s.Distance)})
	}
	payload := struct {
		Route, Shape, Headsign string
		Direction              *int
		Stops                  []occurrence
	}{t.GTFSRouteID, t.ShapeID, t.Headsign, t.DirectionID, stops}
	digest := sha256.Sum256([]byte(encodeJSON(payload)))
	return hex.EncodeToString(digest[:])
}

func encodeJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func sourceDistance(r Raw, v *float64) any {
	if v == nil {
		return nil
	}
	if s := r["shape_dist_traveled"]; s != "" {
		return s
	}
	return *v
}
