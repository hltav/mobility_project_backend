package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"mobility_processor/internal/sptrans"
)

var (
	ErrPredictionIdentity = errors.New("prediction cache: invalid CL or operational direction")
	ErrPredictionSnapshot = errors.New("prediction cache: invalid snapshot")
	ErrPredictionStore    = errors.New("prediction cache: store unavailable")
)

type PredictionCacheOptions struct {
	FreshFor  time.Duration
	RetainFor time.Duration
}

// Only identifiers and raw forecast clocks are cached. No passenger identity,
// passenger GPS, or unnecessary provider vehicle/stop coordinates are retained.
type CachedForecastVehicle struct {
	Prefix         string  `json:"p"`
	ArrivalTimeRaw string  `json:"t"`
	CapturedAtRaw  *string `json:"ta,omitempty"`
}
type CachedForecastStop struct {
	StopCode int64                   `json:"cp"`
	Vehicles []CachedForecastVehicle `json:"vs"`
}
type CachedForecast struct {
	ReferenceTimeRaw string               `json:"hr"`
	Stops            []CachedForecastStop `json:"ps"`
}
type PredictionSnapshot struct {
	SchemaVersion        int       `json:"schema_version"`
	Source               string    `json:"source"`
	SptransLineID        int       `json:"sptrans_line_id"`
	SptransDirection     int       `json:"sptrans_direction"`
	AttemptStartedAt     time.Time `json:"attempt_started_at"`
	AttemptStartedUnixMS int64     `json:"attempt_started_at_unix_ms"`
	// Collection completion, never a date inferred from hr/t/ta.
	FetchedAt         time.Time       `json:"fetched_at"`
	ValidUntil        time.Time       `json:"valid_until"`
	Status            string          `json:"status"` // available, empty, provider_error
	ProviderErrorCode string          `json:"provider_error_code,omitempty"`
	Predictions       *CachedForecast `json:"predictions"`
}
type PredictionCacheRead struct {
	State    string              `json:"cache_state"` // missing, fresh, expired, unavailable
	Snapshot *PredictionSnapshot `json:"snapshot"`
}

type predictionStore interface {
	Get(context.Context, string) ([]byte, error)
	setPredictionIfNewer(context.Context, string, []byte, int64, time.Duration) (bool, error)
}
type PredictionCache struct {
	store   predictionStore
	options PredictionCacheOptions
	now     func() time.Time
}

func NewPredictionCache(client *Client, options PredictionCacheOptions) (*PredictionCache, error) {
	if client == nil || client.rdb == nil {
		return nil, ErrPredictionStore
	}
	if options.FreshFor == 0 {
		options.FreshFor = 20 * time.Second
	}
	if options.RetainFor == 0 {
		options.RetainFor = 2 * time.Minute
	}
	if options.FreshFor < time.Millisecond || options.RetainFor < options.FreshFor {
		return nil, ErrPredictionSnapshot
	}
	return &PredictionCache{store: client, options: options, now: time.Now}, nil
}

func KeyLinePredictions(cl, direction int) (string, error) {
	if cl <= 0 || (direction != 1 && direction != 2) {
		return "", ErrPredictionIdentity
	}
	return fmt.Sprintf("sptrans:prediction:v1:cl:%d:sl:%d", cl, direction), nil
}

// Save records a completed collection; it never calls the provider. The caller
// must supply the actual attempt start/completion times and canonical SPTrans SL.
// false,nil means an equal/newer attempt already won. This is not fetch deduplication.
func (c *PredictionCache) Save(ctx context.Context, cl, direction int, started, fetched time.Time, data *sptrans.LinePredictions, providerErr error) (bool, error) {
	key, err := KeyLinePredictions(cl, direction)
	if err != nil {
		return false, err
	}
	snapshot := PredictionSnapshot{SchemaVersion: 1, Source: "sptrans", SptransLineID: cl, SptransDirection: direction,
		AttemptStartedAt: started.UTC(), AttemptStartedUnixMS: started.UnixMilli(), FetchedAt: fetched.UTC(),
		ValidUntil: fetched.UTC().Add(c.options.FreshFor), Status: "provider_error"}
	if providerErr != nil {
		if data != nil {
			return false, ErrPredictionSnapshot
		}
		snapshot.ProviderErrorCode = predictionFailureCode(providerErr)
	} else {
		if data == nil || data.SptransLineID != cl || data.Stops == nil {
			return false, ErrPredictionSnapshot
		}
		snapshot.Predictions = &CachedForecast{ReferenceTimeRaw: data.ReferenceTimeRaw, Stops: make([]CachedForecastStop, 0, len(data.Stops))}
		snapshot.Status = "empty"
		for _, stop := range data.Stops {
			out := CachedForecastStop{StopCode: stop.StopCode, Vehicles: make([]CachedForecastVehicle, 0, len(stop.Vehicles))}
			if stop.Vehicles == nil {
				return false, ErrPredictionSnapshot
			}
			for _, v := range stop.Vehicles {
				out.Vehicles = append(out.Vehicles, CachedForecastVehicle{Prefix: v.Prefix, ArrivalTimeRaw: v.ArrivalTimeRaw, CapturedAtRaw: v.CapturedAtRaw})
			}
			if len(out.Vehicles) > 0 {
				snapshot.Status = "available"
			}
			snapshot.Predictions.Stops = append(snapshot.Predictions.Stops, out)
		}
	}
	now := c.now().UTC()
	if !validPredictionSnapshot(snapshot, cl, direction) || fetched.After(now.Add(time.Second)) {
		return false, ErrPredictionSnapshot
	}
	ttl := fetched.Add(c.options.RetainFor).Sub(now)
	if ttl < time.Millisecond {
		return false, ErrPredictionSnapshot
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return false, ErrPredictionSnapshot
	}
	saved, err := c.store.setPredictionIfNewer(ctx, key, payload, snapshot.AttemptStartedUnixMS, ttl)
	if err != nil {
		return false, predictionStoreError(ctx, err)
	}
	return saved, nil
}

func (c *PredictionCache) Read(ctx context.Context, cl, direction int) (PredictionCacheRead, error) {
	result := PredictionCacheRead{State: "unavailable"}
	key, err := KeyLinePredictions(cl, direction)
	if err != nil {
		return result, err
	}
	payload, err := c.store.Get(ctx, key)
	if err != nil {
		return result, predictionStoreError(ctx, err)
	}
	if payload == nil {
		result.State = "missing"
		return result, nil
	}
	var snapshot PredictionSnapshot
	if json.Unmarshal(payload, &snapshot) != nil || !validPredictionSnapshot(snapshot, cl, direction) || snapshot.FetchedAt.After(c.now().Add(time.Second)) {
		return result, ErrPredictionSnapshot
	}
	result.Snapshot = &snapshot
	result.State = "fresh"
	if !c.now().Before(snapshot.ValidUntil) {
		result.State = "expired"
	}
	return result, nil
}

func validPredictionSnapshot(s PredictionSnapshot, cl, direction int) bool {
	if s.SchemaVersion != 1 || s.Source != "sptrans" || s.SptransLineID != cl || s.SptransDirection != direction ||
		s.AttemptStartedUnixMS <= 0 || s.AttemptStartedUnixMS != s.AttemptStartedAt.UnixMilli() || s.FetchedAt.Before(s.AttemptStartedAt) || !s.ValidUntil.After(s.FetchedAt) {
		return false
	}
	if s.Status == "provider_error" {
		return s.Predictions == nil && s.ProviderErrorCode != ""
	}
	if s.Predictions == nil || s.Predictions.Stops == nil || strings.TrimSpace(s.Predictions.ReferenceTimeRaw) == "" || s.ProviderErrorCode != "" {
		return false
	}
	vehicles := 0
	for _, stop := range s.Predictions.Stops {
		if stop.StopCode <= 0 || stop.Vehicles == nil {
			return false
		}
		for _, v := range stop.Vehicles {
			if strings.TrimSpace(v.Prefix) == "" || strings.TrimSpace(v.ArrivalTimeRaw) == "" {
				return false
			}
			vehicles++
		}
	}
	return (s.Status == "empty" && vehicles == 0) || (s.Status == "available" && vehicles > 0)
}

func predictionFailureCode(err error) string {
	for _, entry := range []struct {
		err  error
		code string
	}{
		{context.Canceled, "canceled"}, {context.DeadlineExceeded, "timeout"}, {sptrans.ErrAuthentication, "authentication"},
		{sptrans.ErrHTTPStatus, "http_status"}, {sptrans.ErrInvalidResponse, "invalid_response"},
		{sptrans.ErrIncompleteResponse, "incomplete_response"}, {sptrans.ErrResponseTooLarge, "response_too_large"}, {sptrans.ErrTransport, "transport"},
	} {
		if errors.Is(err, entry.err) {
			return entry.code
		}
	}
	return "provider_failure"
}
func predictionStoreError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errors.Join(ErrPredictionStore, ctx.Err())
	}
	if errors.Is(err, context.Canceled) {
		return errors.Join(ErrPredictionStore, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.Join(ErrPredictionStore, context.DeadlineExceeded)
	}
	return ErrPredictionStore
}

// Atomic across replicas. Millisecond ties are conservatively first-writer-wins.
const predictionCAS = `
local old=redis.call('GET',KEYS[1])
if old then
 local ok,value=pcall(cjson.decode,old)
 if not ok or type(value)~='table' or type(value.attempt_started_at_unix_ms)~='number' then
  return redis.error_reply('invalid_prediction_snapshot')
 end
 if value.attempt_started_at_unix_ms>=tonumber(ARGV[2]) then return 0 end
end
redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[3])
return 1`

func (c *Client) setPredictionIfNewer(ctx context.Context, key string, payload []byte, startedMS int64, ttl time.Duration) (bool, error) {
	result, err := c.rdb.Eval(ctx, predictionCAS, []string{key}, string(payload), startedMS, ttl.Milliseconds()).Int64()
	return result == 1, err
}
