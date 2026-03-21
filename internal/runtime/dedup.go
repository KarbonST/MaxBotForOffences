package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"max_bot/internal/maxapi"
)

type Deduper struct {
	ttl  time.Duration
	mu   sync.Mutex
	seen map[string]time.Time
}

func NewDeduper(ttl time.Duration) *Deduper {
	return &Deduper{
		ttl:  ttl,
		seen: make(map[string]time.Time),
	}
}

func (d *Deduper) Seen(update maxapi.Update) bool {
	key := updateKey(update)
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	for k, expiresAt := range d.seen {
		if now.After(expiresAt) {
			delete(d.seen, k)
		}
	}

	if expiresAt, ok := d.seen[key]; ok && now.Before(expiresAt) {
		return true
	}

	d.seen[key] = now.Add(d.ttl)
	return false
}

func updateKey(update maxapi.Update) string {
	if update.Callback != nil && update.Callback.CallbackID != "" {
		return "cb:" + update.Callback.CallbackID
	}

	if update.Message != nil && update.Message.Body.MID != "" {
		userID := int64(0)
		if update.Message.Sender != nil {
			userID = update.Message.Sender.UserID
		}
		return fmt.Sprintf("mid:%d:%s", userID, update.Message.Body.MID)
	}

	signature := struct {
		Type      string `json:"type"`
		Timestamp int64  `json:"timestamp"`
		ChatID    int64  `json:"chat_id,omitempty"`
		Payload   string `json:"payload,omitempty"`
		UserID    int64  `json:"user_id,omitempty"`
	}{
		Type:      update.UpdateType,
		Timestamp: update.Timestamp,
		ChatID:    update.ChatID,
		Payload:   update.Payload,
	}
	if update.User != nil {
		signature.UserID = update.User.UserID
	}

	raw, _ := json.Marshal(signature)
	hash := sha256.Sum256(raw)
	return "sig:" + hex.EncodeToString(hash[:16])
}
