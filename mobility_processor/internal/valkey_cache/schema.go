package cache

import (
	"crypto/sha1"
	"fmt"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// MODELO DE DADOS DE CACHE
//
// Este arquivo define os tipos, formatos JSON e nomes de chaves para cache.
// Todas as chaves são determinísticas (mesma entrada → mesma chave).
// Use os funções helper KeyFor* para gerar chaves previsíveis.
// ─────────────────────────────────────────────────────────────────────────────

// ────────────────────────────────── LINHAS ────────────────────────────────

// CachedLine representa uma linha de ônibus no cache.
// Fonte: SPTrans API (sptrans.RawLine)
// TTL: TTLLines (60 dias)
//
// Exemplo JSON:
//
//	{
//	  "id": 33624,
//	  "code": "8000-10",
//	  "name": "Av. Paulista - Terminal Parque Dom Pedro",
//	  "origin": "Av. Paulista",
//	  "destination": "Terminal Parque Dom Pedro",
//	  "circular": false,
//	  "sptrans_id": 33624,
//	  "updated_at": "2026-06-03T14:30:00Z"
//	}
type CachedLine struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Origin      string `json:"origin"`
	Destination string `json:"destination"`
	Circular    bool   `json:"circular"`
	SptransID   int    `json:"sptrans_id"`
	UpdatedAt   string `json:"updated_at"`
}

// KeyLineByID gera chave de uma linha pelo sptrans_id.
// Format: "lines:id:<sptrans_id>"
func KeyLineByID(sptransID int) string {
	return fmt.Sprintf("lines:id:%d", sptransID)
}

// KeyLineByCode gera chave de resolver uma linha pelo code+direction.
// Format: "lines:code:<code>:dir:<direction>"
// Direction: SPTrans SL 1/2; 0 is unspecified and must not be filled by choosing a sorted ID.
func KeyLineByCode(code string, direction int) string {
	code = strings.TrimSpace(strings.ToUpper(code))
	return fmt.Sprintf("lines:code:%s:dir:%d", code, direction)
}

// KeyLineSearch gera chave para cache de busca por termo.
// Format: "lines:search:term:<term_hash>"
// Armazena: array de sptrans_id ou CachedLine reduzida
func KeyLineSearch(term string) string {
	term = strings.TrimSpace(strings.ToLower(term))
	h := fmt.Sprintf("%x", sha1.Sum([]byte(term)))
	return fmt.Sprintf("lines:search:%s", h[:8]) // primeiros 8 chars do hash
}

// ────────────────────────────────── PARADAS ────────────────────────────────

// CachedStop representa uma parada de ônibus no cache.
// Fonte: SPTrans API (sptrans.Stop)
// TTL: TTLStops (30 dias)
//
// Exemplo JSON:
//
//	{
//	  "sptrans_code": 1023,
//	  "name": "Avenida Paulista",
//	  "address": "Avenida Paulista, 1000",
//	  "lat": -23.5614,
//	  "lng": -46.6558,
//	  "updated_at": "2026-06-03T14:30:00Z"
//	}
type CachedStop struct {
	SptransCode int     `json:"sptrans_code"`
	Name        string  `json:"name"`
	Address     string  `json:"address"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	UpdatedAt   string  `json:"updated_at"`
}

// KeyStopByCP gera chave de uma parada pelo código SPTrans (CP).
// Format: "stops:cp:<sptrans_code>"
func KeyStopByCP(sptransCode int) string {
	return fmt.Sprintf("stops:cp:%d", sptransCode)
}

// KeyStopsForLine gera chave de paradas de uma linha.
// Format: "stops:line:<sptrans_line_id>"
// Armazena: array de CachedStop
func KeyStopsForLine(sptransLineID int) string {
	return fmt.Sprintf("stops:line:%d", sptransLineID)
}

// KeyNearbyStops gera chave de paradas próximas por coordenada+raio.
// Format: "stops:nearby:<lat_int>:<lng_int>:<radius_int>"
// Exemplo: stops:nearby:-2356:-4665:2 para -23.56, -46.65, 2km
// Armazena: array de CachedStop
// TTL: 6 horas (dados consultados com frequência, mas relativamente estáveis)
func KeyNearbyStops(lat, lng, radiusKm float64) string {
	// truncar para 2 casas decimais (precisão ~1km)
	latInt := int(lat * 100)
	lngInt := int(lng * 100)
	radiusInt := int(radiusKm * 100)
	return fmt.Sprintf("stops:nearby:%d:%d:%d", latInt, lngInt, radiusInt)
}

// ────────────────────────────────── GEOCODING ────────────────────────────────

// CachedGeocodeResult representa resultado de geocoding em cache.
// Fonte: Google Maps / serviço de geocoding
// TTL: TTLGeocode (30 dias)
//
// Exemplo JSON:
//
//	{
//	  "lat": -23.5614,
//	  "lng": -46.6558,
//	  "address": "Avenida Paulista, 1000, São Paulo",
//	  "updated_at": "2026-06-03T14:30:00Z"
//	}
type CachedGeocodeResult struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Address   string  `json:"address"`
	UpdatedAt string  `json:"updated_at"`
}

// KeyGeocodeAddress gera chave para resultado de geocoding de um endereço.
// Format: "geocode:addr:<addr_hash>"
// O hash normaliza o endereço (lowercase, trim, sha1) para consistência.
func KeyGeocodeAddress(address string) string {
	address = strings.ToLower(strings.TrimSpace(address))
	h := fmt.Sprintf("%x", sha1.Sum([]byte(address)))
	return fmt.Sprintf("geocode:addr:%s", h[:16]) // primeiros 16 chars
}

// ────────────────────────────────── LINHAS PRÓXIMAS ────────────────────────────────

// CachedNearbyLine representa uma linha próxima a um local.
// Fonte: Consulta spatial no banco (lines.Nearby)
// TTL: 6 horas
//
// Exemplo JSON:
//
//	{
//	  "id": 1,
//	  "code": "8000-10",
//	  "name": "Av. Paulista - Terminal",
//	  "origin": "Av. Paulista",
//	  "destination": "Terminal",
//	  "circular": false,
//	  "sptrans_id": 33624,
//	  "origin_lat": -23.5614,
//	  "origin_lng": -46.6558,
//	  "distance_km": 0.25
//	}
type CachedNearbyLine struct {
	ID          int     `json:"id"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Origin      string  `json:"origin"`
	Destination string  `json:"destination"`
	Circular    bool    `json:"circular"`
	SptransID   int     `json:"sptrans_id"`
	OriginLat   float64 `json:"origin_lat"`
	OriginLng   float64 `json:"origin_lng"`
	DistanceKm  float64 `json:"distance_km"`
}

// KeyNearbyLines gera chave para cache de linhas próximas por coordenada+raio.
// Format: "nearby:lines:<lat>:<lng>:<radius>"
// Exemplo: nearby:lines:-23.5614:-46.6558:1.00
// Armazena: array de CachedNearbyLine
// TTL: 6 horas
func KeyNearbyLines(lat, lng, radiusKm float64) string {
	return fmt.Sprintf("nearby:lines:%.4f:%.4f:%.2f", lat, lng, radiusKm)
}

// ────────────────────────────────── ÔNIBUS ────────────────────────────────

// CachedBusPosition representa posição de um ônibus em tempo real.
// Fonte: SPTrans API (sptrans.BusPosition)
// TTL: TTLBuses (60 segundos) — muito curto, dados voláteis
//
// Exemplo JSON:
//
//	{
//	  "prefix": "5037",
//	  "accessible": false,
//	  "captured_at": "2026-06-03T14:35:12Z",
//	  "lat": -23.5620,
//	  "lng": -46.6550
//	}
type CachedBusPosition struct {
	Prefix     string  `json:"prefix"`
	Accessible bool    `json:"accessible"`
	CapturedAt string  `json:"captured_at"`
	Lat        float64 `json:"lat"`
	Lng        float64 `json:"lng"`
}

// KeyBusesForLine gera chave de posições de ônibus de uma linha.
// Format: "buses:line:<sptrans_line_id>"
// Armazena: array de CachedBusPosition
// TTL: TTLBuses (60 segundos)
func KeyBusesForLine(sptransLineID int) string {
	return fmt.Sprintf("buses:line:%d", sptransLineID)
}

// ────────────────────────────────── METADATA ────────────────────────────────

// CacheMetadata registra informações sobre o estado do cache.
// Útil para invalidação em massa ou versioning.
//
// Exemplo JSON:
//
//	{
//	  "version": "1.0.0",
//	  "lines_synced_at": "2026-06-03T14:30:00Z",
//	  "stops_synced_at": "2026-06-03T14:35:00Z",
//	  "total_lines": 12500,
//	  "total_stops": 18000
//	}
type CacheMetadata struct {
	Version       string `json:"version"`
	LinesSyncedAt string `json:"lines_synced_at"`
	StopsSyncedAt string `json:"stops_synced_at"`
	TotalLines    int    `json:"total_lines"`
	TotalStops    int    `json:"total_stops"`
}

// KeyMetadata gera chave de metadata do cache.
// Format: "meta:cache:version"
func KeyMetadata() string {
	return "meta:cache:version"
}

// ────────────────────────────────── PADRÃO DE NOMENCLATURA ────────────────────────────────
//
// Prefixos de chave (hierarquia):
//   lines:*          → dados sobre linhas
//   lines:id:<id>    → linha individual por sptrans_id
//   lines:code:*     → linha por código
//   lines:search:*   → busca de linhas por termo
//
//   stops:*          → dados sobre paradas
//   stops:cp:<cp>    → parada individual por código SPTrans
//   stops:line:*     → paradas de uma linha
//   stops:nearby:*   → paradas próximas a um local
//
//   nearby:*         → dados de proximidade
//   nearby:lines:*   → linhas próximas a um local
//
//   buses:*          → dados de ônibus
//   buses:line:*     → posições de ônibus de uma linha
//
//   geocode:*        → resultados de geocoding
//   geocode:addr:*   → endereço geocodificado
//
//   meta:*           → metadata e versioning
//
// Versionamento (para quebra de compatibilidade):
//   Adicione `:v2` ou similar ao final da chave se o formato mudar.
//   Exemplo: "lines:id:33624:v2" para nova versão da estrutura.
//
// ────────────────────────────────────────────────────────────────────────────
