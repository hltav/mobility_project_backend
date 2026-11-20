package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"mobility_processor/internal/sptrans"
)

type catalogProvider interface {
	SyncLinesContext(context.Context, []string, int, func(sptrans.RawLine) error) sptrans.SyncResult
}
type catalogLines interface {
	UpsertFromSPTrans(context.Context, sptrans.RawLine) error
	Count(context.Context) (int, error)
}

func (h *Handler) authorizeAdmin(w http.ResponseWriter, r *http.Request) bool {
	if len(h.adminToken) < 32 {
		jsonError(w, "administrative action disabled", http.StatusServiceUnavailable)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.adminToken)) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		jsonError(w, "administrative authorization required", http.StatusUnauthorized)
		return false
	}
	return true
}

func (h *Handler) syncCatalog(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	var request struct {
		Terms          []string `json:"terms"`
		Workers        int      `json:"workers"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		jsonError(w, "invalid catalog request", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		jsonError(w, "one JSON object required", http.StatusBadRequest)
		return
	}
	if request.Workers == 0 {
		request.Workers = 1
	}
	if request.TimeoutSeconds == 0 {
		request.TimeoutSeconds = 60
	}
	if len(request.Terms) < 1 || len(request.Terms) > 10 || request.Workers < 1 || request.Workers > 2 || request.TimeoutSeconds < 1 || request.TimeoutSeconds > 300 {
		jsonError(w, "require 1-10 terms, 1-2 workers and timeout 1-300 seconds", http.StatusBadRequest)
		return
	}
	terms := make([]string, 0, len(request.Terms))
	seen := map[string]bool{}
	for _, term := range request.Terms {
		term = strings.TrimSpace(term)
		if term == "" || len(term) > 100 {
			jsonError(w, "invalid search term", http.StatusBadRequest)
			return
		}
		if !seen[term] {
			terms = append(terms, term)
			seen[term] = true
		}
	}
	if h.catalogProvider == nil || h.catalogLines == nil {
		jsonError(w, "catalog dependencies unavailable", http.StatusServiceUnavailable)
		return
	}
	if !h.catalogRunning.CompareAndSwap(false, true) {
		jsonError(w, "catalog synchronization already running", http.StatusConflict)
		return
	}
	defer h.catalogRunning.Store(false)
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(request.TimeoutSeconds)*time.Second)
	defer cancel()
	result := h.catalogProvider.SyncLinesContext(ctx, terms, request.Workers, func(line sptrans.RawLine) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return h.catalogLines.UpsertFromSPTrans(ctx, line)
	})
	status := "completed"
	if len(result.Errors) > 0 {
		status = "partial"
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		status = "timeout"
	} else if ctx.Err() != nil {
		status = "canceled"
	}
	countCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer stop()
	total, countErr := h.catalogLines.Count(countCtx)
	var totalValue any = total
	errorCount := len(result.Errors)
	if countErr != nil {
		totalValue = nil
		errorCount++
		if status == "completed" {
			status = "partial"
		}
	}
	log.Printf("[catalog sync] status=%s received=%d upserts=%d errors=%d", status, result.Received, result.Success, errorCount)
	jsonOK(w, map[string]any{"status": status, "terms": terms, "workers": request.Workers,
		"received": result.Received, "upserts": result.Success, "error_count": errorCount,
		"total_postgresql": totalValue, "count_available": countErr == nil})
}
