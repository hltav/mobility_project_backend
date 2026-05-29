package haversine

import "math"

const earthRadiusKm = 6371.0

// DistanceKm calcula a distância em km entre dois pontos geográficos.
func DistanceKm(lat1, lng1, lat2, lng2 float64) float64 {
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*
			math.Sin(dLng/2)*math.Sin(dLng/2)

	return earthRadiusKm * 2 * math.Asin(math.Sqrt(a))
}

// Line representa uma linha de ônibus com coordenadas de origem.
type Line struct {
	ID          int     `json:"id"`
	SptransID   int     `json:"sptrans_id"`
	Name        string  `json:"name"`
	Destination string  `json:"destination"`
	OriginLat   float64 `json:"origin_lat"`
	OriginLng   float64 `json:"origin_lng"`
}

// NearbyLine é uma Line enriquecida com a distância calculada.
type NearbyLine struct {
	Line
	DistanceKm float64 `json:"distance_km"`
}

// FilterByRadius filtra e ordena linhas dentro do raio informado.
// Linhas sem coordenadas são ignoradas.
// O processamento é paralelizado com goroutines.
func FilterByRadius(lines []Line, lat, lng, radiusKm float64) []NearbyLine {
	type result struct {
		line     NearbyLine
		distance float64
	}

	resultsCh := make(chan result, len(lines))

	for _, l := range lines {
		go func(l Line) {
			if l.OriginLat == 0 && l.OriginLng == 0 {
				resultsCh <- result{distance: -1}
				return
			}

			dist := DistanceKm(lat, lng, l.OriginLat, l.OriginLng)
			if dist > radiusKm {
				resultsCh <- result{distance: -1}
				return
			}

			resultsCh <- result{
				line:     NearbyLine{Line: l, DistanceKm: math.Round(dist*1000) / 1000},
				distance: dist,
			}
		}(l)
	}

	var nearby []NearbyLine
	for range lines {
		r := <-resultsCh
		if r.distance >= 0 {
			nearby = append(nearby, r.line)
		}
	}

	// Ordena por distância crescente
	for i := 1; i < len(nearby); i++ {
		for j := i; j > 0 && nearby[j].DistanceKm < nearby[j-1].DistanceKm; j-- {
			nearby[j], nearby[j-1] = nearby[j-1], nearby[j]
		}
	}

	return nearby
}

func toRad(deg float64) float64 {
	return deg * math.Pi / 180
}
