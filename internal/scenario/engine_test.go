package scenario

import (
	"context"
	"encoding/json"
	"errors"
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
	errs     []error
}

type reportCreatorMock struct {
	mu       sync.Mutex
	requests []reporting.CreateReportRequest
	result   *reporting.CreatedReport
}

type reportReaderMock struct {
	mu      sync.Mutex
	items   []reporting.ReportSummary
	details map[int64]*reporting.ReportDetail
}

type conversationStoreMock struct {
	mu           sync.Mutex
	state        *reporting.ConversationState
	saveRequests []reporting.SaveConversationRequest
}

type referenceProviderMock struct{}

func (referenceProviderMock) Categories(context.Context) ([]reference.Item, error) {
	return []reference.Item{
		{ID: 11, Sorting: 1, Name: "Тишина и покой в ночное время"},
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

func (m *senderMock) messageTexts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, 0, len(m.messages))
	for _, message := range m.messages {
		result = append(result, message.Text)
	}
	return result
}

func (m *senderMock) texts() []string {
	return m.messageTexts()
}

func (m *senderMock) lastMessage() maxapi.NewMessageBody {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		return maxapi.NewMessageBody{}
	}
	return m.messages[len(m.messages)-1]
}

func inlineKeyboardPayload(t *testing.T, body maxapi.NewMessageBody) maxapi.InlineKeyboardPayload {
	t.Helper()
	if len(body.Attachments) != 1 {
		t.Fatalf("expected one keyboard attachment, got %+v", body.Attachments)
	}
	payload, ok := body.Attachments[0].Payload.(maxapi.InlineKeyboardPayload)
	if !ok {
		t.Fatalf("expected inline keyboard payload, got %#v", body.Attachments[0].Payload)
	}
	return payload
}

func assertMainMenuKeyboard(t *testing.T, body maxapi.NewMessageBody) {
	t.Helper()

	if !strings.Contains(body.Text, "Для просмотра списка нарушений") {
		t.Fatalf("expected main menu helper text, got %q", body.Text)
	}

	payload := inlineKeyboardPayload(t, body)
	if len(payload.Buttons) != 5 {
		t.Fatalf("expected 5 rows, got %+v", payload.Buttons)
	}
	expected := []struct {
		text    string
		payload string
	}{
		{text: "Список нарушений", payload: "menu:violations"},
		{text: "Сообщить о нарушении", payload: "menu:report"},
		{text: "Юридическая информация", payload: "menu:legal"},
		{text: "Мои сообщения", payload: "menu:my_reports"},
		{text: "О боте", payload: "menu:about"},
	}
	for i, row := range payload.Buttons {
		if len(row) != 1 {
			t.Fatalf("expected single button in row %d, got %+v", i, row)
		}
		if row[0].Text != expected[i].text || row[0].Payload != expected[i].payload {
			t.Fatalf("unexpected button in row %d: %+v", i, row[0])
		}
	}
}
func (m *reportSinkMock) Store(_ context.Context, payload report.DialogPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.errs) > 0 {
		err := m.errs[0]
		m.errs = m.errs[1:]
		if err != nil {
			return err
		}
	}
	m.payloads = append(m.payloads, payload)
	return nil
}

func (m *reportSinkMock) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.payloads)
}

func (m *reportSinkMock) snapshot() []report.DialogPayload {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]report.DialogPayload, len(m.payloads))
	copy(result, m.payloads)
	return result
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

func (m *reportReaderMock) ListReportsByMaxUserID(_ context.Context, _ int64) ([]reporting.ReportSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]reporting.ReportSummary, len(m.items))
	copy(result, m.items)
	return result, nil
}

func (m *reportReaderMock) GetReportByID(_ context.Context, id int64) (*reporting.ReportDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if item, ok := m.details[id]; ok {
		copy := *item
		return &copy, nil
	}
	return nil, reporting.ErrNotFound
}

func (m *conversationStoreMock) GetConversation(_ context.Context, _ int64) (*reporting.ConversationState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveRequests = append(m.saveRequests, req)
	state := &reporting.ConversationState{
		MaxUserID: req.MaxUserID,
		Stage:     req.UserStage,
	}
	if req.ActiveDraft != nil {
		draft := *req.ActiveDraft
		state.ActiveDraft = &draft
	}
	m.state = state
	return state, nil
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
	conversations := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:    1,
			MaxUserID: 103,
			Stage:     reporting.UserStageMainMenu,
		},
	}
	engine := New(mock, referenceProviderMock{}, WithConversationStore(conversations))
	userID := int64(103)

	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "привет")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	session := engine.session(userID)
	if session.State != stateMainMenu {
		t.Fatalf("expected state %q, got %q", stateMainMenu, session.State)
	}
	texts := mock.messageTexts()
	if len(texts) != 2 {
		t.Fatalf("expected 2 messages, got %d: %#v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "Не могу распознать вашу команду") {
		t.Fatalf("expected unsupported input response first, got %q", texts[0])
	}
	assertMainMenuKeyboard(t, mock.lastMessage())
}

func TestUnsupportedInputShowsNoticeAndRedirectsToMenu(t *testing.T) {
	mock := &senderMock{}
	conversations := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:    1,
			MaxUserID: 1034,
			Stage:     reporting.UserStageMainMenu,
		},
	}
	engine := New(mock, referenceProviderMock{}, WithConversationStore(conversations))
	userID := int64(1034)

	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "непредусмотренный ввод")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	texts := mock.texts()
	if len(texts) < 2 {
		t.Fatalf("expected at least 2 bot messages, got %d", len(texts))
	}
	if !strings.Contains(texts[len(texts)-2], "Не могу распознать вашу команду, вы будете перенаправлены в главное меню.") {
		t.Fatalf("expected unsupported-input notice before redirect, got %q", texts[len(texts)-2])
	}
	assertMainMenuKeyboard(t, mock.lastMessage())
}

func TestFirstPlainMessageShowsWelcomeAndMainMenu(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{}, WithConversationStore(&conversationStoreMock{}))
	userID := int64(1040)

	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "привет")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	texts := mock.messageTexts()
	if len(texts) != 2 {
		t.Fatalf("expected 2 messages, got %d: %#v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "Данный бот создан для оперативного сбора информации") {
		t.Fatalf("expected welcome text first, got %q", texts[0])
	}
	assertMainMenuKeyboard(t, mock.lastMessage())
}

func TestBotStartedShowsWelcomeMessage(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(1031)

	err := engine.HandleUpdate(context.Background(), maxapi.Update{
		UpdateType: "bot_started",
		User: &maxapi.User{
			UserID: userID,
		},
	})
	if err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	texts := mock.messageTexts()
	if len(texts) != 2 {
		t.Fatalf("expected 2 messages, got %d: %#v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "Данный бот создан для оперативного сбора информации") {
		t.Fatalf("expected welcome text first, got %q", texts[0])
	}
	assertMainMenuKeyboard(t, mock.lastMessage())
}

func TestStartCommandShowsWelcomeMessage(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(1032)

	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "/start")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	texts := mock.messageTexts()
	if len(texts) != 2 {
		t.Fatalf("expected 2 messages, got %d: %#v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "Данный бот создан для оперативного сбора информации") {
		t.Fatalf("expected welcome text first on /start, got %q", texts[0])
	}
	assertMainMenuKeyboard(t, mock.lastMessage())
}

func TestAboutReturnsUserToMainMenuState(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(1033)

	engine.setState(userID, stateReportAddress)

	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb-about", "menu:about")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	texts := mock.messageTexts()
	if len(texts) != 1 {
		t.Fatalf("expected 1 message, got %d: %#v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "Данный бот создан для оперативного сбора информации") {
		t.Fatalf("expected about text, got %q", texts[0])
	}
	payload := inlineKeyboardPayload(t, mock.lastMessage())
	if len(payload.Buttons) != 5 {
		t.Fatalf("expected main menu keyboard after about, got %+v", payload.Buttons)
	}

	session := engine.session(userID)
	if session.State != stateMainMenu {
		t.Fatalf("expected state %q after about, got %q", stateMainMenu, session.State)
	}
}

func TestLegalInfoMatchesSpecTextAndButtons(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(1034)

	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb-legal", "menu:legal")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	last := mock.lastMessage()
	if !strings.Contains(last.Text, "Продолжая использование бота, вы выражаете согласие") {
		t.Fatalf("expected legal consent text, got %q", last.Text)
	}
	if !strings.Contains(last.Text, "Федеральный закон от 27.07.2006 № 152-ФЗ") {
		t.Fatalf("expected detailed legal text, got %q", last.Text)
	}
	payload := inlineKeyboardPayload(t, last)
	if len(payload.Buttons) != 5 {
		t.Fatalf("expected main menu keyboard after legal info, got %+v", payload.Buttons)
	}
	session := engine.session(userID)
	if session.State != stateMainMenu {
		t.Fatalf("expected state %q after legal info, got %q", stateMainMenu, session.State)
	}
}

func TestMainMenuMatchesSpecButtons(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(1035)

	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "меню")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	last := mock.lastMessage()
	assertMainMenuKeyboard(t, last)
}

func TestViolationsListMatchesSpecButtons(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(1036)

	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb-violations", "menu:violations")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	last := mock.lastMessage()
	payload := inlineKeyboardPayload(t, last)
	if len(payload.Buttons) != 5 {
		t.Fatalf("expected main menu keyboard after violations list, got %+v", payload.Buttons)
	}
	session := engine.session(userID)
	if session.State != stateMainMenu {
		t.Fatalf("expected state %q after violations list, got %q", stateMainMenu, session.State)
	}
}

func TestButtonOnlyStatesDoNotFallBackToUnsupportedInput(t *testing.T) {
	testCases := []struct {
		name                string
		userID              int64
		initialUpdate       maxapi.Update
		expectedState       BotState
		expectedTextSnippet string
	}{
		{
			name:                "report consent",
			userID:              1039,
			initialUpdate:       callbackUpdate(1039, "cb-report", "menu:report"),
			expectedState:       stateReportConsent,
			expectedTextSnippet: "Для продолжения используйте кнопки ниже.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &senderMock{}
			engine := New(mock, referenceProviderMock{})

			if err := engine.HandleUpdate(context.Background(), tc.initialUpdate); err != nil {
				t.Fatalf("HandleUpdate() error = %v", err)
			}
			if err := engine.HandleUpdate(context.Background(), textUpdate(tc.userID, "любой текст")); err != nil {
				t.Fatalf("HandleUpdate() error = %v", err)
			}

			last := mock.lastMessage()
			if !strings.Contains(last.Text, tc.expectedTextSnippet) {
				t.Fatalf("expected helper text %q, got %q", tc.expectedTextSnippet, last.Text)
			}
			if strings.Contains(last.Text, "Не могу распознать вашу команду") {
				t.Fatalf("expected to stay in button-only state, got unsupported text %q", last.Text)
			}
			session := engine.session(tc.userID)
			if session.State != tc.expectedState {
				t.Fatalf("expected state %q, got %q", tc.expectedState, session.State)
			}
		})
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

	if reportMock.count() != 2 {
		t.Fatalf("expected 2 stored payloads, got %d", reportMock.count())
	}
	if len(creatorMock.requests) != 1 {
		t.Fatalf("expected 1 create report request, got %d", len(creatorMock.requests))
	}
	if creatorMock.requests[0].DialogDedupKey == "" {
		t.Fatalf("expected dialog dedup key to be passed to creator")
	}
	payloads := reportMock.snapshot()
	if payloads[0].DedupKey != creatorMock.requests[0].DialogDedupKey {
		t.Fatalf("expected dedup key %q, got %q", creatorMock.requests[0].DialogDedupKey, payloads[0].DedupKey)
	}
	if payloads[0].MessageID != nil {
		t.Fatalf("expected initial raw payload without message link, got %+v", payloads[0].MessageID)
	}
	if payloads[1].MessageID == nil || *payloads[1].MessageID != 15 {
		t.Fatalf("expected linked raw payload with message_id=15, got %+v", payloads[1].MessageID)
	}
	if payloads[1].NormalizedAt == nil {
		t.Fatalf("expected linked raw payload with normalized_at timestamp")
	}
	if payloads[1].ReportNumber != "15" {
		t.Fatalf("expected linked raw payload to store real report number, got %q", payloads[1].ReportNumber)
	}
	if !strings.Contains(mock.lastText(), "Сообщение принято с номером 15") {
		t.Fatalf("expected final report confirmation text, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "Текущий статус: На модерации.") {
		t.Fatalf("expected humanized status in final confirmation, got %q", mock.lastText())
	}
}

func TestFlowSendDraftKeepsSuccessWhenRawBackfillFails(t *testing.T) {
	mock := &senderMock{}
	reportMock := &reportSinkMock{errs: []error{nil, errors.New("temporary outbox error")}}
	creatorMock := &reportCreatorMock{}
	engine := New(mock, referenceProviderMock{}, WithReportSink(reportMock), WithReportCreator(creatorMock))
	userID := int64(106)

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

	if len(creatorMock.requests) != 1 {
		t.Fatalf("expected 1 create report request, got %d", len(creatorMock.requests))
	}
	if reportMock.count() != 1 {
		t.Fatalf("expected only the initial raw payload to be stored successfully, got %d", reportMock.count())
	}
	if !strings.Contains(mock.lastText(), "Сообщение принято с номером 15") {
		t.Fatalf("expected final report confirmation text despite raw backfill error, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "Текущий статус: На модерации.") {
		t.Fatalf("expected humanized status in final confirmation despite raw backfill error, got %q", mock.lastText())
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

func TestFlowSkipMediaNotAllowedForNonQuietCategory(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(106)

	steps := []maxapi.Update{
		callbackUpdate(userID, "cb1", "report:consent_yes"),
		textUpdate(userID, "2"),
		textUpdate(userID, "1"),
		textUpdate(userID, "89991234567"),
		textUpdate(userID, "ул. Мира, дом 1"),
		textUpdate(userID, "31/03/26 14:45"),
		textUpdate(userID, "Описание нарушения"),
		callbackUpdate(userID, "cb2", "report:skip_media"),
	}

	for _, step := range steps {
		if err := engine.HandleUpdate(context.Background(), step); err != nil {
			t.Fatalf("HandleUpdate() error = %v", err)
		}
	}

	session := engine.session(userID)
	if session.State != stateReportMedia {
		t.Fatalf("expected state %q, got %q", stateReportMedia, session.State)
	}
	if !strings.Contains(mock.lastText(), "только для категории «Тишина и покой»") {
		t.Fatalf("expected skip media restriction message, got %q", mock.lastText())
	}
}

func TestFlowRejectsMediaWhenVideoDurationTooLong(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(107)

	prepareSteps := []maxapi.Update{
		callbackUpdate(userID, "cb1", "report:consent_yes"),
		textUpdate(userID, "1"),
		textUpdate(userID, "1"),
		textUpdate(userID, "89991234567"),
		textUpdate(userID, "ул. Мира, дом 1"),
		textUpdate(userID, "31/03/26 14:45"),
		textUpdate(userID, "Описание нарушения"),
	}

	for _, step := range prepareSteps {
		if err := engine.HandleUpdate(context.Background(), step); err != nil {
			t.Fatalf("HandleUpdate() error = %v", err)
		}
	}

	raw, err := json.Marshal(map[string]any{"duration": 121})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	if err := engine.HandleUpdate(context.Background(), messageWithAttachmentsUpdate(userID, "", []maxapi.AttachmentBody{
		{Type: "video", RawPayload: raw},
	})); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	session := engine.session(userID)
	if session.State != stateReportMedia {
		t.Fatalf("expected state %q, got %q", stateReportMedia, session.State)
	}
	if !strings.Contains(mock.lastText(), "превышает 2 минуты") {
		t.Fatalf("expected video duration limit message, got %q", mock.lastText())
	}
}

func TestFlowRejectsContactAttachmentOnMediaStep(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(108)

	prepareSteps := []maxapi.Update{
		callbackUpdate(userID, "cb1", "report:consent_yes"),
		textUpdate(userID, "2"),
		textUpdate(userID, "1"),
		textUpdate(userID, "89991234567"),
		textUpdate(userID, "ул. Мира, дом 1"),
		textUpdate(userID, "31/03/26 14:45"),
		textUpdate(userID, "Описание нарушения"),
	}

	for _, step := range prepareSteps {
		if err := engine.HandleUpdate(context.Background(), step); err != nil {
			t.Fatalf("HandleUpdate() error = %v", err)
		}
	}

	raw, err := json.Marshal(map[string]any{
		"vcf_info": "BEGIN:VCARD\r\nVERSION:3.0\r\nTEL;TYPE=cell:79616594137\r\nFN:Михаил\r\nEND:VCARD\r\n",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	if err := engine.HandleUpdate(context.Background(), messageWithAttachmentsUpdate(userID, "", []maxapi.AttachmentBody{
		{
			Type:       "contact",
			RawPayload: raw,
		},
	})); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	session := engine.session(userID)
	if session.State != stateReportMedia {
		t.Fatalf("expected state %q, got %q", stateReportMedia, session.State)
	}
	if !strings.Contains(mock.lastText(), "Поддерживаются только фото и видео.") {
		t.Fatalf("expected unsupported media type message, got %q", mock.lastText())
	}
}

func TestFlowRejectsLocationAttachmentOnMediaStepWithSkipHint(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(109)

	prepareSteps := []maxapi.Update{
		callbackUpdate(userID, "cb1", "report:consent_yes"),
		textUpdate(userID, "1"),
		textUpdate(userID, "1"),
		textUpdate(userID, "89991234567"),
		textUpdate(userID, "ул. Мира, дом 1"),
		textUpdate(userID, "31/03/26 14:45"),
		textUpdate(userID, "Описание нарушения"),
	}

	for _, step := range prepareSteps {
		if err := engine.HandleUpdate(context.Background(), step); err != nil {
			t.Fatalf("HandleUpdate() error = %v", err)
		}
	}

	lat := 48.708
	lon := 44.513
	if err := engine.HandleUpdate(context.Background(), messageWithAttachmentsUpdate(userID, "", []maxapi.AttachmentBody{
		{
			Type:      "location",
			Latitude:  &lat,
			Longitude: &lon,
		},
	})); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	session := engine.session(userID)
	if session.State != stateReportMedia {
		t.Fatalf("expected state %q, got %q", stateReportMedia, session.State)
	}
	if !strings.Contains(mock.lastText(), "Поддерживаются только фото и видео. Попробуйте ещё раз или пропустите шаг.") {
		t.Fatalf("expected unsupported media type message with skip hint, got %q", mock.lastText())
	}
}

func TestFlowSendsMediaPayloadToCreateReport(t *testing.T) {
	mock := &senderMock{}
	reportMock := &reportSinkMock{}
	creatorMock := &reportCreatorMock{}
	engine := New(mock, referenceProviderMock{}, WithReportSink(reportMock), WithReportCreator(creatorMock))
	userID := int64(1300)

	prepareSteps := []maxapi.Update{
		callbackUpdate(userID, "cb1", "report:consent_yes"),
		textUpdate(userID, "1"),
		textUpdate(userID, "1"),
		textUpdate(userID, "79991234567"),
		textUpdate(userID, "ул. Мира, дом 1"),
		textUpdate(userID, "31/03/26 14:45"),
		textUpdate(userID, "Описание нарушения"),
	}
	for _, step := range prepareSteps {
		if err := engine.HandleUpdate(context.Background(), step); err != nil {
			t.Fatalf("HandleUpdate() error = %v", err)
		}
	}

	rawPhoto, err := json.Marshal(map[string]any{"token": "photo_token_1"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := engine.HandleUpdate(context.Background(), messageWithAttachmentsUpdate(userID, "", []maxapi.AttachmentBody{
		{Type: "photo", RawPayload: rawPhoto},
	})); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb2", "report:skip_extra")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb3", "report:send")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if len(creatorMock.requests) != 1 {
		t.Fatalf("expected 1 create report request, got %d", len(creatorMock.requests))
	}
	req := creatorMock.requests[0]
	if len(req.Attachments) != 1 {
		t.Fatalf("expected one attachment in create request, got %+v", req.Attachments)
	}
	if req.Attachments[0].Type != "photo" {
		t.Fatalf("expected attachment type photo, got %+v", req.Attachments[0])
	}
	if strings.TrimSpace(string(req.Attachments[0].Payload)) == "" {
		t.Fatalf("expected attachment payload to be forwarded")
	}
}

func TestFlowMyReportsShowsListAndDetails(t *testing.T) {
	mock := &senderMock{}
	readerMock := &reportReaderMock{
		items: []reporting.ReportSummary{
			{
				ID:               15,
				ReportNumber:     "15",
				MaxUserID:        777,
				CategoryName:     "Парковка",
				MunicipalityName: "Волжский",
				Status:           "moderation",
				Description:      "Машина перекрыла проезд",
				Address:          "ул. Мира, 1",
				CreatedAt:        time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC),
			},
			{
				ID:               16,
				ReportNumber:     "16",
				MaxUserID:        777,
				CategoryName:     "Шум",
				MunicipalityName: "Волгоград",
				Status:           "in_progress",
				Description:      "Шумели ночью возле дома",
				Address:          "ул. Ленина, 2",
				CreatedAt:        time.Date(2026, time.March, 31, 13, 0, 0, 0, time.UTC),
			},
		},
		details: map[int64]*reporting.ReportDetail{
			16: {
				ReportSummary: reporting.ReportSummary{
					ID:               16,
					ReportNumber:     "16",
					MaxUserID:        777,
					CategoryName:     "Шум",
					MunicipalityName: "Волгоград",
					Status:           "in_progress",
					Description:      "Шумели ночью возле дома",
					Address:          "ул. Ленина, 2",
					CreatedAt:        time.Date(2026, time.March, 31, 13, 0, 0, 0, time.UTC),
				},
				IncidentTime:   "31/03/26 01:15",
				AdditionalInfo: "Во дворе кафе",
			},
		},
	}
	engine := New(mock, referenceProviderMock{}, WithReportReader(readerMock))
	userID := int64(777)

	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb1", "menu:my_reports")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if !strings.Contains(mock.lastText(), "Ваши обращения:") {
		t.Fatalf("expected reports list text, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "№15") {
		t.Fatalf("expected first report number in list, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "№16") {
		t.Fatalf("expected second report number in list, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "Дата: 31.03.2026 16:00") {
		t.Fatalf("expected date in list, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "Статус: В работе") {
		t.Fatalf("expected status line in list, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "Отправьте номер сообщения") {
		t.Fatalf("expected prompt to send real report number, got %q", mock.lastText())
	}
	listKeyboard := inlineKeyboardPayload(t, mock.lastMessage())
	if len(listKeyboard.Buttons) != 1 || listKeyboard.Buttons[0][0].Text != "Вернуться в начало" {
		t.Fatalf("expected only back-to-start button in list, got %+v", listKeyboard.Buttons)
	}

	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "16")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if !strings.Contains(mock.lastText(), "Обращение №16") {
		t.Fatalf("expected selected report detail, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "Дата: 31.03.2026 16:00") {
		t.Fatalf("expected Moscow time in detail, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "Доп. информация: Во дворе кафе") {
		t.Fatalf("expected additional info in detail, got %q", mock.lastText())
	}
	detailKeyboard := inlineKeyboardPayload(t, mock.lastMessage())
	if len(detailKeyboard.Buttons) != 2 {
		t.Fatalf("expected 2 rows in detail keyboard, got %+v", detailKeyboard.Buttons)
	}
	if detailKeyboard.Buttons[0][0].Text != "Вернуться к списку сообщений" || detailKeyboard.Buttons[0][0].Payload != "reports:back" {
		t.Fatalf("unexpected first detail button: %+v", detailKeyboard.Buttons[0][0])
	}
	if detailKeyboard.Buttons[1][0].Text != "Вернуться в начало" || detailKeyboard.Buttons[1][0].Payload != "menu:main" {
		t.Fatalf("unexpected second detail button: %+v", detailKeyboard.Buttons[1][0])
	}

	session := engine.session(userID)
	if session.State != stateMyReportDetail {
		t.Fatalf("expected state %q after showing report detail, got %q", stateMyReportDetail, session.State)
	}
	if len(session.Reports) != 2 {
		t.Fatalf("expected 2 cached reports in session, got %d", len(session.Reports))
	}
}

func TestFlowMyReportsDetailBackReturnsToList(t *testing.T) {
	mock := &senderMock{}
	readerMock := &reportReaderMock{
		items: []reporting.ReportSummary{
			{
				ID:           15,
				ReportNumber: "15",
				MaxUserID:    779,
				Status:       "moderation",
				CreatedAt:    time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC),
			},
		},
		details: map[int64]*reporting.ReportDetail{
			15: {
				ReportSummary: reporting.ReportSummary{
					ID:           15,
					ReportNumber: "15",
					MaxUserID:    779,
					Status:       "moderation",
					CreatedAt:    time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC),
				},
			},
		},
	}
	engine := New(mock, referenceProviderMock{}, WithReportReader(readerMock))
	userID := int64(779)

	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb1", "menu:my_reports")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "15")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb2", "reports:back")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if !strings.Contains(mock.lastText(), "Ваши обращения:") {
		t.Fatalf("expected reports list after back callback, got %q", mock.lastText())
	}
	session := engine.session(userID)
	if session.State != stateMyReportsList {
		t.Fatalf("expected state %q after returning to list, got %q", stateMyReportsList, session.State)
	}
}

func TestFlowMyReportsEmpty(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{}, WithReportReader(&reportReaderMock{}))

	if err := engine.HandleUpdate(context.Background(), callbackUpdate(778, "cb1", "menu:my_reports")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if !strings.Contains(mock.lastText(), "У вас пока нет отправленных обращений.") {
		t.Fatalf("expected empty reports message, got %q", mock.lastText())
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

	if phone != "9616594137" {
		t.Fatalf("expected normalized phone from contact attachment, got %q", phone)
	}
}

func TestFlowPersistsDraftStagesToConversationStore(t *testing.T) {
	mock := &senderMock{}
	conversationMock := &conversationStoreMock{}
	engine := New(mock, referenceProviderMock{}, WithConversationStore(conversationMock))
	userID := int64(880)

	steps := []maxapi.Update{
		callbackUpdate(userID, "cb1", "menu:report"),
		callbackUpdate(userID, "cb2", "report:consent_yes"),
		textUpdate(userID, "1"),
		textUpdate(userID, "2"),
	}

	for _, step := range steps {
		if err := engine.HandleUpdate(context.Background(), step); err != nil {
			t.Fatalf("HandleUpdate() error = %v", err)
		}
	}

	if len(conversationMock.saveRequests) == 0 {
		t.Fatalf("expected saved conversation requests, got none")
	}
	first := conversationMock.saveRequests[0]
	if first.UserStage != reporting.UserStageMainMenu {
		t.Fatalf("expected consent screen to keep user in main_menu, got %q", first.UserStage)
	}
	if first.ActiveDraft != nil {
		t.Fatalf("expected no draft before consent confirmation, got %+v", first.ActiveDraft)
	}
	last := conversationMock.saveRequests[len(conversationMock.saveRequests)-1]
	if last.UserStage != reporting.UserStageFillingReport {
		t.Fatalf("expected user stage filling_report, got %q", last.UserStage)
	}
	if last.ActiveDraft == nil || last.ActiveDraft.Stage != reporting.MessageStagePhone {
		t.Fatalf("expected active draft at phone step, got %+v", last.ActiveDraft)
	}
	if last.ActiveDraft.CategoryID != 11 {
		t.Fatalf("expected category id to be persisted, got %+v", last.ActiveDraft)
	}
	if last.ActiveDraft.MunicipalityID != 22 {
		t.Fatalf("expected municipality id to be persisted, got %+v", last.ActiveDraft)
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

func messageWithAttachmentsUpdate(userID int64, text string, attachments []maxapi.AttachmentBody) maxapi.Update {
	return maxapi.Update{
		UpdateType: "message_created",
		Message: &maxapi.Message{
			Sender: &maxapi.User{UserID: userID},
			Body: maxapi.MessageBody{
				Text:        text,
				Attachments: attachments,
			},
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
