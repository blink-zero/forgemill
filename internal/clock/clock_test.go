package clock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSleepReturnsNilAfterTheDuration(t *testing.T) {
	start := time.Now()
	if err := Sleep(context.Background(), 20*time.Millisecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if time.Since(start) < 20*time.Millisecond {
		t.Error("returned before the duration elapsed")
	}
}

func TestSleepReturnsCtxErrAsSoonAsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	start := time.Now()
	err := Sleep(ctx, 5*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Error("cancellation was not honoured promptly")
	}
}

func TestSleepWithAlreadyExpiredContextDoesNotWait(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	start := time.Now()
	if err := Sleep(ctx, 5*time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Error("should return immediately for an expired context")
	}
}
