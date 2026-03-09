package retry

import (
	"context"
	"fmt"
	"os"
	"time"
)

const (
	DefaultMaxAttempts = 3
	DefaultInitDelay   = 2 * time.Second
)

// sleepFn can be overridden in tests to avoid real delays.
var sleepFn = time.Sleep

// logFn can be overridden in tests to capture or suppress output.
var logFn = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// Retry runs fn up to maxAttempts times with exponential backoff.
// It retries on any error returned by fn. Use context cancellation
// to abort early.
func Retry(ctx context.Context, maxAttempts int, initialDelay time.Duration, fn func() error) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var err error
	delay := initialDelay

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}

		if attempt == maxAttempts {
			break
		}

		logFn("Retrying in %s... (attempt %d/%d)", delay, attempt+1, maxAttempts)

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		sleepFn(delay)

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		delay *= 2
	}

	return err
}

// Do is a convenience wrapper using default settings (3 attempts, 2s initial delay).
func Do(ctx context.Context, fn func() error) error {
	return Retry(ctx, DefaultMaxAttempts, DefaultInitDelay, fn)
}
