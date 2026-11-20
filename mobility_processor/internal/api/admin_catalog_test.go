package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mobility_processor/internal/sptrans"
)

const catalogTestToken = "synthetic-administrative-token-12345678"

type catalogSyncFunc func(context.Context, []string, int, func(sptrans.RawLine) error) sptrans.SyncResult

func (f catalogSyncFunc) SyncLinesContext(c context.Context, t []string, w int, save func(sptrans.RawLine) error) sptrans.SyncResult {
	return f(c, t, w, save)
}

type catalogMemory struct {
	mu    sync.Mutex
	lines map[int]sptrans.RawLine
}

func (s *catalogMemory) UpsertFromSPTrans(ctx context.Context, l sptrans.RawLine) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines[l.CL] = l
	return nil
}
func (s *catalogMemory) Count(context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.lines), nil
}
func catalogRequest(h *Handler, body string, ctx context.Context) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/admin/sync/full", strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+catalogTestToken)
	w := httptest.NewRecorder()
	h.AdminFullSync(w, r)
	return w
}

func TestAdminCatalogOneTermPreservesIdentityAndReexecution(t *testing.T) {
	store := &catalogMemory{lines: map[int]sptrans.RawLine{}}
	h := &Handler{adminToken: catalogTestToken, catalogLines: store}
	h.catalogProvider = catalogSyncFunc(func(ctx context.Context, terms []string, workers int, save func(sptrans.RawLine) error) sptrans.SyncResult {
		if len(terms) != 1 || terms[0] != "8000" || workers != 1 {
			t.Error("unsafe request parameters", terms, workers)
		}
		for i := 1; i <= 2; i++ {
			if err := save(sptrans.RawLine{CL: 100 + i, LT: "8000", TL: 1, SL: i}); err != nil {
				t.Error(err)
			}
		}
		return sptrans.SyncResult{Received: 2, Success: 2}
	})
	for i := 0; i < 2; i++ {
		w := catalogRequest(h, `{"terms":["8000"],"workers":1,"timeout_seconds":30}`, context.Background())
		var result map[string]any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result["total_postgresql"] != float64(2) || result["upserts"] != float64(2) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if store.lines[101].SL != 1 || store.lines[102].SL != 2 {
		t.Fatal("direction/CL merged")
	}
}

func TestAdminCatalogAuthorizationAndLimitsBeforeAnyIO(t *testing.T) {
	h := &Handler{catalogProvider: catalogSyncFunc(func(context.Context, []string, int, func(sptrans.RawLine) error) sptrans.SyncResult {
		t.Fatal("unauthorized I/O")
		return sptrans.SyncResult{}
	})}
	if w := catalogRequest(h, `{"terms":["0"]}`, context.Background()); w.Code != 503 {
		t.Fatal(w.Code)
	}
	h.adminToken = catalogTestToken
	r := httptest.NewRequest(http.MethodPost, "/admin/sync/full", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.AdminFullSync(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	for _, body := range []string{`{}`, `{"terms":["0"],"workers":3}`, `{"terms":[""]}`, `{"terms":["0"],"timeout_seconds":301}`, `{"terms":["0"]} {}`} {
		if w := catalogRequest(h, body, context.Background()); w.Code != 400 {
			t.Fatal(w.Code, body)
		}
	}
}

func TestAdminCatalogConcurrentRequestsCancellationAndLockRelease(t *testing.T) {
	entered := make(chan struct{}, 1)
	h := &Handler{adminToken: catalogTestToken, catalogLines: &catalogMemory{lines: map[int]sptrans.RawLine{}}}
	h.catalogProvider = catalogSyncFunc(func(ctx context.Context, _ []string, _ int, _ func(sptrans.RawLine) error) sptrans.SyncResult {
		entered <- struct{}{}
		<-ctx.Done()
		return sptrans.SyncResult{Errors: []error{ctx.Err()}}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- catalogRequest(h, `{"terms":["0"]}`, ctx) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("sync did not start")
	}
	if w := catalogRequest(h, `{"terms":["1"]}`, context.Background()); w.Code != 409 {
		t.Fatal(w.Code)
	}
	cancel()
	select {
	case w := <-done:
		if !strings.Contains(w.Body.String(), `"status":"canceled"`) {
			t.Fatal(w.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("sync abandoned")
	}
	if h.catalogRunning.Load() {
		t.Fatal("lock not released")
	}
}

func TestAdminCatalogPartialProviderFailureIsReported(t *testing.T) {
	h := &Handler{adminToken: catalogTestToken, catalogLines: &catalogMemory{lines: map[int]sptrans.RawLine{}},
		catalogProvider: catalogSyncFunc(func(context.Context, []string, int, func(sptrans.RawLine) error) sptrans.SyncResult {
			return sptrans.SyncResult{Received: 1, Success: 1, Errors: []error{errors.New("synthetic provider failure")}}
		})}
	w := catalogRequest(h, `{"terms":["0","1"]}`, context.Background())
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"partial"`) || strings.Contains(w.Body.String(), "synthetic provider failure") {
		t.Fatal(w.Code, w.Body.String())
	}
}
