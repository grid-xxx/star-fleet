package retry

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func stubSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	origSleep := sleepFn
	origLog := logFn
	t.Cleanup(func() {
		sleepFn = origSleep
		logFn = origLog
	})

	var delays []time.Duration
	sleepFn = func(d time.Duration) { delays = append(delays, d) }
	logFn = func(string, ...any) {}
	return &delays
}

func TestRetry_ImmediateSuccess(t *testing.T) {
	delays := stubSleep(t)

	calls := 0
	err := Retry(context.Background(), 3, 2*time.Second, func() error {
		calls++
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("fn called %d times, want 1", calls)
	}
	if len(*delays) != 0 {
		t.Errorf("expected no delays, got %v", *delays)
	}
}

func TestRetry_EventualSuccess(t *testing.T) {
	delays := stubSleep(t)

	calls := 0
	err := Retry(context.Background(), 3, 2*time.Second, func() error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("fn called %d times, want 3", calls)
	}
	if len(*delays) != 2 {
		t.Fatalf("expected 2 delays, got %d", len(*delays))
	}
	if (*delays)[0] != 2*time.Second {
		t.Errorf("first delay = %v, want 2s", (*delays)[0])
	}
	if (*delays)[1] != 4*time.Second {
		t.Errorf("second delay = %v, want 4s", (*delays)[1])
	}
}

func TestRetry_Exhaustion(t *testing.T) {
	stubSleep(t)

	sentinel := errors.New("persistent failure")
	calls := 0
	err := Retry(context.Background(), 3, time.Second, func() error {
		calls++
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if calls != 3 {
		t.Errorf("fn called %d times, want 3", calls)
	}
}

func TestRetry_ContextCancellation(t *testing.T) {
	origSleep := sleepFn
	origLog := logFn
	t.Cleanup(func() {
		sleepFn = origSleep
		logFn = origLog
	})
	logFn = func(string, ...any) {}

	ctx, cancel := context.WithCancel(context.Background())

	var calls int32
	sleepFn = func(time.Duration) {
		// Cancel context during the first sleep, simulating
		// cancellation while waiting between retries.
		cancel()
	}

	err := Retry(ctx, 5, time.Second, func() error {
		atomic.AddInt32(&calls, 1)
		return errors.New("fail")
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if c := atomic.LoadInt32(&calls); c > 2 {
		t.Errorf("fn called %d times, expected at most 2 after cancel", c)
	}
}

func TestRetry_ContextAlreadyCancelled(t *testing.T) {
	stubSleep(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := Retry(ctx, 3, time.Second, func() error {
		calls++
		return errors.New("fail")
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("fn called %d times, want 1 (should check ctx after first failure)", calls)
	}
}

func TestRetry_SingleAttempt(t *testing.T) {
	stubSleep(t)

	sentinel := errors.New("only once")
	err := Retry(context.Background(), 1, time.Second, func() error {
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
}

func TestRetry_ZeroAttemptsTreatedAsOne(t *testing.T) {
	stubSleep(t)

	calls := 0
	err := Retry(context.Background(), 0, time.Second, func() error {
		calls++
		return errors.New("fail")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("fn called %d times, want 1", calls)
	}
}

func TestDo_UsesDefaults(t *testing.T) {
	delays := stubSleep(t)

	calls := 0
	err := Do(context.Background(), func() error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("fn called %d times, want 3", calls)
	}
	if len(*delays) != 2 {
		t.Fatalf("expected 2 delays, got %d", len(*delays))
	}
	if (*delays)[0] != DefaultInitDelay {
		t.Errorf("first delay = %v, want %v", (*delays)[0], DefaultInitDelay)
	}
	if (*delays)[1] != DefaultInitDelay*2 {
		t.Errorf("second delay = %v, want %v", (*delays)[1], DefaultInitDelay*2)
	}
}

func TestRetry_LogMessages(t *testing.T) {
	origSleep := sleepFn
	origLog := logFn
	t.Cleanup(func() {
		sleepFn = origSleep
		logFn = origLog
	})
	sleepFn = func(time.Duration) {}

	var messages []string
	logFn = func(format string, args ...any) {
		messages = append(messages, fmt.Sprintf(format, args...))
	}

	calls := 0
	_ = Retry(context.Background(), 3, 2*time.Second, func() error {
		calls++
		if calls < 3 {
			return errors.New("oops")
		}
		return nil
	})

	if len(messages) != 2 {
		t.Fatalf("expected 2 log messages, got %d: %v", len(messages), messages)
	}
	if messages[0] != "Retrying in 2s... (attempt 2/3)" {
		t.Errorf("message[0] = %q", messages[0])
	}
	if messages[1] != "Retrying in 4s... (attempt 3/3)" {
		t.Errorf("message[1] = %q", messages[1])
	}
}
