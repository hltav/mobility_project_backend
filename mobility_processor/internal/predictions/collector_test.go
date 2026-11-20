package predictions

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mobility_processor/internal/sptrans"
	cache "mobility_processor/internal/valkey_cache"
)

// Synthetic provider/cache and a coordinator model. No network, server or
// production activation. These tests do not execute the Valkey Lua scripts.
type providerFunc func(context.Context, int) (*sptrans.LinePredictions, error)

func (f providerFunc) GetLinePredictions(ctx context.Context, cl int) (*sptrans.LinePredictions, error) {
	return f(ctx, cl)
}

type snapshotStub struct {
	readErr    error
	saveErr    error
	fresh      bool
	superseded bool
	reads      atomic.Int32
	saves      atomic.Int32
	save       func(int, int, time.Time, time.Time, *sptrans.LinePredictions, error)
}

func (s *snapshotStub) Read(context.Context, int, int) (cache.PredictionCacheRead, error) {
	s.reads.Add(1)
	if s.readErr != nil {
		return cache.PredictionCacheRead{State: "unavailable"}, s.readErr
	}
	if s.fresh {
		return cache.PredictionCacheRead{State: "fresh", Snapshot: &cache.PredictionSnapshot{Status: "available"}}, nil
	}
	return cache.PredictionCacheRead{State: "missing"}, nil
}
func (s *snapshotStub) Save(_ context.Context, cl, sl int, start, end time.Time, data *sptrans.LinePredictions, providerErr error) (bool, error) {
	s.saves.Add(1)
	if s.save != nil {
		s.save(cl, sl, start, end, data, providerErr)
	}
	return !s.superseded, s.saveErr
}

type coordinatorStub struct {
	mu           sync.Mutex
	held         map[int]string
	blocked      bool
	failure      bool
	lost         bool
	acquireErr   error
	finishErr    error
	acquisitions int
	finishes     int
}

func (s *coordinatorStub) AcquirePredictionLease(ctx context.Context, cl, sl int, token string, lease, interval time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acquisitions++
	if s.acquireErr != nil {
		return false, s.acquireErr
	}
	if s.blocked || s.held[cl] != "" {
		return false, nil
	}
	if s.held == nil {
		s.held = map[int]string{}
	}
	s.held[cl] = token
	return true, nil
}
func (s *coordinatorStub) FinishPredictionLease(ctx context.Context, cl, sl int, token string, failed bool, interval, maxBackoff time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishes++
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if s.finishErr != nil {
		return false, s.finishErr
	}
	if s.lost || s.held[cl] != token {
		return false, nil
	}
	delete(s.held, cl)
	s.blocked = true
	s.failure = failed
	return true, nil
}
func syntheticPrediction() *sptrans.LinePredictions {
	captured := "2026-11-19T23:57:00-03:00"
	return &sptrans.LinePredictions{SptransLineID: 2506, ReferenceTimeRaw: "23:58", Stops: []sptrans.StopPrediction{{StopCode: 880001,
		Vehicles: []sptrans.VehiclePrediction{{Prefix: "00017", ArrivalTimeRaw: "00:03", CapturedAtRaw: &captured}}}}}
}
func collectorFixture(t *testing.T, provider Provider, snapshots *snapshotStub, coordinator *coordinatorStub, options Options) *Collector {
	t.Helper()
	c, err := NewCollector(provider, snapshots, coordinator, options)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCollectorExplicitOnlyValidatesBeforeAnyIO(t *testing.T) {
	snapshots, coordinator := &snapshotStub{}, &coordinatorStub{}
	var calls atomic.Int32
	provider := providerFunc(func(context.Context, int) (*sptrans.LinePredictions, error) {
		calls.Add(1)
		return syntheticPrediction(), nil
	})
	c := collectorFixture(t, provider, snapshots, coordinator, Options{})
	if calls.Load() != 0 || snapshots.reads.Load() != 0 || coordinator.acquisitions != 0 {
		t.Fatal("construction activated collection")
	}
	for _, identity := range [][2]int{{0, 1}, {2506, 0}, {2506, 3}} {
		if _, err := c.Collect(context.Background(), identity[0], identity[1]); !errors.Is(err, cache.ErrPredictionIdentity) {
			t.Fatal(err)
		}
	}
	if calls.Load() != 0 || snapshots.reads.Load() != 0 {
		t.Fatal("invalid identity reached provider/cache")
	}
}

func TestCollectorRejectsUnsafeLeaseAndEnforcesLocalConcurrencyLimit(t *testing.T) {
	snapshots, coordinator := &snapshotStub{}, &coordinatorStub{}
	provider := providerFunc(func(context.Context, int) (*sptrans.LinePredictions, error) {
		t.Fatal("busy collector called provider")
		return nil, nil
	})
	if _, err := NewCollector(provider, snapshots, coordinator, Options{Timeout: time.Second, LeaseTTL: time.Millisecond}); !errors.Is(err, ErrConfiguration) {
		t.Fatal(err)
	}
	c := collectorFixture(t, provider, snapshots, coordinator, Options{MaxConcurrent: 1})
	c.slots <- struct{}{}
	result, err := c.Collect(context.Background(), 2506, 1)
	<-c.slots
	if err != nil || result.Outcome != "busy" || snapshots.reads.Load() != 0 {
		t.Fatal(result, err)
	}
}

func TestCollectorPreservesRawForecastAndAttemptTimes(t *testing.T) {
	snapshots, coordinator := &snapshotStub{}, &coordinatorStub{}
	snapshots.save = func(cl, sl int, start, end time.Time, data *sptrans.LinePredictions, err error) {
		if cl != 2506 || sl != 1 || err != nil || start.IsZero() || end.Before(start) || data.ReferenceTimeRaw != "23:58" ||
			data.Stops[0].StopCode != 880001 || data.Stops[0].Vehicles[0].Prefix != "00017" || data.Stops[0].Vehicles[0].ArrivalTimeRaw != "00:03" {
			t.Fatal("lost source identity/clocks")
		}
	}
	c := collectorFixture(t, providerFunc(func(context.Context, int) (*sptrans.LinePredictions, error) { return syntheticPrediction(), nil }), snapshots, coordinator, Options{})
	result, err := c.Collect(context.Background(), 2506, 1)
	if err != nil || !result.Saved || result.Outcome != "collected" || result.SnapshotStatus != "available" || coordinator.failure || coordinator.finishes != 1 {
		t.Fatal(result, err)
	}
}

func TestCollectorEmptyAndProviderErrorsAreDistinct(t *testing.T) {
	for _, providerErr := range []error{nil, sptrans.ErrAuthentication, sptrans.ErrTransport} {
		snapshots, coordinator := &snapshotStub{}, &coordinatorStub{}
		snapshots.save = func(_, _ int, _, _ time.Time, data *sptrans.LinePredictions, err error) {
			if providerErr != nil && (data != nil || !errors.Is(err, providerErr)) {
				t.Fatal("provider failure became an empty success")
			}
		}
		c := collectorFixture(t, providerFunc(func(context.Context, int) (*sptrans.LinePredictions, error) {
			if providerErr != nil {
				return nil, providerErr
			}
			return &sptrans.LinePredictions{SptransLineID: 2506, ReferenceTimeRaw: "12:00", Stops: []sptrans.StopPrediction{}}, nil
		}), snapshots, coordinator, Options{})
		result, err := c.Collect(context.Background(), 2506, 1)
		if providerErr == nil {
			if err != nil || result.SnapshotStatus != "empty" || coordinator.failure {
				t.Fatal(result, err)
			}
		} else {
			if !errors.Is(err, providerErr) || !coordinator.failure || result.Outcome != "provider_error" {
				t.Fatal(result, err)
			}
		}
	}
}

func TestCollectorFailsClosedOnCacheOrCoordinationErrors(t *testing.T) {
	provider := providerFunc(func(context.Context, int) (*sptrans.LinePredictions, error) {
		t.Fatal("uncoordinated provider call")
		return nil, nil
	})
	for _, setup := range []struct {
		readErr, acquireErr error
		fresh, blocked      bool
	}{
		{readErr: errors.New("synthetic secret")}, {acquireErr: errors.New("synthetic secret")}, {fresh: true}, {blocked: true},
	} {
		snapshots := &snapshotStub{readErr: setup.readErr, fresh: setup.fresh}
		coordinator := &coordinatorStub{acquireErr: setup.acquireErr, blocked: setup.blocked}
		c := collectorFixture(t, provider, snapshots, coordinator, Options{})
		result, err := c.Collect(context.Background(), 2506, 1)
		if setup.readErr != nil && !errors.Is(err, ErrCache) {
			t.Fatal(err)
		}
		if setup.acquireErr != nil && !errors.Is(err, ErrCoordination) {
			t.Fatal(err)
		}
		if setup.fresh && result.Outcome != "cached" {
			t.Fatal(result)
		}
		if setup.blocked && result.Outcome != "throttled" {
			t.Fatal(result)
		}
	}
}

func TestCollectorSharedLeasePreventsConcurrentFetchAcrossInstances(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	provider := providerFunc(func(ctx context.Context, _ int) (*sptrans.LinePredictions, error) {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
			return syntheticPrediction(), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	coordinator := &coordinatorStub{}
	a := collectorFixture(t, provider, &snapshotStub{}, coordinator, Options{})
	b := collectorFixture(t, provider, &snapshotStub{}, coordinator, Options{})
	done := make(chan error, 1)
	go func() { _, err := a.Collect(context.Background(), 2506, 1); done <- err }()
	<-entered
	result, err := b.Collect(context.Background(), 2506, 1)
	if err != nil || result.Outcome != "throttled" || calls.Load() != 1 {
		t.Fatal(result, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	result, err = b.Collect(context.Background(), 2506, 1)
	if err != nil || result.Outcome != "throttled" || calls.Load() != 1 {
		t.Fatal("cooldown ignored", result, err)
	}
}

func TestCollectorCancellationTimeoutAndBoundedCleanup(t *testing.T) {
	snapshots, coordinator := &snapshotStub{}, &coordinatorStub{}
	c := collectorFixture(t, providerFunc(func(ctx context.Context, _ int) (*sptrans.LinePredictions, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}), snapshots, coordinator, Options{Timeout: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Collect(ctx, 2506, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	result, err := c.Collect(context.Background(), 2506, 1)
	if !errors.Is(err, context.DeadlineExceeded) || result.AttemptFinishedAt.IsZero() || snapshots.saves.Load() != 0 || !coordinator.failure || coordinator.finishes != 1 {
		t.Fatal(result, err)
	}
}

func TestCollectorSaveFailureSupersededAndLostLease(t *testing.T) {
	provider := providerFunc(func(context.Context, int) (*sptrans.LinePredictions, error) { return syntheticPrediction(), nil })
	for _, tc := range []struct {
		saveErr          error
		superseded, lost bool
	}{
		{saveErr: cache.ErrPredictionStore}, {superseded: true}, {lost: true},
	} {
		snapshots := &snapshotStub{saveErr: tc.saveErr, superseded: tc.superseded}
		coordinator := &coordinatorStub{lost: tc.lost}
		c := collectorFixture(t, provider, snapshots, coordinator, Options{})
		result, err := c.Collect(context.Background(), 2506, 1)
		if tc.saveErr != nil && (!errors.Is(err, ErrCache) || !coordinator.failure) {
			t.Fatal(result, err)
		}
		if tc.superseded && (err != nil || result.Outcome != "superseded" || result.Saved) {
			t.Fatal(result, err)
		}
		if tc.lost && !errors.Is(err, ErrLeaseLost) {
			t.Fatal(err)
		}
	}
}
