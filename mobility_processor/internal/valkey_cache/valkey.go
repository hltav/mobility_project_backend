package cache

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	rdb *redis.Client
}

func NewClientFromEnv() (*Client, error) {
	host := envOr("VALKEY_HOST", "127.0.0.1")
	port := envOr("VALKEY_PORT", "6379")
	password := os.Getenv("VALKEY_PASSWORD")

	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", host, port),
		Password: password,
		DB:       0,
	})

	if err := pingWithRetry(rdb); err != nil {
		return nil, fmt.Errorf("conectar no Valkey %s:%s: %w", host, port, err)
	}

	return &Client{rdb: rdb}, nil
}

func (c *Client) Close() error {
	return c.rdb.Close()
}

// Set armazena um valor JSON com TTL.
func (c *Client) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, key, value, ttl).Err()
}

// Get recupera um valor. Retorna nil, nil se a chave não existir.
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get %q: %w", key, err)
	}
	return val, nil
}

// Delete remove uma chave manualmente (invalidação).
func (c *Client) Delete(ctx context.Context, keys ...string) error {
	return c.rdb.Del(ctx, keys...).Err()
}

// Exists verifica se uma chave existe.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.rdb.Exists(ctx, key).Result()
	return n > 0, err
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func pingWithRetry(rdb *redis.Client) error {
	var lastErr error
	backoff := 500 * time.Millisecond
	for attempt := 1; attempt <= 5; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := rdb.Ping(ctx).Err()
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < 5 {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return lastErr
}
