package runtime

import (
	"testing"
	"time"

	"max_bot/internal/maxapi"
)

func TestDeduperDetectsDuplicateCallback(t *testing.T) {
	deduper := NewDeduper(2 * time.Second)
	update := maxapi.Update{
		UpdateType: "message_callback",
		Callback: &maxapi.Callback{
			CallbackID: "cb-1",
		},
	}

	if deduper.Seen(update) {
		t.Fatalf("first update must not be duplicate")
	}
	if !deduper.Seen(update) {
		t.Fatalf("second update must be duplicate")
	}
}

func TestDeduperExpiresKeysByTTL(t *testing.T) {
	deduper := NewDeduper(30 * time.Millisecond)
	update := maxapi.Update{
		UpdateType: "message_created",
		Message: &maxapi.Message{
			Sender: &maxapi.User{UserID: 10},
			Body:   maxapi.MessageBody{MID: "mid-1"},
		},
	}

	if deduper.Seen(update) {
		t.Fatalf("first update must not be duplicate")
	}
	time.Sleep(50 * time.Millisecond)
	if deduper.Seen(update) {
		t.Fatalf("update should expire after TTL")
	}
}
