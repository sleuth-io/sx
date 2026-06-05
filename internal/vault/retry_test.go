package vault

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fastRetryBackoff shrinks the backoff so multi-attempt tests don't sleep
// for seconds. Restores the production defaults on cleanup.
func fastRetryBackoff(t *testing.T) {
	t.Helper()
	prevInitial := httpRetryInitialBackoff
	prevMax := httpRetryMaxBackoff
	httpRetryInitialBackoff = time.Millisecond
	httpRetryMaxBackoff = 2 * time.Millisecond
	t.Cleanup(func() {
		httpRetryInitialBackoff = prevInitial
		httpRetryMaxBackoff = prevMax
	})
}

func TestDoHTTPWithRetry_RetriesTransientThenSucceeds(t *testing.T) {
	fastRetryBackoff(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		// First two calls return 502, then succeed. Mirrors the skills.new
		// failure mode in issue #124 where a single retry recovers.
		if n < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("upstream unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := doHTTPWithRetry(context.Background(), srv.Client(), req)
	if err != nil {
		t.Fatalf("doHTTPWithRetry: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("server call count = %d, want 3", got)
	}
}

func TestDoHTTPWithRetry_DoesNotRetryPermanentStatus(t *testing.T) {
	fastRetryBackoff(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := doHTTPWithRetry(context.Background(), srv.Client(), req)
	if err != nil {
		t.Fatalf("doHTTPWithRetry: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("server call count = %d, want 1 (no retries on 404)", got)
	}
}

func TestDoHTTPWithRetry_ReturnsFinalTransientResponse(t *testing.T) {
	fastRetryBackoff(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("still down"))
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := doHTTPWithRetry(context.Background(), srv.Client(), req)
	if err != nil {
		t.Fatalf("doHTTPWithRetry: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	// The body of the final response must remain readable so the caller
	// can surface a meaningful error message.
	buf := make([]byte, 16)
	n, _ := resp.Body.Read(buf)
	if string(buf[:n]) != "still down" {
		t.Fatalf("body = %q, want %q", string(buf[:n]), "still down")
	}
	if got := atomic.LoadInt32(&calls); got != int32(httpRetryMaxAttempts) {
		t.Fatalf("server call count = %d, want %d", got, httpRetryMaxAttempts)
	}
}

func TestDoHTTPWithRetry_ContextCancelled(t *testing.T) {
	fastRetryBackoff(t)
	// Use a longer backoff so the cancel races the sleep, not the first Do.
	httpRetryInitialBackoff = 200 * time.Millisecond

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after the first response, while the retry is sleeping.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := doHTTPWithRetry(ctx, srv.Client(), req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	// Should have called the server at most twice (one initial attempt,
	// maybe one retry that races the cancel). The key assertion is that
	// we didn't burn through all attempts.
	if got := atomic.LoadInt32(&calls); got >= int32(httpRetryMaxAttempts) {
		t.Fatalf("server call count = %d, expected < %d after cancel", got, httpRetryMaxAttempts)
	}
}
