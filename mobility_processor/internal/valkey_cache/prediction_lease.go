package cache

import (
	"context"
	"time"
)

const acquirePredictionLease = `
if redis.call('EXISTS',KEYS[1])==1 or redis.call('EXISTS',KEYS[2])==1 then return 0 end
redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[2])
redis.call('SET',KEYS[2],'1','PX',ARGV[3])
return 1`

const finishPredictionLease = `
if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end
local delay=tonumber(ARGV[3])
if ARGV[2]=='1' then
 local failures=redis.call('INCR',KEYS[3])
 redis.call('PEXPIRE',KEYS[3],tonumber(ARGV[4])*2)
 delay=math.min(tonumber(ARGV[4]),delay*2^math.min(failures,20))
else redis.call('DEL',KEYS[3]) end
redis.call('SET',KEYS[2],'1','PX',math.floor(delay))
redis.call('DEL',KEYS[1])
return 1`

func (c *Client) AcquirePredictionLease(ctx context.Context, cl, direction int, token string, lease, minInterval time.Duration) (bool, error) {
	key, err := KeyLinePredictions(cl, direction)
	if err != nil {
		return false, err
	}
	if len(token) < 16 || len(token) > 128 || lease < time.Millisecond || minInterval < time.Millisecond {
		return false, ErrPredictionSnapshot
	}
	result, err := c.rdb.Eval(ctx, acquirePredictionLease, []string{key + ":lease", key + ":cooldown"}, token, lease.Milliseconds(), minInterval.Milliseconds()).Int64()
	if err != nil {
		return false, predictionStoreError(ctx, err)
	}
	return result == 1, nil
}

// Release, cooldown and backoff update are one token-guarded atomic operation.
func (c *Client) FinishPredictionLease(ctx context.Context, cl, direction int, token string, failed bool, minInterval, maxBackoff time.Duration) (bool, error) {
	key, err := KeyLinePredictions(cl, direction)
	if err != nil {
		return false, err
	}
	if len(token) < 16 || len(token) > 128 || minInterval < time.Millisecond || maxBackoff < minInterval {
		return false, ErrPredictionSnapshot
	}
	failure := 0
	if failed {
		failure = 1
	}
	result, err := c.rdb.Eval(ctx, finishPredictionLease, []string{key + ":lease", key + ":cooldown", key + ":failures"}, token, failure, minInterval.Milliseconds(), maxBackoff.Milliseconds()).Int64()
	if err != nil {
		return false, predictionStoreError(ctx, err)
	}
	return result == 1, nil
}
