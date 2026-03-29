package reporting

import (
	"context"
	"errors"
	"testing"
	"time"
)

type storeMock struct {
	createReq  *CreateReportRequest
	createResp *CreatedReport
	createErr  error
}

func (m *storeMock) CreateReport(_ context.Context, req CreateReportRequest) (*CreatedReport, error) {
	copied := req
	m.createReq = &copied
	return m.createResp, m.createErr
}

func (m *storeMock) ListReports(context.Context, ListReportsFilter) ([]ReportSummary, error) {
	return nil, nil
}

func (m *storeMock) ListReportsByMaxUserID(context.Context, int64) ([]ReportSummary, error) {
	return nil, nil
}

func (m *storeMock) GetReportByID(context.Context, int64) (*ReportDetail, error) {
	return nil, ErrNotFound
}

func TestServiceCreateReportValidatesInput(t *testing.T) {
	service := NewService(&storeMock{})

	_, err := service.CreateReport(context.Background(), CreateReportRequest{
		MaxUserID:      100,
		CategoryID:     1,
		MunicipalityID: 2,
		Phone:          "123",
		Address:        "ул. Мира",
		IncidentTime:   "ночь",
		Description:    "Описание",
	})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestServiceCreateReportNormalizesAndDelegates(t *testing.T) {
	expected := &CreatedReport{
		ID:           12,
		ReportNumber: "12",
		Status:       "moderation",
		Stage:        "sended",
		CreatedAt:    time.Now().UTC(),
	}
	store := &storeMock{createResp: expected}
	service := NewService(store)

	result, err := service.CreateReport(context.Background(), CreateReportRequest{
		MaxUserID:      100,
		CategoryID:     1,
		MunicipalityID: 2,
		Phone:          "+7 (999) 123-45-67",
		Address:        "  ул. Мира, 1  ",
		IncidentTime:   " ночь ",
		Description:    " описание ",
		AdditionalInfo: " доп ",
	})
	if err != nil {
		t.Fatalf("CreateReport() error = %v", err)
	}
	if result.ID != expected.ID {
		t.Fatalf("unexpected result: %+v", result)
	}
	if store.createReq == nil {
		t.Fatalf("store request was not captured")
	}
	if store.createReq.Phone != "9991234567" {
		t.Fatalf("expected normalized phone, got %q", store.createReq.Phone)
	}
	if store.createReq.Address != "ул. Мира, 1" {
		t.Fatalf("expected trimmed address, got %q", store.createReq.Address)
	}
}
