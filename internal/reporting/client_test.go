package reporting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientCreateReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/reports" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":15,"report_number":"15","status":"moderation","stage":"sended","user_id":1,"max_user_id":777,"created_at":"2026-03-29T12:00:00Z","updated_at":"2026-03-29T12:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, ClientOptions{})
	result, err := client.CreateReport(context.Background(), CreateReportRequest{
		MaxUserID:      777,
		CategoryID:     1,
		MunicipalityID: 2,
		Phone:          "9991234567",
		Address:        "ул. Мира",
		IncidentTime:   "ночь",
		Description:    "Описание",
	})
	if err != nil {
		t.Fatalf("CreateReport() error = %v", err)
	}
	if result.ID != 15 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
