package api

import (
	"mobility_processor/internal/lines"
	"mobility_processor/internal/sptrans"
	"mobility_processor/internal/stops"
	cache "mobility_processor/internal/valkey_cache"
	"net/http"
	"sync/atomic"
)

// Handler agrupa as dependências dos endpoints.
type Handler struct {
	sptrans          *sptrans.Client
	lines            *lines.Repository
	stops            *stops.Repository
	cache            *cache.Client
	predictionDemand interface{ RecordDemand(int) bool }
	catalogProvider  catalogProvider
	catalogLines     catalogLines
	catalogRunning   atomic.Bool
	adminToken       string
	stopProvider     stopSyncProvider
	stopStore        stopSyncStore
	stopIdentity     stopSyncIdentity
	stopAllowed      map[int]bool
	stopRunning      atomic.Bool
}

// SetCatalogAdminToken must be called before serving; empty disables this action.
func (h *Handler) SetCatalogAdminToken(token string) { h.adminToken = token }

// SetPredictionDemandRecorder is configured once before serving requests.
func (h *Handler) SetPredictionDemandRecorder(recorder interface{ RecordDemand(int) bool }) {
	h.predictionDemand = recorder
}

func NewHandler(sptransClient *sptrans.Client, lineRepo *lines.Repository, stopRepo *stops.Repository, cacheClient *cache.Client) *Handler {
	h := &Handler{
		sptrans: sptransClient,
		lines:   lineRepo,
		stops:   stopRepo,
		cache:   cacheClient,
	}
	if sptransClient != nil {
		h.catalogProvider = sptransClient
		h.stopProvider = sptransClient
	}
	if lineRepo != nil {
		h.catalogLines = lineRepo
		h.stopIdentity = lineRepo
	}
	if stopRepo != nil {
		h.stopStore = stopRepo
	}
	return h
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
