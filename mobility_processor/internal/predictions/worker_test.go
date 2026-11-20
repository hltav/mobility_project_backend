package predictions

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mobility_processor/internal/sptrans"
)

type demandCollectFunc func(context.Context, int, int) (Result, error)

func (f demandCollectFunc) Collect(ctx context.Context, cl, sl int) (Result, error) {
	return f(ctx, cl, sl)
}

type directionFunc func(context.Context, int) (int, error)

func (f directionFunc) ResolvePredictionDirection(ctx context.Context, cl int) (int, error) {
	return f(ctx, cl)
}

func TestCollectionRequiresExplicitEnable(t *testing.T) {
	for _, value := range []string{"", "false", "0", "1", "TRUE", "invalid"} {
		if CollectionEnabled(value) {
			t.Fatal("enabled without explicit true", value)
		}
	}
	if !CollectionEnabled("true") {
		t.Fatal("explicit opt-in rejected")
	}
}

func TestDemandDeduplicatesExpiresAndNeverScansCatalog(t *testing.T) {
	var calls [][2]int
	w, _ := NewDemandWorker(demandCollectFunc(func(_ context.Context, cl, sl int) (Result, error) {
		calls = append(calls, [2]int{cl, sl})
		return Result{}, nil
	}), directionFunc(func(_ context.Context, cl int) (int, error) {
		if cl != 2506 {
			t.Fatal("unexpected line lookup", cl)
		}
		return 2, nil
	}))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return now }
	w.step(context.Background())
	if len(calls) != 0 {
		t.Fatal("collected without demand")
	}
	var users sync.WaitGroup
	for i := 0; i < 50; i++ {
		users.Add(1)
		go func() { defer users.Done(); w.RecordDemand(2506) }()
	}
	users.Wait()
	w.step(context.Background())
	w.step(context.Background())
	if len(calls) != 1 || calls[0] != [2]int{2506, 2} || len(w.lines) != 1 {
		t.Fatal("demand duplicated or direction inferred", calls)
	}
	now = now.Add(w.ttl)
	w.step(context.Background())
	if len(calls) != 1 || len(w.lines) != 0 {
		t.Fatal("expired demand collected")
	}
}

func TestDemandInvalidIdentityAndBoundedMemory(t *testing.T) {
	for _, direction := range []int{0, 3} {
		w, _ := NewDemandWorker(demandCollectFunc(func(context.Context, int, int) (Result, error) {
			t.Fatal("invalid direction reached collector")
			return Result{}, nil
		}), directionFunc(func(context.Context, int) (int, error) { return direction, nil }))
		if w.RecordDemand(0) || w.RecordDemand(-1) {
			t.Fatal("invalid CL accepted")
		}
		w.limit = 1
		if !w.RecordDemand(2506) || w.RecordDemand(35274) {
			t.Fatal("demand capacity ignored")
		}
		w.step(context.Background())
	}
	w, _ := NewDemandWorker(demandCollectFunc(func(context.Context, int, int) (Result, error) {
		t.Fatal("unproved identity collected")
		return Result{}, nil
	}), directionFunc(func(context.Context, int) (int, error) { return 1, errors.New("ambiguous") }))
	w.RecordDemand(2506)
	w.step(context.Background())
}

func TestDemandWorkerSingleCollectionAndShutdown(t *testing.T) {
	started := make(chan struct{}, 2)
	var active, maximum atomic.Int32
	w, _ := NewDemandWorker(demandCollectFunc(func(ctx context.Context, _, _ int) (Result, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
		}
		started <- struct{}{}
		<-ctx.Done()
		return Result{}, ctx.Err()
	}), directionFunc(func(context.Context, int) (int, error) { return 1, nil }))
	w.interval = time.Millisecond
	w.RecordDemand(2506)
	w.RecordDemand(35274)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker never dispatched")
	}
	// Recording further demand does not await the blocked collection.
	recorded := make(chan struct{})
	go func() { w.RecordDemand(2506); close(recorded) }()
	select {
	case <-recorded:
	case <-time.After(time.Second):
		t.Fatal("demand blocked on provider")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker abandoned at shutdown")
	}
	if maximum.Load() != 1 || active.Load() != 0 {
		t.Fatal("concurrency or shutdown violation")
	}
}

func TestDemandWorkerCacheFailureNeverCallsProvider(t *testing.T) {
	collector := collectorFixture(t, providerFunc(func(context.Context, int) (*sptrans.LinePredictions, error) {
		t.Fatal("uncoordinated provider call")
		return nil, nil
	}), &snapshotStub{readErr: errors.New("cache offline")}, &coordinatorStub{}, Options{})
	w, _ := NewDemandWorker(collector, directionFunc(func(context.Context, int) (int, error) { return 1, nil }))
	w.RecordDemand(2506)
	w.step(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.step(ctx)
}

func TestDemandWorkerProviderErrorDoesNotBlockDemand(t *testing.T) {
	snapshots := &snapshotStub{}
	collector := collectorFixture(t, providerFunc(func(context.Context, int) (*sptrans.LinePredictions, error) {
		return nil, sptrans.ErrTransport
	}), snapshots, &coordinatorStub{}, Options{})
	w, _ := NewDemandWorker(collector, directionFunc(func(context.Context, int) (int, error) { return 1, nil }))
	w.RecordDemand(2506)
	w.step(context.Background())
	if snapshots.saves.Load() != 1 || !w.RecordDemand(35274) {
		t.Fatal("provider failure interrupted demand/cache")
	}
}
