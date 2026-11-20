package sptrans

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

const syntheticCatalog = `[{"cl":101,"lc":false,"lt":"0001","tl":10,"sl":2,"tp":"A","ts":"B"}]`

func TestCatalogSyncContextLimitsConcurrencyAndJoinsWorkers(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var active, peak atomic.Int32
	c := syntheticClient(t, func(r *http.Request) (*http.Response, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return syntheticHTTP(200, syntheticCatalog), nil
	}, true)
	done := make(chan SyncResult, 1)
	go func() {
		done <- c.SyncLinesContext(context.Background(), []string{"0", "1"}, 2, func(l RawLine) error {
			if l.CL != 101 || l.SL != 2 || l.LT != "0001" {
				t.Error("identity changed")
			}
			return nil
		})
	}()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("workers not dispatched")
		}
	}
	close(release)
	select {
	case result := <-done:
		if result.Received != 2 || result.Success != 2 || len(result.Errors) != 0 {
			t.Fatal(result)
		}
	case <-time.After(time.Second):
		t.Fatal("workers not joined")
	}
	if peak.Load() != 2 || active.Load() != 0 {
		t.Fatal("concurrency/lifecycle violation")
	}
}

func TestCatalogSyncTimeoutStopsTransportAndReturns(t *testing.T) {
	c := syntheticClient(t, func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() }, true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := c.SyncLinesContext(ctx, []string{"0", "1"}, 1, func(RawLine) error { t.Fatal("canceled response persisted"); return nil })
	if ctx.Err() == nil || len(result.Errors) == 0 || result.Success != 0 {
		t.Fatal(result)
	}
}
