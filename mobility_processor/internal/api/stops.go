package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"mobility_processor/internal/sptrans"
	cache "mobility_processor/internal/valkey_cache"
)

func (h *Handler) SyncStops(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SptransLineID int `json:"sptrans_line_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SptransLineID <= 0 {
		jsonError(w, "campo 'sptrans_line_id' obrigatório", http.StatusBadRequest)
		return
	}
	if h.stops == nil {
		jsonError(w, "repositório de paradas indisponível", http.StatusServiceUnavailable)
		return
	}

	lineStops, err := h.sptrans.GetStopsByLine(body.SptransLineID)
	if err != nil {
		log.Printf("[SyncStops] erro SPTrans: %v", err)
		jsonError(w, "falha ao buscar paradas", http.StatusBadGateway)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	persisted, linked, err := h.stops.UpsertStopsForLine(ctx, body.SptransLineID, lineStops)
	if err != nil {
		log.Printf("[SyncStops] erro banco: %v", err)
		jsonError(w, "falha ao salvar paradas", http.StatusBadGateway)
		return
	}

	go func(lineID int, stops []sptrans.Stop) {
		bgCtx := context.Background()
		_ = h.cache.SetJSON(bgCtx, cache.KeyStopsForLine(lineID), stops, cache.TTLStops)
		for _, s := range stops {
			payload := cache.CachedStop{
				SptransCode: s.CP,
				Name:        s.NP,
				Address:     s.Ed,
				Lat:         s.Lat,
				Lng:         s.Lng,
				UpdatedAt:   time.Now().Format(time.RFC3339),
			}
			_ = h.cache.SetJSON(bgCtx, cache.KeyStopByCP(s.CP), payload, cache.TTLStops)
		}
	}(body.SptransLineID, lineStops)

	jsonOK(w, map[string]any{
		"stops_received": len(lineStops),
		"stops_saved":    persisted,
		"links_saved":    linked,
	})
}

func (h *Handler) NearbyStops(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Lat           float64 `json:"lat"`
		Lng           float64 `json:"lng"`
		RadiusKm      float64 `json:"radius_km"`
		RadiusKmCamel float64 `json:"radiusKm"`
		Limit         int     `json:"limit"`
		OnlyWithLines bool    `json:"only_with_lines"`
		OnlyCamel     bool    `json:"onlyWithLines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "body inválido", http.StatusBadRequest)
		return
	}
	if h.stops == nil {
		jsonError(w, "repositório de paradas indisponível", http.StatusServiceUnavailable)
		return
	}

	if body.RadiusKm <= 0 {
		body.RadiusKm = body.RadiusKmCamel
	}
	if body.RadiusKm <= 0 {
		body.RadiusKm = 2.0
	}
	if body.Limit <= 0 {
		body.Limit = 1
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	onlyWithLines := body.OnlyWithLines || body.OnlyCamel
	nearby, err := h.stops.Nearby(ctx, body.Lat, body.Lng, body.RadiusKm, body.Limit, onlyWithLines)
	if err != nil {
		log.Printf("[NearbyStops] erro: %v", err)
		jsonError(w, "falha ao buscar paradas próximas", http.StatusBadGateway)
		return
	}

	jsonOK(w, map[string]any{"stops": nearby})
}
