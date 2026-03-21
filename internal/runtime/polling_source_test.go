package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"max_bot/internal/maxapi"
)

func TestPollingSourcePollOnce(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/updates" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&requestCount, 1)
		_, _ = fmt.Fprint(w, `{"updates":[{"update_type":"message_created","timestamp":1}],"marker":1}`)
	}))
	defer server.Close()

	client := maxapi.NewClient(server.URL, "token")
	source := NewPollingSource(client, PollingConfig{
		TimeoutSeconds: 1,
		Limit:          10,
		PollOnce:       true,
		LogEmptyPolls:  true,
		UpdateTypes:    []string{"message_created"},
	}, nil)

	var handled int32
	handler := func(context.Context, maxapi.Update) error {
		atomic.AddInt32(&handled, 1)
		return nil
	}

	if err := source.Run(context.Background(), handler); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("expected one poll request, got %d", requestCount)
	}
	if atomic.LoadInt32(&handled) != 1 {
		t.Fatalf("expected one handled update, got %d", handled)
	}
}
