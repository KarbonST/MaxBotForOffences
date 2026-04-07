package reporting

import (
	"context"
	"errors"
	"testing"
	"time"
)

type storeMock struct {
	createReq        *CreateReportRequest
	createResp       *CreatedReport
	createErr        error
	conversation     *ConversationState
	conversationErr  error
	saveConversation *SaveConversationRequest
	saveResponse     *ConversationState
	saveError        error
	notifications    []NotificationItem
	notificationsErr error
	clarification    *ClarificationPrompt
	clarificationErr error
	answerReq        *ClarificationAnswerRequest
	answerErr        error
	rejectReq        *ClarificationRejectRequest
	rejectErr        error
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

func (m *storeMock) GetConversation(context.Context, int64) (*ConversationState, error) {
	return m.conversation, m.conversationErr
}

func (m *storeMock) SaveConversation(_ context.Context, req SaveConversationRequest) (*ConversationState, error) {
	copied := req
	m.saveConversation = &copied
	return m.saveResponse, m.saveError
}

func (m *storeMock) ListPendingNotifications(context.Context, int) ([]NotificationItem, error) {
	return m.notifications, m.notificationsErr
}

func (m *storeMock) MarkNotificationSent(context.Context, int64) error {
	return nil
}

func (m *storeMock) MarkNotificationError(context.Context, int64) error {
	return nil
}

func (m *storeMock) GetPendingClarification(context.Context, int64) (*ClarificationPrompt, error) {
	return m.clarification, m.clarificationErr
}

func (m *storeMock) AnswerClarification(_ context.Context, req ClarificationAnswerRequest) error {
	copied := req
	m.answerReq = &copied
	return m.answerErr
}

func (m *storeMock) RejectClarification(_ context.Context, req ClarificationRejectRequest) error {
	copied := req
	m.rejectReq = &copied
	return m.rejectErr
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

func TestServiceSaveConversationValidatesInput(t *testing.T) {
	service := NewService(&storeMock{})

	_, err := service.SaveConversation(context.Background(), SaveConversationRequest{
		MaxUserID: 100,
		UserStage: UserStageFillingReport,
		ActiveDraft: &DraftMessage{
			Stage: MessageStageUnset,
		},
	})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestServiceSaveConversationNormalizesAndDelegates(t *testing.T) {
	expected := &ConversationState{
		MaxUserID: 100,
		Stage:     UserStageFillingReport,
	}
	store := &storeMock{saveResponse: expected}
	service := NewService(store)

	result, err := service.SaveConversation(context.Background(), SaveConversationRequest{
		MaxUserID: 100,
		UserStage: UserStageFillingReport,
		ActiveDraft: &DraftMessage{
			Stage:          MessageStagePhone,
			Phone:          "+7 (999) 123-45-67",
			Address:        "  ул. Мира, 1  ",
			IncidentTime:   " ночь ",
			Description:    " описание ",
			AdditionalInfo: " доп ",
		},
	})
	if err != nil {
		t.Fatalf("SaveConversation() error = %v", err)
	}
	if result.Stage != expected.Stage {
		t.Fatalf("unexpected result: %+v", result)
	}
	if store.saveConversation == nil || store.saveConversation.ActiveDraft == nil {
		t.Fatalf("store request was not captured")
	}
	if store.saveConversation.ActiveDraft.Phone != "9991234567" {
		t.Fatalf("expected normalized phone, got %q", store.saveConversation.ActiveDraft.Phone)
	}
	if store.saveConversation.ActiveDraft.Address != "ул. Мира, 1" {
		t.Fatalf("expected trimmed address, got %q", store.saveConversation.ActiveDraft.Address)
	}
}

func TestServiceAnswerClarificationNormalizesAndDelegates(t *testing.T) {
	store := &storeMock{}
	service := NewService(store)

	err := service.AnswerClarification(context.Background(), ClarificationAnswerRequest{
		ClarificationID: 10,
		MaxUserID:       100,
		Answer:          "  Уточняю адрес  ",
	})
	if err != nil {
		t.Fatalf("AnswerClarification() error = %v", err)
	}
	if store.answerReq == nil {
		t.Fatalf("expected answer request to be delegated")
	}
	if store.answerReq.Answer != "Уточняю адрес" {
		t.Fatalf("expected trimmed answer, got %q", store.answerReq.Answer)
	}
}
