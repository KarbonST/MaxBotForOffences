package reporting

import (
	"encoding/json"
	"testing"
)

func TestMaterializeAttachmentFallsBackToRawPayload(t *testing.T) {
	raw := json.RawMessage(`{"token":"abc123"}`)
	content, fileName, mimeType, ext := materializeAttachment(MediaAttachment{
		Type:    "photo",
		Payload: raw,
	}, 42, 1)

	if string(content) != `{"token":"abc123"}` {
		t.Fatalf("expected raw payload content, got %q", string(content))
	}
	if fileName != "42_01.jpg" {
		t.Fatalf("expected generated filename, got %q", fileName)
	}
	if mimeType != "" {
		t.Fatalf("expected empty mime from attachment, got %q", mimeType)
	}
	if ext != ".jpg" {
		t.Fatalf("expected .jpg extension, got %q", ext)
	}
}

func TestPickMIMEUsesFallbackByExtension(t *testing.T) {
	if got := pickMIME("", ".mp4"); got != "video/mp4" {
		t.Fatalf("expected video mime, got %q", got)
	}
	if got := pickMIME("", ".jpg"); got != "image/jpeg" {
		t.Fatalf("expected image mime, got %q", got)
	}
}
