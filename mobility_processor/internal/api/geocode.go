package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"mobility_processor/internal/geocoding"
	cache "mobility_processor/internal/valkey_cache"
)

func (h *Handler) Geocode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Address == "" {
		jsonError(w, "campo 'address' obrigatório", http.StatusBadRequest)
		return
	}

	addrNorm := strings.ToLower(strings.TrimSpace(body.Address))
	key := cache.KeyGeocodeAddress(addrNorm)
	ctx := r.Context()

	var cached cache.CachedGeocodeResult
	if hit, err := h.cache.GetJSON(ctx, key, &cached); err != nil {
		log.Printf("[CACHE ERROR] GET address=%q err=%v", body.Address, err)
	} else if hit {
		log.Printf("[CACHE HIT] address=%q lat=%f lng=%f", body.Address, cached.Lat, cached.Lng)
		jsonOK(w, map[string]any{"coords": map[string]any{"lat": cached.Lat, "lng": cached.Lng}})
		return
	}

	log.Printf("[CACHE MISS] address=%q", body.Address)

	coords, err := geocoding.Geocode(body.Address)
	if err != nil {
		log.Printf("[GEOCODE ERROR] address=%q err=%v", body.Address, err)
		jsonError(w, "falha ao geocodificar", http.StatusBadGateway)
		return
	}
	if coords == nil {
		jsonOK(w, map[string]any{"coords": nil})
		return
	}

	payload := cache.CachedGeocodeResult{
		Lat:       coords.Lat,
		Lng:       coords.Lng,
		Address:   body.Address,
		UpdatedAt: time.Now().Format(time.RFC3339),
	}
	if err := h.cache.SetJSON(ctx, key, payload, cache.TTLGeocode); err != nil {
		log.Printf("[CACHE ERROR] SET address=%q err=%v", body.Address, err)
	}

	jsonOK(w, map[string]any{"coords": map[string]any{"lat": coords.Lat, "lng": coords.Lng}})
}

func (h *Handler) ReverseGeocode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "body inválido", http.StatusBadRequest)
		return
	}

	address, err := geocoding.Reverse(body.Lat, body.Lng)
	if err != nil {
		log.Printf("[Reverse] erro: %v", err)
		jsonError(w, "falha no reverse geocoding", http.StatusBadGateway)
		return
	}

	jsonOK(w, map[string]any{"address": address})
}
