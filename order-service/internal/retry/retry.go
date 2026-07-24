package retry

import (
	"context"
	"math/rand/v2"
	"time"
)

type Config struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration

	// OnRetry, if set, is called after a retryable failure and before
	// sleeping ahead of the next attempt. It is not called after the final
	// attempt, since there's nothing left to retry.
	OnRetry func(attempt int, delay time.Duration, err error)
}

// Do calls fn, retrying with exponential backoff (equal jitter) while
// isRetryable(err) is true, up to cfg.MaxAttempts total calls. It returns
// early if ctx is cancelled while waiting between attempts.
func Do(ctx context.Context, cfg Config, isRetryable func(error) bool, fn func() error) error {
	var err error

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !isRetryable(err) || attempt == cfg.MaxAttempts-1 {
			return err
		}

		delay := backoffDelay(cfg, attempt)
		if cfg.OnRetry != nil {
			cfg.OnRetry(attempt+1, delay, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return err
}

// backoffDelay computes base*2^attempt capped at MaxDelay, then applies equal
// jitter: half the cap, plus a uniform random value between 0 and the other
// half. That guarantees each wait is at least half of the computed cap --
// unlike full jitter, an unlucky run of low draws can't collapse the whole
// backoff schedule down to near-zero, which matters here since the retry
// budget is sized to plausibly bridge a real dependency restart, not just
// desynchronize retrying clients.
func backoffDelay(cfg Config, attempt int) time.Duration {
	d := cfg.BaseDelay * time.Duration(int64(1)<<uint(attempt))
	if d <= 0 || d > cfg.MaxDelay {
		d = cfg.MaxDelay
	}
	if d <= 0 {
		return 0
	}
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(d-half)+1))
}
