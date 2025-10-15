package util

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// 2-stage rate limiting, token bucket + adaptive refill rate
type tokenBucket struct {
	tokens        int
	capacity      int
	refillRate    int
	counterSwitch bool // false == dump counter, true == fillup counter
	dump          int  // consecutive times of draining
	fillup        int  // consecutive times of filling up
	mu            sync.Mutex
	lastRefill    time.Time
}

var (
	bucket tokenBucket
	// return true if the request is to be denied
	RateLimit    = defaultLimit
	defaultLimit = func() bool { return false } // default == no limit

	ErrRateLimited = errors.New("request denied by rate limiter")
)

func (tb *tokenBucket) rateLimit() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	if d := time.Since(tb.lastRefill); d > 1*time.Second {
		tb.refill(int(d.Seconds()))
	}

	if tb.tokens <= 0 {
		return true
	}
	return !tb.takeToken()
}

func (tb *tokenBucket) refill(seconds int) {
	tb.tokens += tb.refillRate * seconds
	tb.lastRefill = time.Now()
	if tb.tokens < tb.capacity {
		tb.fillup = 0
		return
	}
	// fillup
	tb.tokens = tb.capacity
	tb.dump = 0 // dump count becomes non-consecutive
	if !tb.counterSwitch || len(Config.TargetRateLimit) != 4 {
		return
	}
	// second limiter triggered,
	// wait for a reversed condition (consecutively
	// fill up the bucket for x times) to lift throttling
	tb.fillup++
	if tb.fillup < Config.TargetRateLimit[2] {
		return
	}
	tb.fillup = 0 // reset and switch to dump counter
	tb.counterSwitch = false
	LogDebug("second limiter lifted, restore request quota %dr/s -> %dr/s",
		tb.refillRate, Config.TargetRateLimit[1])
	tb.refillRate = Config.TargetRateLimit[1]
}

func (tb *tokenBucket) takeToken() bool {
	tb.tokens--
	if tb.tokens > 0 {
		return true
	}
	// drained
	LogDebug("rate limit triggered (%dr/s), remaining quota %dr/s",
		tb.capacity, tb.refillRate)
	tb.fillup = 0 // fillup count becomes non-consecutive
	if tb.counterSwitch || len(Config.TargetRateLimit) != 4 {
		return false
	}
	// trigger second limiter if
	// the bucket gets drained immediately after
	// a non-filled-up refill() call for x consecutive times
	tb.dump++
	if tb.dump < Config.TargetRateLimit[2] {
		return false
	}
	tb.dump = 0 // reset and switch to fillup counter
	tb.counterSwitch = true
	LogDebug("second limiter triggered, set request quota %dr/s -> %dr/s",
		tb.refillRate, Config.TargetRateLimit[3])
	tb.refillRate = Config.TargetRateLimit[3]
	return false
}

func setRateLimit() error {
	l := len(Config.TargetRateLimit)
	// not configured == no limit
	if l == 0 {
		RateLimit = defaultLimit
		return nil
	}

	if l != 2 && l != 4 {
		return errors.New("target_rate_limit must have either 2 or 4 values")
	}
	// ↓ will this cause a problem and should be checked?
	// refill rate > bucket cap || reduced refill rate > refill rate
	for i, v := range Config.TargetRateLimit {
		if v <= 0 {
			return fmt.Errorf("invalid value %d for target_rate_limit[%d]", v, i)
		}
	}

	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	bucket.capacity = Config.TargetRateLimit[0]
	bucket.tokens = bucket.capacity
	bucket.refillRate = Config.TargetRateLimit[1]

	RateLimit = bucket.rateLimit

	return nil
}
