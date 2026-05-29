package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"

	"mobility_processor/internal/geocoding"
	"mobility_processor/internal/haversine"
	"mobility_processor/internal/sptrans"
)

// Handler agrupa as dependências dos endpoints.
type Handler struct {
	sptrans *sptrans.Client
}

func NewHandler(sptransClient *sptrans.Client) *Handler {
	return &Handler{sptrans: sptransClient}
}

// RegisterRoutes registra as rotas no mux padrão.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /buses/{sptrans_id}", h.BusPositions)
	mux.HandleFunc("POST /geocode", h.Geocode)
	mux.HandleFunc("POST /reverse", h.ReverseGeocode)
	mux.HandleFunc("POST /nearby", h.Nearby)
	mux.HandleFunc("POST /sync", h.Sync)
}

// GET /buses/{sptrans_id}
// Retorna as posições dos ônibus de uma linha em tempo real.
func (h *Handler) BusPositions(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("sptrans_id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		jsonError(w, "sptrans_id inválido", http.StatusBadRequest)
		return
	}

	positions, err := h.sptrans.GetBusPositionsByLine(id)
	if err != nil {
		log.Printf("[BusPositions] erro: %v", err)
		jsonError(w, "falha ao buscar posições", http.StatusBadGateway)
		return
	}

	jsonOK(w, map[string]any{"buses": positions})
}

// POST /geocode
// Body: {"address": "Av. Paulista, 1000"}
// Retorna: {"lat": -23.56, "lng": -46.65}
func (h *Handler) Geocode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Address == "" {
		jsonError(w, "campo 'address' obrigatório", http.StatusBadRequest)
		return
	}

	coords, err := geocoding.Geocode(body.Address)
	if err != nil {
		log.Printf("[Geocode] erro: %v", err)
		jsonError(w, "falha ao geocodificar", http.StatusBadGateway)
		return
	}
	if coords == nil {
		jsonOK(w, map[string]any{"coords": nil})
		return
	}

	jsonOK(w, map[string]any{"coords": coords})
}

// POST /reverse
// Body: {"lat": -23.56, "lng": -46.65}
// Retorna: {"address": "Avenida Paulista, Bela Vista, São Paulo"}
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

// POST /nearby
// Body: {"address": "...", "radius_km": 1.0, "lines": [...]}
// Retorna linhas próximas ordenadas por distância.
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

// POST /sync
// Body: {"terms": ["8000", "1", "2", ...]}
// Autentica na SPTrans, busca as linhas pelos termos em paralelo e devolve os dados brutos.
// O Laravel recebe, persiste no banco.
func (h *Handler) Sync(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Terms []string `json:"terms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Terms) == 0 {
		jsonError(w, "campo 'terms' obrigatório", http.StatusBadRequest)
		return
	}

	if err := h.sptrans.Authenticate(); err != nil {
		log.Printf("[Sync] auth falhou: %v", err)
		jsonError(w, "falha na autenticação com SPTrans", http.StatusBadGateway)
		return
	}

	var (
		mu    sync.Mutex
		lines []sptrans.RawLine
	)

	_ = h.sptrans.SyncLines(body.Terms, 10, func(line sptrans.RawLine) error {
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
		return nil
	})

	jsonOK(w, map[string]any{"lines": lines})
}

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
