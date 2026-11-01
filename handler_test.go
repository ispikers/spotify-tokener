package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCachedTokenCoalescesAndSurvivesCanceledCaller(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	s := &server{ctx: context.Background()}
	s.fetch = func(context.Context) ([]byte, error) {
		calls.Add(1)
		<-release
		return fmt.Appendf(nil, `{"accessToken":"t","accessTokenExpirationTimestampMs":%d}`, time.Now().Add(time.Hour).UnixMilli()), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := s.cachedToken(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.cachedToken(context.Background())
			errs <- err
		}()
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	start := time.Now()
	if _, err := s.cachedToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Fatalf("cached token took %s", took)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected 1 fetch, got %d", n)
	}
}
