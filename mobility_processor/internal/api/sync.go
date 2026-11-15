package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"mobility_processor/internal/sptrans"
	cache "mobility_processor/internal/valkey_cache"
)

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

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	_ = h.sptrans.SyncLines(body.Terms, 10, func(line sptrans.RawLine) error {
		if h.lines != nil {
			if err := h.lines.UpsertFromSPTrans(ctx, line); err != nil {
				return err
			}
		}

		go func(l sptrans.RawLine) {
			bgCtx := context.Background()
			payload := cache.CachedLine{
				Code:        fmt.Sprintf("%s-%d", strings.TrimSpace(l.LT), l.TL),
				Name:        strings.TrimSpace(l.TP + " - " + l.TS),
				Origin:      strings.TrimSpace(l.TP),
				Destination: strings.TrimSpace(l.TS),
				Circular:    l.LC,
				SptransID:   l.CL,
				UpdatedAt:   time.Now().Format(time.RFC3339),
			}
			_ = h.cache.SetJSON(bgCtx, cache.KeyLineByID(l.CL), payload, cache.TTLLines)
			_ = h.cache.SetJSON(bgCtx, cache.KeyLineByCode(fmt.Sprintf("%s-%d", strings.TrimSpace(l.LT), l.TL), l.SL), l.CL, cache.TTLLines)
		}(line)

		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()

		return nil
	})

	jsonOK(w, map[string]any{"lines": lines})
}
