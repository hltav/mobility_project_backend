// Package predictions provides explicit collection only. Constructing a Collector
// starts no goroutine, scheduler, HTTP endpoint or provider request.
package predictions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"mobility_processor/internal/sptrans"
	cache "mobility_processor/internal/valkey_cache"
)

var (
	ErrConfiguration = errors.New("prediction collector: invalid configuration")
	ErrCache         = errors.New("prediction collector: cache unavailable")
	ErrCoordination  = errors.New("prediction collector: coordination unavailable")
	ErrLeaseLost     = errors.New("prediction collector: lease ownership lost")
	ErrProvider      = errors.New("prediction collector: provider failure")
)

type Provider interface {
	GetLinePredictions(context.Context, int) (*sptrans.LinePredictions, error)
}
type SnapshotCache interface {
	Read(context.Context, int, int) (cache.PredictionCacheRead, error)
	Save(context.Context, int, int, time.Time, time.Time, *sptrans.LinePredictions, error) (bool, error)
}
type Coordinator interface {
	AcquirePredictionLease(context.Context, int, int, string, time.Duration, time.Duration) (bool, error)
	FinishPredictionLease(context.Context, int, int, string, bool, time.Duration, time.Duration) (bool, error)
}
type Options struct {
	Timeout        time.Duration
	CleanupTimeout time.Duration
	LeaseTTL       time.Duration
	MinInterval    time.Duration
	MaxBackoff     time.Duration
	MaxConcurrent  int
}
type Result struct {
	Outcome        string // cached, busy, throttled, collected, provider_error, superseded, unavailable, canceled
	SnapshotStatus string
	Saved          bool
	// Zero for a skipped collection; otherwise actual provider-attempt boundaries.
	AttemptStartedAt  time.Time
	AttemptFinishedAt time.Time
}
type Collector struct {
	provider    Provider
	cache       SnapshotCache
	coordinator Coordinator
	options     Options
	slots       chan struct{}
	now         func() time.Time
	token       func() (string, error)
}

func NewCollector(provider Provider, snapshots SnapshotCache, coordinator Coordinator, options Options) (*Collector, error) {
	if provider == nil || snapshots == nil || coordinator == nil {
		return nil, ErrConfiguration
	}
	if options.Timeout == 0 {
		options.Timeout = 10 * time.Second
	}
	if options.CleanupTimeout == 0 {
		options.CleanupTimeout = 2 * time.Second
	}
	if options.MinInterval == 0 {
		options.MinInterval = 20 * time.Second
	}
	if options.MaxBackoff == 0 {
		options.MaxBackoff = 5 * time.Minute
	}
	if options.MaxConcurrent == 0 {
		options.MaxConcurrent = 2
	}
	if options.LeaseTTL == 0 {
		options.LeaseTTL = options.Timeout + options.CleanupTimeout + time.Second
	}
	if options.Timeout <= 0 || options.CleanupTimeout <= 0 || options.MinInterval < time.Millisecond ||
		options.MaxBackoff < options.MinInterval || options.MaxConcurrent < 1 || options.LeaseTTL < options.Timeout+options.CleanupTimeout+time.Second {
		return nil, ErrConfiguration
	}
	return &Collector{provider: provider, cache: snapshots, coordinator: coordinator, options: options, slots: make(chan struct{}, options.MaxConcurrent), now: time.Now,
		token: func() (string, error) {
			var bytes [16]byte
			if _, err := rand.Read(bytes[:]); err != nil {
				return "", ErrCoordination
			}
			return hex.EncodeToString(bytes[:]), nil
		}}, nil
}

// Collect requires a canonical CL/SL pair already proved by the caller. It does
// not resolve GTFS directions or read PostgreSQL. Providers must honor context.
func (c *Collector) Collect(parent context.Context, cl, direction int) (result Result, err error) {
	result.Outcome = "unavailable"
	if _, err = cache.KeyLinePredictions(cl, direction); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(parent, c.options.Timeout)
	defer cancel()
	if err = ctx.Err(); err != nil {
		result.Outcome = "canceled"
		return result, err
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		result.Outcome = "busy"
		return result, nil
	}
	cached, readErr := c.cache.Read(ctx, cl, direction)
	if readErr != nil || cached.State == "unavailable" {
		return result, safeFailure(ErrCache, readErr)
	}
	if cached.State == "fresh" {
		if cached.Snapshot == nil {
			return result, ErrCache
		}
		result.Outcome = "cached"
		result.SnapshotStatus = cached.Snapshot.Status
		return result, nil
	}
	if cached.State != "missing" && cached.State != "expired" {
		return result, ErrCache
	}
	token, tokenErr := c.token()
	if tokenErr != nil {
		return result, ErrCoordination
	}
	acquired, acquireErr := c.coordinator.AcquirePredictionLease(ctx, cl, direction, token, c.options.LeaseTTL, c.options.MinInterval)
	if acquireErr != nil {
		return result, safeFailure(ErrCoordination, acquireErr)
	}
	if !acquired {
		result.Outcome = "throttled"
		return result, nil
	}
	failed := true
	defer func() {
		// Bounded cleanup still releases only our lease after request cancellation.
		cleanup, stop := context.WithTimeout(context.WithoutCancel(parent), c.options.CleanupTimeout)
		defer stop()
		released, releaseErr := c.coordinator.FinishPredictionLease(cleanup, cl, direction, token, failed, c.options.MinInterval, c.options.MaxBackoff)
		if releaseErr != nil {
			err = errors.Join(err, safeFailure(ErrCoordination, releaseErr))
		} else if !released {
			err = errors.Join(err, ErrLeaseLost)
		}
	}()
	if ctx.Err() != nil {
		result.Outcome = "canceled"
		return result, ctx.Err()
	}
	result.AttemptStartedAt = c.now().UTC()
	data, providerErr := c.provider.GetLinePredictions(ctx, cl)
	result.AttemptFinishedAt = c.now().UTC()
	if ctx.Err() != nil {
		result.Outcome = "canceled"
		return result, ctx.Err()
	}
	if providerErr != nil {
		data = nil
	}
	saved, saveErr := c.cache.Save(ctx, cl, direction, result.AttemptStartedAt, result.AttemptFinishedAt, data, providerErr)
	if saveErr != nil {
		return result, errors.Join(safeFailure(ErrCache, saveErr), safeProviderError(providerErr))
	}
	result.Saved = saved
	failed = providerErr != nil
	if providerErr != nil {
		result.Outcome = "provider_error"
		result.SnapshotStatus = "provider_error"
		return result, safeProviderError(providerErr)
	}
	result.Outcome = "collected"
	if !saved {
		result.Outcome = "superseded"
	}
	result.SnapshotStatus = "empty"
	if data != nil {
		for _, stop := range data.Stops {
			if len(stop.Vehicles) > 0 {
				result.SnapshotStatus = "available"
				break
			}
		}
	}
	return result, nil
}

func safeFailure(kind, cause error) error {
	if errors.Is(cause, context.Canceled) {
		return errors.Join(kind, context.Canceled)
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return errors.Join(kind, context.DeadlineExceeded)
	}
	return kind
}
func safeProviderError(err error) error {
	if err == nil {
		return nil
	}
	for _, kind := range []error{context.Canceled, context.DeadlineExceeded, sptrans.ErrAuthentication, sptrans.ErrHTTPStatus,
		sptrans.ErrTransport, sptrans.ErrInvalidResponse, sptrans.ErrIncompleteResponse, sptrans.ErrResponseTooLarge} {
		if errors.Is(err, kind) {
			return errors.Join(ErrProvider, kind)
		}
	}
	return ErrProvider
}
