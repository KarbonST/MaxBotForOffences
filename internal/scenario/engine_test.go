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
	err      error
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

type clarificationStoreMock struct {
	mu                sync.Mutex
	prompt            *reporting.ClarificationPrompt
	afterAnswerPrompt *reporting.ClarificationPrompt
	answerReqs        []reporting.ClarificationAnswerRequest
	rejectReqs        []reporting.ClarificationRejectRequest
	answerErr         error
	rejectErr         error
	getErr            error
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

func assertAcceptedReportMessage(t *testing.T, text, reportNumber string) {
	t.Helper()

	if !strings.Contains(text, "Сообщение о правонарушении принято") {
		t.Fatalf("expected accepted message text, got %q", text)
	}
	if !strings.Contains(text, "номером "+reportNumber) && !strings.Contains(text, "номер "+reportNumber) {
		t.Fatalf("expected report number %s in final confirmation, got %q", reportNumber, text)
	}
	if !strings.Contains(text, "Статусы рассмотрения сообщения:") {
		t.Fatalf("expected review statuses block in final confirmation, got %q", text)
	}
	if !strings.Contains(text, "• **модерация**") || !strings.Contains(text, "• рассмотрено") {
		t.Fatalf("expected status flow in final confirmation, got %q", text)
	}
	if strings.Contains(text, "• **в работе**/**отклонено**") || strings.Contains(text, "• **рассмотрено**") {
		t.Fatalf("expected status flow in final confirmation, got %q", text)
	}
	if !strings.Contains(text, "При изменении статуса сообщения вам поступит уведомление.") {
		t.Fatalf("expected notification note in final confirmation, got %q", text)
	}
	if !strings.Contains(text, "Сообщение будет храниться не более 30 дней.") {
		t.Fatalf("expected retention note in final confirmation, got %q", text)
	}
}

func assertMarkdownFormat(t *testing.T, body maxapi.NewMessageBody) {
	t.Helper()
	if body.Format != "markdown" {
		t.Fatalf("expected markdown message format, got %q", body.Format)
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
	if m.err != nil {
		return nil, m.err
	}
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

func TestFlowSendDraftBlockedOutsideConfirmState(t *testing.T) {
	mock := &senderMock{}
	reportMock := &reportSinkMock{}
	creatorMock := &reportCreatorMock{}
	engine := New(mock, referenceProviderMock{}, WithReportSink(reportMock), WithReportCreator(creatorMock))
	userID := int64(1901)

	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb-outside-send", "report:send")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if got := mock.lastText(); !strings.Contains(got, "Черновик не готов к отправке") {
		t.Fatalf("unexpected bot response: %q", got)
	}
	if reportMock.count() != 0 {
		t.Fatalf("expected no outbox payload writes, got %d", reportMock.count())
	}
	if len(creatorMock.requests) != 0 {
		t.Fatalf("expected no create report requests, got %d", len(creatorMock.requests))
	}
}

func TestFlowSendDraftBlockedWhenDraftInvalid(t *testing.T) {
	mock := &senderMock{}
	reportMock := &reportSinkMock{}
	creatorMock := &reportCreatorMock{}
	engine := New(mock, referenceProviderMock{}, WithReportSink(reportMock), WithReportCreator(creatorMock))
	userID := int64(1902)

	session := engine.session(userID)
	applyState(session, stateReportConfirm)
	session.Loaded = true
	session.HasUserRecord = true
	session.Draft.CategoryID = 11
	session.Draft.MunicipalityID = 21
	// Intentionally leave required fields empty to emulate stale/incomplete confirm.

	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb-invalid-send", "report:send")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if got := mock.lastText(); !strings.Contains(got, "Черновик заполнен не полностью") {
		t.Fatalf("unexpected bot response: %q", got)
	}
	if reportMock.count() != 0 {
		t.Fatalf("expected no outbox payload writes, got %d", reportMock.count())
	}
	if len(creatorMock.requests) != 0 {
		t.Fatalf("expected no create report requests, got %d", len(creatorMock.requests))
	}
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
	if m.state != nil {
		state.UserID = m.state.UserID
		if req.PreviousStage != "" {
			state.PreviousStage = req.PreviousStage
		} else {
			state.PreviousStage = m.state.PreviousStage
		}
		if !req.DeleteDraft && req.ActiveDraft == nil && m.state.ActiveDraft != nil {
			draft := *m.state.ActiveDraft
			state.ActiveDraft = &draft
		}
	}
	if req.ActiveDraft != nil {
		draft := *req.ActiveDraft
		state.ActiveDraft = &draft
	}
	if req.DeleteDraft {
		state.ActiveDraft = nil
	}
	m.state = state
	return state, nil
}

func (m *clarificationStoreMock) GetPendingClarification(_ context.Context, _ int64) (*reporting.ClarificationPrompt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.prompt == nil {
		return nil, reporting.ErrNotFound
	}
	copy := *m.prompt
	return &copy, nil
}

func (m *clarificationStoreMock) AnswerClarification(_ context.Context, req reporting.ClarificationAnswerRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.answerReqs = append(m.answerReqs, req)
	if m.answerErr != nil {
		return m.answerErr
	}
	if m.afterAnswerPrompt != nil {
		next := *m.afterAnswerPrompt
		m.prompt = &next
		m.afterAnswerPrompt = nil
	} else {
		m.prompt = nil
	}
	return nil
}

func (m *clarificationStoreMock) RejectClarification(_ context.Context, req reporting.ClarificationRejectRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rejectReqs = append(m.rejectReqs, req)
	if m.rejectErr != nil {
		return m.rejectErr
	}
	m.prompt = nil
	return nil
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
	if got := mock.lastText(); got != "Категория с номером «abc» не найдена, повторите попытку" {
		t.Fatalf("expected spec validation text, got %q", got)
	}
}

func TestFlowValidationMunicipalityError(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(1021)

	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb1", "report:consent_yes")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "1")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "abc")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	session := engine.session(userID)
	if session.State != stateReportMunicipal {
		t.Fatalf("expected state %q, got %q", stateReportMunicipal, session.State)
	}
	if got := mock.lastText(); got != "Муниципалитет с номером «abc» не найден, повторите попытку" {
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
	if len(payload.Buttons) != 2 {
		t.Fatalf("expected legal info keyboard (2 rows), got %+v", payload.Buttons)
	}
	if payload.Buttons[0][0].Text != "Вернуться в начало" || payload.Buttons[0][0].Payload != "menu:main" {
		t.Fatalf("unexpected first legal button: %+v", payload.Buttons[0][0])
	}
	if payload.Buttons[1][0].Text != "Сообщить о нарушении" || payload.Buttons[1][0].Payload != "menu:report" {
		t.Fatalf("unexpected second legal button: %+v", payload.Buttons[1][0])
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
	if !strings.Contains(last.Text, "Список нарушений, ответственность за совершение которых предусмотрена региональным законодательством:") {
		t.Fatalf("expected violations heading from spec, got %q", last.Text)
	}
	if !strings.Contains(last.Text, "1 - Тишина и покой в ночное время") {
		t.Fatalf("expected numbered violations list, got %q", last.Text)
	}
	payload := inlineKeyboardPayload(t, last)
	if len(payload.Buttons) != 2 {
		t.Fatalf("expected violations keyboard (2 rows), got %+v", payload.Buttons)
	}
	if payload.Buttons[0][0].Text != "Вернуться в начало" || payload.Buttons[0][0].Payload != "menu:main" {
		t.Fatalf("unexpected first violations button: %+v", payload.Buttons[0][0])
	}
	if payload.Buttons[1][0].Text != "Сообщить о нарушении" || payload.Buttons[1][0].Payload != "menu:report" {
		t.Fatalf("unexpected second violations button: %+v", payload.Buttons[1][0])
	}
	session := engine.session(userID)
	if session.State != stateMainMenu {
		t.Fatalf("expected state %q after violations list, got %q", stateMainMenu, session.State)
	}
}

func TestCategoryPromptMatchesSpec(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(10361)

	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb-category", "report:consent_yes")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	last := mock.lastText()
	if !strings.Contains(last, "Список категорий административных правонарушений") {
		t.Fatalf("expected category heading from spec, got %q", last)
	}
	if !strings.Contains(last, "1 - Тишина и покой в ночное время") {
		t.Fatalf("expected numbered categories list, got %q", last)
	}
	if !strings.Contains(last, "Для продолжения отправьте номер категории.") {
		t.Fatalf("expected category continuation prompt, got %q", last)
	}
}

func TestMunicipalityPromptMatchesSpec(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(10362)

	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb-category", "report:consent_yes")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "1")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	last := mock.lastText()
	if !strings.Contains(last, "Список муниципалитетов") {
		t.Fatalf("expected municipality heading from spec, got %q", last)
	}
	if !strings.Contains(last, "1 - Муниципалитет 1") {
		t.Fatalf("expected numbered municipalities list, got %q", last)
	}
	if !strings.Contains(last, "Для продолжения отправьте номер муниципалитета.") {
		t.Fatalf("expected municipality continuation prompt, got %q", last)
	}
}

func TestMyReportDetailMessageUsesStatusSpecificContext(t *testing.T) {
	cases := []struct {
		name      string
		detail    reporting.ReportDetail
		want      string
		notWanted string
	}{
		{
			name: "resolved uses result label",
			detail: reporting.ReportDetail{
				ReportSummary: reporting.ReportSummary{ReportNumber: "21", Status: "resolved"},
				Answer:        "Нарушение устранено",
			},
			want:      "Результат рассмотрения: Нарушение устранено",
			notWanted: "Ответ:",
		},
		{
			name: "rejected uses reason label",
			detail: reporting.ReportDetail{
				ReportSummary: reporting.ReportSummary{ReportNumber: "22", Status: "rejected"},
				Answer:        "Недостаточно сведений",
			},
			want:      "Причина отклонения: Недостаточно сведений",
			notWanted: "Ответ:",
		},
		{
			name: "clarification uses status context",
			detail: reporting.ReportDetail{
				ReportSummary: reporting.ReportSummary{ReportNumber: "23", Status: "clarification_requested"},
				StatusContext: "Уточните адрес дома",
			},
			want:      "Запрошенное уточнение информации: Уточните адрес дома",
			notWanted: "Ответ:",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := myReportDetailMessage(&tc.detail)
			if !strings.Contains(text, tc.want) {
				t.Fatalf("expected %q in detail text, got %q", tc.want, text)
			}
			if tc.notWanted != "" && strings.Contains(text, tc.notWanted) {
				t.Fatalf("did not expect %q in detail text, got %q", tc.notWanted, text)
			}
		})
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
	assertAcceptedReportMessage(t, mock.lastText(), "15")
	assertMarkdownFormat(t, mock.lastMessage())
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
	assertAcceptedReportMessage(t, mock.lastText(), "15")
	assertMarkdownFormat(t, mock.lastMessage())
}

func TestFlowAllowsSecondReportAfterFirstSend(t *testing.T) {
	mock := &senderMock{}
	reportMock := &reportSinkMock{}
	creatorMock := &reportCreatorMock{}
	engine := New(mock, referenceProviderMock{}, WithReportSink(reportMock), WithReportCreator(creatorMock))
	userID := int64(1903)

	firstFlow := []maxapi.Update{
		callbackUpdate(userID, "cb1", "report:consent_yes"),
		textUpdate(userID, "1"),
		textUpdate(userID, "2"),
		textUpdate(userID, "79991234567"),
		textUpdate(userID, "ул. Мира, дом 1"),
		textUpdate(userID, "31/03/26 14:45"),
		textUpdate(userID, "Описание нарушения №1"),
		callbackUpdate(userID, "cb2", "report:skip_media"),
		callbackUpdate(userID, "cb3", "report:skip_extra"),
		callbackUpdate(userID, "cb4", "report:send"),
	}
	for _, step := range firstFlow {
		if err := engine.HandleUpdate(context.Background(), step); err != nil {
			t.Fatalf("HandleUpdate() first flow error = %v", err)
		}
	}

	secondFlow := []maxapi.Update{
		callbackUpdate(userID, "cb5", "menu:report"),
		callbackUpdate(userID, "cb6", "report:consent_yes"),
		textUpdate(userID, "2"),
		textUpdate(userID, "1"),
		textUpdate(userID, "79991234567"),
		textUpdate(userID, "ул. Ленина, дом 2"),
		textUpdate(userID, "01/04/26 10:20"),
		textUpdate(userID, "Описание нарушения №2"),
		callbackUpdate(userID, "cb7", "report:skip_media"),
		callbackUpdate(userID, "cb8", "report:skip_extra"),
		callbackUpdate(userID, "cb9", "report:send"),
	}
	for _, step := range secondFlow {
		if err := engine.HandleUpdate(context.Background(), step); err != nil {
			t.Fatalf("HandleUpdate() second flow error = %v", err)
		}
	}

	if len(creatorMock.requests) != 2 {
		t.Fatalf("expected 2 create report requests, got %d", len(creatorMock.requests))
	}
	if reportMock.count() != 4 {
		t.Fatalf("expected 4 outbox payload writes (2 initial + 2 backfill), got %d", reportMock.count())
	}

	for _, txt := range mock.messageTexts() {
		if strings.Contains(txt, "Не удалось создать обращение") {
			t.Fatalf("unexpected create error message in flow: %q", txt)
		}
	}

	assertAcceptedReportMessage(t, mock.lastText(), "15")
	assertMarkdownFormat(t, mock.lastMessage())
}

func TestFlowAllowsFreeformIncidentTime(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(105)

	steps := []maxapi.Update{
		callbackUpdate(userID, "cb1", "report:consent_yes"),
		textUpdate(userID, "1"),
		textUpdate(userID, "1"),
		textUpdate(userID, "89991234567"),
		textUpdate(userID, "ул. Мира, дом 1"),
		textUpdate(userID, "в ночь с пятницы на субботу, примерно в 23:30"),
	}

	for _, step := range steps {
		if err := engine.HandleUpdate(context.Background(), step); err != nil {
			t.Fatalf("HandleUpdate() error = %v", err)
		}
	}

	session := engine.session(userID)
	if session.State != stateReportDesc {
		t.Fatalf("expected state %q, got %q", stateReportDesc, session.State)
	}
	if session.Draft.IncidentTime != "в ночь с пятницы на субботу, примерно в 23:30" {
		t.Fatalf("expected freeform incident time to be saved, got %q", session.Draft.IncidentTime)
	}
}

func TestFlowValidationIncidentTimeError(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(106)

	tooLongTime := strings.Repeat("а", 101)

	steps := []maxapi.Update{
		callbackUpdate(userID, "cb1", "report:consent_yes"),
		textUpdate(userID, "1"),
		textUpdate(userID, "1"),
		textUpdate(userID, "89991234567"),
		textUpdate(userID, "ул. Мира, дом 1"),
		textUpdate(userID, tooLongTime),
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
	if !strings.Contains(mock.lastText(), "от 1 до 100 символов") {
		t.Fatalf("expected incident time validation text, got %q", mock.lastText())
	}
}

func TestFlowSkipMediaNotAllowedForNonQuietCategory(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(107)

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

func TestFlowAllowsVideoWithoutDurationMetadata(t *testing.T) {
	mock := &senderMock{}
	engine := New(mock, referenceProviderMock{})
	userID := int64(1007)

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

	raw, err := json.Marshal(map[string]any{"token": "video_token_without_duration"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	if err := engine.HandleUpdate(context.Background(), messageWithAttachmentsUpdate(userID, "", []maxapi.AttachmentBody{
		{Type: "video", RawPayload: raw},
	})); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	session := engine.session(userID)
	if session.State != stateReportExtra {
		t.Fatalf("expected state %q, got %q", stateReportExtra, session.State)
	}
	if !strings.Contains(mock.lastText(), "Вложения получены.") {
		t.Fatalf("expected successful media receive message, got %q", mock.lastText())
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

	texts := mock.messageTexts()
	if len(texts) < 2 {
		t.Fatalf("expected progress and final confirmation messages, got %d", len(texts))
	}
	if !strings.Contains(texts[len(texts)-2], "Загружаем приложенные вами файлы и формируем сообщение...") {
		t.Fatalf("expected progress message before final confirmation, got %q", texts[len(texts)-2])
	}
	assertAcceptedReportMessage(t, mock.lastText(), "15")
	assertMarkdownFormat(t, mock.lastMessage())
}

func TestFlowRestoresDraftMediaAfterReload(t *testing.T) {
	mock := &senderMock{}
	reportMock := &reportSinkMock{}
	creatorMock := &reportCreatorMock{}
	rawPhoto, err := json.Marshal(map[string]any{"url": "https://example.com/photo.webp"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	convMock := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:    44,
			MaxUserID: 1301,
			Stage:     reporting.UserStageFillingReport,
			ActiveDraft: &reporting.DraftMessage{
				ID:             91,
				Status:         reporting.MessageStatusDraft,
				Stage:          reporting.MessageStageAdditional,
				CategoryID:     11,
				MunicipalityID: 21,
				Phone:          "9991234567",
				Address:        "ул. Мира, дом 1",
				IncidentTime:   "ночью",
				Description:    "Описание нарушения",
				AttachmentLog:  []string{"- photo"},
				Attachments: []reporting.MediaAttachment{
					{
						Type:     "photo",
						Payload:  rawPhoto,
						FileName: "restored.webp",
						MIMEType: "image/webp",
					},
				},
			},
		},
	}
	engine := New(
		mock,
		referenceProviderMock{},
		WithConversationStore(convMock),
		WithReportSink(reportMock),
		WithReportCreator(creatorMock),
	)

	userID := int64(1301)
	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb1", "report:skip_extra")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb2", "report:send")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if len(creatorMock.requests) != 1 {
		t.Fatalf("expected 1 create report request, got %d", len(creatorMock.requests))
	}
	req := creatorMock.requests[0]
	if len(req.Attachments) != 1 {
		t.Fatalf("expected restored attachment in create request, got %+v", req.Attachments)
	}
	if req.Attachments[0].Type != "photo" {
		t.Fatalf("unexpected attachment type: %+v", req.Attachments[0])
	}
	if len(req.AttachmentLog) != 1 || req.AttachmentLog[0] != "- photo" {
		t.Fatalf("unexpected attachment log: %+v", req.AttachmentLog)
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

	if !strings.Contains(mock.lastText(), "Список сообщений, написанных заявителем") {
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
	if !strings.Contains(mock.lastText(), "Категория: Парковка") {
		t.Fatalf("expected category in list, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "Статус: **В работе**") {
		t.Fatalf("expected status line in list, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "Для просмотра детальной информации по сообщению отправьте номер сообщения в чат.") {
		t.Fatalf("expected prompt to send real report number, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "Сообщения хранятся не более 30 дней.") {
		t.Fatalf("expected retention note in list, got %q", mock.lastText())
	}
	listKeyboard := inlineKeyboardPayload(t, mock.lastMessage())
	assertMarkdownFormat(t, mock.lastMessage())
	if len(listKeyboard.Buttons) != 1 || listKeyboard.Buttons[0][0].Text != "Вернуться в начало" {
		t.Fatalf("expected only back-to-start button in list, got %+v", listKeyboard.Buttons)
	}

	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "16")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	assertMarkdownFormat(t, mock.lastMessage())

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
	assertMarkdownFormat(t, mock.lastMessage())
	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "15")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	assertMarkdownFormat(t, mock.lastMessage())
	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb2", "reports:back")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if !strings.Contains(mock.lastText(), "Список сообщений, написанных заявителем") {
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

func TestClarificationAllowsOpeningReportDetailFromMyReports(t *testing.T) {
	mock := &senderMock{}
	readerMock := &reportReaderMock{
		items: []reporting.ReportSummary{
			{
				ID:           15,
				ReportNumber: "15",
				MaxUserID:    1402,
				Status:       "clarification_requested",
				CreatedAt:    time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC),
			},
		},
		details: map[int64]*reporting.ReportDetail{
			15: {
				ReportSummary: reporting.ReportSummary{
					ID:               15,
					ReportNumber:     "15",
					MaxUserID:        1402,
					Status:           "clarification_requested",
					CategoryName:     "Тишина",
					MunicipalityName: "Волгоград",
					CreatedAt:        time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC),
				},
				StatusContext: "Уточните адрес дома.",
			},
		},
	}
	clarifications := &clarificationStoreMock{
		prompt: &reporting.ClarificationPrompt{
			ID:               93,
			MessageID:        15,
			NotificationID:   803,
			NotificationText: "По сообщению №15 нужно уточнить адрес.",
			Status:           reporting.RequestStatusNew,
			CreatedAt:        time.Now().UTC(),
		},
	}
	engine := New(
		mock,
		referenceProviderMock{},
		WithReportReader(readerMock),
		WithClarificationStore(clarifications),
	)

	userID := int64(1402)
	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb1", "menu:my_reports")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "15")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	assertMarkdownFormat(t, mock.lastMessage())
	if len(clarifications.answerReqs) != 0 {
		t.Fatalf("expected report number not to be treated as clarification answer, got %+v", clarifications.answerReqs)
	}
	if !strings.Contains(mock.lastText(), "Обращение №15") {
		t.Fatalf("expected report detail to be opened, got %q", mock.lastText())
	}
	if !strings.Contains(mock.lastText(), "Запрошенное уточнение информации: Уточните адрес дома.") {
		t.Fatalf("expected clarification context in report detail, got %q", mock.lastText())
	}
	session := engine.session(userID)
	if session.State != stateMyReportDetail {
		t.Fatalf("expected state %q, got %q", stateMyReportDetail, session.State)
	}
}

func TestClarificationDoesNotStealTextInMyReportDetail(t *testing.T) {
	mock := &senderMock{}
	readerMock := &reportReaderMock{
		items: []reporting.ReportSummary{
			{
				ID:           16,
				ReportNumber: "16",
				MaxUserID:    1403,
				Status:       "clarification_requested",
				CreatedAt:    time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC),
			},
		},
		details: map[int64]*reporting.ReportDetail{
			16: {
				ReportSummary: reporting.ReportSummary{
					ID:           16,
					ReportNumber: "16",
					MaxUserID:    1403,
					Status:       "clarification_requested",
					CreatedAt:    time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC),
				},
				StatusContext: "Уточните ориентир.",
			},
		},
	}
	clarifications := &clarificationStoreMock{
		prompt: &reporting.ClarificationPrompt{
			ID:               94,
			MessageID:        16,
			NotificationID:   804,
			NotificationText: "По сообщению №16 нужно уточнить ориентир.",
			Status:           reporting.RequestStatusNew,
			CreatedAt:        time.Now().UTC(),
		},
	}
	engine := New(
		mock,
		referenceProviderMock{},
		WithReportReader(readerMock),
		WithClarificationStore(clarifications),
	)

	userID := int64(1403)
	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb1", "menu:my_reports")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "16")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	assertMarkdownFormat(t, mock.lastMessage())
	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "какой-то текст")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if len(clarifications.answerReqs) != 0 {
		t.Fatalf("expected text in report detail not to be treated as clarification answer, got %+v", clarifications.answerReqs)
	}
	if mock.lastText() != "Для навигации используйте кнопки ниже." {
		t.Fatalf("expected detail navigation hint, got %q", mock.lastText())
	}
	session := engine.session(userID)
	if session.State != stateMyReportDetail {
		t.Fatalf("expected state %q, got %q", stateMyReportDetail, session.State)
	}
}

func TestClarificationAnswerHasPriorityOverDraftAndResumesFlow(t *testing.T) {
	mock := &senderMock{}
	conversationMock := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:        70,
			MaxUserID:     1400,
			Stage:         reporting.UserStageFillingReport,
			PreviousStage: reporting.UserStageMainMenu,
			ActiveDraft: &reporting.DraftMessage{
				ID:             501,
				Status:         reporting.MessageStatusDraft,
				Stage:          reporting.MessageStageAddress,
				CategoryID:     11,
				MunicipalityID: 21,
				Phone:          "9991234567",
			},
		},
	}
	clarifications := &clarificationStoreMock{
		prompt: &reporting.ClarificationPrompt{
			ID:               91,
			MessageID:        15,
			NotificationID:   801,
			NotificationText: "По сообщению №15 нужно уточнить адрес.",
			Status:           reporting.RequestStatusNew,
			CreatedAt:        time.Now().UTC(),
		},
	}
	engine := New(
		mock,
		referenceProviderMock{},
		WithConversationStore(conversationMock),
		WithClarificationStore(clarifications),
		WithClarificationAckDelay(0),
	)

	userID := int64(1400)
	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "Это ответ на уточнение")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if len(clarifications.answerReqs) != 1 {
		t.Fatalf("expected one clarification answer, got %d", len(clarifications.answerReqs))
	}
	if clarifications.answerReqs[0].ClarificationID != 91 {
		t.Fatalf("expected clarification id 91, got %+v", clarifications.answerReqs[0])
	}
	if clarifications.answerReqs[0].Answer != "Это ответ на уточнение" {
		t.Fatalf("unexpected clarification answer payload: %+v", clarifications.answerReqs[0])
	}
	texts := mock.messageTexts()
	if len(texts) != 2 {
		t.Fatalf("expected acknowledgment and resumed prompt, got %d messages: %#v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "Спасибо за уточнение.") {
		t.Fatalf("expected acknowledgment first, got %q", texts[0])
	}
	if !strings.Contains(mock.lastText(), "Введите адрес или место совершения правонарушения.") {
		t.Fatalf("expected draft flow to resume after clarification, got %q", mock.lastText())
	}

	session := engine.session(userID)
	if session.State != stateReportAddress {
		t.Fatalf("expected resumed draft state %q, got %q", stateReportAddress, session.State)
	}
}

func TestClarificationRejectResumesDraftFlow(t *testing.T) {
	mock := &senderMock{}
	conversationMock := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:    71,
			MaxUserID: 1401,
			Stage:     reporting.UserStageFillingReport,
			ActiveDraft: &reporting.DraftMessage{
				ID:             502,
				Status:         reporting.MessageStatusDraft,
				Stage:          reporting.MessageStagePhone,
				CategoryID:     11,
				MunicipalityID: 21,
			},
		},
	}
	clarifications := &clarificationStoreMock{
		prompt: &reporting.ClarificationPrompt{
			ID:               92,
			MessageID:        16,
			NotificationID:   802,
			NotificationText: "Уточните обстоятельства по сообщению №16.",
			Status:           reporting.RequestStatusNew,
			CreatedAt:        time.Now().UTC(),
		},
	}
	engine := New(
		mock,
		referenceProviderMock{},
		WithConversationStore(conversationMock),
		WithClarificationStore(clarifications),
		WithClarificationAckDelay(0),
	)

	userID := int64(1401)
	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb-clarification-reject", "clarification:reject")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if len(clarifications.rejectReqs) != 1 {
		t.Fatalf("expected one clarification reject, got %d", len(clarifications.rejectReqs))
	}
	if clarifications.rejectReqs[0].ClarificationID != 92 {
		t.Fatalf("expected clarification id 92, got %+v", clarifications.rejectReqs[0])
	}
	texts := mock.messageTexts()
	if len(texts) != 2 {
		t.Fatalf("expected rejection acknowledgment and resumed prompt, got %d messages: %#v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "Уточнение отклонено.") {
		t.Fatalf("expected rejection acknowledgment first, got %q", texts[0])
	}
	if !strings.Contains(mock.lastText(), "Введите номер телефона") {
		t.Fatalf("expected draft flow to resume after reject, got %q", mock.lastText())
	}
}

func TestClarificationAnswerAcknowledgesBeforeNextClarification(t *testing.T) {
	mock := &senderMock{}
	conversationMock := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:        74,
			MaxUserID:     1404,
			Stage:         reporting.UserStageWaitingClarification,
			PreviousStage: reporting.UserStageFillingReport,
			ActiveDraft: &reporting.DraftMessage{
				ID:    505,
				Stage: reporting.MessageStageAddress,
			},
		},
	}
	clarifications := &clarificationStoreMock{
		prompt: &reporting.ClarificationPrompt{
			ID:               95,
			MessageID:        18,
			NotificationID:   803,
			NotificationText: "По сообщению №18 нужно первое уточнение.",
			Status:           reporting.RequestStatusNew,
			CreatedAt:        time.Now().UTC(),
		},
		afterAnswerPrompt: &reporting.ClarificationPrompt{
			ID:               96,
			MessageID:        19,
			NotificationID:   804,
			NotificationText: "По сообщению №19 нужно ещё одно уточнение.",
			Status:           reporting.RequestStatusNew,
			CreatedAt:        time.Now().UTC(),
		},
	}
	engine := New(
		mock,
		referenceProviderMock{},
		WithConversationStore(conversationMock),
		WithClarificationStore(clarifications),
		WithClarificationAckDelay(0),
	)

	userID := int64(1404)
	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "Первый ответ")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	texts := mock.messageTexts()
	if len(texts) != 2 {
		t.Fatalf("expected acknowledgment and next clarification prompt, got %d messages: %#v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "Спасибо за уточнение. Есть ещё одно обращение") {
		t.Fatalf("expected acknowledgment before next clarification, got %q", texts[0])
	}
	if !strings.Contains(texts[1], "По сообщению №19 нужно ещё одно уточнение.") {
		t.Fatalf("expected next clarification prompt, got %q", texts[1])
	}
}

func TestClarificationAnswerRepairsPreviousStageAndClearsItAfterResume(t *testing.T) {
	mock := &senderMock{}
	conversationMock := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:        73,
			MaxUserID:     1403,
			Stage:         reporting.UserStageWaitingClarification,
			PreviousStage: reporting.UserStageWaitingClarification,
			ActiveDraft: &reporting.DraftMessage{
				ID:             504,
				Status:         reporting.MessageStatusDraft,
				Stage:          reporting.MessageStageMunicipality,
				CategoryID:     11,
				MunicipalityID: 0,
			},
		},
	}
	clarifications := &clarificationStoreMock{
		prompt: &reporting.ClarificationPrompt{
			ID:               94,
			MessageID:        18,
			NotificationID:   803,
			NotificationText: "Уточните детали по сообщению №18.",
			Status:           reporting.RequestStatusNew,
			CreatedAt:        time.Now().UTC(),
		},
	}
	engine := New(
		mock,
		referenceProviderMock{},
		WithConversationStore(conversationMock),
		WithClarificationStore(clarifications),
		WithClarificationAckDelay(0),
	)

	userID := int64(1403)
	if err := engine.HandleUpdate(context.Background(), textUpdate(userID, "Ответ")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	if got := len(conversationMock.saveRequests); got < 2 {
		t.Fatalf("expected clarification save and resume save, got %d", got)
	}
	firstSave := conversationMock.saveRequests[0]
	if firstSave.UserStage != reporting.UserStageWaitingClarification || firstSave.PreviousStage != reporting.UserStageFillingReport {
		t.Fatalf("expected repaired waiting_clarification save, got %+v", firstSave)
	}
	lastSave := conversationMock.saveRequests[len(conversationMock.saveRequests)-1]
	if lastSave.UserStage != reporting.UserStageFillingReport {
		t.Fatalf("expected resumed filling_report save, got %+v", lastSave)
	}
	if lastSave.PreviousStage != "" {
		t.Fatalf("expected previous stage to be cleared after resume, got %+v", lastSave)
	}
	if !strings.Contains(mock.lastText(), "Выберите муниципалитет") && !strings.Contains(mock.lastText(), "Муниципалитет") {
		t.Fatalf("expected municipality step to resume, got %q", mock.lastText())
	}
}

func TestClarificationMenuShowsMainMenuWithoutDeletingDraft(t *testing.T) {
	mock := &senderMock{}
	conversationMock := &conversationStoreMock{
		state: &reporting.ConversationState{
			UserID:    72,
			MaxUserID: 1402,
			Stage:     reporting.UserStageFillingReport,
			ActiveDraft: &reporting.DraftMessage{
				ID:             503,
				Status:         reporting.MessageStatusDraft,
				Stage:          reporting.MessageStageDescription,
				CategoryID:     11,
				MunicipalityID: 21,
				Phone:          "9991234567",
				Address:        "ул. Мира, дом 1",
			},
		},
	}
	clarifications := &clarificationStoreMock{
		prompt: &reporting.ClarificationPrompt{
			ID:               93,
			MessageID:        17,
			NotificationText: "Нужно уточнить детали.",
			Status:           reporting.RequestStatusNew,
			CreatedAt:        time.Now().UTC(),
		},
	}
	engine := New(
		mock,
		referenceProviderMock{},
		WithConversationStore(conversationMock),
		WithClarificationStore(clarifications),
		WithClarificationAckDelay(0),
	)

	userID := int64(1402)
	if err := engine.HandleUpdate(context.Background(), callbackUpdate(userID, "cb-clarification-menu", "menu:main")); err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}

	assertMainMenuKeyboard(t, mock.lastMessage())
	if got := len(clarifications.rejectReqs); got != 0 {
		t.Fatalf("expected no clarification reject on menu, got %d", got)
	}
	lastSave := conversationMock.saveRequests[len(conversationMock.saveRequests)-1]
	if lastSave.DeleteDraft {
		t.Fatalf("expected menu from clarification to preserve draft")
	}
	if conversationMock.state == nil || conversationMock.state.ActiveDraft == nil {
		t.Fatalf("expected draft to remain in conversation store")
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
