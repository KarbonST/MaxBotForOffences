package reporting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	debug      bool
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
		debug:      envBoolDefaultFalse("REPORT_MEDIA_DEBUG"),
	}
}

func (f *mediaFetcher) fetch(ctx context.Context, item MediaAttachment) ([]byte, string, string, error) {
	downloadURL, err := f.resolveDownloadURL(ctx, item)
	if err != nil {
		f.debugLog("не удалось определить URL скачивания вложения", "type", item.Type, "error", err.Error())
		return nil, "", "", err
	}
	if strings.TrimSpace(downloadURL) == "" {
		f.debugLog("получен пустой URL скачивания вложения", "type", item.Type)
		return nil, "", "", fmt.Errorf("media download URL is empty")
	}

	candidates := preferredMediaDownloadURLs(downloadURL)
	if len(candidates) > 1 {
		f.debugLog("для скачивания вложения подготовлены альтернативные URL", "type", item.Type, "primary_url", summarizeURL(candidates[0]), "fallback_url", summarizeURL(candidates[1]))
	}

	var lastErr error
	for index, candidateURL := range candidates {
		if index == 0 {
			f.debugLog("начинаем скачивание вложения", "type", item.Type, "download_url", summarizeURL(candidateURL), "attempt", index+1, "attempts_total", len(candidates))
		} else {
			f.debugLog("повторная попытка скачивания вложения по резервному URL", "type", item.Type, "download_url", summarizeURL(candidateURL), "attempt", index+1, "attempts_total", len(candidates))
		}

		content, mimeType, err := f.downloadOnce(ctx, candidateURL, item.Type)
		if err != nil {
			lastErr = err
			f.debugLog("не удалось скачать вложение по URL-кандидату", "type", item.Type, "download_url", summarizeURL(candidateURL), "attempt", index+1, "error", err.Error())
			continue
		}

		f.debugLog("вложение скачано", "type", item.Type, "download_url", summarizeURL(candidateURL), "bytes", len(content), "mime_type", mimeType, "attempt", index+1)
		return content, mimeType, mediaExtFromURLOrMIME(candidateURL, mimeType), nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("media download URL is empty")
	}
	return nil, "", "", lastErr
}

func (f *mediaFetcher) downloadOnce(ctx context.Context, downloadURL string, mediaType string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create media request: %w", err)
	}
	if f.botToken != "" {
		req.Header.Set("Authorization", f.botToken)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		f.debugLog("ошибка запроса на скачивание вложения", "type", mediaType, "download_url", summarizeURL(downloadURL), "error", err.Error())
		return nil, "", fmt.Errorf("download media %q: %w", downloadURL, err)
	}
	defer resp.Body.Close()
	f.debugLog("получен ответ на скачивание вложения", "type", mediaType, "download_url", summarizeURL(downloadURL), "status_code", resp.StatusCode)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download media %q returned %d", downloadURL, resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read media body: %w", err)
	}

	mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if parsed, _, err := mime.ParseMediaType(mimeType); err == nil {
		mimeType = parsed
	}

	return content, mimeType, nil
}

func (f *mediaFetcher) resolveDownloadURL(ctx context.Context, item MediaAttachment) (string, error) {
	payload, err := parseMediaPayload(item.Payload)
	if err != nil {
		f.debugLog("не удалось разобрать payload вложения", "type", item.Type, "error", err.Error())
		return "", err
	}
	f.debugLog("разобран payload вложения", "type", item.Type, "payload_url", summarizeURL(payload.URL), "token", shortenToken(payload.Token))

	if strings.EqualFold(strings.TrimSpace(item.Type), "video") && payload.Token != "" {
		f.debugLog("пытаемся получить прямой mp4 URL для видео", "token", shortenToken(payload.Token))
		videoURL, err := f.resolveVideoURL(ctx, payload.Token)
		if err == nil && strings.TrimSpace(videoURL) != "" {
			f.debugLog("получен прямой mp4 URL для видео", "token", shortenToken(payload.Token), "video_url", summarizeURL(videoURL))
			return videoURL, nil
		}
		if err != nil {
			f.debugLog("не удалось получить прямой mp4 URL для видео, используем payload URL", "token", shortenToken(payload.Token), "error", err.Error())
		}
	}

	f.debugLog("используем payload URL для скачивания вложения", "type", item.Type, "payload_url", summarizeURL(payload.URL))
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
	f.debugLog("запрашиваем детали видео у MAX API", "token", shortenToken(videoToken), "endpoint", summarizeURL(endpoint))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create video details request: %w", err)
	}
	req.Header.Set("Authorization", f.botToken)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		f.debugLog("ошибка запроса деталей видео", "token", shortenToken(videoToken), "endpoint", summarizeURL(endpoint), "error", err.Error())
		return "", fmt.Errorf("request video details: %w", err)
	}
	defer resp.Body.Close()
	f.debugLog("получен ответ от MAX API по деталям видео", "token", shortenToken(videoToken), "endpoint", summarizeURL(endpoint), "status_code", resp.StatusCode)

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
			f.debugLog("выбран URL видео для скачивания", "token", shortenToken(videoToken), "video_url", summarizeURL(candidate))
			return candidate, nil
		}
	}

	return "", fmt.Errorf("video details do not contain downloadable mp4 urls")
}

func (f *mediaFetcher) debugLog(msg string, args ...any) {
	if f == nil || !f.debug {
		return
	}
	slog.Info(msg, args...)
}

func preferredMediaDownloadURLs(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if secure := upgradeToHTTPSURL(trimmed); secure != "" && secure != trimmed {
		return []string{secure, trimmed}
	}
	return []string{trimmed}
}

func upgradeToHTTPSURL(raw string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if !strings.EqualFold(parsed.Scheme, "http") {
		return ""
	}
	parsed.Scheme = "https"
	return parsed.String()
}

func summarizeURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := neturl.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		if len(trimmed) > 160 {
			return trimmed[:160] + "..."
		}
		return trimmed
	}
	result := parsed.Scheme + "://" + parsed.Host + parsed.Path
	if result == "://" {
		return trimmed
	}
	return result
}

func shortenToken(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 10 {
		return trimmed
	}
	return trimmed[:4] + "..." + trimmed[len(trimmed)-4:]
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
