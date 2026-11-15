package ratelimit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var redisWindowScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('PTTL', KEYS[1])
if ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {current, ttl}
`)

type RedisLimiter struct {
	client redis.Scripter
	prefix string
}

func NewRedisLimiter(client redis.Scripter, prefix string) *RedisLimiter {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "ratelimit"
	}
	return &RedisLimiter{client: client, prefix: prefix}
}

func (r *RedisLimiter) Allow(ctx context.Context, key string, limit Limit) (Result, error) {
	if !valid(limit) {
		return allowAll(limit), nil
	}
	if r == nil || r.client == nil {
		return Result{}, fmt.Errorf("ratelimit: redis client is not configured")
	}

	windowMS := limit.Window.Milliseconds()
	res, err := redisWindowScript.Run(ctx, r.client, []string{r.redisKey(key)}, windowMS).Result()
	if err != nil {
		return Result{}, fmt.Errorf("ratelimit: redis script: %w", err)
	}

	values, ok := res.([]any)
	if !ok || len(values) != 2 {
		return Result{}, fmt.Errorf("ratelimit: unexpected redis reply %T", res)
	}

	count, ok := toInt64(values[0])
	if !ok {
		return Result{}, fmt.Errorf("ratelimit: unexpected counter reply %T", values[0])
	}
	ttlMS, ok := toInt64(values[1])
	if !ok {
		return Result{}, fmt.Errorf("ratelimit: unexpected ttl reply %T", values[1])
	}

	resetAfter := time.Duration(ttlMS) * time.Millisecond
	if resetAfter < 0 {
		resetAfter = limit.Window
	}

	if count > int64(limit.Requests) {
		return Result{
			Allowed:    false,
			Limit:      limit.Requests,
			Remaining:  0,
			RetryAfter: resetAfter,
			ResetAfter: resetAfter,
		}, nil
	}

	return Result{
		Allowed:    true,
		Limit:      limit.Requests,
		Remaining:  limit.Requests - int(count),
		ResetAfter: resetAfter,
	}, nil
}

func (r *RedisLimiter) redisKey(key string) string {
	key = sanitizeKey(key)
	return r.prefix + ":" + key
}

func sanitizeKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "unknown"
	}
	const maxKeyLength = 256
	if len(key) > maxKeyLength {
		key = key[:maxKeyLength]
	}
	return key
}

func toInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case uint64:
		return int64(v), true
	default:
		return 0, false
	}
}
