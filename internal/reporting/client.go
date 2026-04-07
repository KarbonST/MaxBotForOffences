package reporting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPStatusError struct {
	StatusCode int
	Message    string
}

func (e *HTTPStatusError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("http status %d", e.StatusCode)
	}
	return fmt.Sprintf("http status %d: %s", e.StatusCode, e.Message)
}

type ClientOptions struct {
	HTTPClient *http.Client
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string, options ClientOptions) *Client {
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}

	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *Client) CreateReport(ctx context.Context, req CreateReportRequest) (*CreatedReport, error) {
	var result CreatedReport
	if err := c.doJSON(ctx, http.MethodPost, "/api/bot/reports", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ListReportsByMaxUserID(ctx context.Context, maxUserID int64) ([]ReportSummary, error) {
	var result struct {
		Items []ReportSummary `json:"items"`
	}
	path := fmt.Sprintf("/api/bot/reports/by-user/%d", maxUserID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *Client) GetReportByID(ctx context.Context, id int64) (*ReportDetail, error) {
	var result ReportDetail
	path := fmt.Sprintf("/api/bot/reports/%d", id)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GetConversation(ctx context.Context, maxUserID int64) (*ConversationState, error) {
	var result ConversationState
	path := fmt.Sprintf("/api/bot/conversations/%d", maxUserID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) SaveConversation(ctx context.Context, req SaveConversationRequest) (*ConversationState, error) {
	var result ConversationState
	path := fmt.Sprintf("/api/bot/conversations/%d", req.MaxUserID)
	if err := c.doJSON(ctx, http.MethodPut, path, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ListPendingNotifications(ctx context.Context, limit int) ([]NotificationItem, error) {
	var result struct {
		Items []NotificationItem `json:"items"`
	}
	path := fmt.Sprintf("/api/bot/notifications/pending?limit=%d", limit)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *Client) MarkNotificationSent(ctx context.Context, notificationID int64) error {
	path := fmt.Sprintf("/api/bot/notifications/%d/sent", notificationID)
	return c.doJSON(ctx, http.MethodPost, path, map[string]any{}, nil)
}

func (c *Client) MarkNotificationError(ctx context.Context, notificationID int64) error {
	path := fmt.Sprintf("/api/bot/notifications/%d/error", notificationID)
	return c.doJSON(ctx, http.MethodPost, path, map[string]any{}, nil)
}

func (c *Client) GetPendingClarification(ctx context.Context, maxUserID int64) (*ClarificationPrompt, error) {
	var result ClarificationPrompt
	path := fmt.Sprintf("/api/bot/clarifications/pending/%d", maxUserID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) AnswerClarification(ctx context.Context, req ClarificationAnswerRequest) error {
	path := fmt.Sprintf("/api/bot/clarifications/%d/answer", req.ClarificationID)
	return c.doJSON(ctx, http.MethodPost, path, req, nil)
}

func (c *Client) RejectClarification(ctx context.Context, req ClarificationRejectRequest) error {
	path := fmt.Sprintf("/api/bot/clarifications/%d/reject", req.ClarificationID)
	return c.doJSON(ctx, http.MethodPost, path, req, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, requestBody any, responseBody any) error {
	endpoint := strings.TrimRight(c.baseURL, "/") + path
	if !strings.Contains(path, "?") {
		var err error
		endpoint, err = url.JoinPath(c.baseURL, path)
		if err != nil {
			return fmt.Errorf("join url path: %w", err)
		}
	}

	var body io.Reader
	if requestBody != nil {
		raw, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return classifyHTTPError(resp.StatusCode, method, endpoint, strings.TrimSpace(string(raw)))
	}

	if responseBody == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func classifyHTTPError(statusCode int, method, endpoint, message string) error {
	base := &HTTPStatusError{
		StatusCode: statusCode,
		Message:    message,
	}

	switch statusCode {
	case http.StatusBadRequest:
		return fmt.Errorf("%w: request %s %s failed with %d: %s", ErrInvalidRequest, method, endpoint, statusCode, base.Message)
	case http.StatusNotFound:
		return fmt.Errorf("%w: request %s %s failed with %d: %s", ErrNotFound, method, endpoint, statusCode, base.Message)
	default:
		return fmt.Errorf("request %s %s failed with %d: %s", method, endpoint, statusCode, base.Message)
	}
}
