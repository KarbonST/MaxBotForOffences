package reference

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientLoadsCategories(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/reference/categories" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, listResponse{
			Items: []Item{{ID: 1, Sorting: 1, Name: "One"}},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, ClientOptions{})
	items, err := client.Categories(context.Background())
	if err != nil {
		t.Fatalf("Categories() error = %v", err)
	}

	if len(items) != 1 || items[0].Name != "One" {
		t.Fatalf("unexpected categories: %+v", items)
	}
}

func TestClientReturnsErrorOnNonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewClient(server.URL, ClientOptions{})
	if _, err := client.Municipalities(context.Background()); err == nil {
		t.Fatalf("expected error, got nil")
	}
}
