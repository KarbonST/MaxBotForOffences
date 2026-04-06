package reporting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"strings"
	"time"
)

const defaultMaxAPIBase = "https://platform-api.max.ru"

type mediaFetcher struct {
	httpClient *http.Client
	apiBaseURL string
	botToken   string
}

type mediaPayload struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type videoAttachmentDetails struct {
	Token string     `json:"token"`
	URLs  *videoURLs `json:"urls"`
}

type videoURLs struct {
	MP41080 string `json:"mp4_1080"`
	MP4720  string `json:"mp4_720"`
	MP4480  string `json:"mp4_480"`
	MP4360  string `json:"mp4_360"`
	MP4240  string `json:"mp4_240"`
	MP4144  string `json:"mp4_144"`
	HLS     string `json:"hls"`
}

func newMediaFetcherFromEnv() *mediaFetcher {
	apiBaseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("MAX_API_BASE")), "/")
	if apiBaseURL == "" {
		apiBaseURL = defaultMaxAPIBase
	}

	return &mediaFetcher{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiBaseURL: apiBaseURL,
		botToken:   strings.TrimSpace(os.Getenv("MAX_BOT_TOKEN")),
	}
}

func (f *mediaFetcher) fetch(ctx context.Context, item MediaAttachment) ([]byte, string, string, error) {
	downloadURL, err := f.resolveDownloadURL(ctx, item)
	if err != nil {
		return nil, "", "", err
	}
	if strings.TrimSpace(downloadURL) == "" {
		return nil, "", "", fmt.Errorf("media download URL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("create media request: %w", err)
	}
	if f.botToken != "" {
		req.Header.Set("Authorization", f.botToken)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("download media %q: %w", downloadURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("download media %q returned %d", downloadURL, resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", fmt.Errorf("read media body: %w", err)
	}

	mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if parsed, _, err := mime.ParseMediaType(mimeType); err == nil {
		mimeType = parsed
	}

	return content, mimeType, mediaExtFromURLOrMIME(downloadURL, mimeType), nil
}

func (f *mediaFetcher) resolveDownloadURL(ctx context.Context, item MediaAttachment) (string, error) {
	payload, err := parseMediaPayload(item.Payload)
	if err != nil {
		return "", err
	}

	if strings.EqualFold(strings.TrimSpace(item.Type), "video") && payload.Token != "" {
		videoURL, err := f.resolveVideoURL(ctx, payload.Token)
		if err == nil && strings.TrimSpace(videoURL) != "" {
			return videoURL, nil
		}
	}

	return payload.URL, nil
}

func parseMediaPayload(raw json.RawMessage) (mediaPayload, error) {
	var payload mediaPayload
	if len(raw) == 0 {
		return payload, nil
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, fmt.Errorf("decode media payload: %w", err)
	}
	payload.URL = strings.TrimSpace(payload.URL)
	payload.Token = strings.TrimSpace(payload.Token)
	return payload, nil
}

func (f *mediaFetcher) resolveVideoURL(ctx context.Context, videoToken string) (string, error) {
	if f == nil || strings.TrimSpace(f.apiBaseURL) == "" || strings.TrimSpace(f.botToken) == "" {
		return "", fmt.Errorf("max api config is incomplete")
	}

	endpoint := strings.TrimRight(f.apiBaseURL, "/") + "/videos/" + neturl.PathEscape(videoToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create video details request: %w", err)
	}
	req.Header.Set("Authorization", f.botToken)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request video details: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("video details returned %d", resp.StatusCode)
	}

	var details videoAttachmentDetails
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return "", fmt.Errorf("decode video details: %w", err)
	}

	if details.URLs == nil {
		return "", fmt.Errorf("video details do not contain urls")
	}

	for _, candidate := range []string{
		details.URLs.MP41080,
		details.URLs.MP4720,
		details.URLs.MP4480,
		details.URLs.MP4360,
		details.URLs.MP4240,
		details.URLs.MP4144,
	} {
		if strings.TrimSpace(candidate) != "" {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("video details do not contain downloadable mp4 urls")
}

func mediaExtFromURLOrMIME(rawURL, mimeType string) string {
	if parsed, err := neturl.Parse(strings.TrimSpace(rawURL)); err == nil {
		if ext := strings.ToLower(path.Ext(parsed.Path)); ext != "" {
			return ext
		}
	}

	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "video/mp4":
		return ".mp4"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}
