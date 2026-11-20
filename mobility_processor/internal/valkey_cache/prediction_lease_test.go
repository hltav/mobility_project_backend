package cache

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Intercepts commands before the network. Verifies the real adapter's arguments
// and error paths, not Redis execution/atomicity of the Lua scripts.
type predictionLeaseHook struct {
	process func(context.Context, redis.Cmder) error
}

func (h predictionLeaseHook) DialHook(redis.DialHook) redis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("network forbidden in lease test")
	}
}
func (h predictionLeaseHook) ProcessHook(redis.ProcessHook) redis.ProcessHook { return h.process }
func (h predictionLeaseHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestPredictionLeaseAdapterIdentityTokenIntervalsAndErrors(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "unused-synthetic-address"})
	defer rdb.Close()
	calls := 0
	rdb.AddHook(predictionLeaseHook{process: func(_ context.Context, cmd redis.Cmder) error {
		calls++
		args := cmd.Args()
		if args[0] != "eval" || args[3] != "sptrans:prediction:v1:cl:2506:sl:1:lease" || args[4] != "sptrans:prediction:v1:cl:2506:sl:1:cooldown" {
			t.Fatal(args)
		}
		if calls == 1 {
			if args[1] != acquirePredictionLease || args[5] != "synthetic-token-123456" || args[6] != int64(13000) || args[7] != int64(20000) {
				t.Fatal(args)
			}
			cmd.(*redis.Cmd).SetVal(int64(1))
			return nil
		}
		if calls == 2 {
			if args[1] != finishPredictionLease || args[5] != "sptrans:prediction:v1:cl:2506:sl:1:failures" || args[6] != "synthetic-token-123456" || args[7] != 1 || args[8] != int64(20000) || args[9] != int64(300000) {
				t.Fatal(args)
			}
			cmd.(*redis.Cmd).SetVal(int64(0))
			return nil
		}
		return errors.New("synthetic confidential store failure")
	}})
	client := &Client{rdb: rdb}
	acquired, err := client.AcquirePredictionLease(context.Background(), 2506, 1, "synthetic-token-123456", 13*time.Second, 20*time.Second)
	if err != nil || !acquired {
		t.Fatal(acquired, err)
	}
	released, err := client.FinishPredictionLease(context.Background(), 2506, 1, "synthetic-token-123456", true, 20*time.Second, 5*time.Minute)
	if err != nil || released {
		t.Fatal("lost ownership was reported as released", err)
	}
	if _, err := client.AcquirePredictionLease(context.Background(), 2506, 1, "synthetic-token-123456", 13*time.Second, 20*time.Second); !errors.Is(err, ErrPredictionStore) {
		t.Fatal(err)
	}
	before := calls
	if _, err := client.AcquirePredictionLease(context.Background(), 2506, 0, "synthetic-token-123456", 13*time.Second, 20*time.Second); !errors.Is(err, ErrPredictionIdentity) {
		t.Fatal(err)
	}
	if calls != before {
		t.Fatal("invalid identity reached Valkey")
	}
}
