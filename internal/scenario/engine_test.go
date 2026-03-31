package scenario

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"max_bot/internal/maxapi"
	"max_bot/internal/reference"
	"max_bot/internal/report"
	"max_bot/internal/reporting"
)

type senderMock struct {
	mu              sync.Mutex
	messages        []maxapi.NewMessageBody
	callbackAnswers []string
}

type reportSinkMock struct {
	mu       sync.Mutex
	payloads []report.DialogPayload
}

type reportCreatorMock struct {
	mu       sync.Mutex
	requests []reporting.CreateReportRequest
	result   *reporting.CreatedReport
}

type referenceProviderMock struct{}

func (referenceProviderMock) Categories(context.Context) ([]reference.Item, error) {
	return []reference.Item{
		{ID: 11, Sorting: 1, Name: "Категория 1"},
		{ID: 12, Sorting: 2, Name: "Категория 2"},
	}, nil
}

func (referenceProviderMock) Municipalities(context.Context) ([]reference.Item, error) {
	return []reference.Item{
		{ID: 21, Sorting: 1, Name: "Муниципалитет 1"},
		{ID: 22, Sorting: 2, Name: "Муниципалитет 2"},
	}, nil
}

func (m *senderMock) SendMessage(_ context.Context, _ int64, body maxapi.NewMessageBody) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, body)
	return nil
}

func (m *senderMock) AnswerCallback(_ context.Context, callbackID, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbackAnswers = append(m.callbackAnswers, callbackID)
	return nil
}

func (m *senderMock) lastText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[len(m.messages)-1].Text
}

func (m *reportSinkMock) Store(_ context.Context, payload report.DialogPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.payloads = append(m.payloads, payload)
	return nil
}

func (m *reportSinkMock) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.payloads)
}

func (m *reportCreatorMock) CreateReport(_ context.Context, req reporting.CreateReportRequest) (*reporting.CreatedReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	if m.result != nil {
		return m.result, nil
	}
	return &reporting.CreatedReport{
		ID:           15,
		ReportNumber: "15",
		Status:       "moderation",
		Stage:        "sended",
		CreatedAt:    time.Now().UTC(),
	}, nil
}

func TestFlowHappyPathToConfirm(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(101)

	steps := []maxapi.Update{
		callbackUpdate(userID, "cb1", "report:consent_yes"),
		textUpdate(userID, "1"),
		textUpdate(userID, "2"),
		textUpdate(userID, "79991234567"),
		textUpdate(userID, "ул. Мира, дом 1"),
		textUpdate(userID, "31/03/26 14:45"),
		textUpdate(userID, "Описание нарушения"),
		callbackUpdate(userID, "cb2", "report:skip_media"),
		callbackUpdate(userID, "cb3", "report:skip_extra"),
	}

	for _, step := range steps {
		if err := engine.HandleUpdate(context.Background(), step); err != nil {
			t.Fatalf("HandleUpdate() error = %v", err)
		}
	}

	session := engine.session(userID)
	if session.State != stateReportConfirm {
		t.Fatalf("expected state %q, got %q", stateReportConfirm, session.State)
	}

	if !strings.Contains(mock.lastText(), "Черновик сообщения готов") {
		t.Fatalf("expected summary text in last message, got %q", mock.lastText())
	}

	if len(mock.callbackAnswers) != 3 {
		t.Fatalf("expected 3 callback answers, got %d", len(mock.callbackAnswers))
	}
}

func TestFlowValidationCategoryError(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(102)

	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb1", "report:consent_yes")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "abc")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	session := engine.session(userID)
	if session.State != stateReportCategory {
		t.Fatalf("expected state %q, got %q", stateReportCategory, session.State)
	}
	if !strings.Contains(mock.lastText(), "Категория не найдена") {
		t.Fatalf("expected validation text, got %q", mock.lastText())
	}
}

func TestFlowFallbackToMenuForUnknownState(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(103)

	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "привет")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	session := engine.session(userID)
	if session.State != stateMainMenu {
		t.Fatalf("expected state %q, got %q", stateMainMenu, session.State)
	}
	if !strings.Contains(mock.lastText(), "Главное меню") {
		t.Fatalf("expected main menu response, got %q", mock.lastText())
	}
}

func TestFlowSendDraftStoresDialogPayload(t *testing.T) {
	mock := &senderMock{}
	reportMock := &reportSinkMock{}
	creatorMock := &reportCreatorMock{}
	engine := New(mock, referenceProviderMock{}, WithReportSink(reportMock), WithReportCreator(creatorMock))
	userID := int64(104)

	steps := []maxapi.Update{
		callbackUpdate(userID, "cb1", "report:consent_yes"),
		textUpdate(userID, "1"),
		textUpdate(userID, "2"),
		textUpdate(userID, "79991234567"),
		textUpdate(userID, "ул. Мира, дом 1"),
		textUpdate(userID, "31/03/26 14:45"),
		textUpdate(userID, "Описание нарушения"),
		callbackUpdate(userID, "cb2", "report:skip_media"),
		callbackUpdate(userID, "cb3", "report:skip_extra"),
		callbackUpdate(userID, "cb4", "report:send"),
	}

	for _, step := range steps {
		if err := engine.HandleUpdate(context.Background(), step); err != nil {
			t.Fatalf("HandleUpdate() error = %v", err)
		}
	}

	if reportMock.count() != 1 {
		t.Fatalf("expected 1 stored payload, got %d", reportMock.count())
	}
	if len(creatorMock.requests) != 1 {
		t.Fatalf("expected 1 create report request, got %d", len(creatorMock.requests))
	}
	if creatorMock.requests[0].DialogDedupKey == "" {
		t.Fatalf("expected dialog dedup key to be passed to creator")
	}
	if !strings.Contains(mock.lastText(), "Сообщение принято с номером 15") {
		t.Fatalf("expected final report confirmation text, got %q", mock.lastText())
	}
}

func TestFlowValidationIncidentTimeError(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(105)

	steps := []maxapi.Update{
		callbackUpdate(userID, "cb1", "report:consent_yes"),
		textUpdate(userID, "1"),
		textUpdate(userID, "1"),
		textUpdate(userID, "89991234567"),
		textUpdate(userID, "ул. Мира, дом 1"),
		textUpdate(userID, "завтра вечером"),
	}

	for _, step := range steps {
		if err := engine.HandleUpdate(context.Background(), step); err != nil {
			t.Fatalf("HandleUpdate() error = %v", err)
		}
	}

	session := engine.session(userID)
	if session.State != stateReportTime {
		t.Fatalf("expected state %q, got %q", stateReportTime, session.State)
	}
	if !strings.Contains(mock.lastText(), "формате дд/мм/гг чч:мм") {
		t.Fatalf("expected incident time validation text, got %q", mock.lastText())
	}
}

func TestParsePhoneFromContactAttachment(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"vcf_info": "BEGIN:VCARD\r\nVERSION:3.0\r\nTEL;TYPE=cell:79616594137\r\nFN:Михаил\r\nEND:VCARD\r\n",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	phone := parsePhone("", []maxapi.AttachmentBody{
		{
			Type:       "contact",
			RawPayload: raw,
		},
	})

	if phone != "79616594137" {
		t.Fatalf("expected phone from contact attachment, got %q", phone)
	}
}

func textUpdate(userID int64, text string) maxapi.Update {
	return maxapi.Update{
		UpdateType: "message_created",
		Message: &maxapi.Message{
			Sender: &maxapi.User{UserID: userID},
			Body:   maxapi.MessageBody{Text: text},
		},
	}
}

func callbackUpdate(userID int64, callbackID, payload string) maxapi.Update {
	return maxapi.Update{
		UpdateType: "message_callback",
		Callback: &maxapi.Callback{
			CallbackID: callbackID,
			Payload:    payload,
			User: maxapi.User{
				UserID: userID,
			},
		},
	}
}
