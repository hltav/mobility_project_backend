package cache

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"mobility_processor/internal/sptrans"
)

// Synthetic fixtures and an in-memory store: no network, Valkey server, or
// homologated provider sample. The fake models CAS's contract, not Lua execution.
type predictionMemoryStore struct {
	mu          sync.Mutex
	values      map[string][]byte
	started     map[string]int64
	expirations map[string]time.Time
	now         func() time.Time
	failure     bool
}

func (m *predictionMemoryStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failure {
		return nil, errors.New("synthetic connection error with confidential details")
	}
	if expiration, ok := m.expirations[key]; ok && !m.now().Before(expiration) {
		delete(m.values, key)
		delete(m.started, key)
	}
	return append([]byte(nil), m.values[key]...), nil
}
func (m *predictionMemoryStore) setPredictionIfNewer(ctx context.Context, key string, data []byte, started int64, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failure {
		return false, errors.New("synthetic store error")
	}
	if old, ok := m.started[key]; ok && old >= started {
		return false, nil
	}
	m.values[key] = append([]byte(nil), data...)
	m.started[key] = started
	m.expirations[key] = m.now().Add(ttl)
	return true, nil
}
func predictionFixture(t *testing.T) (*PredictionCache, *predictionMemoryStore, *time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	memory := &predictionMemoryStore{values: map[string][]byte{}, started: map[string]int64{}, expirations: map[string]time.Time{}, now: func() time.Time { return now }}
	// Constructor itself must reuse the supplied client, without dialing it.
	client := &Client{rdb: &redis.Client{}}
	cache, err := NewPredictionCache(client, PredictionCacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cache.store != client || cache.options.FreshFor != 20*time.Second || cache.options.RetainFor != 2*time.Minute {
		t.Fatal("connection/default policy changed")
	}
	cache.store = memory
	cache.now = func() time.Time { return now }
	return cache, memory, &now
}
func syntheticForecast() *sptrans.LinePredictions {
	return &sptrans.LinePredictions{SptransLineID: 2506, ReferenceTimeRaw: "23:58", Stops: []sptrans.StopPrediction{{StopCode: 880001,
		Vehicles: []sptrans.VehiclePrediction{{Prefix: "00017", ArrivalTimeRaw: "00:03"}}}}}
}

func TestPredictionCacheIdentityAndOptions(t *testing.T) {
	key, err := KeyLinePredictions(2506, 1)
	if err != nil || key != "sptrans:prediction:v1:cl:2506:sl:1" {
		t.Fatal(key, err)
	}
	other, _ := KeyLinePredictions(2506, 2)
	if other == key {
		t.Fatal("direction collision")
	}
	for _, args := range [][2]int{{0, 1}, {2506, 0}, {2506, 3}} {
		if _, err := KeyLinePredictions(args[0], args[1]); !errors.Is(err, ErrPredictionIdentity) {
			t.Fatal(err)
		}
	}
	if _, err := NewPredictionCache(nil, PredictionCacheOptions{}); !errors.Is(err, ErrPredictionStore) {
		t.Fatal(err)
	}
	if _, err := NewPredictionCache(&Client{rdb: &redis.Client{}}, PredictionCacheOptions{FreshFor: time.Minute, RetainFor: time.Second}); !errors.Is(err, ErrPredictionSnapshot) {
		t.Fatal(err)
	}
}

func TestPredictionCacheRoundTripSemanticExpiryAndPhysicalRetention(t *testing.T) {
	c, store, now := predictionFixture(t)
	result, err := c.Read(context.Background(), 2506, 1)
	if err != nil || result.State != "missing" {
		t.Fatal(result, err)
	}
	saved, err := c.Save(context.Background(), 2506, 1, now.Add(-time.Second), *now, syntheticForecast(), nil)
	if !saved || err != nil {
		t.Fatal(saved, err)
	}
	result, err = c.Read(context.Background(), 2506, 1)
	if err != nil || result.State != "fresh" || result.Snapshot.Status != "available" || result.Snapshot.Predictions.ReferenceTimeRaw != "23:58" || result.Snapshot.Predictions.Stops[0].Vehicles[0].Prefix != "00017" || result.Snapshot.Predictions.Stops[0].Vehicles[0].ArrivalTimeRaw != "00:03" {
		t.Fatal(result, err)
	}
	if result.Snapshot.ValidUntil.Sub(result.Snapshot.FetchedAt) != 20*time.Second {
		t.Fatal("wrong semantic freshness")
	}
	key, _ := KeyLinePredictions(2506, 1)
	if store.expirations[key].Sub(*now) != 2*time.Minute {
		t.Fatal("physical TTL confused with freshness")
	}
	*now = now.Add(20 * time.Second)
	result, err = c.Read(context.Background(), 2506, 1)
	if err != nil || result.State != "expired" || result.Snapshot == nil {
		t.Fatal(result, err)
	}
	*now = now.Add(100 * time.Second)
	result, err = c.Read(context.Background(), 2506, 1)
	if err != nil || result.State != "missing" {
		t.Fatal(result, err)
	}
}

func TestPredictionEmptyAndProviderFailureAreDistinct(t *testing.T) {
	c, _, now := predictionFixture(t)
	empty := &sptrans.LinePredictions{SptransLineID: 2506, ReferenceTimeRaw: "12:00", Stops: []sptrans.StopPrediction{}}
	if _, err := c.Save(context.Background(), 2506, 1, now.Add(-time.Second), *now, empty, nil); err != nil {
		t.Fatal(err)
	}
	result, _ := c.Read(context.Background(), 2506, 1)
	if result.State != "fresh" || result.Snapshot.Status != "empty" || result.Snapshot.Predictions == nil {
		t.Fatal(result)
	}
	if _, err := c.Save(context.Background(), 2506, 2, now.Add(-time.Second), *now, nil, sptrans.ErrAuthentication); err != nil {
		t.Fatal(err)
	}
	result, _ = c.Read(context.Background(), 2506, 2)
	if result.Snapshot.Status != "provider_error" || result.Snapshot.Predictions != nil || result.Snapshot.ProviderErrorCode != "authentication" {
		t.Fatal(result)
	}
	result, _ = c.Read(context.Background(), 35274, 2)
	if result.State != "missing" {
		t.Fatal("line identity leaked")
	}
}

func TestPredictionWritesRejectOlderAndEqualAttempts(t *testing.T) {
	c, _, now := predictionFixture(t)
	for _, tc := range []struct {
		started time.Time
		want    bool
	}{
		{now.Add(-time.Second), true}, {now.Add(-2 * time.Second), false}, {now.Add(-time.Second), false},
	} {
		saved, err := c.Save(context.Background(), 2506, 1, tc.started, *now, syntheticForecast(), nil)
		if err != nil || saved != tc.want {
			t.Fatal("wrong attempt ordering decision", saved, err)
		}
	}

	saved, err := c.Save(context.Background(), 2506, 1, now.Add(-time.Second), *now, syntheticForecast(), nil)
	if err != nil || saved {
		t.Fatal("equal attempt replaced existing snapshot", err)
	}
}

func TestPredictionConcurrentWriteContractKeepsNewestAttempt(t *testing.T) {
	c, _, now := predictionFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, started := range []time.Time{now.Add(-2 * time.Second), now.Add(-time.Second)} {
		wg.Add(1)
		go func(start time.Time) {
			defer wg.Done()
			_, err := c.Save(context.Background(), 2506, 1, start, *now, syntheticForecast(), nil)
			errs <- err
		}(started)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := c.Read(context.Background(), 2506, 1)
	if err != nil || result.Snapshot.AttemptStartedUnixMS != now.Add(-time.Second).UnixMilli() {
		t.Fatal(result, err)
	}
}

func TestPredictionPrivacyStoreFailureAndCancellation(t *testing.T) {
	c, store, now := predictionFixture(t)
	forecast := syntheticForecast()
	lat, lon := -23.55, -46.63
	forecast.Stops[0].Latitude = &lat
	forecast.Stops[0].Longitude = &lon
	forecast.Stops[0].Vehicles[0].Latitude = &lat
	forecast.Stops[0].Vehicles[0].Longitude = &lon
	if _, err := c.Save(context.Background(), 2506, 1, now.Add(-time.Second), *now, forecast, nil); err != nil {
		t.Fatal(err)
	}
	key, _ := KeyLinePredictions(2506, 1)
	raw := string(store.values[key])
	for _, forbidden := range []string{`"py"`, `"px"`, `passenger`, `user_id`, `-23.55`, `-46.63`, `eta_minutes`, `predicted_arrival_at`} {
		if strings.Contains(raw, forbidden) {
			t.Fatal("unnecessary data serialized", forbidden)
		}
	}
	store.failure = true
	result, err := c.Read(context.Background(), 2506, 1)
	if result.State != "unavailable" || !errors.Is(err, ErrPredictionStore) || strings.Contains(err.Error(), "confidential") {
		t.Fatal(result, err)
	}
	if _, err := c.Save(context.Background(), 2506, 1, *now, *now, forecast, nil); !errors.Is(err, ErrPredictionStore) {
		t.Fatal(err)
	}
	redacted := predictionStoreError(context.Background(), errors.Join(context.DeadlineExceeded, errors.New("synthetic confidential details")))
	if !errors.Is(redacted, context.DeadlineExceeded) || strings.Contains(redacted.Error(), "confidential") {
		t.Fatal("wrapped context error leaked store details")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Read(ctx, 2506, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPredictionCacheRejectsMismatchedAndCorruptSnapshots(t *testing.T) {
	c, store, now := predictionFixture(t)
	forecast := syntheticForecast()
	forecast.SptransLineID = 35274
	if _, err := c.Save(context.Background(), 2506, 1, *now, *now, forecast, nil); !errors.Is(err, ErrPredictionSnapshot) {
		t.Fatal(err)
	}
	key, _ := KeyLinePredictions(2506, 1)
	store.values[key] = []byte(`{"schema_version":1}`)
	if _, err := c.Read(context.Background(), 2506, 1); !errors.Is(err, ErrPredictionSnapshot) {
		t.Fatal(err)
	}
	store.values[key] = []byte(`invalid`)
	if _, err := c.Read(context.Background(), 2506, 1); !errors.Is(err, ErrPredictionSnapshot) {
		t.Fatal(err)
	}
	// Unknown provider error messages must never be stored verbatim.
	store.values = map[string][]byte{}
	if _, err := c.Save(context.Background(), 2506, 1, *now, *now, nil, errors.New("synthetic credentials")); err != nil {
		t.Fatal(err)
	}
	var snapshot PredictionSnapshot
	if json.Unmarshal(store.values[key], &snapshot) != nil || snapshot.ProviderErrorCode != "provider_failure" || strings.Contains(string(store.values[key]), "credentials") {
		t.Fatal(snapshot)
	}
}
