package runtime

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"max_bot/internal/maxapi"
)

func TestWebhookSecretValidation(t *testing.T) {
	source := NewWebhookSource(WebhookConfig{
		Path:      "/webhook/max",
		Secret:    "secret",
		QueueSize: 8,
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var handled int32
	handler := func(context.Context, maxapi.Update) error {
		atomic.AddInt32(&handled, 1)
		return nil
	}

	httpHandler, cleanup := source.newHTTPHandler(ctx, handler)
	defer cleanup()
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	body := []byte(`{"update_type":"message_created"}`)

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/webhook/max", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request without secret failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodPost, server.URL+"/webhook/max", bytes.NewReader(body))
	req2.Header.Set(webhookSecretHeader, "secret")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("request with secret failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&handled) == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected handler to process update")
}

func TestWebhookRespondsFastWithQueuedWorker(t *testing.T) {
	source := NewWebhookSource(WebhookConfig{
		Path:      "/webhook/max",
		QueueSize: 8,
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var handled int32
	handler := func(context.Context, maxapi.Update) error {
		time.Sleep(200 * time.Millisecond)
		atomic.AddInt32(&handled, 1)
		return nil
	}

	httpHandler, cleanup := source.newHTTPHandler(ctx, handler)
	defer cleanup()
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	start := time.Now()
	resp, err := http.Post(server.URL+"/webhook/max", "application/json", bytes.NewBufferString(`{"update_type":"message_created"}`))
	if err != nil {
		t.Fatalf("post webhook failed: %v", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if elapsed > 120*time.Millisecond {
		t.Fatalf("expected fast webhook ack, got %v", elapsed)
	}

	deadline := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&handled) == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected queued handler execution")
}

func TestWebhookHealthAndReady(t *testing.T) {
	source := NewWebhookSource(WebhookConfig{
		Path:      "/webhook/max",
		QueueSize: 8,
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	httpHandler, cleanup := source.newHTTPHandler(ctx, func(context.Context, maxapi.Update) error { return nil })
	defer cleanup()
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s failed: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", path, resp.StatusCode)
		}
	}
}
