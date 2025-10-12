package util

import (
	"errors"
	"fmt"
	"time"
)

var (
	tokenBucket, bucketStat chan bool

	// second limiter should be triggered only if
	// the tokenBucket gets drained immediately after
	// a refiller call for x consecutive times,
	// so there must be a sync between second limiter and refiller
	secondLimiterReady = make(chan bool, 1)

	// recycle old refiller goroutine after reloading
	refillerRC = make(chan bool)

	// return true if the request is to be denied
	RateLimit    = defaultLimit
	defaultLimit = func() bool { return false } // default == no limit

	ErrRateLimited = errors.New("request denied by rate limiter")
)

func setRateLimit() error {
	l := len(Config.TargetRateLimit)
	if l == 0 { // not configured == no limit
		recycleRefiller()
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

	tokenBucket = make(chan bool, Config.TargetRateLimit[0])
	for range Config.TargetRateLimit[0] {
		tokenBucket <- true
	}
	// 2-stage rate limiting:
	// bucketStat will count the times of tokenBucket being drained
	// or filled up, prepare for invoking the second limit rule
	hasSndLmt := l == 4
	if hasSndLmt {
		bucketStat = make(chan bool, Config.TargetRateLimit[2])
	}

	RateLimit = getRateLimiter(getSecondLimiter(hasSndLmt))

	recycleRefiller()
	go getRefiller(getSecondUnlimiter(hasSndLmt))()

	return nil
}

// degrade accessibility if bucket gets drained too frequently.
// because our minimum goal is to put CPU usage under control,
// we don't prioritize usability
func getSecondLimiter(hasSecondLimiter bool) func() {
	bucketCap := Config.TargetRateLimit[0]
	newRefill := Config.TargetRateLimit[3]

	if !hasSecondLimiter {
		return func() {
			LogDebug("rate limit triggered (%dr/s), remaining quota %dr/s",
				bucketCap, Config.TargetRateLimit[1])
		}
	}

	return func() {
		select {
		case secondLimiterReady <- true:
		default:
			return
		}

		LogDebug("first limiter triggered (%dr/s), remaining quota %dr/s",
			bucketCap, Config.TargetRateLimit[1])

		select {
		case bucketStat <- true:
		default:
			for range Config.TargetRateLimit[2] {
				select {
				case v := <-bucketStat:
					if !v {
						return
					}
				default:
					return
				}
			}
			Config.TargetRateLimit[1] = newRefill
			LogDebug("second limiter triggered, set request quota %dr/s -> %dr/s",
				Config.TargetRateLimit[1], newRefill)
		}
	}
}

// if second limiter is triggered, wait for a reversed condition to lift penalty
// (consecutively fill up the bucket for x times)
func getSecondUnlimiter(hasSecondLimiter bool) func() {
	if !hasSecondLimiter {
		return func() {}
	}

	originalRefill := Config.TargetRateLimit[1]

	return func() {
		select {
		case bucketStat <- false:
		default:
			for range Config.TargetRateLimit[2] {
				select {
				case v := <-bucketStat:
					if v {
						return
					}
				default:
					return
				}
			}
			currentRefill := Config.TargetRateLimit[1]
			if currentRefill == originalRefill {
				return
			}

			Config.TargetRateLimit[1] = originalRefill
			LogDebug("second limiter lifted, request quota revert %dr/s -> %dr/s",
				currentRefill, originalRefill)
		}
	}
}

func getRateLimiter(secondLimiter func()) func() bool {
	return func() bool {
		select {
		case <-tokenBucket:
			return false
		default:
			secondLimiter()
			return true
		}
	}
}

// try to cancel existing refiller
func recycleRefiller() {
	select {
	case refillerRC <- true:
	default:
	}
}

func getRefiller(unlimiter func()) func() {
	return func() {
		for {
			select {
			case <-refillerRC: // cancelled
				return
			default:
			}

			for range Config.TargetRateLimit[1] {
				select {
				case tokenBucket <- true:
				default:
					unlimiter()
					tokenBucket <- true
				}
			}

			select {
			case <-secondLimiterReady:
			default:
			}

			time.Sleep(1 * time.Second)
		}
	}
}
