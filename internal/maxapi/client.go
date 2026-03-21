package maxapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

type ClientOptions struct {
	HTTPClient *http.Client
	Logger     *slog.Logger
	Retry      RetryConfig
}

type APIError struct {
	Method     string
	Path       string
	StatusCode int
	RequestID  string
	Body       string
}

func (e *APIError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("api %s %s returned %d request_id=%s body=%s", e.Method, e.Path, e.StatusCode, e.RequestID, e.Body)
	}
	return fmt.Sprintf("api %s %s returned %d body=%s", e.Method, e.Path, e.StatusCode, e.Body)
}

func (e *APIError) Retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= http.StatusInternalServerError
}

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	logger     *slog.Logger
	retry      RetryConfig
}

func NewClient(baseURL, token string) *Client {
	return NewClientWithOptions(baseURL, token, ClientOptions{})
}

func NewClientWithOptions(baseURL, token string, options ClientOptions) *Client {
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 35 * time.Second,
		}
	}

	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	retry := options.Retry
	if retry.MaxRetries < 0 {
		retry.MaxRetries = 0
	}
	if retry.BaseDelay <= 0 {
		retry.BaseDelay = 250 * time.Millisecond
	}
	if retry.MaxDelay < retry.BaseDelay {
		retry.MaxDelay = 3 * time.Second
	}

	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: httpClient,
		logger:     logger,
		retry:      retry,
	}
}

func (c *Client) GetMe(ctx context.Context) (*BotInfo, error) {
	var result BotInfo
	if err := c.doJSON(ctx, http.MethodGet, "/me", nil, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GetUpdates(ctx context.Context, marker *int64, timeoutSec, limit int, types []string) (*UpdateList, error) {
	query := url.Values{}
	query.Set("timeout", strconv.Itoa(timeoutSec))
	query.Set("limit", strconv.Itoa(limit))
	if marker != nil {
		query.Set("marker", strconv.FormatInt(*marker, 10))
	}
	if len(types) > 0 {
		query.Set("types", strings.Join(types, ","))
	}

	var result UpdateList
	if err := c.doJSON(ctx, http.MethodGet, "/updates", query, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) SendMessage(ctx context.Context, userID int64, body NewMessageBody) error {
	query := url.Values{}
	query.Set("user_id", strconv.FormatInt(userID, 10))

	return c.doJSON(ctx, http.MethodPost, "/messages", query, body, nil)
}

func (c *Client) AnswerCallback(ctx context.Context, callbackID, notification string) error {
	query := url.Values{}
	query.Set("callback_id", callbackID)

	payload := CallbackAnswer{
		Notification: notification,
	}

	return c.doJSON(ctx, http.MethodPost, "/answers", query, payload, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, requestBody any, responseBody any) error {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var requestRaw []byte
	if requestBody != nil {
		raw, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		requestRaw = raw
	}

	maxAttempts := c.retry.MaxRetries + 1
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		rawResponse, requestID, err := c.doOnce(ctx, method, endpoint, path, requestRaw)
		if err == nil {
			if responseBody == nil || len(rawResponse) == 0 {
				return nil
			}
			if err := json.Unmarshal(rawResponse, responseBody); err != nil {
				return fmt.Errorf("decode response: %w; body=%s", err, strings.TrimSpace(string(rawResponse)))
			}
			return nil
		}

		if !c.shouldRetry(err) || attempt == maxAttempts {
			return err
		}

		delay := c.backoffWithJitter(attempt)
		c.logger.Warn(
			"max api request retry",
			"method", method,
			"path", path,
			"status", statusFromError(err),
			"request_id", coalesce(requestID, requestIDFromError(err)),
			"attempt", attempt,
			"max_retries", c.retry.MaxRetries,
			"delay_ms", delay.Milliseconds(),
			"error", err.Error(),
		)
		if err := sleepWithContext(ctx, delay); err != nil {
			return err
		}
	}

	return errors.New("unreachable retry loop")
}

func (c *Client) doOnce(ctx context.Context, method, endpoint, path string, requestRaw []byte) ([]byte, string, error) {
	var body io.Reader
	if requestRaw != nil {
		body = bytes.NewReader(requestRaw)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", c.token)
	if requestRaw != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request %s %s: %w", method, endpoint, err)
	}
	defer resp.Body.Close()

	requestID := requestIDFromHeaders(resp.Header)
	rawResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, requestID, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, requestID, &APIError{
			Method:     method,
			Path:       path,
			StatusCode: resp.StatusCode,
			RequestID:  requestID,
			Body:       compactBody(rawResp),
		}
	}

	return rawResp, requestID, nil
}

func (c *Client) shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}

	return true
}

func (c *Client) backoffWithJitter(attempt int) time.Duration {
	delay := c.retry.BaseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= c.retry.MaxDelay {
			delay = c.retry.MaxDelay
			break
		}
	}

	if delay > c.retry.MaxDelay {
		delay = c.retry.MaxDelay
	}

	// Adds up to 25% jitter to reduce synchronized retries.
	jitterMax := delay / 4
	if jitterMax <= 0 {
		return delay
	}

	jitter := time.Duration(rand.Int63n(int64(jitterMax)))
	return delay + jitter
}

func requestIDFromHeaders(headers http.Header) string {
	keys := []string{
		"X-Request-ID",
		"X-Request-Id",
		"X-Correlation-ID",
		"X-Correlation-Id",
	}
	for _, key := range keys {
		if value := headers.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func requestIDFromError(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.RequestID
	}
	return ""
}

func statusFromError(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

func compactBody(raw []byte) string {
	const maxSize = 1024
	body := strings.TrimSpace(string(raw))
	if len(body) <= maxSize {
		return body
	}
	return body[:maxSize] + "...(truncated)"
}

func coalesce(first, second string) string {
	if first != "" {
		return first
	}
	return second
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
