package service

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type trongridCooldown struct {
	until    time.Time
	failures int
}

// All TRC20 callers, including manual checks and pagination, share pacing.
// Cooldowns are keyed by credential so rotating a blocked key can recover.
var trongridRequests = struct {
	sync.Mutex
	next      time.Time
	cooldowns map[string]trongridCooldown
}{cooldowns: make(map[string]trongridCooldown)}

func waitForTrongrid(ctx context.Context, apiKey string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		trongridRequests.Lock()
		now := time.Now()
		cooldown := trongridRequests.cooldowns[apiKey]
		if now.Before(cooldown.until) {
			trongridRequests.Unlock()
			return fmt.Errorf("TronGrid queries paused for %s after provider failure", cooldown.until.Sub(now).Round(time.Second))
		}
		delay := trongridRequests.next.Sub(now)
		if delay <= 0 {
			trongridRequests.next = now.Add(time.Second)
			trongridRequests.Unlock()
			return nil
		}
		trongridRequests.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func recordTrongridResult(apiKey string, status int, retryAfter string, failed bool) {
	trongridRequests.Lock()
	defer trongridRequests.Unlock()
	now := time.Now()
	cooldown := trongridRequests.cooldowns[apiKey]
	if !failed {
		// An earlier in-flight success must not clear a newer failure's cooldown.
		if !now.Before(cooldown.until) {
			delete(trongridRequests.cooldowns, apiKey)
		}
		return
	}
	cooldown.failures = min(cooldown.failures+1, 9)
	delay := 5 * time.Second
	if status == http.StatusTooManyRequests || status == http.StatusForbidden {
		delay = time.Minute
	}
	delay = min(delay*time.Duration(1<<(cooldown.failures-1)), 15*time.Minute)
	delay += time.Duration(rand.Int64N(int64(delay / 5)))
	// Respect the provider's seconds or HTTP-date, bounded against bad headers.
	if seconds, err := strconv.ParseInt(retryAfter, 10, 64); err == nil && seconds > 0 {
		delay = max(delay, time.Duration(min(seconds, 86400))*time.Second)
	} else if deadline, err := http.ParseTime(retryAfter); err == nil {
		delay = max(delay, min(deadline.Sub(now), 24*time.Hour))
	}
	if deadline := now.Add(delay); deadline.After(cooldown.until) {
		cooldown.until = deadline
	}
	trongridRequests.cooldowns[apiKey] = cooldown
}
