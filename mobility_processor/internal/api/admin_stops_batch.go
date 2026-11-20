package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"mobility_processor/internal/sptrans"
)

type stopSyncProvider interface {
	GetStopsByLineContext(context.Context, int) ([]sptrans.Stop, error)
}
type stopSyncStore interface {
	UpsertStopsForLine(context.Context, int, []sptrans.Stop) (int64, int64, error)
}
type stopSyncIdentity interface {
	ResolvePredictionDirection(context.Context, int) (int, error)
}

// Configure once before serving. No production CL is hardcoded.
func (h *Handler) SetStopSyncAllowedCLs(value string) error {
	h.stopAllowed = nil
	if value == "" {
		return nil
	}
	allowed := map[int]bool{}
	for _, part := range strings.Split(value, ",") {
		cl, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || cl <= 0 || allowed[cl] || len(allowed) >= 60 {
			return errors.New("invalid CL allowlist")
		}
		allowed[cl] = true
	}
	h.stopAllowed = allowed
	return nil
}

type stopSyncLineResult struct {
	CL        int    `json:"cl"`
	Direction int    `json:"sptrans_direction"`
	Received  int    `json:"stops_returned"`
	Persisted int64  `json:"stops_persisted"`
	Links     int64  `json:"new_legacy_links"`
	Status    string `json:"status"`
}

func (h *Handler) syncStopsBatch(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	if len(h.stopAllowed) == 0 || h.stopProvider == nil || h.stopStore == nil || h.stopIdentity == nil {
		jsonError(w, "stop collection disabled or dependencies unavailable", 503)
		return
	}
	var req struct {
		CLs     []int `json:"sptrans_line_ids"`
		CL      int   `json:"sptrans_line_id"`
		Workers int   `json:"workers"`
		Timeout int   `json:"timeout_seconds"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF {
		jsonError(w, "invalid stop collection request", 400)
		return
	}
	if req.CL != 0 {
		if len(req.CLs) > 0 {
			jsonError(w, "choose one CL selector", 400)
			return
		}
		req.CLs = []int{req.CL}
	}
	if req.Workers == 0 {
		req.Workers = 1
	}
	if req.Timeout == 0 {
		req.Timeout = 300
	}
	if len(req.CLs) < 1 || len(req.CLs) > 60 || req.Workers < 1 || req.Workers > 2 || req.Timeout < 1 || req.Timeout > 300 {
		jsonError(w, "require 1-60 explicit CLs, 1-2 workers, timeout 1-300 seconds", 400)
		return
	}
	seen := map[int]bool{}
	for _, cl := range req.CLs {
		if cl <= 0 || seen[cl] || !h.stopAllowed[cl] {
			jsonError(w, "CL duplicate or outside explicit allowlist", 400)
			return
		}
		seen[cl] = true
	}
	if !h.stopRunning.CompareAndSwap(false, true) {
		jsonError(w, "stop collection already running", 409)
		return
	}
	defer h.stopRunning.Store(false)
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(req.Timeout)*time.Second)
	defer cancel()
	jobs := make(chan stopSyncLineResult, len(req.CLs))
	// Validate the entire list against canonical PostgreSQL CL/SL before ANY provider call.
	for _, cl := range req.CLs {
		direction, err := h.stopIdentity.ResolvePredictionDirection(ctx, cl)
		if ctx.Err() != nil {
			jsonError(w, "validation canceled or timed out; nothing collected", http.StatusRequestTimeout)
			return
		}
		if err != nil || (direction != 1 && direction != 2) {
			jsonError(w, "canonical CL/direction unavailable; nothing collected", 400)
			return
		}
		jobs <- stopSyncLineResult{CL: cl, Direction: direction}
	}
	close(jobs)
	results := make(chan stopSyncLineResult, len(req.CLs))
	var workers sync.WaitGroup
	for i := 0; i < req.Workers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				stops, err := h.stopProvider.GetStopsByLineContext(ctx, job.CL)
				job.Received = len(stops)
				job.Status = "provider_error"
				if err == nil {
					if len(stops) == 0 {
						job.Status = "empty"
					} else {
						job.Persisted, job.Links, err = h.stopStore.UpsertStopsForLine(ctx, job.CL, stops)
						job.Status = "collected"
						if err != nil {
							job.Status = "persistence_error"
						}
					}
				}
				results <- job
			}
		}()
	}
	workers.Wait()
	close(results)
	lines := []stopSyncLineResult{}
	returned, empty, failed := 0, 0, 0
	var persisted, links int64
	for line := range results {
		lines = append(lines, line)
		returned += line.Received
		persisted += line.Persisted
		links += line.Links
		if line.Status == "empty" {
			empty++
		}
		if line.Status == "provider_error" || line.Status == "persistence_error" {
			failed++
		}
	}
	status := "completed"
	if failed > 0 {
		status = "partial"
	}
	if ctx.Err() != nil {
		status = "canceled"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = "timeout"
		}
	}
	jsonOK(w, map[string]any{"status": status, "cls_requested": len(req.CLs), "cls_processed": len(lines), "cls_unprocessed": len(req.CLs) - len(lines),
		"stops_returned": returned, "stops_persisted": persisted, "new_legacy_links": links, "cls_without_data": empty, "errors": failed, "lines": lines})
}
