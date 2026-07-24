package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

var testConfig = Config{
	MaxAttempts: 4,
	BaseDelay:   1 * time.Millisecond,
	MaxDelay:    5 * time.Millisecond,
}

var errRetryable = errors.New("retryable")
var errPermanent = errors.New("permanent")

func alwaysRetryable(err error) bool {
	return errors.Is(err, errRetryable)
}

func TestDoSucceedsFirstTry(t *testing.T) {
	calls := 0
	err := Do(context.Background(), testConfig, alwaysRetryable, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoSucceedsAfterRetries(t *testing.T) {
	calls := 0
	err := Do(context.Background(), testConfig, alwaysRetryable, func() error {
		calls++
		if calls < 3 {
			return errRetryable
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestDoStopsImmediatelyOnNonRetryableError(t *testing.T) {
	calls := 0
	err := Do(context.Background(), testConfig, alwaysRetryable, func() error {
		calls++
		return errPermanent
	})
	if !errors.Is(err, errPermanent) {
		t.Fatalf("error = %v, want errPermanent", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (should not retry a non-retryable error)", calls)
	}
}

func TestDoExhaustsMaxAttempts(t *testing.T) {
	calls := 0
	err := Do(context.Background(), testConfig, alwaysRetryable, func() error {
		calls++
		return errRetryable
	})
	if !errors.Is(err, errRetryable) {
		t.Fatalf("error = %v, want errRetryable", err)
	}
	if calls != testConfig.MaxAttempts {
		t.Fatalf("calls = %d, want %d", calls, testConfig.MaxAttempts)
	}
}

func TestDoStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	cfg := Config{MaxAttempts: 10, BaseDelay: 50 * time.Millisecond, MaxDelay: 50 * time.Millisecond}

	err := Do(ctx, cfg, alwaysRetryable, func() error {
		calls++
		if calls == 1 {
			cancel()
		}
		return errRetryable
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (should stop waiting once context is cancelled)", calls)
	}
}

func TestDoCallsOnRetryBeforeEachRetryButNotAfterFinalAttempt(t *testing.T) {
	var attempts []int
	cfg := testConfig
	cfg.OnRetry = func(attempt int, delay time.Duration, err error) {
		attempts = append(attempts, attempt)
	}

	calls := 0
	_ = Do(context.Background(), cfg, alwaysRetryable, func() error {
		calls++
		return errRetryable
	})

	// MaxAttempts=4 means 3 retries (OnRetry calls before attempts 2, 3, 4).
	want := []int{1, 2, 3}
	if len(attempts) != len(want) {
		t.Fatalf("OnRetry called with attempts %v, want %v", attempts, want)
	}
	for i, a := range attempts {
		if a != want[i] {
			t.Fatalf("OnRetry called with attempts %v, want %v", attempts, want)
		}
	}
}
