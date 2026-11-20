package matching

import "mobility_processor/internal/haversine"

// ShapeEndpoint representa o primeiro ou último ponto de um shape GTFS.
type ShapeEndpoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// spatialScore retorna 1.0 se a origem da linha SPTrans está muito próxima
// de um dos extremos do shape GTFS, decaindo linearmente até 0 a partir de maxDistanceKm.
func spatialScore(lineOrigin ShapeEndpoint, shapeStart, shapeEnd ShapeEndpoint, maxDistanceKm float64) float64 {
	dStart := haversine.DistanceKm(lineOrigin.Lat, lineOrigin.Lng, shapeStart.Lat, shapeStart.Lng)
	dEnd := haversine.DistanceKm(lineOrigin.Lat, lineOrigin.Lng, shapeEnd.Lat, shapeEnd.Lng)

	d := dStart
	if dEnd < d {
		d = dEnd
	}

	if d >= maxDistanceKm {
		return 0
	}
	return 1 - (d / maxDistanceKm)
}
