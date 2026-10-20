package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestDispatchStartsBeyondLegacySlotCeilingsAndJoinsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const count = 128
	started := make(chan struct{}, count)
	var claimed, finished atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- dispatch(ctx, time.Hour, func(context.Context) (func() error, error) {
			if claimed.Add(1) > count {
				return nil, nil
			}
			return func() error { started <- struct{}{}; <-ctx.Done(); finished.Add(1); return ctx.Err() }, nil
		})
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for i := 0; i < count; i++ {
		select {
		case <-started:
		case <-deadline.C:
			t.Fatal("dispatcher retained a task-count ceiling", i)
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || finished.Load() != count {
			t.Fatal("shutdown did not join every execution", err, finished.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dispatcher did not stop promptly")
	}
}

func TestEmptyQueueAndClaimFailureBackOffWithoutSpawningWork(t *testing.T) {
	for _, fault := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		var calls atomic.Int32
		called := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			done <- dispatch(ctx, time.Hour, func(context.Context) (func() error, error) {
				calls.Add(1)
				called <- struct{}{}
				if fault {
					return nil, errors.New("fixture claim failure")
				}
				return nil, nil
			})
		}()
		select {
		case <-called:
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("claim not attempted")
		}
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("backoff did not respect cancellation")
		}
		if calls.Load() != 1 {
			t.Fatal("empty/failing queue spun instead of backing off", calls.Load())
		}
	}
}
