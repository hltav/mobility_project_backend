// package api

// import (
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"log"
// 	"net/http"
// 	"strings"
// 	"time"

// 	"mobility_processor/internal/geocoding"
// 	"mobility_processor/internal/sptrans"
// 	cache "mobility_processor/internal/valkey_cache"
// )

// func (h *Handler) AdminHealth(w http.ResponseWriter, r *http.Request) {
// 	jsonOK(w, map[string]any{
// 		"status":    "ok",
// 		"service":   "mobility-processor",
// 		"timestamp": time.Now().UTC(),
// 	})
// }

// // GET /admin/stats
// func (h *Handler) AdminStats(w http.ResponseWriter, r *http.Request) {
// 	jsonOK(w, map[string]any{
// 		"status": "ok",
// 		"stats": map[string]any{
// 			"lines":      "implement Count()",
// 			"stops":      "implement Count()",
// 			"line_stops": "implement CountLinks()",
// 		},
// 	})
// }

// // POST /admin/sync/line
// func (h *Handler) AdminSyncLine(w http.ResponseWriter, r *http.Request) {

// 	var body struct {
// 		Term string `json:"term"`
// 	}

// 	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
// 		jsonError(w, "body inválido", http.StatusBadRequest)
// 		return
// 	}

// 	if body.Term == "" {
// 		jsonError(w, "campo 'term' obrigatório", http.StatusBadRequest)
// 		return
// 	}

// 	if err := h.sptrans.Authenticate(); err != nil {
// 		jsonError(w, "falha na autenticação SPTrans", http.StatusBadGateway)
// 		return
// 	}

// 	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
// 	defer cancel()

// 	var synced []sptrans.RawLine

// 	result := h.sptrans.SyncLines(
// 		[]string{body.Term},
// 		10,
// 		func(line sptrans.RawLine) error {

// 			if h.lines != nil {
// 				if err := h.lines.UpsertFromSPTrans(ctx, line); err != nil {
// 					return err
// 				}
// 			}

// 			synced = append(synced, line)

// 			return nil
// 		},
// 	)

// 	if len(result.Errors) > 0 {
// 		log.Printf("[AdminSyncLine] erros: %v", result.Errors)

// 		jsonError(
// 			w,
// 			"falha ao sincronizar linhas",
// 			http.StatusBadGateway,
// 		)

// 		return
// 	}

// 	jsonOK(w, map[string]any{
// 		"term":            body.Term,
// 		"lines_processed": len(synced),
// 		"lines":           synced,
// 	})
// }

// // POST /admin/sync/stops
// func (h *Handler) AdminSyncStops(w http.ResponseWriter, r *http.Request) {

// 	var body struct {
// 		SptransLineID int `json:"sptrans_line_id"`
// 	}

// 	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
// 		jsonError(w, "body inválido", http.StatusBadRequest)
// 		return
// 	}

// 	if body.SptransLineID <= 0 {
// 		jsonError(w, "campo 'sptrans_line_id' obrigatório", http.StatusBadRequest)
// 		return
// 	}

// 	lineStops, err := h.sptrans.GetStopsByLine(body.SptransLineID)
// 	if err != nil {
// 		log.Printf("[AdminSyncStops] erro SPTrans: %v", err)

// 		jsonError(
// 			w,
// 			"falha ao buscar paradas",
// 			http.StatusBadGateway,
// 		)

// 		return
// 	}

// 	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
// 	defer cancel()

// 	persisted, linked, err := h.stops.UpsertStopsForLine(
// 		ctx,
// 		body.SptransLineID,
// 		lineStops,
// 	)

// 	if err != nil {
// 		log.Printf("[AdminSyncStops] erro banco: %v", err)

// 		jsonError(
// 			w,
// 			"falha ao salvar paradas",
// 			http.StatusBadGateway,
// 		)

// 		return
// 	}

// 	jsonOK(w, map[string]any{
// 		"line_id":       body.SptransLineID,
// 		"stops_fetched": len(lineStops),
// 		"stops_saved":   persisted,
// 		"links_saved":   linked,
// 	})
// }

// // POST /admin/sync/full
// func (h *Handler) AdminFullSync(w http.ResponseWriter, r *http.Request) {

// 	go func() {
// 		log.Println("[ADMIN FULL SYNC] iniciado")

// 		terms := []string{
// 			"0", "1", "2", "3", "4",
// 			"5", "6", "7", "8", "9",
// 		}

// 		if err := h.sptrans.Authenticate(); err != nil {
// 			log.Printf("[ADMIN FULL SYNC] auth erro: %v", err)
// 			return
// 		}

// 		ctx := context.Background()

// 		var totalLines int
// 		var totalStops int64

// 		result := h.sptrans.SyncLines(
// 			terms,
// 			10,
// 			func(line sptrans.RawLine) error {
// 				totalLines++

// 				if totalLines%100 == 0 {
// 					log.Printf("[ADMIN FULL SYNC] progresso: %d linhas, %d paradas", totalLines, totalStops)
// 				}

// 				if h.lines != nil {
// 					if err := h.lines.UpsertFromSPTrans(ctx, line); err != nil {
// 						return err
// 					}
// 				}

// 				// Grava linha no Valkey
// 				if h.cache != nil {
// 					payload := cache.CachedLine{
// 						Code:        strings.TrimSpace(line.LT),
// 						Name:        strings.TrimSpace(line.TP + " - " + line.TS),
// 						Origin:      strings.TrimSpace(line.TP),
// 						Destination: strings.TrimSpace(line.TS),
// 						Circular:    line.LC,
// 						SptransID:   line.CL,
// 						UpdatedAt:   time.Now().Format(time.RFC3339),
// 					}
// 					_ = h.cache.SetJSON(ctx, cache.KeyLineByID(line.CL), payload, cache.TTLLines)
// 					_ = h.cache.SetJSON(ctx, cache.KeyLineByCode(line.LT, 0), line.CL, cache.TTLLines)
// 				}

// 				stops, err := h.sptrans.GetStopsByLine(line.CL)
// 				if err != nil {
// 					log.Printf("[ADMIN FULL SYNC] stops erro line=%d err=%v", line.CL, err)
// 					return nil
// 				}

// 				if h.stops != nil {
// 					saved, _, err := h.stops.UpsertStopsForLine(ctx, line.CL, stops)
// 					if err != nil {
// 						return err
// 					}
// 					totalStops += saved

// 					// Grava paradas no Valkey
// 					if h.cache != nil {
// 						_ = h.cache.SetJSON(ctx, cache.KeyStopsForLine(line.CL), stops, cache.TTLStops)
// 						for _, s := range stops {
// 							payload := cache.CachedStop{
// 								SptransCode: s.CP,
// 								Name:        s.NP,
// 								Address:     s.Ed,
// 								Lat:         s.Lat,
// 								Lng:         s.Lng,
// 								UpdatedAt:   time.Now().Format(time.RFC3339),
// 							}
// 							_ = h.cache.SetJSON(ctx, cache.KeyStopByCP(s.CP), payload, cache.TTLStops)
// 						}
// 					}
// 				}

// 				return nil
// 			},
// 		)

// 		if len(result.Errors) > 0 {
// 			for _, e := range result.Errors {
// 				log.Printf("[ADMIN FULL SYNC] erro: %v", e)
// 			}
// 			return
// 		}

// 		log.Printf("[ADMIN FULL SYNC] finalizado lines=%d stops=%d", totalLines, totalStops)
// 	}()

// 	jsonOK(w, map[string]any{"status": "started"})
// }

// // POST /admin/sync/valkey
// // Sincroniza linhas + paradas + geocodificação direto no Valkey, sem MySQL.
// // Aceita {"terms": ["874", "8000"]} ou omite terms para usar dígitos 0-9.
// func (h *Handler) AdminSyncValkey(w http.ResponseWriter, r *http.Request) {
// 	var body struct {
// 		Terms []string `json:"terms"`
// 	}
// 	json.NewDecoder(r.Body).Decode(&body)
// 	if len(body.Terms) == 0 {
// 		body.Terms = []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}
// 	}

// 	go func(terms []string) {
// 		log.Printf("[VALKEY SYNC] iniciado terms=%v", terms)

// 		ctx := context.Background()

// 		if err := h.sptrans.Authenticate(); err != nil {
// 			log.Printf("[VALKEY SYNC] auth erro: %v", err)
// 			return
// 		}

// 		var totalLines, totalStops, totalGeocoded int

// 		result := h.sptrans.SyncLines(terms, 1, func(line sptrans.RawLine) error {
// 			totalLines++

// 			if totalLines%50 == 0 {
// 				log.Printf("[VALKEY SYNC] progresso: lines=%d stops=%d geocoded=%d",
// 					totalLines, totalStops, totalGeocoded)
// 			}

// 			// Geocodifica origem
// 			var originLat, originLng float64
// 			originCoords, err := geocoding.Geocode(line.TP)
// 			if err != nil {
// 				log.Printf("[VALKEY SYNC] geocode erro line=%d origin=%q err=%v", line.CL, line.TP, err)
// 			} else if originCoords != nil {
// 				originLat = originCoords.Lat
// 				originLng = originCoords.Lng
// 				totalGeocoded++
// 			}

// 			// Grava linha no Valkey com coords
// 			payload := cache.CachedLine{
// 				Code:        strings.TrimSpace(line.LT),
// 				Name:        strings.TrimSpace(line.TP + " - " + line.TS),
// 				Origin:      strings.TrimSpace(line.TP),
// 				Destination: strings.TrimSpace(line.TS),
// 				Circular:    line.LC,
// 				SptransID:   line.CL,
// 				UpdatedAt:   time.Now().Format(time.RFC3339),
// 			}
// 			_ = h.cache.SetJSON(ctx, cache.KeyLineByID(line.CL), payload, cache.TTLLines)
// 			_ = h.cache.SetJSON(ctx, cache.KeyLineByCode(line.LT, 0), line.CL, cache.TTLLines)

// 			// Grava coords separado para busca espacial
// 			if originLat != 0 && originLng != 0 {
// 				coordsPayload := map[string]any{
// 					"sptrans_id": line.CL,
// 					"code":       strings.TrimSpace(line.LT),
// 					"origin_lat": originLat,
// 					"origin_lng": originLng,
// 				}
// 				_ = h.cache.SetJSON(ctx,
// 					fmt.Sprintf("lines:coords:%d", line.CL),
// 					coordsPayload,
// 					cache.TTLLines,
// 				)
// 			}

// 			// Sync paradas
// 			stops, err := h.sptrans.GetStopsByLine(line.CL)
// 			if err != nil {
// 				log.Printf("[VALKEY SYNC] stops erro line=%d err=%v", line.CL, err)
// 				return nil
// 			}

// 			_ = h.cache.SetJSON(ctx, cache.KeyStopsForLine(line.CL), stops, cache.TTLStops)

// 			for _, s := range stops {
// 				stopPayload := cache.CachedStop{
// 					SptransCode: s.CP,
// 					Name:        s.NP,
// 					Address:     s.Ed,
// 					Lat:         s.Lat,
// 					Lng:         s.Lng,
// 					UpdatedAt:   time.Now().Format(time.RFC3339),
// 				}
// 				_ = h.cache.SetJSON(ctx, cache.KeyStopByCP(s.CP), stopPayload, cache.TTLStops)
// 				totalStops++
// 			}

// 			return nil
// 		})

// 		if len(result.Errors) > 0 {
// 			for _, e := range result.Errors {
// 				log.Printf("[VALKEY SYNC] erro: %v", e)
// 			}
// 		}

// 		// Grava metadata
// 		meta := cache.CacheMetadata{
// 			Version:       "1.0.0",
// 			LinesSyncedAt: time.Now().Format(time.RFC3339),
// 			StopsSyncedAt: time.Now().Format(time.RFC3339),
// 			TotalLines:    totalLines,
// 			TotalStops:    totalStops,
// 		}
// 		_ = h.cache.SetJSON(ctx, cache.KeyMetadata(), meta, cache.TTLLines)

// 		log.Printf("[VALKEY SYNC] finalizado lines=%d stops=%d geocoded=%d",
// 			totalLines, totalStops, totalGeocoded)
// 	}(body.Terms)

//		jsonOK(w, map[string]any{"status": "started", "terms": body.Terms})
//	}
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mobility_processor/internal/geocoding"
	"mobility_processor/internal/sptrans"
	cache "mobility_processor/internal/valkey_cache"
)

func (h *Handler) AdminHealth(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{
		"status":    "ok",
		"service":   "mobility-processor",
		"timestamp": time.Now().UTC(),
	})
}

// GET /admin/stats
func (h *Handler) AdminStats(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{
		"status": "ok",
		"stats": map[string]any{
			"lines":      "implement Count()",
			"stops":      "implement Count()",
			"line_stops": "implement CountLinks()",
		},
	})
}

// POST /admin/sync/line
func (h *Handler) AdminSyncLine(w http.ResponseWriter, r *http.Request) {

	var body struct {
		Term string `json:"term"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "body inválido", http.StatusBadRequest)
		return
	}

	if body.Term == "" {
		jsonError(w, "campo 'term' obrigatório", http.StatusBadRequest)
		return
	}

	if err := h.sptrans.Authenticate(); err != nil {
		jsonError(w, "falha na autenticação SPTrans", http.StatusBadGateway)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	var (
		mu     sync.Mutex
		synced []sptrans.RawLine
	)

	result := h.sptrans.SyncLines(
		[]string{body.Term},
		10,
		func(line sptrans.RawLine) error {

			if h.lines != nil {
				if err := h.lines.UpsertFromSPTrans(ctx, line); err != nil {
					return err
				}
			}

			mu.Lock()
			synced = append(synced, line)
			mu.Unlock()

			return nil
		},
	)

	if len(result.Errors) > 0 {
		log.Printf("[AdminSyncLine] erros: %v", result.Errors)

		jsonError(
			w,
			"falha ao sincronizar linhas",
			http.StatusBadGateway,
		)

		return
	}

	jsonOK(w, map[string]any{
		"term":            body.Term,
		"lines_processed": len(synced),
		"lines":           synced,
	})
}

// POST /admin/sync/stops
func (h *Handler) AdminSyncStops(w http.ResponseWriter, r *http.Request) {

	var body struct {
		SptransLineID int `json:"sptrans_line_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "body inválido", http.StatusBadRequest)
		return
	}

	if body.SptransLineID <= 0 {
		jsonError(w, "campo 'sptrans_line_id' obrigatório", http.StatusBadRequest)
		return
	}

	lineStops, err := h.sptrans.GetStopsByLine(body.SptransLineID)
	if err != nil {
		log.Printf("[AdminSyncStops] erro SPTrans: %v", err)

		jsonError(
			w,
			"falha ao buscar paradas",
			http.StatusBadGateway,
		)

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	persisted, linked, err := h.stops.UpsertStopsForLine(
		ctx,
		body.SptransLineID,
		lineStops,
	)

	if err != nil {
		log.Printf("[AdminSyncStops] erro banco: %v", err)

		jsonError(
			w,
			"falha ao salvar paradas",
			http.StatusBadGateway,
		)

		return
	}

	jsonOK(w, map[string]any{
		"line_id":       body.SptransLineID,
		"stops_fetched": len(lineStops),
		"stops_saved":   persisted,
		"links_saved":   linked,
	})
}

// POST /admin/sync/full
func (h *Handler) AdminFullSync(w http.ResponseWriter, r *http.Request) {

	go func() {
		log.Println("[ADMIN FULL SYNC] iniciado")

		terms := []string{
			"0", "1", "2", "3", "4",
			"5", "6", "7", "8", "9",
		}

		if err := h.sptrans.Authenticate(); err != nil {
			log.Printf("[ADMIN FULL SYNC] auth erro: %v", err)
			return
		}

		ctx := context.Background()

		var totalLines atomic.Int64
		var totalStops atomic.Int64

		result := h.sptrans.SyncLines(
			terms,
			10,
			func(line sptrans.RawLine) error {
				currentLines := totalLines.Add(1)

				if currentLines%100 == 0 {
					log.Printf("[ADMIN FULL SYNC] progresso: %d linhas, %d paradas", currentLines, totalStops.Load())
				}

				if h.lines != nil {
					if err := h.lines.UpsertFromSPTrans(ctx, line); err != nil {
						return err
					}
				}

				// Grava linha no Valkey
				if h.cache != nil {
					fullCode := fmt.Sprintf("%s-%d", strings.TrimSpace(line.LT), line.TL)
					payload := cache.CachedLine{
						Code:        fullCode,
						Name:        strings.TrimSpace(line.TP + " - " + line.TS),
						Origin:      strings.TrimSpace(line.TP),
						Destination: strings.TrimSpace(line.TS),
						Circular:    line.LC,
						SptransID:   line.CL,
						UpdatedAt:   time.Now().Format(time.RFC3339),
					}
					_ = h.cache.SetJSON(ctx, cache.KeyLineByID(line.CL), payload, cache.TTLLines)
					_ = h.cache.SetJSON(ctx, cache.KeyLineByCode(fullCode, line.SL), line.CL, cache.TTLLines)
				}

				stops, err := h.sptrans.GetStopsByLine(line.CL)
				if err != nil {
					log.Printf("[ADMIN FULL SYNC] stops erro line=%d err=%v", line.CL, err)
					return nil
				}

				if h.stops != nil {
					saved, _, err := h.stops.UpsertStopsForLine(ctx, line.CL, stops)
					if err != nil {
						return err
					}
					totalStops.Add(saved)

					// Grava paradas no Valkey
					if h.cache != nil {
						_ = h.cache.SetJSON(ctx, cache.KeyStopsForLine(line.CL), stops, cache.TTLStops)
						for _, s := range stops {
							payload := cache.CachedStop{
								SptransCode: s.CP,
								Name:        s.NP,
								Address:     s.Ed,
								Lat:         s.Lat,
								Lng:         s.Lng,
								UpdatedAt:   time.Now().Format(time.RFC3339),
							}
							_ = h.cache.SetJSON(ctx, cache.KeyStopByCP(s.CP), payload, cache.TTLStops)
						}
					}
				}

				return nil
			},
		)

		if len(result.Errors) > 0 {
			for _, e := range result.Errors {
				log.Printf("[ADMIN FULL SYNC] erro: %v", e)
			}
			return
		}

		log.Printf("[ADMIN FULL SYNC] finalizado lines=%d stops=%d", totalLines.Load(), totalStops.Load())
	}()

	jsonOK(w, map[string]any{"status": "started"})
}

// POST /admin/sync/valkey
// Sincroniza linhas + paradas + geocodificação direto no Valkey, sem MySQL.
// Aceita {"terms": ["874", "8000"]} ou omite terms para usar dígitos 0-9.
func (h *Handler) AdminSyncValkey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Terms []string `json:"terms"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if len(body.Terms) == 0 {
		body.Terms = []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}
	}

	go func(terms []string) {
		log.Printf("[VALKEY SYNC] iniciado terms=%v", terms)

		ctx := context.Background()

		if err := h.sptrans.Authenticate(); err != nil {
			log.Printf("[VALKEY SYNC] auth erro: %v", err)
			return
		}

		var totalLines, totalStops, totalGeocoded int

		result := h.sptrans.SyncLines(terms, 1, func(line sptrans.RawLine) error {
			totalLines++

			if totalLines%50 == 0 {
				log.Printf("[VALKEY SYNC] progresso: lines=%d stops=%d geocoded=%d",
					totalLines, totalStops, totalGeocoded)
			}

			fullCode := fmt.Sprintf("%s-%d", strings.TrimSpace(line.LT), line.TL)

			// Geocodifica origem
			var originLat, originLng float64
			originCoords, err := geocoding.Geocode(line.TP)
			if err != nil {
				log.Printf("[VALKEY SYNC] geocode erro line=%d origin=%q err=%v", line.CL, line.TP, err)
			} else if originCoords != nil {
				originLat = originCoords.Lat
				originLng = originCoords.Lng
				totalGeocoded++
			}

			// Grava linha no Valkey com coords
			payload := cache.CachedLine{
				Code:        fullCode,
				Name:        strings.TrimSpace(line.TP + " - " + line.TS),
				Origin:      strings.TrimSpace(line.TP),
				Destination: strings.TrimSpace(line.TS),
				Circular:    line.LC,
				SptransID:   line.CL,
				UpdatedAt:   time.Now().Format(time.RFC3339),
			}
			_ = h.cache.SetJSON(ctx, cache.KeyLineByID(line.CL), payload, cache.TTLLines)
			_ = h.cache.SetJSON(ctx, cache.KeyLineByCode(fullCode, line.SL), line.CL, cache.TTLLines)

			// Grava coords separado para busca espacial
			if originLat != 0 && originLng != 0 {
				coordsPayload := map[string]any{
					"sptrans_id": line.CL,
					"code":       fullCode,
					"origin_lat": originLat,
					"origin_lng": originLng,
				}
				_ = h.cache.SetJSON(ctx,
					fmt.Sprintf("lines:coords:%d", line.CL),
					coordsPayload,
					cache.TTLLines,
				)
			}

			// Sync paradas
			stops, err := h.sptrans.GetStopsByLine(line.CL)
			if err != nil {
				log.Printf("[VALKEY SYNC] stops erro line=%d err=%v", line.CL, err)
				return nil
			}

			_ = h.cache.SetJSON(ctx, cache.KeyStopsForLine(line.CL), stops, cache.TTLStops)

			for _, s := range stops {
				stopPayload := cache.CachedStop{
					SptransCode: s.CP,
					Name:        s.NP,
					Address:     s.Ed,
					Lat:         s.Lat,
					Lng:         s.Lng,
					UpdatedAt:   time.Now().Format(time.RFC3339),
				}
				_ = h.cache.SetJSON(ctx, cache.KeyStopByCP(s.CP), stopPayload, cache.TTLStops)
				totalStops++
			}

			return nil
		})

		if len(result.Errors) > 0 {
			for _, e := range result.Errors {
				log.Printf("[VALKEY SYNC] erro: %v", e)
			}
		}

		// Grava metadata
		meta := cache.CacheMetadata{
			Version:       "1.0.0",
			LinesSyncedAt: time.Now().Format(time.RFC3339),
			StopsSyncedAt: time.Now().Format(time.RFC3339),
			TotalLines:    totalLines,
			TotalStops:    totalStops,
		}
		_ = h.cache.SetJSON(ctx, cache.KeyMetadata(), meta, cache.TTLLines)

		log.Printf("[VALKEY SYNC] finalizado lines=%d stops=%d geocoded=%d",
			totalLines, totalStops, totalGeocoded)
	}(body.Terms)

	jsonOK(w, map[string]any{"status": "started", "terms": body.Terms})
}
