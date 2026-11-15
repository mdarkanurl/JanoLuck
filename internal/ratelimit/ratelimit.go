package ratelimit

import (
	"context"
	"time"
)

type Limit struct {
	Requests int
	Window   time.Duration
}

type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
	ResetAfter time.Duration
}

type Limiter interface {
	Allow(ctx context.Context, key string, limit Limit) (Result, error)
}

func valid(limit Limit) bool {
	return limit.Requests > 0 && limit.Window > 0
}

func allowAll(limit Limit) Result {
	return Result{
		Allowed:   true,
		Limit:     limit.Requests,
		Remaining: limit.Requests,
	}
}
