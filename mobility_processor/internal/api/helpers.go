package api

import (
	"encoding/json"
	"math"
	"net/http"
)

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func numberFromMap(point map[string]interface{}, keys ...string) (float64, bool) {
	for _, key := range keys {
		value, ok := point[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return typed, true
		case int:
			return float64(typed), true
		case json.Number:
			if f, err := typed.Float64(); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

func distanceKm(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusKm = 6371.0
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return earthRadiusKm * 2 * math.Asin(math.Sqrt(a))
}

func toRad(deg float64) float64 {
	return deg * math.Pi / 180
}

func sortPointsByDistance(points []map[string]interface{}) {
	distance := func(point map[string]interface{}) float64 {
		value, _ := numberFromMap(point, "distance_km")
		return value
	}

	// Insertion sort para slices pequenos
	for i := 1; i < len(points); i++ {
		for j := i; j > 0 && distance(points[j]) < distance(points[j-1]); j-- {
			points[j], points[j-1] = points[j-1], points[j]
		}
	}
}
