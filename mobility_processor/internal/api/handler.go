package api

import (
	"mobility_processor/internal/lines"
	"mobility_processor/internal/sptrans"
	"mobility_processor/internal/stops"
	cache "mobility_processor/internal/valkey_cache"
	"net/http"
)

// Handler agrupa as dependências dos endpoints.
type Handler struct {
	sptrans *sptrans.Client
	lines   *lines.Repository
	stops   *stops.Repository
	cache   *cache.Client
}

func NewHandler(sptransClient *sptrans.Client, lineRepo *lines.Repository, stopRepo *stops.Repository, cacheClient *cache.Client) *Handler {
	return &Handler{
		sptrans: sptransClient,
		lines:   lineRepo,
		stops:   stopRepo,
		cache:   cacheClient,
	}
}

// RegisterRoutes registra as rotas no mux padrão.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /buses/{sptrans_id}", h.BusPositions)
	mux.HandleFunc("POST /geocode", h.Geocode)
	mux.HandleFunc("POST /reverse", h.ReverseGeocode)
	mux.HandleFunc("POST /nearby", h.Nearby)
	mux.HandleFunc("POST /nearby/spatial", h.NearbySpatial)
	mux.HandleFunc("POST /distance/sort", h.DistanceSort)
	mux.HandleFunc("POST /sync", h.Sync)
	mux.HandleFunc("POST /sync/stops", h.SyncStops)
	mux.HandleFunc("GET /lines/resolve", h.ResolveLine)
	mux.HandleFunc("POST /stops/nearby", h.NearbyStops)

	mux.HandleFunc("GET /admin/health", h.AdminHealth)
	mux.HandleFunc("GET /admin/stats", h.AdminStats)
	mux.HandleFunc("POST /admin/sync/line", h.AdminSyncLine)
	mux.HandleFunc("POST /admin/sync/stops", h.AdminSyncStops)
	mux.HandleFunc("POST /admin/sync/full", h.AdminFullSync)
	mux.HandleFunc("POST /admin/sync/valkey", h.AdminSyncValkey)
}
