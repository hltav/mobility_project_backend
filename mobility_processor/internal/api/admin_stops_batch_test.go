package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mobility_processor/internal/sptrans"
)

type stopFetchFunc func(context.Context, int) ([]sptrans.Stop, error)

func (f stopFetchFunc) GetStopsByLineContext(c context.Context, cl int) ([]sptrans.Stop, error) {
	return f(c, cl)
}

type stopIdentityFunc func(context.Context, int) (int, error)

func (f stopIdentityFunc) ResolvePredictionDirection(c context.Context, cl int) (int, error) {
	return f(c, cl)
}

type stopStoreStub struct {
	mu  sync.Mutex
	cls []int
}

func (s *stopStoreStub) UpsertStopsForLine(_ context.Context, cl int, stops []sptrans.Stop) (int64, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cls = append(s.cls, cl)
	return int64(len(stops)), 1, nil
}
func stopBatchFixture() *Handler {
	h := &Handler{adminToken: catalogTestToken, stopStore: &stopStoreStub{}, stopIdentity: stopIdentityFunc(func(context.Context, int) (int, error) { return 2, nil }),
		stopProvider: stopFetchFunc(func(context.Context, int) ([]sptrans.Stop, error) {
			return []sptrans.Stop{{CP: 123, NP: "Synthetic", Lat: -23.55, Lng: -46.63}}, nil
		})}
	_ = h.SetStopSyncAllowedCLs("101,102")
	return h
}
func stopBatchRequest(h *Handler, body string, ctx context.Context) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/admin/sync/stops", strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+catalogTestToken)
	w := httptest.NewRecorder()
	h.AdminSyncStops(w, r)
	return w
}

func TestStopBatchExplicitIdentitiesAndCounts(t *testing.T) {
	h := stopBatchFixture()
	w := stopBatchRequest(h, `{"sptrans_line_ids":[101,102],"workers":1,"timeout_seconds":30}`, context.Background())
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"stops_persisted":2`) || !strings.Contains(w.Body.String(), `"sptrans_direction":2`) {
		t.Fatal(w.Code, w.Body.String())
	}
	s := h.stopStore.(*stopStoreStub)
	if len(s.cls) != 2 || s.cls[0] != 101 || s.cls[1] != 102 {
		t.Fatal(s.cls)
	}
}

func TestStopBatchInvalidAllowlistDisablesCollection(t *testing.T) {
	h := stopBatchFixture()
	for _, value := range []string{"101,101", "0", "not-a-cl"} {
		if h.SetStopSyncAllowedCLs(value) == nil || len(h.stopAllowed) != 0 {
			t.Fatal("unsafe allowlist accepted", value)
		}
	}
}

func TestStopBatchRejectsMoreThanSixtyCLs(t *testing.T) {
	h := stopBatchFixture()
	values := make([]string, 61)
	for i := range values {
		values[i] = "101"
	}
	w := stopBatchRequest(h, `{"sptrans_line_ids":[`+strings.Join(values, ",")+`]}`, context.Background())
	if w.Code != http.StatusBadRequest {
		t.Fatal("oversized batch accepted", w.Code)
	}
}

func TestStopBatchRejectsUnauthorizedUnlistedDuplicatesAndUnsafeLimits(t *testing.T) {
	h := stopBatchFixture()
	h.stopProvider = stopFetchFunc(func(context.Context, int) ([]sptrans.Stop, error) {
		t.Fatal("unexpected provider call")
		return nil, nil
	})
	for _, body := range []string{`{}`, `{"sptrans_line_ids":[999]}`, `{"sptrans_line_ids":[101,101]}`, `{"sptrans_line_ids":[101],"workers":3}`, `{"sptrans_line_ids":[101],"timeout_seconds":301}`} {
		if w := stopBatchRequest(h, body, context.Background()); w.Code != 400 {
			t.Fatal(w.Code, body)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/admin/sync/stops", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.AdminSyncStops(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	h.SetCatalogAdminToken("")
	if w := stopBatchRequest(h, `{}`, context.Background()); w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestStopBatchValidatesAllCLsBeforeCollectingAny(t *testing.T) {
	h := stopBatchFixture()
	h.stopIdentity = stopIdentityFunc(func(_ context.Context, cl int) (int, error) {
		if cl == 102 {
			return 0, errors.New("missing canonical identity")
		}
		return 1, nil
	})
	h.stopProvider = stopFetchFunc(func(context.Context, int) ([]sptrans.Stop, error) {
		t.Fatal("partial validation triggered collection")
		return nil, nil
	})
	if w := stopBatchRequest(h, `{"sptrans_line_ids":[101,102]}`, context.Background()); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestStopBatchEmptyAndProviderErrorsAreDifferent(t *testing.T) {
	h := stopBatchFixture()
	h.stopProvider = stopFetchFunc(func(_ context.Context, cl int) ([]sptrans.Stop, error) {
		if cl == 101 {
			return []sptrans.Stop{}, nil
		}
		return nil, errors.New("synthetic upstream error")
	})
	w := stopBatchRequest(h, `{"sptrans_line_ids":[101,102]}`, context.Background())
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"cls_without_data":1`) || !strings.Contains(w.Body.String(), `"errors":1`) || strings.Contains(w.Body.String(), "synthetic upstream error") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestStopBatchMutexAndCancellationJoinWorkers(t *testing.T) {
	h := stopBatchFixture()
	entered := make(chan struct{}, 2)
	h.stopProvider = stopFetchFunc(func(ctx context.Context, _ int) ([]sptrans.Stop, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); stopBatchRequest(h, `{"sptrans_line_ids":[101,102],"workers":2}`, ctx) }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("bounded workers not started")
		}
	}
	if w := stopBatchRequest(h, `{"sptrans_line_ids":[101]}`, context.Background()); w.Code != 409 {
		t.Fatal(w.Code)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("workers abandoned")
	}
	if h.stopRunning.Load() {
		t.Fatal("lock retained")
	}
}
