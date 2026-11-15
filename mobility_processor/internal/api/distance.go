package api

import (
	"encoding/json"
	"math"
	"net/http"
)

// POST /distance/sort
func (h *Handler) DistanceSort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Lat      float64                  `json:"lat"`
		Lng      float64                  `json:"lng"`
		RadiusKm float64                  `json:"radius_km"`
		Points   []map[string]interface{} `json:"points"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "body inválido", http.StatusBadRequest)
		return
	}
	if body.RadiusKm <= 0 {
		body.RadiusKm = 2.0
	}

	var result []map[string]interface{}
	for _, point := range body.Points {
		lat, okLat := numberFromMap(point, "lat", "latitude")
		lng, okLng := numberFromMap(point, "lng", "longitude")
		if !okLat || !okLng {
			continue
		}
		distance := distanceKm(body.Lat, body.Lng, lat, lng)
		if distance > body.RadiusKm {
			continue
		}
		point["distance_km"] = math.Round(distance*1000) / 1000
		result = append(result, point)
	}
	sortPointsByDistance(result)

	jsonOK(w, map[string]any{"points": result})
}
