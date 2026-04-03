package reference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type providerStub struct {
	categories     []Item
	municipalities []Item
}

func (p providerStub) Categories(context.Context) ([]Item, error) {
	return p.categories, nil
}

func (p providerStub) Municipalities(context.Context) ([]Item, error) {
	return p.municipalities, nil
}

func TestNewHandlerServesCategories(t *testing.T) {
	handler := NewHandler(providerStub{
		categories: []Item{{ID: 1, Sorting: 1, Name: "Test category"}},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/bot/reference/categories", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	var body listResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Name != "Test category" {
		t.Fatalf("unexpected response: %+v", body.Items)
	}
}

func TestNewHandlerRejectsWrongMethod(t *testing.T) {
	handler := NewHandler(providerStub{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/bot/reference/categories", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.Code)
	}
}
