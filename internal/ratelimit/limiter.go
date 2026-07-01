// In-memory token-bucket rate limiting, keyed by whatever string the caller
// hands in (usually "<category>:<client-ip>"). Good enough for a single node;
// a Redis-backed limiter can slot in behind the same Allow() shape later.
package ratelimit

import (
	"hash/maphash"
	"sync"
	"time"
)

// Rule is a refill rate plus a burst ceiling.
type Rule struct {
	// Every says one token is added this often.
	Every time.Duration
	// Burst is the bucket size (max tokens, and the most you can spend at once).
	Burst float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

// shardCount trades a little memory overhead for a lot less lock contention:
// under many concurrent distinct clients hitting the same category (e.g.
// "create" from thousands of different IPs at once), a single mutex+map
// serializes every one of them even though their keys never collide. 32
// shards means, on average, 32x fewer callers ever wait on the same lock.
const shardCount = 32

type shard struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

// Limiter holds buckets for one rule, sharded by key so unrelated callers
// don't serialize on each other. Stale buckets are swept periodically so
// memory doesn't grow without bound under IP churn.
type Limiter struct {
	rule   Rule
	shards [shardCount]*shard
	seed   maphash.Seed
	now    func() time.Time
}

func New(rule Rule) *Limiter {
	l := &Limiter{rule: rule, seed: maphash.MakeSeed(), now: time.Now}
	for i := range l.shards {
		l.shards[i] = &shard{buckets: make(map[string]*bucket)}
	}
	return l
}

func (l *Limiter) shardFor(key string) *shard {
	h := maphash.String(l.seed, key)
	return l.shards[h%shardCount]
}

// Allow spends one token for key, returning false when the bucket is empty.
func (l *Limiter) Allow(key string) bool {
	return l.AllowN(key, 1)
}

func (l *Limiter) AllowN(key string, n float64) bool {
	s := l.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	now := l.now()
	b := s.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.rule.Burst, last: now}
		s.buckets[key] = b
	}
	// refill based on elapsed time
	if l.rule.Every > 0 {
		refill := now.Sub(b.last).Seconds() / l.rule.Every.Seconds()
		b.tokens = min(l.rule.Burst, b.tokens+refill)
	}
	b.last = now
	if b.tokens >= n {
		b.tokens -= n
		return true
	}
	return false
}

// sweep drops buckets that have sat full and idle long enough to not matter.
func (l *Limiter) sweep(maxIdle time.Duration) {
	cutoff := l.now().Add(-maxIdle)
	for _, s := range l.shards {
		s.mu.Lock()
		for k, b := range s.buckets {
			if b.last.Before(cutoff) {
				delete(s.buckets, k)
			}
		}
		s.mu.Unlock()
	}
}
