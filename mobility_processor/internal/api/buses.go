package api

import (
	"log"
	"mobility_processor/internal/sptrans"
	cache "mobility_processor/internal/valkey_cache"
	"net/http"
	"strconv"
)

// GET /buses/{sptrans_id}
func (h *Handler) BusPositions(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("sptrans_id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		jsonError(w, "sptrans_id inválido", http.StatusBadRequest)
		return
	}
	if h.predictionDemand != nil {
		h.predictionDemand.RecordDemand(id)
	}

	positions, err := h.sptrans.GetBusPositionsByLine(id)
	if err != nil {
		log.Printf("[BusPositions] erro: %v", err)
		jsonError(w, "falha ao buscar posições", http.StatusBadGateway)
		return
	}

	// Reuse the observation already fetched by this endpoint. Progress readers
	// never trigger a new external SPTrans request.
	if h.cache != nil {
		snapshot := busSnapshot(positions)
		if err := h.cache.SetJSON(r.Context(), cache.KeyBusesForLine(id), snapshot, cache.TTLBuses); err != nil {
			log.Print("[BusPositions] snapshot cache unavailable")
		}
	}
	jsonOK(w, map[string]any{"buses": positions})
}

func busSnapshot(positions []sptrans.BusPosition) []cache.CachedBusPosition {
	snapshot := make([]cache.CachedBusPosition, 0, len(positions))
	for _, p := range positions {
		snapshot = append(snapshot, cache.CachedBusPosition{Prefix: p.Prefix, Accessible: p.Accessible, CapturedAt: p.CapturedAt, Lat: p.Latitude, Lng: p.Longitude})
	}
	return snapshot
}
