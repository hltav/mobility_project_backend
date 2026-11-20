package predictions

import (
	"context"
	"sync"
	"time"
)

type DemandCollector interface {
	Collect(context.Context, int, int) (Result, error)
}

type DirectionResolver interface {
	ResolvePredictionDirection(context.Context, int) (int, error)
}

// CollectionEnabled requires explicit opt-in; unknown values fail closed.
func CollectionEnabled(value string) bool { return value == "true" }

type demand struct{ seen, attempted time.Time }

// DemandWorker owns one collection loop, never a goroutine per request/line.
// Demand is bounded, process-local and contains only operational CLs and times.
type DemandWorker struct {
	collector            DemandCollector
	resolver             DirectionResolver
	mu                   sync.Mutex
	lines                map[int]demand
	now                  func() time.Time
	ttl, interval, retry time.Duration
	limit                int
}

func NewDemandWorker(collector DemandCollector, resolver DirectionResolver) (*DemandWorker, error) {
	if collector == nil || resolver == nil {
		return nil, ErrConfiguration
	}
	return &DemandWorker{collector: collector, resolver: resolver, lines: make(map[int]demand), now: time.Now,
		ttl: 2 * time.Minute, interval: time.Second, retry: 5 * time.Second, limit: 256}, nil
}

// RecordDemand performs no I/O and never waits for collection or provider data.
func (w *DemandWorker) RecordDemand(cl int) bool {
	if cl <= 0 {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	w.prune(now)
	d, exists := w.lines[cl]
	if !exists && len(w.lines) >= w.limit {
		return false
	}
	d.seen = now
	w.lines[cl] = d
	return true
}

func (w *DemandWorker) prune(now time.Time) {
	for cl, d := range w.lines {
		if !now.Before(d.seen.Add(w.ttl)) {
			delete(w.lines, cl)
		}
	}
}

func (w *DemandWorker) next() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	w.prune(now)
	chosen := 0
	var oldest time.Time
	for cl, d := range w.lines {
		if !d.attempted.IsZero() && now.Before(d.attempted.Add(w.retry)) {
			continue
		}
		if chosen == 0 || d.attempted.Before(oldest) || (d.attempted.Equal(oldest) && cl < chosen) {
			chosen, oldest = cl, d.attempted
		}
	}
	if chosen != 0 {
		d := w.lines[chosen]
		d.attempted = now
		w.lines[chosen] = d
	}
	return chosen
}

func (w *DemandWorker) step(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	cl := w.next()
	if cl == 0 {
		return
	}
	lookup, cancel := context.WithTimeout(ctx, 2*time.Second)
	direction, err := w.resolver.ResolvePredictionDirection(lookup, cl)
	cancel()
	if err != nil || (direction != 1 && direction != 2) || ctx.Err() != nil {
		return
	}
	// Expired demand must not initiate a provider attempt after a slow lookup.
	w.mu.Lock()
	d, exists := w.lines[cl]
	active := exists && w.now().Before(d.seen.Add(w.ttl))
	w.mu.Unlock()
	if active {
		_, _ = w.collector.Collect(ctx, cl, direction)
	}
}

// Run blocks until cancellation. Call once, under the server lifecycle.
// At most one CL is dispatched per tick; existing collector limits still apply.
func (w *DemandWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.step(ctx)
		}
	}
}
