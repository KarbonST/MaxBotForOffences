package scenario

import (
	"context"
	"strings"
	"sync"
	"testing"

	"max_bot/internal/maxapi"
)

type senderMock struct {
	mu              sync.Mutex
	messages        []maxapi.NewMessageBody
	callbackAnswers []string
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

func TestFlowHappyPathToConfirm(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock)
	userID := int64(101)

	steps := []maxapi.Update{
		callbackUpdate(userID, "cb1", "report:consent_yes"),
		textUpdate(userID, "1"),
		textUpdate(userID, "2"),
		textUpdate(userID, "9991234567"),
		textUpdate(userID, "ул. Мира, дом 1"),
		textUpdate(userID, "ночь"),
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
	engine := New(mock)
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
	engine := New(mock)
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
