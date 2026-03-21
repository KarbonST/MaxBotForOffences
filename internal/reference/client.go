package reference

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

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

func (c *Client) Categories(ctx context.Context) ([]Item, error) {
	return c.fetchList(ctx, "/api/reference/categories")
}

func (c *Client) Municipalities(ctx context.Context) ([]Item, error) {
	return c.fetchList(ctx, "/api/reference/municipalities")
}

func (c *Client) fetchList(ctx context.Context, path string) ([]Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create request %s: %w", path, err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("request %s failed with %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result listResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", path, err)
	}

	return cloneItems(result.Items), nil
}
