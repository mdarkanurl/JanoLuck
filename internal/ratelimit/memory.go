package ratelimit

import (
	"context"
	"sync"
	"time"
)

type MemoryLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	buckets map[string]*memoryBucket
	calls   uint64
}

type memoryBucket struct {
	count int
	reset time.Time
}

const memoryPruneEvery = 1024

func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{
		now:     time.Now,
		buckets: make(map[string]*memoryBucket),
	}
}

func (m *MemoryLimiter) Allow(_ context.Context, key string, limit Limit) (Result, error) {
	if !valid(limit) {
		return allowAll(limit), nil
	}

	now := m.now()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls++
	if m.calls%memoryPruneEvery == 0 {
		m.prune(now)
	}

	bucket, ok := m.buckets[key]
	if !ok || !now.Before(bucket.reset) {
		bucket = &memoryBucket{reset: now.Add(limit.Window)}
		m.buckets[key] = bucket
	}

	bucket.count++

	resetAfter := bucket.reset.Sub(now)
	if bucket.count > limit.Requests {
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
		Remaining:  limit.Requests - bucket.count,
		ResetAfter: resetAfter,
	}, nil
}

func (m *MemoryLimiter) prune(now time.Time) {
	for key, bucket := range m.buckets {
		if !now.Before(bucket.reset) {
			delete(m.buckets, key)
		}
	}
}
