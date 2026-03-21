package maxapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSendMessageRetriesOn5xx(t *testing.T) {
	var calls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			http.NotFound(w, r)
			return
		}
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.Header().Set("X-Request-ID", "req-retry")
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithOptions(server.URL, "token", ClientOptions{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Retry: RetryConfig{
			MaxRetries: 3,
			BaseDelay:  time.Millisecond,
			MaxDelay:   2 * time.Millisecond,
		},
	})

	err := client.SendMessage(context.Background(), 1001, NewMessageBody{Text: "hello"})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 calls, got %d", got)
	}
}

func TestSendMessageRetriesOn429(t *testing.T) {
	var calls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			http.NotFound(w, r)
			return
		}
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("X-Request-ID", "req-429")
			http.Error(w, "rate limit", http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithOptions(server.URL, "token", ClientOptions{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Retry: RetryConfig{
			MaxRetries: 2,
			BaseDelay:  time.Millisecond,
			MaxDelay:   2 * time.Millisecond,
		},
	})

	err := client.SendMessage(context.Background(), 1001, NewMessageBody{Text: "hello"})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 calls, got %d", got)
	}
}

func TestSendMessageDoesNotRetryOn400(t *testing.T) {
	var calls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&calls, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClientWithOptions(server.URL, "token", ClientOptions{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Retry: RetryConfig{
			MaxRetries: 5,
			BaseDelay:  time.Millisecond,
			MaxDelay:   2 * time.Millisecond,
		},
	})

	err := client.SendMessage(context.Background(), 1001, NewMessageBody{Text: "hello"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 status, got %d", apiErr.StatusCode)
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call, got %d", got)
	}
}
