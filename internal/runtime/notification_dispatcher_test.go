package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"max_bot/internal/maxapi"
	"max_bot/internal/reporting"
)

type notificationStoreMock struct {
	items    []reporting.NotificationItem
	listErr  error
	sentIDs  []int64
	errorIDs []int64
	sentErr  error
	errorErr error
}

type clarificationStoreMock struct {
	prompt *reporting.ClarificationPrompt
	err    error
}

type conversationStoreMock struct {
	state *reporting.ConversationState
	reqs  []reporting.SaveConversationRequest
	err   error
}

type notificationSenderMock struct {
	messages []sentNotification
	err      error
}

type sentNotification struct {
	userID int64
	body   maxapi.NewMessageBody
}

func (m *notificationStoreMock) ListPendingNotifications(context.Context, int) ([]reporting.NotificationItem, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return append([]reporting.NotificationItem(nil), m.items...), nil
}

func (m *notificationStoreMock) MarkNotificationSent(_ context.Context, notificationID int64) error {
	m.sentIDs = append(m.sentIDs, notificationID)
	return m.sentErr
}

func (m *notificationStoreMock) MarkNotificationError(_ context.Context, notificationID int64) error {
	m.errorIDs = append(m.errorIDs, notificationID)
	return m.errorErr
}

func (m *clarificationStoreMock) GetPendingClarification(context.Context, int64) (*reporting.ClarificationPrompt, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.prompt == nil {
		return nil, reporting.ErrNotFound
	}
	copy := *m.prompt
	return &copy, nil
}

func (m *conversationStoreMock) GetConversation(context.Context, int64) (*reporting.ConversationState, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.state == nil {
		return &reporting.ConversationState{Stage: reporting.UserStageMainMenu}, nil
	}
	copy := *m.state
	if m.state.ActiveDraft != nil {
		draft := *m.state.ActiveDraft
		copy.ActiveDraft = &draft
	}
	return &copy, nil
}

func (m *conversationStoreMock) SaveConversation(_ context.Context, req reporting.SaveConversationRequest) (*reporting.ConversationState, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.reqs = append(m.reqs, req)
	state := &reporting.ConversationState{
		MaxUserID:     req.MaxUserID,
		Stage:         req.UserStage,
		PreviousStage: req.PreviousStage,
	}
	if req.ActiveDraft != nil {
		draft := *req.ActiveDraft
		state.ActiveDraft = &draft
	}
	m.state = state
	return state, nil
}

func (m *notificationSenderMock) SendMessage(_ context.Context, userID int64, body maxapi.NewMessageBody) error {
	if m.err != nil {
		return m.err
	}
	m.messages = append(m.messages, sentNotification{userID: userID, body: body})
	return nil
}

func TestNotificationDispatcherDispatchesGenericNotification(t *testing.T) {
	store := &notificationStoreMock{
		items: []reporting.NotificationItem{
			{ID: 1, MaxUserID: 1001, Notification: "Ваше сообщение №13 прошло модерацию и находится в статусе «в работе»."},
		},
	}
	sender := &notificationSenderMock{}
	conversations := &conversationStoreMock{}
	dispatcher := NewNotificationDispatcher(store, &clarificationStoreMock{}, conversations, sender, NotificationDispatcherConfig{
		Interval:  10 * time.Millisecond,
		BatchSize: 10,
	}, nil)

	if err := dispatcher.dispatchBatch(context.Background()); err != nil {
		t.Fatalf("dispatchBatch() error = %v", err)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("expected one sent message, got %d", len(sender.messages))
	}
	if sender.messages[0].userID != 1001 {
		t.Fatalf("expected user 1001, got %+v", sender.messages[0])
	}
	if sender.messages[0].body.Text != "Ваше сообщение №13 прошло **модерацию** и находится в статусе «**в работе**»." {
		t.Fatalf("unexpected message text: %+v", sender.messages[0])
	}
	if sender.messages[0].body.Format != "markdown" {
		t.Fatalf("expected markdown format for status notification, got %+v", sender.messages[0].body)
	}
	if len(sender.messages[0].body.Attachments) != 0 {
		t.Fatalf("did not expect attachments for generic notification, got %+v", sender.messages[0].body.Attachments)
	}
	if len(conversations.reqs) != 0 {
		t.Fatalf("did not expect conversation stage changes for generic notification, got %+v", conversations.reqs)
	}
	if len(store.sentIDs) != 1 || store.sentIDs[0] != 1 {
		t.Fatalf("expected notification 1 to be marked sent, got %+v", store.sentIDs)
	}
	if len(store.errorIDs) != 0 {
		t.Fatalf("did not expect error notifications, got %+v", store.errorIDs)
	}
}

func TestNotificationDispatcherKeepsDraftFlowOnGenericNotification(t *testing.T) {
	store := &notificationStoreMock{
		items: []reporting.NotificationItem{
			{ID: 10, MaxUserID: 1011, Notification: "Ваше сообщение №21 находится в статусе «в работе»."},
		},
	}
	conversations := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:    21,
			MaxUserID: 1011,
			Stage:     reporting.UserStageFillingReport,
			ActiveDraft: &reporting.DraftMessage{
				ID:    99,
				Stage: reporting.MessageStageDescription,
			},
		},
	}
	sender := &notificationSenderMock{}
	dispatcher := NewNotificationDispatcher(store, &clarificationStoreMock{}, conversations, sender, NotificationDispatcherConfig{
		Interval:  10 * time.Millisecond,
		BatchSize: 10,
	}, nil)

	if err := dispatcher.dispatchBatch(context.Background()); err != nil {
		t.Fatalf("dispatchBatch() error = %v", err)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("expected one sent message, got %d", len(sender.messages))
	}
	if len(sender.messages[0].body.Attachments) != 0 {
		t.Fatalf("did not expect buttons while user fills draft, got %+v", sender.messages[0].body.Attachments)
	}
	if len(conversations.reqs) != 0 {
		t.Fatalf("did not expect draft stage override for passive notification, got %+v", conversations.reqs)
	}
}

func TestNotificationDispatcherAddsClarificationKeyboardAndWaitingStage(t *testing.T) {
	store := &notificationStoreMock{
		items: []reporting.NotificationItem{
			{ID: 2, MaxUserID: 1002, Notification: "По вашему сообщению требуется уточнение"},
		},
	}
	clarifications := &clarificationStoreMock{
		prompt: &reporting.ClarificationPrompt{
			ID:             50,
			MessageID:      15,
			NotificationID: 2,
			Status:         reporting.RequestStatusNew,
			CreatedAt:      time.Now().UTC(),
		},
	}
	conversations := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:    10,
			MaxUserID: 1002,
			Stage:     reporting.UserStageFillingReport,
			ActiveDraft: &reporting.DraftMessage{
				ID:    41,
				Stage: reporting.MessageStageAddress,
			},
		},
	}
	sender := &notificationSenderMock{}
	dispatcher := NewNotificationDispatcher(store, clarifications, conversations, sender, NotificationDispatcherConfig{
		Interval:  10 * time.Millisecond,
		BatchSize: 10,
	}, nil)

	if err := dispatcher.dispatchBatch(context.Background()); err != nil {
		t.Fatalf("dispatchBatch() error = %v", err)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("expected one sent clarification, got %d", len(sender.messages))
	}
	if len(sender.messages[0].body.Attachments) != 1 {
		t.Fatalf("expected clarification keyboard attachment, got %+v", sender.messages[0].body.Attachments)
	}
	payload, ok := sender.messages[0].body.Attachments[0].Payload.(maxapi.InlineKeyboardPayload)
	if !ok {
		t.Fatalf("expected inline keyboard payload, got %#v", sender.messages[0].body.Attachments[0].Payload)
	}
	if payload.Buttons[0][0].Payload != "clarification:reject" {
		t.Fatalf("unexpected first clarification button: %+v", payload.Buttons[0][0])
	}
	if len(conversations.reqs) != 1 {
		t.Fatalf("expected waiting_clarification save request, got %d", len(conversations.reqs))
	}
	if conversations.reqs[0].UserStage != reporting.UserStageWaitingClarification {
		t.Fatalf("expected waiting_clarification stage, got %+v", conversations.reqs[0])
	}
	if conversations.reqs[0].PreviousStage != reporting.UserStageFillingReport {
		t.Fatalf("expected previous stage filling_report, got %+v", conversations.reqs[0])
	}
	if conversations.reqs[0].ActiveDraft == nil || conversations.reqs[0].ActiveDraft.ID != 41 {
		t.Fatalf("expected active draft to be preserved, got %+v", conversations.reqs[0].ActiveDraft)
	}
}

func TestNotificationDispatcherRepairsBrokenPreviousStageForClarification(t *testing.T) {
	store := &notificationStoreMock{
		items: []reporting.NotificationItem{
			{ID: 20, MaxUserID: 1010, Notification: "Уточнение по обращению"},
		},
	}
	clarifications := &clarificationStoreMock{
		prompt: &reporting.ClarificationPrompt{
			ID:             88,
			MessageID:      99,
			NotificationID: 20,
			Status:         reporting.RequestStatusNew,
			CreatedAt:      time.Now().UTC(),
		},
	}
	conversations := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:        15,
			MaxUserID:     1010,
			Stage:         reporting.UserStageWaitingClarification,
			PreviousStage: reporting.UserStageWaitingClarification,
			ActiveDraft: &reporting.DraftMessage{
				ID:    77,
				Stage: reporting.MessageStageMunicipality,
			},
		},
	}
	sender := &notificationSenderMock{}
	dispatcher := NewNotificationDispatcher(store, clarifications, conversations, sender, NotificationDispatcherConfig{
		Interval:  10 * time.Millisecond,
		BatchSize: 10,
	}, nil)

	if err := dispatcher.dispatchBatch(context.Background()); err != nil {
		t.Fatalf("dispatchBatch() error = %v", err)
	}

	if len(conversations.reqs) != 1 {
		t.Fatalf("expected one save request, got %d", len(conversations.reqs))
	}
	if conversations.reqs[0].PreviousStage != reporting.UserStageFillingReport {
		t.Fatalf("expected repaired previous stage filling_report, got %+v", conversations.reqs[0])
	}
}

func TestNotificationDispatcherMarksErrorWhenSendFails(t *testing.T) {
	store := &notificationStoreMock{
		items: []reporting.NotificationItem{
			{ID: 3, MaxUserID: 1003, Notification: "Не удалось доставить"},
		},
	}
	sender := &notificationSenderMock{err: errors.New("send failed")}
	dispatcher := NewNotificationDispatcher(store, &clarificationStoreMock{}, &conversationStoreMock{}, sender, NotificationDispatcherConfig{
		Interval:  10 * time.Millisecond,
		BatchSize: 10,
	}, nil)

	if err := dispatcher.dispatchBatch(context.Background()); err != nil {
		t.Fatalf("dispatchBatch() error = %v", err)
	}

	if len(store.errorIDs) != 1 || store.errorIDs[0] != 3 {
		t.Fatalf("expected notification 3 to be marked error, got %+v", store.errorIDs)
	}
	if len(store.sentIDs) != 0 {
		t.Fatalf("did not expect sent notifications, got %+v", store.sentIDs)
	}
}

func TestNotificationDispatcherDefersSecondClarificationWhileFirstIsActive(t *testing.T) {
	store := &notificationStoreMock{
		items: []reporting.NotificationItem{
			{ID: 4, MaxUserID: 1004, Notification: "Уточнение по сообщению №1"},
			{ID: 5, MaxUserID: 1004, Notification: "Уточнение по сообщению №2"},
		},
	}
	clarifications := &clarificationStoreMock{
		prompt: &reporting.ClarificationPrompt{
			ID:             70,
			MessageID:      1,
			NotificationID: 4,
			Status:         reporting.RequestStatusNew,
			CreatedAt:      time.Now().UTC(),
		},
	}
	conversations := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:    11,
			MaxUserID: 1004,
			Stage:     reporting.UserStageFillingReport,
		},
	}
	sender := &notificationSenderMock{}
	dispatcher := NewNotificationDispatcher(store, clarifications, conversations, sender, NotificationDispatcherConfig{
		Interval:  10 * time.Millisecond,
		BatchSize: 10,
	}, nil)

	if err := dispatcher.dispatchBatch(context.Background()); err != nil {
		t.Fatalf("dispatchBatch() error = %v", err)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("expected only one clarification to be sent, got %d", len(sender.messages))
	}
	if sender.messages[0].body.Text != "Уточнение по сообщению №1" {
		t.Fatalf("unexpected first clarification: %+v", sender.messages[0])
	}
	if len(store.sentIDs) != 1 || store.sentIDs[0] != 4 {
		t.Fatalf("expected only notification 4 to be marked sent, got %+v", store.sentIDs)
	}
	if len(store.errorIDs) != 0 {
		t.Fatalf("did not expect error notifications, got %+v", store.errorIDs)
	}
}

func TestNotificationDispatcherSendsOnlyCurrentClarificationNotification(t *testing.T) {
	store := &notificationStoreMock{
		items: []reporting.NotificationItem{
			{ID: 6, MaxUserID: 1005, Notification: "Более позднее уточнение"},
			{ID: 7, MaxUserID: 1005, Notification: "Текущее активное уточнение"},
		},
	}
	clarifications := &clarificationStoreMock{
		prompt: &reporting.ClarificationPrompt{
			ID:             71,
			MessageID:      1,
			NotificationID: 7,
			Status:         reporting.RequestStatusNew,
			CreatedAt:      time.Now().UTC(),
		},
	}
	conversations := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:    12,
			MaxUserID: 1005,
			Stage:     reporting.UserStageFillingReport,
		},
	}
	sender := &notificationSenderMock{}
	dispatcher := NewNotificationDispatcher(store, clarifications, conversations, sender, NotificationDispatcherConfig{
		Interval:  10 * time.Millisecond,
		BatchSize: 10,
	}, nil)

	if err := dispatcher.dispatchBatch(context.Background()); err != nil {
		t.Fatalf("dispatchBatch() error = %v", err)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("expected only active clarification notification to be sent, got %d", len(sender.messages))
	}
	if sender.messages[0].body.Text != "Текущее активное уточнение" {
		t.Fatalf("unexpected sent clarification: %+v", sender.messages[0])
	}
	if len(store.sentIDs) != 1 || store.sentIDs[0] != 7 {
		t.Fatalf("expected only notification 7 to be marked sent, got %+v", store.sentIDs)
	}
}
