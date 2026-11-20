package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"mobility_processor/internal/sptrans"
)

// Explicit opt-in; never uses operational VALKEY_* configuration or defaults.
const predictionTestMarker = "test:ope38:lua:instance"

type predictionNamespaceHook struct {
	prefix string
	mu     sync.Mutex
	keys   map[string]struct{}
}

func (h *predictionNamespaceHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *predictionNamespaceHook) rewrite(cmd redis.Cmder) error {
	args := cmd.Args()
	positions := []int{}
	switch strings.ToLower(cmd.Name()) {
	case "hello":
		// go-redis clones hooks into each new pooled connection, including
		// its handshake. These commands have no keys to namespace.
		return nil
	case "client":
		if len(args) > 1 {
			subcommand, _ := args[1].(string)
			switch strings.ToLower(subcommand) {
			case "setinfo", "maint_notifications":
				return nil
			}
		}
		return errors.New("CLIENT subcommand forbidden in namespaced Lua test")
	case "get", "set", "pttl", "pexpire", "exists", "incr":
		positions = []int{1}
	case "del":
		for i := 1; i < len(args); i++ {
			positions = append(positions, i)
		}
	case "eval":
		if len(args) < 3 {
			return errors.New("invalid test EVAL keys")
		}
		count, ok := args[2].(int)
		if !ok || count < 1 || count > len(args)-3 {
			return errors.New("invalid test EVAL keys")
		}
		for i := 0; i < count; i++ {
			positions = append(positions, 3+i)
		}
	default:
		return errors.New("command forbidden in namespaced Lua test")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, i := range positions {
		if i >= len(args) {
			return errors.New("missing test key")
		}
		key, ok := args[i].(string)
		if !ok {
			return errors.New("invalid test key")
		}
		if !strings.HasPrefix(key, h.prefix) {
			key = h.prefix + key
		}
		args[i] = key
		h.keys[key] = struct{}{}
	}
	return nil
}

// No server is required: exercise the command forms used by go-redis during
// connection initialization, through both hook paths used by the real client.
func TestPredictionNamespaceHookConnectionInitialization(t *testing.T) {
	h := &predictionNamespaceHook{prefix: "test:hook:", keys: map[string]struct{}{}}
	commands := [][]interface{}{
		{"hello", 2},
		{"hello", 3},
		{"client", "setinfo", "LIB-NAME", "go-redis"},
		{"client", "setinfo", "LIB-VER", "synthetic"},
		{"client", "maint_notifications", "on", "moving-endpoint-type", "none"},
		{"client", "maint_notifications", "off"},
	}
	var pipeline []redis.Cmder
	called := 0
	process := h.ProcessHook(func(context.Context, redis.Cmder) error {
		called++
		return nil
	})
	for _, args := range commands {
		cmd := redis.NewCmd(context.Background(), append([]interface{}(nil), args...)...)
		if err := process(context.Background(), cmd); err != nil {
			t.Fatal("connection initialization blocked", args[0], err)
		}
		if !reflect.DeepEqual(cmd.Args(), args) {
			t.Fatal("connection initialization arguments changed")
		}
		pipeline = append(pipeline, cmd)
	}
	processPipeline := h.ProcessPipelineHook(func(_ context.Context, cmds []redis.Cmder) error {
		called += len(cmds)
		return nil
	})
	if err := processPipeline(context.Background(), pipeline); err != nil {
		t.Fatal("connection initialization pipeline blocked", err)
	}
	if called != 2*len(commands) || len(h.keys) != 0 {
		t.Fatal("handshake was not forwarded without recording keys")
	}
}

func TestPredictionNamespaceHookEvalIsolation(t *testing.T) {
	h := &predictionNamespaceHook{prefix: "test:hook:", keys: map[string]struct{}{}}
	args := []interface{}{"eval", acquirePredictionLease, 2, "lease", "cooldown", "synthetic-token-123456", int64(5000), int64(200)}
	cmd := redis.NewCmd(context.Background(), args...)
	want := []interface{}{"eval", acquirePredictionLease, 2, "test:hook:lease", "test:hook:cooldown", "synthetic-token-123456", int64(5000), int64(200)}
	for i := 0; i < 2; i++ {
		if err := h.rewrite(cmd); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cmd.Args(), want) || len(h.keys) != 2 {
			t.Fatal("EVAL keys must be prefixed once; script, key count and ARGV must be preserved")
		}
	}
}

func TestPredictionNamespaceHookRejectsUnsafeCommands(t *testing.T) {
	for _, args := range [][]interface{}{
		{"flushdb"}, {"flushall"}, {"client", "kill", "all"},
		{"select", 1}, {"eval"}, {"eval", "return 1", 0},
		{"eval", "return 1", 2, "only-one-key"}, {"get"},
	} {
		h := &predictionNamespaceHook{prefix: "test:hook:", keys: map[string]struct{}{}}
		if err := h.rewrite(redis.NewCmd(context.Background(), args...)); err == nil {
			t.Fatal("unsafe or malformed command accepted", args[0])
		}
		if len(h.keys) != 0 {
			t.Fatal("rejected command recorded keys")
		}
	}
}
func (h *predictionNamespaceHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err := h.rewrite(cmd); err != nil {
			return err
		}
		return next(ctx, cmd)
	}
}
func (h *predictionNamespaceHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, commands []redis.Cmder) error {
		for _, cmd := range commands {
			if err := h.rewrite(cmd); err != nil {
				return err
			}
		}
		return next(ctx, commands)
	}
}

func isolatedPredictionClient(t *testing.T) (*Client, *predictionNamespaceHook) {
	t.Helper()
	addr, run := os.Getenv("VALKEY_LUA_TEST_ADDR"), os.Getenv("VALKEY_LUA_TEST_RUN_ID")
	if addr == "" || run == "" || os.Getenv("VALKEY_LUA_TEST_ISOLATED") != "1" {
		t.Skip("isolated Lua tests require VALKEY_LUA_TEST_ADDR, VALKEY_LUA_TEST_RUN_ID and VALKEY_LUA_TEST_ISOLATED=1")
	}
	host, port, err := net.SplitHostPort(addr)
	ip := net.ParseIP(host)
	number, parseErr := strconv.Atoi(port)
	if err != nil || ip == nil || !ip.IsLoopback() || parseErr != nil || number < 1 || number > 65535 || number == 6379 {
		t.Fatal("test address must be an explicit loopback IP and non-operational port, never 6379")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{16,80}$`).MatchString(run) {
		t.Fatal("invalid isolated execution ID")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 0, DialTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	marker, err := rdb.Get(ctx, predictionTestMarker).Result()
	if err != nil || marker != run {
		t.Fatal("isolated Valkey marker missing/mismatched; no test writes allowed")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal("test namespace generation failed")
	}
	h := &predictionNamespaceHook{prefix: "test:ope38:lua:" + run + ":" + hex.EncodeToString(random[:]) + ":", keys: map[string]struct{}{}}
	rdb.AddHook(h)
	t.Cleanup(func() {
		h.mu.Lock()
		keys := make([]string, 0, len(h.keys))
		for key := range h.keys {
			if !strings.HasPrefix(key, h.prefix) {
				h.mu.Unlock()
				t.Error("refusing non-owned cleanup key")
				return
			}
			keys = append(keys, key)
		}
		h.mu.Unlock()
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if len(keys) > 0 {
			if err := rdb.Del(cleanup, keys...).Err(); err != nil {
				t.Error("failed to delete this test's keys")
			}
		}
	})
	return &Client{rdb: rdb}, h
}

func luaForecast() *sptrans.LinePredictions {
	return &sptrans.LinePredictions{SptransLineID: 2506, ReferenceTimeRaw: "23:58", Stops: []sptrans.StopPrediction{{StopCode: 880001, Vehicles: []sptrans.VehiclePrediction{{Prefix: "00017", ArrivalTimeRaw: "00:03"}}}}}
}
func luaRequire(t *testing.T, operation func() (bool, error)) {
	t.Helper()
	ok, err := operation()
	if err != nil || !ok {
		t.Fatal("Lua operation failed", ok, err)
	}
}
func waitPredictionKeyExpiry(t *testing.T, c *Client, key string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		count, err := c.rdb.Exists(ctx, key).Result()
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("test key did not expire within bounded wait")
}

func TestPredictionLuaSnapshotOrderingConcurrentWrites(t *testing.T) {
	c, _ := isolatedPredictionClient(t)
	snapshots, err := NewPredictionCache(c, PredictionCacheOptions{FreshFor: 20 * time.Second, RetainFor: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	count := 16
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := snapshots.Save(ctx, 2506, 1, now.Add(-time.Duration(count-i)*time.Millisecond), now, luaForecast(), nil)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := snapshots.Read(ctx, 2506, 1)
	if err != nil || result.Snapshot == nil || result.Snapshot.AttemptStartedUnixMS != now.Add(-time.Millisecond).UnixMilli() {
		t.Fatal("newest attempt did not win", result, err)
	}
	if result.Snapshot.Predictions.Stops[0].Vehicles[0].Prefix != "00017" {
		t.Fatal("text prefix changed")
	}
	before, err := c.rdb.PTTL(ctx, "sptrans:prediction:v1:cl:2506:sl:1").Result()
	if err != nil || before <= 0 {
		t.Fatal(before, err)
	}
	saved, err := snapshots.Save(ctx, 2506, 1, now.Add(-time.Second), now, luaForecast(), nil)
	if err != nil || saved {
		t.Fatal("older snapshot was accepted", err)
	}
	saved, err = snapshots.Save(ctx, 2506, 1, now.Add(-time.Millisecond), now, luaForecast(), nil)
	if err != nil || saved {
		t.Fatal("equal snapshot was accepted", err)
	}
	after, err := c.rdb.PTTL(ctx, "sptrans:prediction:v1:cl:2506:sl:1").Result()
	if err != nil || after > before {
		t.Fatal("rejected write renewed retention", after, before, err)
	}
}

func TestPredictionLuaLeaseConcurrentAcquisitionAndTokenOwnership(t *testing.T) {
	c, h := isolatedPredictionClient(t)
	options := *c.rdb.Options()
	secondRedis := redis.NewClient(&options)
	secondRedis.AddHook(h)
	t.Cleanup(func() { _ = secondRedis.Close() })
	second := &Client{rdb: secondRedis}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const base = "sptrans:prediction:v1:cl:2506:sl:1"
	tokens := make(chan string, 16)
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token := "synthetic-token-" + strconv.Itoa(i)
			client := c
			if i%2 == 1 {
				client = second
			}
			ok, err := client.AcquirePredictionLease(ctx, 2506, 1, token, 5*time.Second, 200*time.Millisecond)
			if err != nil {
				errs <- err
			}
			if ok {
				tokens <- token
			}
		}(i)
	}
	wg.Wait()
	close(tokens)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	winners := []string{}
	for token := range tokens {
		winners = append(winners, token)
	}
	if len(winners) != 1 {
		t.Fatal("concurrent callers obtained duplicate leases", winners)
	}
	released, err := c.FinishPredictionLease(ctx, 2506, 1, "wrong-token-123456", true, 200*time.Millisecond, time.Second)
	if err != nil || released {
		t.Fatal("wrong token released lease", err)
	}
	owner, err := c.rdb.Get(ctx, base+":lease").Result()
	if err != nil || owner != winners[0] {
		t.Fatal("lease ownership changed", err)
	}
	if failures, err := c.rdb.Exists(ctx, base+":failures").Result(); err != nil || failures != 0 {
		t.Fatal("wrong token changed backoff", err)
	}
	luaRequire(t, func() (bool, error) {
		return c.FinishPredictionLease(ctx, 2506, 1, winners[0], false, 200*time.Millisecond, time.Second)
	})
	acquired, err := c.AcquirePredictionLease(ctx, 2506, 1, "another-token-123456", 5*time.Second, 200*time.Millisecond)
	if err != nil || acquired {
		t.Fatal("cooldown ignored", err)
	}
	waitPredictionKeyExpiry(t, c, base+":cooldown")
	luaRequire(t, func() (bool, error) {
		return c.AcquirePredictionLease(ctx, 2506, 1, "another-token-123456", 5*time.Second, 200*time.Millisecond)
	})
	luaRequire(t, func() (bool, error) {
		return c.FinishPredictionLease(ctx, 2506, 1, "another-token-123456", false, 200*time.Millisecond, time.Second)
	})
}

func TestPredictionLuaBackoffCapResetAndExpiredOwner(t *testing.T) {
	c, _ := isolatedPredictionClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const base = "sptrans:prediction:v1:cl:2506:sl:1"
	const min = 100 * time.Millisecond
	const capDelay = 350 * time.Millisecond
	for i, want := range []time.Duration{200 * time.Millisecond, capDelay, capDelay} {
		token := "failure-token-123456-" + strconv.Itoa(i)
		luaRequire(t, func() (bool, error) { return c.AcquirePredictionLease(ctx, 2506, 1, token, time.Second, min) })
		at := time.Now()
		luaRequire(t, func() (bool, error) { return c.FinishPredictionLease(ctx, 2506, 1, token, true, min, capDelay) })
		ttl, err := c.rdb.PTTL(ctx, base+":cooldown").Result()
		if err != nil || ttl <= 0 || ttl > want || ttl < want-time.Since(at)-30*time.Millisecond {
			t.Fatal("invalid backoff TTL", ttl, err)
		}
		// Logical presence plus lower bound at the longer intervals detects a missing exponential backoff.
		if i > 0 && ttl < min {
			t.Fatal("exponential backoff not applied", ttl)
		}
		ok, err := c.AcquirePredictionLease(ctx, 2506, 1, "blocked-token-123456", time.Second, min)
		if err != nil || ok {
			t.Fatal("backoff ignored", err)
		}
		waitPredictionKeyExpiry(t, c, base+":cooldown")
	}
	luaRequire(t, func() (bool, error) {
		return c.AcquirePredictionLease(ctx, 2506, 1, "success-token-123456", time.Second, min)
	})
	luaRequire(t, func() (bool, error) {
		return c.FinishPredictionLease(ctx, 2506, 1, "success-token-123456", false, min, capDelay)
	})
	if count, err := c.rdb.Exists(ctx, base+":failures").Result(); err != nil || count != 0 {
		t.Fatal("success did not reset failures", err)
	}
	waitPredictionKeyExpiry(t, c, base+":cooldown")
	luaRequire(t, func() (bool, error) {
		return c.AcquirePredictionLease(ctx, 2506, 1, "expired-token-123456", 50*time.Millisecond, 20*time.Millisecond)
	})
	waitPredictionKeyExpiry(t, c, base+":lease")
	waitPredictionKeyExpiry(t, c, base+":cooldown")
	luaRequire(t, func() (bool, error) {
		return c.AcquirePredictionLease(ctx, 2506, 1, "new-owner-token-123456", time.Second, min)
	})
	ok, err := c.FinishPredictionLease(ctx, 2506, 1, "expired-token-123456", false, min, capDelay)
	if err != nil || ok {
		t.Fatal("expired owner released replacement lease", err)
	}
	luaRequire(t, func() (bool, error) {
		return c.FinishPredictionLease(ctx, 2506, 1, "new-owner-token-123456", false, min, capDelay)
	})
}
