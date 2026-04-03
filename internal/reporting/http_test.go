package reporting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"max_bot/internal/reference"
)

type serviceStub struct {
	createFunc     func(context.Context, CreateReportRequest) (*CreatedReport, error)
	listFunc       func(context.Context, ListReportsFilter) ([]ReportSummary, error)
	listByUserFunc func(context.Context, int64) ([]ReportSummary, error)
	getFunc        func(context.Context, int64) (*ReportDetail, error)
	getConversationFunc  func(context.Context, int64) (*ConversationState, error)
	saveConversationFunc func(context.Context, SaveConversationRequest) (*ConversationState, error)
}

func (s serviceStub) CreateReport(ctx context.Context, req CreateReportRequest) (*CreatedReport, error) {
	return s.createFunc(ctx, req)
}

func (s serviceStub) ListReports(ctx context.Context, filter ListReportsFilter) ([]ReportSummary, error) {
	return s.listFunc(ctx, filter)
}

func (s serviceStub) ListReportsByMaxUserID(ctx context.Context, id int64) ([]ReportSummary, error) {
	return s.listByUserFunc(ctx, id)
}

func (s serviceStub) GetReportByID(ctx context.Context, id int64) (*ReportDetail, error) {
	return s.getFunc(ctx, id)
}

func (s serviceStub) GetConversation(ctx context.Context, id int64) (*ConversationState, error) {
	return s.getConversationFunc(ctx, id)
}

func (s serviceStub) SaveConversation(ctx context.Context, req SaveConversationRequest) (*ConversationState, error) {
	return s.saveConversationFunc(ctx, req)
}

type referenceStub struct{}

func (referenceStub) Categories(context.Context) ([]reference.Item, error) {
	return []reference.Item{{ID: 1, Sorting: 1, Name: "Категория"}}, nil
}

func (referenceStub) Municipalities(context.Context) ([]reference.Item, error) {
	return []reference.Item{{ID: 2, Sorting: 1, Name: "Муниципалитет"}}, nil
}

func TestHandlerCreateReport(t *testing.T) {
	service := &Service{store: serviceStub{
		createFunc: func(_ context.Context, req CreateReportRequest) (*CreatedReport, error) {
			if req.MaxUserID != 777 {
				t.Fatalf("unexpected max user id: %+v", req)
			}
			return &CreatedReport{ID: 15, ReportNumber: "15", Status: "moderation", Stage: "sended", CreatedAt: time.Now().UTC()}, nil
		},
	}}

	handler := NewHandler(service, referenceStub{}, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/bot/reports", strings.NewReader(`{"max_user_id":777,"category_id":1,"municipality_id":2,"phone":"9991234567","address":"ул. Мира","incident_time":"ночь","description":"Описание"}`))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.Code)
	}
}

func TestHandlerListReportsByUser(t *testing.T) {
	service := &Service{store: serviceStub{
		listByUserFunc: func(_ context.Context, id int64) ([]ReportSummary, error) {
			return []ReportSummary{{ID: 10, ReportNumber: "10", MaxUserID: id}}, nil
		},
	}}
	handler := NewHandler(service, referenceStub{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/bot/reports/by-user/777", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), `"report_number":"10"`) {
		t.Fatalf("unexpected body: %s", resp.Body.String())
	}
}

func TestHandlerGetReportByID(t *testing.T) {
	service := &Service{store: serviceStub{
		getFunc: func(_ context.Context, id int64) (*ReportDetail, error) {
			return &ReportDetail{
				ReportSummary: ReportSummary{ID: id, ReportNumber: "15", MaxUserID: 777},
				Phone:         "9991234567",
			}, nil
		},
	}}
	handler := NewHandler(service, referenceStub{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/bot/reports/15", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), `"id":15`) {
		t.Fatalf("unexpected body: %s", resp.Body.String())
	}
}

func TestHandlerGetConversation(t *testing.T) {
	service := &Service{store: serviceStub{
		getConversationFunc: func(_ context.Context, id int64) (*ConversationState, error) {
			return &ConversationState{MaxUserID: id, Stage: UserStageMainMenu}, nil
		},
	}}
	handler := NewHandler(service, referenceStub{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/bot/conversations/777", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), `"max_user_id":777`) {
		t.Fatalf("unexpected body: %s", resp.Body.String())
	}
}

func TestHandlerSaveConversation(t *testing.T) {
	service := &Service{store: serviceStub{
		saveConversationFunc: func(_ context.Context, req SaveConversationRequest) (*ConversationState, error) {
			if req.MaxUserID != 777 {
				t.Fatalf("unexpected max user id: %+v", req)
			}
			if req.UserStage != UserStageFillingReport {
				t.Fatalf("unexpected user stage: %+v", req)
			}
			return &ConversationState{
				MaxUserID: 777,
				Stage:     UserStageFillingReport,
				ActiveDraft: &DraftMessage{
					Stage: MessageStageCategory,
				},
			}, nil
		},
	}}
	handler := NewHandler(service, referenceStub{}, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/bot/conversations/777", strings.NewReader(`{"user_stage":"filling_report","active_draft":{"stage":"category"}}`))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), `"stage":"filling_report"`) {
		t.Fatalf("unexpected body: %s", resp.Body.String())
	}
}
