package reporting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaterializeAttachmentFallsBackToRawPayload(t *testing.T) {
	raw := json.RawMessage(`{"token":"abc123"}`)
	store := &PostgresStore{}
	content, fileName, mimeType, ext, err := store.materializeAttachment(context.Background(), MediaAttachment{
		Type:    "photo",
		Payload: raw,
	}, 42, 1)
	if err != nil {
		t.Fatalf("materializeAttachment() error = %v", err)
	}

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

func TestMaterializeAttachmentDownloadsPhotoFromPayloadURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/media/photo.jpg" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-binary"))
	}))
	defer server.Close()

	raw := json.RawMessage(`{"url":"` + server.URL + `/media/photo.jpg","token":"photo_token_1"}`)
	store := &PostgresStore{
		mediaFetcher: &mediaFetcher{
			httpClient: server.Client(),
			botToken:   "bot-token",
		},
	}

	content, fileName, mimeType, ext, err := store.materializeAttachment(context.Background(), MediaAttachment{
		Type:    "photo",
		Payload: raw,
	}, 42, 1)
	if err != nil {
		t.Fatalf("materializeAttachment() error = %v", err)
	}

	if string(content) != "jpeg-binary" {
		t.Fatalf("expected downloaded jpeg content, got %q", string(content))
	}
	if fileName != "42_01.jpg" {
		t.Fatalf("expected generated filename, got %q", fileName)
	}
	if mimeType != "image/jpeg" {
		t.Fatalf("expected image/jpeg mime, got %q", mimeType)
	}
	if ext != ".jpg" {
		t.Fatalf("expected .jpg extension, got %q", ext)
	}
}

func TestMaterializeAttachmentDownloadsVideoViaDetailsEndpoint(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/videos/video_token_1":
			if got := r.Header.Get("Authorization"); got != "bot-token" {
				t.Fatalf("expected Authorization header for video details, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"video_token_1","urls":{"mp4_720":"` + server.URL + `/media/video.mp4"}}`))
		case "/media/video.mp4":
			if got := r.Header.Get("Authorization"); got != "bot-token" {
				t.Fatalf("expected Authorization header for media download, got %q", got)
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("mp4-binary"))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	raw := json.RawMessage(`{"url":"https://example.invalid/original","token":"video_token_1"}`)
	store := &PostgresStore{
		mediaFetcher: &mediaFetcher{
			httpClient: server.Client(),
			apiBaseURL: server.URL,
			botToken:   "bot-token",
		},
	}

	content, fileName, mimeType, ext, err := store.materializeAttachment(context.Background(), MediaAttachment{
		Type:    "video",
		Payload: raw,
	}, 42, 1)
	if err != nil {
		t.Fatalf("materializeAttachment() error = %v", err)
	}

	if string(content) != "mp4-binary" {
		t.Fatalf("expected downloaded video content, got %q", string(content))
	}
	if fileName != "42_01.mp4" {
		t.Fatalf("expected generated filename, got %q", fileName)
	}
	if mimeType != "video/mp4" {
		t.Fatalf("expected video/mp4 mime, got %q", mimeType)
	}
	if ext != ".mp4" {
		t.Fatalf("expected .mp4 extension, got %q", ext)
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

func TestBuildMediaFileNameAppendsExtensionWhenMissing(t *testing.T) {
	if got := buildMediaFileName("photo", 42, 1, ".jpg"); got != "photo.jpg" {
		t.Fatalf("expected appended extension, got %q", got)
	}
	if got := buildMediaFileName("../video.mp4", 42, 1, ".mp4"); got != "video.mp4" {
		t.Fatalf("expected sanitized base name, got %q", got)
	}
}

func TestMediaExtFromURLOrMIMEUsesMimeFallback(t *testing.T) {
	if got := mediaExtFromURLOrMIME("https://example.invalid/media", "video/mp4"); got != ".mp4" {
		t.Fatalf("expected .mp4 ext, got %q", got)
	}
	if got := mediaExtFromURLOrMIME("https://example.invalid/media", "image/jpeg"); got != ".jpg" {
		t.Fatalf("expected .jpg ext, got %q", got)
	}
	if got := mediaExtFromURLOrMIME("https://example.invalid/media/file.jpeg?download=1", ""); !strings.EqualFold(got, ".jpeg") {
		t.Fatalf("expected .jpeg ext from URL, got %q", got)
	}
}
