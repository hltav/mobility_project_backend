package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"mobility_processor/internal/geocoding"
	"mobility_processor/internal/haversine"
	cache "mobility_processor/internal/valkey_cache"
)

func (h *Handler) Nearby(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address  string           `json:"address"`
		RadiusKm float64          `json:"radius_km"`
		Lines    []haversine.Line `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Address == "" {
		jsonError(w, "body inválido", http.StatusBadRequest)
		return
	}
	if body.RadiusKm <= 0 {
		body.RadiusKm = 1.0
	}

	coords, err := geocoding.Geocode(body.Address)
	if err != nil || coords == nil {
		jsonOK(w, map[string]any{"origin": nil, "lines": []any{}})
		return
	}

	nearby := haversine.FilterByRadius(body.Lines, coords.Lat, coords.Lng, body.RadiusKm)

	jsonOK(w, map[string]any{
		"origin": map[string]any{
			"address": body.Address,
			"lat":     coords.Lat,
			"lng":     coords.Lng,
		},
		"lines": nearby,
	})
}

func (h *Handler) NearbySpatial(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address  string  `json:"address"`
		RadiusKm float64 `json:"radius_km"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Address == "" {
		jsonError(w, "body inválido", http.StatusBadRequest)
		return
	}
	if body.RadiusKm <= 0 {
		body.RadiusKm = 1.0
	}
	if h.lines == nil {
		jsonError(w, "repositório de linhas indisponível", http.StatusServiceUnavailable)
		return
	}

	coords, err := geocoding.Geocode(body.Address)
	if err != nil || coords == nil {
		jsonOK(w, map[string]any{"origin": nil, "lines": []any{}})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	k := cache.KeyNearbyLines(coords.Lat, coords.Lng, body.RadiusKm)
	var cached []map[string]interface{}
	if hit, _ := h.cache.GetJSON(ctx, k, &cached); hit {
		jsonOK(w, map[string]any{
			"origin": map[string]any{
				"address": body.Address,
				"lat":     coords.Lat,
				"lng":     coords.Lng,
			},
			"lines": cached,
		})
		return
	}

	nearby, err := h.lines.Nearby(ctx, coords.Lat, coords.Lng, body.RadiusKm)
	if err != nil {
		log.Printf("[NearbySpatial] erro: %v", err)
		jsonError(w, "falha ao buscar linhas próximas", http.StatusBadGateway)
		return
	}

	_ = h.cache.SetJSON(ctx, k, nearby, 6*time.Hour)

	jsonOK(w, map[string]any{
		"origin": map[string]any{
			"address": body.Address,
			"lat":     coords.Lat,
			"lng":     coords.Lng,
		},
		"lines": nearby,
	})
}

func (h *Handler) ResolveLine(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	direction, _ := strconv.Atoi(r.URL.Query().Get("direction"))
	if code == "" {
		jsonError(w, "campo 'code' obrigatório", http.StatusBadRequest)
		return
	}
	if h.lines == nil {
		jsonError(w, "repositório de linhas indisponível", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	ck := cache.KeyLineByCode(code, direction)
	// Resolve identity from PostgreSQL; old cache keys may contain inferred/ambiguous directions.

	id, err := h.lines.ResolveSptransID(ctx, code, direction)
	if err != nil {
		log.Printf("[ResolveLine] erro: %v", err)
		jsonError(w, "falha ao resolver linha", http.StatusBadGateway)
		return
	}

	if id > 0 && h.cache != nil {
		_ = h.cache.SetJSON(ctx, ck, id, cache.TTLLines)
	}

	jsonOK(w, map[string]any{"sptrans_id": id})
}
