package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"max_bot/internal/maxapi"
	"max_bot/internal/reference"
	"max_bot/internal/report"
	"max_bot/internal/reporting"
)

var phoneRe = regexp.MustCompile(`^9\d{9}$`)

const maxTotalVideoDurationSeconds = 120

type Engine struct {
	client         Sender
	refData        reference.Provider
	reportSink     ReportSink
	reportCreator  ReportCreator
	reportReader   ReportReader
	conversations  ConversationStore
	clarifications ClarificationStore
	ackDelay       time.Duration
	mu             sync.Mutex
	sessions       map[int64]*Session
}

type Sender interface {
	SendMessage(ctx context.Context, userID int64, body maxapi.NewMessageBody) error
	AnswerCallback(ctx context.Context, callbackID, notification string) error
}

type Session struct {
	State         BotState
	UserStage     reporting.UserStage
	PreviousStage reporting.UserStage
	MessageStage  reporting.MessageStage
	StartedAt     time.Time
	Draft         Draft
	Trace         []report.DialogStep
	Reports       []reporting.ReportSummary
	Loaded        bool
	HasUserRecord bool
}

type Draft struct {
	CategoryID       int
	CategoryName     string
	MunicipalityID   int
	MunicipalityName string
	Phone            string
	Address          string
	IncidentTime     string
	Description      string
	ExtraInfo        string
	AttachmentLog    []string
	Media            []reporting.MediaAttachment
}

type ReportSink interface {
	Store(context.Context, report.DialogPayload) error
}

type ReportCreator interface {
	CreateReport(context.Context, reporting.CreateReportRequest) (*reporting.CreatedReport, error)
}

type ReportReader interface {
	ListReportsByMaxUserID(context.Context, int64) ([]reporting.ReportSummary, error)
	GetReportByID(context.Context, int64) (*reporting.ReportDetail, error)
}

type ConversationStore interface {
	GetConversation(context.Context, int64) (*reporting.ConversationState, error)
	SaveConversation(context.Context, reporting.SaveConversationRequest) (*reporting.ConversationState, error)
}

type ClarificationStore interface {
	GetPendingClarification(context.Context, int64) (*reporting.ClarificationPrompt, error)
	AnswerClarification(context.Context, reporting.ClarificationAnswerRequest) error
	RejectClarification(context.Context, reporting.ClarificationRejectRequest) error
}

type Option func(*Engine)

func WithReportSink(sink ReportSink) Option {
	return func(e *Engine) {
		if sink != nil {
			e.reportSink = sink
		}
	}
}

func WithReportCreator(creator ReportCreator) Option {
	return func(e *Engine) {
		if creator != nil {
			e.reportCreator = creator
		}
	}
}

func WithReportReader(reader ReportReader) Option {
	return func(e *Engine) {
		if reader != nil {
			e.reportReader = reader
		}
	}
}

func WithConversationStore(store ConversationStore) Option {
	return func(e *Engine) {
		if store != nil {
			e.conversations = store
		}
	}
}

func WithClarificationStore(store ClarificationStore) Option {
	return func(e *Engine) {
		if store != nil {
			e.clarifications = store
		}
	}
}

func WithClarificationAckDelay(delay time.Duration) Option {
	return func(e *Engine) {
		if delay >= 0 {
			e.ackDelay = delay
		}
	}
}

func New(client Sender, refData reference.Provider, options ...Option) *Engine {
	engine := &Engine{
		client:         client,
		refData:        refData,
		reportSink:     noopReportSink{},
		reportCreator:  noopReportCreator{},
		reportReader:   noopReportReader{},
		conversations:  noopConversationStore{},
		clarifications: noopClarificationStore{},
		ackDelay:       2 * time.Second,
		sessions:       make(map[int64]*Session),
	}
	for _, option := range options {
		option(engine)
	}
	return engine
}

type noopReportSink struct{}

func (noopReportSink) Store(context.Context, report.DialogPayload) error { return nil }

type noopReportCreator struct{}

func (noopReportCreator) CreateReport(context.Context, reporting.CreateReportRequest) (*reporting.CreatedReport, error) {
	return &reporting.CreatedReport{
		ID:           0,
		ReportNumber: "DEV",
		Status:       "moderation",
		Stage:        "sended",
	}, nil
}

type noopReportReader struct{}

func (noopReportReader) ListReportsByMaxUserID(context.Context, int64) ([]reporting.ReportSummary, error) {
	return nil, nil
}

func (noopReportReader) GetReportByID(context.Context, int64) (*reporting.ReportDetail, error) {
	return nil, reporting.ErrNotFound
}

type noopConversationStore struct{}

func (noopConversationStore) GetConversation(context.Context, int64) (*reporting.ConversationState, error) {
	return &reporting.ConversationState{Stage: reporting.UserStageMainMenu}, nil
}

func (noopConversationStore) SaveConversation(_ context.Context, req reporting.SaveConversationRequest) (*reporting.ConversationState, error) {
	state := &reporting.ConversationState{
		MaxUserID:     req.MaxUserID,
		Stage:         req.UserStage,
		PreviousStage: req.PreviousStage,
	}
	if req.ActiveDraft != nil {
		copied := *req.ActiveDraft
		state.ActiveDraft = &copied
	}
	return state, nil
}

type noopClarificationStore struct{}

func (noopClarificationStore) GetPendingClarification(context.Context, int64) (*reporting.ClarificationPrompt, error) {
	return nil, reporting.ErrNotFound
}

func (noopClarificationStore) AnswerClarification(context.Context, reporting.ClarificationAnswerRequest) error {
	return reporting.ErrNotFound
}

func (noopClarificationStore) RejectClarification(context.Context, reporting.ClarificationRejectRequest) error {
	return reporting.ErrNotFound
}

func (e *Engine) HandleUpdate(ctx context.Context, upd maxapi.Update) error {
	switch upd.UpdateType {
	case "bot_started":
		if upd.User == nil {
			return nil
		}
		slog.Info("бот запущен пользователем", "user_id", upd.User.UserID, "payload", upd.Payload)
		return e.showWelcome(ctx, upd.User.UserID)
	case "message_callback":
		return e.handleCallback(ctx, upd)
	case "message_created":
		return e.handleMessage(ctx, upd)
	default:
		slog.Debug("обновление пропущено", "type", upd.UpdateType)
		return nil
	}
}

func (e *Engine) handleCallback(ctx context.Context, upd maxapi.Update) error {
	if upd.Callback == nil {
		return nil
	}

	userID := upd.Callback.User.UserID
	payload := upd.Callback.Payload
	session := e.session(userID)
	if err := e.ensureSessionLoaded(ctx, userID, session); err != nil {
		return e.sendText(ctx, userID, "Не удалось восстановить ваше состояние. Попробуйте ещё раз немного позже.", backToMenuKeyboard())
	}
	e.appendTrace(userID, report.DialogStep{
		At:      time.Now().UTC(),
		Kind:    "callback",
		State:   string(session.State),
		Payload: payload,
	})
	slog.Info("получен callback", "user_id", userID, "payload", payload)

	if err := e.client.AnswerCallback(ctx, upd.Callback.CallbackID, "Принято"); err != nil {
		slog.Warn("не удалось ответить на callback", "error", err.Error())
	}

	handled, err := e.handleClarificationCallback(ctx, userID, session, payload)
	if handled || err != nil {
		return err
	}

	switch payload {
	case "menu:main":
		return e.showMainMenu(ctx, userID)
	case "menu:about":
		return e.showAbout(ctx, userID)
	case "menu:feedback":
		return e.showFeedback(ctx, userID)
	case "reports:back":
		return e.showMyReportsList(ctx, userID)
	case "menu:legal":
		e.setState(userID, stateMainMenu)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		text, attachments := legalMessage()
		return e.replyMarkdown(ctx, userID, text, attachments)
	case "menu:violations":
		categories, err := e.loadCategories(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		e.setState(userID, stateMainMenu)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		text, attachments := violationsMessage(categories)
		return e.reply(ctx, userID, text, attachments)
	case "menu:report":
		e.setState(userID, stateReportConsent)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		text, attachments := consentMessage()
		return e.replyMarkdown(ctx, userID, text, attachments)
	case "menu:my_reports":
		return e.showMyReportsList(ctx, userID)
	case "report:consent_yes":
		categories, err := e.loadCategories(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		e.setState(userID, stateReportCategory)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendText(ctx, userID, categoriesPrompt(categories), backToMenuKeyboard())
	case "report:skip_media":
		if !canSkipMedia(session.Draft) {
			return e.sendText(ctx, userID, "Пропуск вложений доступен только для категории «Тишина и покой». Отправьте фото и/или видео.", mediaKeyboard(false))
		}
		e.setState(userID, stateReportExtra)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendText(ctx, userID, "Напишите дополнительную информацию или нажмите \"Пропустить\".", extraInfoKeyboard())
	case "report:skip_extra":
		e.setState(userID, stateReportConfirm)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendDraftSummary(ctx, userID)
	case "report:send":
		return e.finishDraft(ctx, userID)
	case "report:cancel":
		e.resetSession(userID)
		reset := e.session(userID)
		if err := e.persistStateOrReply(ctx, userID, reset, true); err != nil {
			return err
		}
		return e.sendText(ctx, userID, "Отправка черновика отменена.", backToMenuKeyboard())
	default:
		return e.showUnsupportedInput(ctx, userID)
	}
}

func (e *Engine) handleMessage(ctx context.Context, upd maxapi.Update) error {
	if upd.Message == nil || upd.Message.Sender == nil {
		return nil
	}

	userID := upd.Message.Sender.UserID
	text := strings.TrimSpace(upd.Message.Body.Text)
	attachments := upd.Message.Body.Attachments

	logIncomingMessage(userID, text, attachments)
	session := e.session(userID)
	if err := e.ensureSessionLoaded(ctx, userID, session); err != nil {
		return e.sendText(ctx, userID, "Не удалось восстановить ваше состояние. Попробуйте ещё раз немного позже.", backToMenuKeyboard())
	}
	if !session.HasUserRecord && text != "/start" {
		return e.showWelcome(ctx, userID)
	}

	e.appendTrace(userID, report.DialogStep{
		At:              time.Now().UTC(),
		Kind:            "message",
		State:           string(session.State),
		Text:            text,
		AttachmentTypes: attachmentTypes(attachments),
	})

	handled, err := e.handleReportNavigationMessageBeforeClarification(ctx, userID, session, text)
	if handled || err != nil {
		return err
	}

	handled, err = e.handleClarificationMessage(ctx, userID, session, text)
	if handled || err != nil {
		return err
	}

	if text == "/start" {
		return e.showWelcome(ctx, userID)
	}
	if strings.EqualFold(text, "меню") {
		return e.showMainMenu(ctx, userID)
	}

	switch session.State {
	case stateFeedback:
		return e.showUnsupportedInput(ctx, userID)
	case stateLegalInfo:
		text, attachments := legalMessage()
		return e.sendTextMarkdown(ctx, userID, "В этом разделе используйте кнопки ниже.\n\n"+text, attachments)
	case stateViolationsList:
		categories, err := e.loadCategories(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		text, attachments := violationsMessage(categories)
		return e.sendText(ctx, userID, "В этом разделе используйте кнопки ниже.\n\n"+text, attachments)
	case stateReportConsent:
		text, attachments := consentMessage()
		return e.sendTextMarkdown(ctx, userID, "Для продолжения используйте кнопки ниже.\n\n"+text, attachments)
	case stateMyReportsList, stateMyReportDetail:
		return e.handleReportNavigationMessage(ctx, userID, session, text)
	case stateReportCategory:
		categories, err := e.loadCategories(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		index, ok := parseChoice(text, len(categories))
		if !ok {
			return e.sendText(ctx, userID, fmt.Sprintf("Категория с номером «%s» не найдена, повторите попытку", strings.TrimSpace(text)), backToMenuKeyboard())
		}
		selected := categories[index-1]
		session.Draft.CategoryID = selected.ID
		session.Draft.CategoryName = selected.Name
		applyState(session, stateReportMunicipal)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		municipalities, err := e.loadMunicipalities(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		return e.sendText(ctx, userID, municipalitiesPrompt(municipalities), backToMenuKeyboard())
	case stateReportMunicipal:
		municipalities, err := e.loadMunicipalities(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		index, ok := parseChoice(text, len(municipalities))
		if !ok {
			return e.sendText(ctx, userID, fmt.Sprintf("Муниципалитет с номером «%s» не найден, повторите попытку", strings.TrimSpace(text)), backToMenuKeyboard())
		}
		selected := municipalities[index-1]
		session.Draft.MunicipalityID = selected.ID
		session.Draft.MunicipalityName = selected.Name
		applyState(session, stateReportPhone)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendText(ctx, userID, "Введите номер телефона в формате 10 цифр, начиная с 9, или отправьте контакт по кнопке ниже.", phoneKeyboard())
	case stateReportPhone:
		phone := parsePhone(text, attachments)
		if !phoneRe.MatchString(phone) {
			return e.sendText(ctx, userID, "Номер не соответствует формату. Введите 10 цифр, начиная с 9.", phoneKeyboard())
		}
		session.Draft.Phone = phone
		applyState(session, stateReportAddress)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendText(ctx, userID, "Введите адрес или место совершения правонарушения.", backToMenuKeyboard())
	case stateReportAddress:
		if len(text) == 0 || len([]rune(text)) > 1000 {
			return e.sendText(ctx, userID, "Адрес должен быть от 1 до 1000 символов.", backToMenuKeyboard())
		}
		session.Draft.Address = text
		applyState(session, stateReportTime)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendText(ctx, userID, "Введите дату, время или период совершения правонарушения. Максимум 100 символов.", backToMenuKeyboard())
	case stateReportTime:
		incidentTime := strings.TrimSpace(text)
		if len(incidentTime) == 0 || len([]rune(incidentTime)) > 100 {
			return e.sendText(ctx, userID, "Дата и время должны быть от 1 до 100 символов.", backToMenuKeyboard())
		}
		session.Draft.IncidentTime = incidentTime
		applyState(session, stateReportDesc)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendText(ctx, userID, "Опишите суть правонарушения. Максимум 3900 символов.", backToMenuKeyboard())
	case stateReportDesc:
		if len(text) == 0 || len([]rune(text)) > 3900 {
			return e.sendText(ctx, userID, "Описание должно быть от 1 до 3900 символов.", backToMenuKeyboard())
		}
		session.Draft.Description = text
		applyState(session, stateReportMedia)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		allowSkip := canSkipMedia(session.Draft)
		if allowSkip {
			return e.sendText(ctx, userID, "Отправьте фото/видео или нажмите \"Пропустить\".", mediaKeyboard(true))
		}
		return e.sendText(ctx, userID, "Отправьте фото/видео.", mediaKeyboard(false))
	case stateReportMedia:
		allowSkip := canSkipMedia(session.Draft)
		if len(attachments) == 0 {
			if allowSkip {
				return e.sendText(ctx, userID, "На этом шаге ждём фото/видео или кнопку \"Пропустить\".", mediaKeyboard(true))
			}
			return e.sendText(ctx, userID, "На этом шаге ждём фото/видео.", mediaKeyboard(false))
		}
		if len(attachments) > 5 {
			return e.sendText(ctx, userID, "Получено больше 5 вложений. Повторите попытку.", mediaKeyboard(allowSkip))
		}
		totalVideoDuration, unknownVideoDurationCount, err := totalVideoDurationSeconds(attachments)
		if err != nil {
			return e.sendText(ctx, userID, "Не удалось определить длительность видео. Проверьте вложения и повторите попытку.", mediaKeyboard(allowSkip))
		}
		if unknownVideoDurationCount > 0 {
			slog.Warn(
				"для части видео не удалось определить длительность, ограничение 2 минуты проверено только по доступным данным",
				"user_id", userID,
				"unknown_video_count", unknownVideoDurationCount,
			)
		}
		if totalVideoDuration > maxTotalVideoDurationSeconds {
			return e.sendText(ctx, userID, "Суммарная длительность видео превышает 2 минуты. Сократите длительность и отправьте вложения снова.", mediaKeyboard(allowSkip))
		}
		logs, ok := attachmentSummary(attachments)
		if !ok {
			if allowSkip {
				return e.sendText(ctx, userID, "Поддерживаются только фото и видео. Попробуйте ещё раз или пропустите шаг.", mediaKeyboard(true))
			}
			return e.sendText(ctx, userID, "Поддерживаются только фото и видео. Попробуйте ещё раз.", mediaKeyboard(false))
		}
		session.Draft.AttachmentLog = append([]string(nil), logs...)
		session.Draft.Media = collectMediaAttachments(attachments)
		applyState(session, stateReportExtra)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendText(ctx, userID, "Вложения получены. Напишите дополнительную информацию или нажмите \"Пропустить\".", extraInfoKeyboard())
	case stateReportExtra:
		if len([]rune(text)) > 3900 {
			return e.sendText(ctx, userID, "Дополнительная информация должна быть не длиннее 3900 символов.", extraInfoKeyboard())
		}
		session.Draft.ExtraInfo = text
		applyState(session, stateReportConfirm)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendDraftSummary(ctx, userID)
	default:
		return e.showUnsupportedInput(ctx, userID)
	}
}

func (e *Engine) handleReportNavigationMessageBeforeClarification(ctx context.Context, userID int64, session *Session, text string) (bool, error) {
	switch session.State {
	case stateMyReportsList, stateMyReportDetail:
		if text == "/start" {
			return true, e.showWelcome(ctx, userID)
		}
		if strings.EqualFold(text, "меню") {
			return true, e.showMainMenu(ctx, userID)
		}
		return true, e.handleReportNavigationMessage(ctx, userID, session, text)
	default:
		return false, nil
	}
}

func (e *Engine) handleReportNavigationMessage(ctx context.Context, userID int64, session *Session, text string) error {
	switch session.State {
	case stateMyReportsList:
		if len(session.Reports) == 0 {
			return e.sendText(ctx, userID, "Список обращений устарел. Вернитесь в начало и откройте раздел снова.", myReportsKeyboard())
		}
		selected, ok := findReportByNumber(text, session.Reports)
		if !ok {
			return e.sendText(ctx, userID, "Отправьте номер сообщения из списка, чтобы посмотреть подробности.", myReportsKeyboard())
		}
		detail, err := e.reportReader.GetReportByID(ctx, selected.ID)
		if err != nil {
			slog.Error("не удалось загрузить карточку обращения", "user_id", userID, "report_id", selected.ID, "error", err.Error())
			return e.sendText(ctx, userID, "Не удалось загрузить подробную информацию по обращению. Попробуйте ещё раз.", myReportsKeyboard())
		}
		applyState(session, stateMyReportDetail)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendTextMarkdown(ctx, userID, myReportDetailMessage(detail), myReportDetailKeyboard())
	case stateMyReportDetail:
		return e.sendText(ctx, userID, "Для навигации используйте кнопки ниже.", myReportDetailKeyboard())
	default:
		return e.showUnsupportedInput(ctx, userID)
	}
}

func (e *Engine) showMainMenu(ctx context.Context, userID int64) error {
	e.resetSession(userID)
	session := e.session(userID)
	if err := e.persistStateOrReply(ctx, userID, session, true); err != nil {
		return err
	}
	text, attachments := mainMenuMessage()
	return e.reply(ctx, userID, text, attachments)
}

func (e *Engine) showMainMenuWithoutReset(ctx context.Context, userID int64, session *Session) error {
	applyState(session, stateMainMenu)
	if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
		return err
	}
	text, attachments := mainMenuMessage()
	return e.reply(ctx, userID, text, attachments)
}

func (e *Engine) showMyReportsList(ctx context.Context, userID int64) error {
	session := e.session(userID)
	items, err := e.reportReader.ListReportsByMaxUserID(ctx, userID)
	if err != nil {
		slog.Error("не удалось загрузить обращения пользователя", "user_id", userID, "error", err.Error())
		return e.sendText(ctx, userID, "Не удалось загрузить ваши обращения. Попробуйте ещё раз немного позже.", backToStartKeyboard())
	}
	applyState(session, stateMyReportsList)
	session.Reports = append([]reporting.ReportSummary(nil), items...)
	if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
		return err
	}
	if len(items) == 0 {
		return e.sendText(ctx, userID, "У вас пока нет отправленных обращений.", backToStartKeyboard())
	}
	return e.sendTextMarkdown(ctx, userID, myReportsListMessage(items), myReportsKeyboard())
}

func (e *Engine) showWelcome(ctx context.Context, userID int64) error {
	e.resetSession(userID)
	session := e.session(userID)
	e.setState(userID, stateAbout)
	session = e.session(userID)
	if err := e.persistStateOrReply(ctx, userID, session, true); err != nil {
		return err
	}
	text, attachments := welcomeMessage()
	if err := e.reply(ctx, userID, text, attachments); err != nil {
		return err
	}
	return e.showMainMenu(ctx, userID)
}

func (e *Engine) showAbout(ctx context.Context, userID int64) error {
	session := e.session(userID)
	applyState(session, stateMainMenu)
	if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
		return err
	}
	text, _ := aboutMessage()
	return e.sendText(ctx, userID, text, mainMenuKeyboard())
}

func (e *Engine) showFeedback(ctx context.Context, userID int64) error {
	session := e.session(userID)
	applyState(session, stateFeedback)
	if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
		return err
	}
	text, attachments := feedbackMessage()
	return e.sendText(ctx, userID, text, attachments)
}

func (e *Engine) showUnsupportedInput(ctx context.Context, userID int64) error {
	e.resetSession(userID)
	e.setState(userID, stateUnsupportedInput)
	session := e.session(userID)
	if err := e.persistStateOrReply(ctx, userID, session, true); err != nil {
		return err
	}
	text, attachments := unsupportedInputMessage()
	if err := e.reply(ctx, userID, text, attachments); err != nil {
		return err
	}
	return e.showMainMenu(ctx, userID)
}

func (e *Engine) handleClarificationCallback(ctx context.Context, userID int64, session *Session, payload string) (bool, error) {
	prompt, handled, err := e.currentClarificationPrompt(ctx, userID, session)
	if handled || err != nil {
		return true, err
	}
	if prompt == nil {
		return false, nil
	}

	if payload == "clarification:reject" {
		if err := e.clarifications.RejectClarification(ctx, reporting.ClarificationRejectRequest{
			ClarificationID: prompt.ID,
			MaxUserID:       userID,
		}); err != nil {
			slog.Error("не удалось отклонить уточнение", "user_id", userID, "clarification_id", prompt.ID, "error", err.Error())
			return true, e.sendText(ctx, userID, "Не удалось обработать отказ от уточнения. Попробуйте ещё раз немного позже.", clarificationKeyboard())
		}
		return true, e.resumeAfterClarification(ctx, userID, session, "Уточнение отклонено.")
	}
	if payload == "menu:main" {
		return true, e.showMainMenuWithoutReset(ctx, userID, session)
	}
	if strings.HasPrefix(payload, "menu:") {
		return false, nil
	}

	return true, e.sendClarificationPrompt(ctx, userID, prompt)
}

func (e *Engine) handleClarificationMessage(ctx context.Context, userID int64, session *Session, text string) (bool, error) {
	prompt, handled, err := e.currentClarificationPrompt(ctx, userID, session)
	if handled || err != nil {
		return true, err
	}
	if prompt == nil {
		return false, nil
	}

	if strings.TrimSpace(text) == "" {
		return true, e.sendClarificationPrompt(ctx, userID, prompt)
	}

	if err := e.clarifications.AnswerClarification(ctx, reporting.ClarificationAnswerRequest{
		ClarificationID: prompt.ID,
		MaxUserID:       userID,
		Answer:          text,
	}); err != nil {
		slog.Error("не удалось сохранить ответ на уточнение", "user_id", userID, "clarification_id", prompt.ID, "error", err.Error())
		return true, e.sendText(ctx, userID, "Не удалось сохранить ответ на уточнение. Попробуйте ещё раз немного позже.", clarificationKeyboard())
	}

	return true, e.resumeAfterClarification(ctx, userID, session, "Спасибо за уточнение.")
}

func (e *Engine) currentClarificationPrompt(ctx context.Context, userID int64, session *Session) (*reporting.ClarificationPrompt, bool, error) {
	prompt, err := e.clarifications.GetPendingClarification(ctx, userID)
	if err != nil {
		if errors.Is(err, reporting.ErrNotFound) {
			if session.State == stateClarificationRequested || session.UserStage == reporting.UserStageWaitingClarification {
				return nil, true, e.resumeAfterClarification(ctx, userID, session, "")
			}
			return nil, false, nil
		}
		slog.Error("не удалось получить активное уточнение", "user_id", userID, "error", err.Error())
		return nil, true, e.sendText(ctx, userID, "Не удалось получить активное уточнение. Попробуйте ещё раз немного позже.", backToMenuKeyboard())
	}

	e.ensurePreviousStage(session)
	applyState(session, stateClarificationRequested)
	if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
		return nil, true, err
	}
	return prompt, false, nil
}

func (e *Engine) resumeAfterClarification(ctx context.Context, userID int64, session *Session, acknowledgment string) error {
	prompt, err := e.clarifications.GetPendingClarification(ctx, userID)
	if err == nil {
		if acknowledgment != "" {
			if err := e.sendText(ctx, userID, acknowledgment+" Есть ещё одно обращение, по которому требуется уточнение.", nil); err != nil {
				return err
			}
			if err := e.pauseAfterClarificationAck(ctx); err != nil {
				return err
			}
		}
		applyState(session, stateClarificationRequested)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendClarificationPrompt(ctx, userID, prompt)
	}
	if err != nil && !errors.Is(err, reporting.ErrNotFound) {
		slog.Error("не удалось перепроверить уточнения", "user_id", userID, "error", err.Error())
		return e.sendText(ctx, userID, "Не удалось обновить состояние после уточнения. Попробуйте ещё раз немного позже.", backToMenuKeyboard())
	}

	conversation, err := e.conversations.GetConversation(ctx, userID)
	if err != nil {
		slog.Error("не удалось восстановить состояние после уточнения", "user_id", userID, "error", err.Error())
		return e.sendText(ctx, userID, "Не удалось восстановить состояние после уточнения. Попробуйте ещё раз немного позже.", backToMenuKeyboard())
	}

	if conversation != nil && conversation.ActiveDraft != nil {
		if acknowledgment != "" {
			if err := e.sendText(ctx, userID, acknowledgment+" Возвращаем вас к заполнению черновика обращения.", nil); err != nil {
				return err
			}
			if err := e.pauseAfterClarificationAck(ctx); err != nil {
				return err
			}
		}
		e.restoreDraftFromConversation(session, conversation)
		session.PreviousStage = ""
		applyState(session, stateFromConversation(reporting.UserStageFillingReport, conversation.ActiveDraft.Stage))
		if err := e.hydrateDraftNames(ctx, session); err != nil {
			slog.Warn("не удалось восстановить названия справочников для черновика после уточнения", "user_id", userID, "error", err.Error())
		}
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendCurrentStatePrompt(ctx, userID, session)
	}

	session.Draft = Draft{}
	session.Reports = nil
	session.PreviousStage = ""
	if acknowledgment != "" {
		if err := e.sendText(ctx, userID, acknowledgment+" Возвращаем вас в главное меню.", nil); err != nil {
			return err
		}
		if err := e.pauseAfterClarificationAck(ctx); err != nil {
			return err
		}
	}
	applyState(session, stateMainMenu)
	if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
		return err
	}
	text, attachments := mainMenuMessage()
	return e.reply(ctx, userID, text, attachments)
}

func (e *Engine) sendClarificationPrompt(ctx context.Context, userID int64, prompt *reporting.ClarificationPrompt) error {
	lines := []string{prompt.NotificationText, "", "Напишите ответ сообщением или нажмите «Отклонить ввод ответа»."}
	return e.sendText(ctx, userID, strings.Join(lines, "\n"), clarificationKeyboard())
}

func (e *Engine) restoreDraftFromConversation(session *Session, conversation *reporting.ConversationState) {
	session.Draft = Draft{}
	session.Reports = nil
	if conversation == nil || conversation.ActiveDraft == nil {
		return
	}
	session.Draft.CategoryID = conversation.ActiveDraft.CategoryID
	session.Draft.MunicipalityID = conversation.ActiveDraft.MunicipalityID
	session.Draft.Phone = conversation.ActiveDraft.Phone
	session.Draft.Address = conversation.ActiveDraft.Address
	session.Draft.IncidentTime = conversation.ActiveDraft.IncidentTime
	session.Draft.Description = conversation.ActiveDraft.Description
	session.Draft.ExtraInfo = conversation.ActiveDraft.AdditionalInfo
	session.Draft.AttachmentLog = append([]string(nil), conversation.ActiveDraft.AttachmentLog...)
	session.Draft.Media = append([]reporting.MediaAttachment(nil), conversation.ActiveDraft.Attachments...)
}

func (e *Engine) sendCurrentStatePrompt(ctx context.Context, userID int64, session *Session) error {
	switch session.State {
	case stateReportCategory:
		categories, err := e.loadCategories(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		return e.sendText(ctx, userID, categoriesPrompt(categories), backToMenuKeyboard())
	case stateReportMunicipal:
		municipalities, err := e.loadMunicipalities(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		return e.sendText(ctx, userID, municipalitiesPrompt(municipalities), backToMenuKeyboard())
	case stateReportPhone:
		return e.sendText(ctx, userID, "Введите номер телефона в формате 10 цифр, начиная с 9, или отправьте контакт по кнопке ниже.", phoneKeyboard())
	case stateReportAddress:
		return e.sendText(ctx, userID, "Введите адрес или место совершения правонарушения.", backToMenuKeyboard())
	case stateReportTime:
		return e.sendText(ctx, userID, "Введите дату, время или период совершения правонарушения. Максимум 100 символов.", backToMenuKeyboard())
	case stateReportDesc:
		return e.sendText(ctx, userID, "Опишите суть правонарушения. Максимум 3900 символов.", backToMenuKeyboard())
	case stateReportMedia:
		if canSkipMedia(session.Draft) {
			return e.sendText(ctx, userID, "Отправьте фото/видео или нажмите \"Пропустить\".", mediaKeyboard(true))
		}
		return e.sendText(ctx, userID, "Отправьте фото/видео.", mediaKeyboard(false))
	case stateReportExtra:
		return e.sendText(ctx, userID, "Напишите дополнительную информацию или нажмите \"Пропустить\".", extraInfoKeyboard())
	case stateReportConfirm:
		return e.sendDraftSummary(ctx, userID)
	default:
		text, attachments := mainMenuMessage()
		return e.reply(ctx, userID, text, attachments)
	}
}

func (e *Engine) finishDraft(ctx context.Context, userID int64) error {
	startedAt := time.Now()
	session := e.session(userID)
	if session.State != stateReportConfirm {
		slog.Warn("получен report:send вне экрана подтверждения", "user_id", userID, "state", session.State)
		return e.sendText(ctx, userID, "Черновик не готов к отправке. Заполните обращение через «Подать обращение».", backToMenuKeyboard())
	}

	createReq := e.buildCreateReportRequest(userID, "", session)
	createReq.Normalize()
	if err := createReq.Validate(); err != nil {
		slog.Warn("черновик не прошел локальную валидацию перед отправкой", "user_id", userID, "error", err.Error())
		return e.sendText(ctx, userID, "Черновик заполнен не полностью или с ошибками. Проверьте данные и нажмите «Отправить» ещё раз.", confirmKeyboard())
	}
	if err := e.sendText(ctx, userID, "Загружаем приложенные вами файлы и формируем сообщение...", nil); err != nil {
		slog.Warn("не удалось отправить промежуточное сообщение о формировании обращения", "user_id", userID, "error", err.Error())
	}

	completedAt := time.Now().UTC()
	payload := e.buildDialogPayload(userID, fmt.Sprintf("RAW-%d", completedAt.UnixNano()), completedAt, session)
	if err := e.reportSink.Store(ctx, payload); err != nil {
		slog.Error("не удалось сохранить диалог в outbox", "user_id", userID, "dedup_key", payload.DedupKey, "error", err.Error(), "elapsed_ms", time.Since(startedAt).Milliseconds())
		return e.sendText(ctx, userID, "Не удалось сохранить сообщение. Попробуйте отправить ещё раз немного позже.", confirmKeyboard())
	}

	createReq.DialogDedupKey = payload.DedupKey
	createStartedAt := time.Now()
	created, err := e.reportCreator.CreateReport(ctx, createReq)
	if err != nil {
		if errors.Is(err, reporting.ErrInvalidRequest) {
			slog.Warn("core api отклонил черновик как невалидный", "user_id", userID, "dedup_key", payload.DedupKey, "error", err.Error(), "create_elapsed_ms", time.Since(createStartedAt).Milliseconds(), "total_elapsed_ms", time.Since(startedAt).Milliseconds())
			return e.sendText(ctx, userID, "Черновик устарел или заполнен не полностью. Проверьте данные и попробуйте отправить снова.", confirmKeyboard())
		}
		slog.Error("не удалось создать обращение в основной БД", "user_id", userID, "dedup_key", payload.DedupKey, "error", err.Error(), "create_elapsed_ms", time.Since(createStartedAt).Milliseconds(), "total_elapsed_ms", time.Since(startedAt).Milliseconds())
		return e.sendText(ctx, userID, "Не удалось создать обращение. Черновик сохранён, попробуйте ещё раз немного позже.", confirmKeyboard())
	}

	payload.ReportNumber = created.ReportNumber
	payload.MessageID = &created.ID
	normalizedAt := time.Now().UTC()
	payload.NormalizedAt = &normalizedAt
	if err := e.reportSink.Store(ctx, payload); err != nil {
		slog.Warn("не удалось обновить raw-слепок диалога ссылкой на сообщение", "user_id", userID, "report_number", created.ReportNumber, "dedup_key", payload.DedupKey, "error", err.Error())
	}

	slog.Info("черновик принят", "user_id", userID, "report_number", created.ReportNumber, "dedup_key", payload.DedupKey, "create_elapsed_ms", time.Since(createStartedAt).Milliseconds(), "total_elapsed_ms", time.Since(startedAt).Milliseconds())
	e.resetSession(userID)
	reset := e.session(userID)
	if err := e.persistStateOrReply(ctx, userID, reset, true); err != nil {
		return err
	}
	return e.sendTextMarkdown(ctx, userID, acceptedReportMessage(created), backToMenuKeyboard())
}

func (e *Engine) sendDraftSummary(ctx context.Context, userID int64) error {
	session := e.session(userID)
	lines := []string{
		"Черновик сообщения готов.",
		fmt.Sprintf("Категория: %s", session.Draft.CategoryName),
		fmt.Sprintf("Муниципалитет: %s", session.Draft.MunicipalityName),
		fmt.Sprintf("Телефон: %s", session.Draft.Phone),
		fmt.Sprintf("Адрес: %s", session.Draft.Address),
		fmt.Sprintf("Время: %s", session.Draft.IncidentTime),
		fmt.Sprintf("Описание: %s", session.Draft.Description),
	}
	if session.Draft.ExtraInfo != "" {
		lines = append(lines, fmt.Sprintf("Доп. информация: %s", session.Draft.ExtraInfo))
	}
	if len(session.Draft.AttachmentLog) > 0 {
		lines = append(lines, "Вложения:")
		lines = append(lines, session.Draft.AttachmentLog...)
	}
	lines = append(lines, "", "Нажмите \"Отправить\" или \"Отменить\".")
	return e.sendText(ctx, userID, strings.Join(lines, "\n"), confirmKeyboard())
}

func (e *Engine) reply(ctx context.Context, userID int64, text string, attachments []maxapi.AttachmentRequest) error {
	return e.sendTextWithFormat(ctx, userID, text, attachments, "")
}

func (e *Engine) sendText(ctx context.Context, userID int64, text string, attachments []maxapi.AttachmentRequest) error {
	return e.sendTextWithFormat(ctx, userID, text, attachments, "")
}

func (e *Engine) replyMarkdown(ctx context.Context, userID int64, text string, attachments []maxapi.AttachmentRequest) error {
	return e.sendTextWithFormat(ctx, userID, text, attachments, "markdown")
}

func (e *Engine) sendTextMarkdown(ctx context.Context, userID int64, text string, attachments []maxapi.AttachmentRequest) error {
	return e.sendTextWithFormat(ctx, userID, text, attachments, "markdown")
}

func (e *Engine) sendTextWithFormat(ctx context.Context, userID int64, text string, attachments []maxapi.AttachmentRequest, format string) error {
	body := maxapi.NewMessageBody{
		Text:        text,
		Attachments: attachments,
	}
	if strings.TrimSpace(format) != "" {
		body.Format = format
	}
	return e.client.SendMessage(ctx, userID, body)
}

func (e *Engine) loadCategories(ctx context.Context) ([]reference.Item, error) {
	return e.refData.Categories(ctx)
}

func (e *Engine) loadMunicipalities(ctx context.Context) ([]reference.Item, error) {
	return e.refData.Municipalities(ctx)
}

func (e *Engine) sendReferenceLookupError(ctx context.Context, userID int64) error {
	return e.sendText(ctx, userID, "Не удалось загрузить справочники. Попробуйте ещё раз немного позже.", backToMenuKeyboard())
}

func (e *Engine) ensureSessionLoaded(ctx context.Context, userID int64, session *Session) error {
	if session.Loaded {
		return nil
	}

	conversation, err := e.conversations.GetConversation(ctx, userID)
	if err != nil {
		return err
	}

	if conversation != nil {
		session.PreviousStage = conversation.PreviousStage
		applyState(session, stateFromConversation(conversation.Stage, draftStage(conversation)))
		if conversation.ActiveDraft != nil {
			session.Draft.CategoryID = conversation.ActiveDraft.CategoryID
			session.Draft.MunicipalityID = conversation.ActiveDraft.MunicipalityID
			session.Draft.Phone = conversation.ActiveDraft.Phone
			session.Draft.Address = conversation.ActiveDraft.Address
			session.Draft.IncidentTime = conversation.ActiveDraft.IncidentTime
			session.Draft.Description = conversation.ActiveDraft.Description
			session.Draft.ExtraInfo = conversation.ActiveDraft.AdditionalInfo
			session.Draft.AttachmentLog = append([]string(nil), conversation.ActiveDraft.AttachmentLog...)
			session.Draft.Media = append([]reporting.MediaAttachment(nil), conversation.ActiveDraft.Attachments...)
		}
		if err := e.hydrateDraftNames(ctx, session); err != nil {
			slog.Warn("не удалось восстановить названия справочников для черновика", "user_id", userID, "error", err.Error())
		}
		session.HasUserRecord = conversation.UserID > 0
	}

	session.Loaded = true
	return nil
}

func draftStage(conversation *reporting.ConversationState) reporting.MessageStage {
	if conversation == nil || conversation.ActiveDraft == nil {
		return reporting.MessageStageUnset
	}
	return conversation.ActiveDraft.Stage
}

func (e *Engine) hydrateDraftNames(ctx context.Context, session *Session) error {
	if session.Draft.CategoryID > 0 && session.Draft.CategoryName == "" {
		categories, err := e.loadCategories(ctx)
		if err != nil {
			return err
		}
		for _, item := range categories {
			if item.ID == session.Draft.CategoryID {
				session.Draft.CategoryName = item.Name
				break
			}
		}
	}
	if session.Draft.MunicipalityID > 0 && session.Draft.MunicipalityName == "" {
		municipalities, err := e.loadMunicipalities(ctx)
		if err != nil {
			return err
		}
		for _, item := range municipalities {
			if item.ID == session.Draft.MunicipalityID {
				session.Draft.MunicipalityName = item.Name
				break
			}
		}
	}
	return nil
}

func (e *Engine) persistSession(ctx context.Context, userID int64, session *Session, deleteDraft bool) error {
	req := reporting.SaveConversationRequest{
		MaxUserID:     userID,
		UserStage:     session.UserStage,
		PreviousStage: session.PreviousStage,
		DeleteDraft:   deleteDraft,
	}
	if !deleteDraft && session.MessageStage != reporting.MessageStageUnset {
		req.ActiveDraft = &reporting.DraftMessage{
			Stage:          session.MessageStage,
			CategoryID:     session.Draft.CategoryID,
			MunicipalityID: session.Draft.MunicipalityID,
			Phone:          session.Draft.Phone,
			Address:        session.Draft.Address,
			IncidentTime:   session.Draft.IncidentTime,
			Description:    session.Draft.Description,
			AdditionalInfo: session.Draft.ExtraInfo,
			AttachmentLog:  append([]string(nil), session.Draft.AttachmentLog...),
			Attachments:    append([]reporting.MediaAttachment(nil), session.Draft.Media...),
		}
	}
	state, err := e.conversations.SaveConversation(ctx, req)
	if err == nil {
		session.Loaded = true
		if state != nil {
			session.HasUserRecord = state.UserID > 0 || state.MaxUserID == userID
			session.PreviousStage = state.PreviousStage
		}
	}
	return err
}

func (e *Engine) ensurePreviousStage(session *Session) {
	if session.PreviousStage != "" && session.PreviousStage != reporting.UserStageWaitingClarification {
		return
	}

	switch {
	case session.MessageStage != reporting.MessageStageUnset || session.Draft.CategoryID > 0 || session.Draft.MunicipalityID > 0:
		session.PreviousStage = reporting.UserStageFillingReport
	case len(session.Reports) > 0 || session.State == stateMyReportsList || session.State == stateMyReportDetail:
		session.PreviousStage = reporting.UserStageViewingMessages
	default:
		session.PreviousStage = reporting.UserStageMainMenu
	}
}

func (e *Engine) pauseAfterClarificationAck(ctx context.Context) error {
	if e.ackDelay <= 0 {
		return nil
	}

	timer := time.NewTimer(e.ackDelay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (e *Engine) persistStateOrReply(ctx context.Context, userID int64, session *Session, deleteDraft bool) error {
	if err := e.persistSession(ctx, userID, session, deleteDraft); err != nil {
		slog.Error("не удалось сохранить состояние пользователя", "user_id", userID, "state", session.State, "error", err.Error())
		return e.sendText(ctx, userID, "Не удалось сохранить состояние диалога. Попробуйте ещё раз немного позже.", backToMenuKeyboard())
	}
	return nil
}

func (e *Engine) session(userID int64) *Session {
	e.mu.Lock()
	defer e.mu.Unlock()

	s, ok := e.sessions[userID]
	if !ok {
		s = &Session{StartedAt: time.Now().UTC()}
		applyState(s, stateMainMenu)
		e.sessions[userID] = s
	}
	return s
}

func (e *Engine) resetSession(userID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	session := &Session{StartedAt: time.Now().UTC()}
	applyState(session, stateMainMenu)
	e.sessions[userID] = session
}

func (e *Engine) setState(userID int64, state BotState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.sessions[userID]
	if !ok {
		s = &Session{StartedAt: time.Now().UTC()}
		e.sessions[userID] = s
	}
	applyState(s, state)
}

func (e *Engine) appendTrace(userID int64, step report.DialogStep) {
	e.mu.Lock()
	defer e.mu.Unlock()

	s, ok := e.sessions[userID]
	if !ok {
		s = &Session{StartedAt: time.Now().UTC()}
		applyState(s, stateMainMenu)
		e.sessions[userID] = s
	}
	s.Trace = append(s.Trace, step)
}

func (e *Engine) buildDialogPayload(userID int64, reportNumber string, completedAt time.Time, session *Session) report.DialogPayload {
	startedAt := session.StartedAt
	if startedAt.IsZero() {
		startedAt = completedAt
	}

	payload := report.DialogPayload{
		SchemaVersion: report.CurrentSchemaVersion,
		Source:        "max_bot",
		UserID:        userID,
		ReportNumber:  reportNumber,
		CreatedAt:     startedAt.UTC(),
		CompletedAt:   completedAt.UTC(),
		Draft: report.DialogDraft{
			CategoryID:       session.Draft.CategoryID,
			CategoryName:     session.Draft.CategoryName,
			MunicipalityID:   session.Draft.MunicipalityID,
			MunicipalityName: session.Draft.MunicipalityName,
			Phone:            session.Draft.Phone,
			Address:          session.Draft.Address,
			IncidentTime:     session.Draft.IncidentTime,
			Description:      session.Draft.Description,
			ExtraInfo:        session.Draft.ExtraInfo,
			AttachmentLog:    append([]string(nil), session.Draft.AttachmentLog...),
		},
		Steps: append([]report.DialogStep(nil), session.Trace...),
	}
	payload.Normalize(completedAt)
	return payload
}

func (e *Engine) buildCreateReportRequest(userID int64, dialogDedupKey string, session *Session) reporting.CreateReportRequest {
	return reporting.CreateReportRequest{
		DialogDedupKey: dialogDedupKey,
		MaxUserID:      userID,
		CategoryID:     session.Draft.CategoryID,
		MunicipalityID: session.Draft.MunicipalityID,
		Phone:          session.Draft.Phone,
		Address:        session.Draft.Address,
		IncidentTime:   session.Draft.IncidentTime,
		Description:    session.Draft.Description,
		AdditionalInfo: session.Draft.ExtraInfo,
		AttachmentLog:  append([]string(nil), session.Draft.AttachmentLog...),
		Attachments:    append([]reporting.MediaAttachment(nil), session.Draft.Media...),
	}
}

func parseChoice(text string, max int) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || n < 1 || n > max {
		return 0, false
	}
	return n, true
}

func findReportByNumber(text string, reports []reporting.ReportSummary) (reporting.ReportSummary, bool) {
	normalized := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "№"))
	for _, item := range reports {
		if strings.TrimSpace(item.ReportNumber) == normalized {
			return item, true
		}
	}
	return reporting.ReportSummary{}, false
}

func parsePhone(text string, attachments []maxapi.AttachmentBody) string {
	if phone := normalizePhone(text); phone != "" {
		return phone
	}

	for _, item := range attachments {
		if item.Type != "contact" {
			continue
		}

		var payload maxapi.ContactPayload
		if err := json.Unmarshal(item.RawPayload, &payload); err != nil {
			continue
		}
		if phone := phoneFromContactPayload(payload); phone != "" {
			return phone
		}
	}

	return ""
}

func phoneFromContactPayload(payload maxapi.ContactPayload) string {
	if phone := normalizePhone(payload.VCFPhone); phone != "" {
		return phone
	}
	if phone := normalizePhone(extractPhoneFromVCF(payload.VCFInfo)); phone != "" {
		return phone
	}
	return ""
}

func extractPhoneFromVCF(vcf string) string {
	if strings.TrimSpace(vcf) == "" {
		return ""
	}

	normalized := strings.ReplaceAll(vcf, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	for _, line := range strings.Split(normalized, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(strings.ToUpper(line), "TEL") {
			continue
		}

		separator := strings.LastIndex(line, ":")
		if separator < 0 || separator == len(line)-1 {
			continue
		}

		return digitsOnly(line[separator+1:])
	}

	return ""
}

func normalizePhone(value string) string {
	phone := digitsOnly(value)
	if len(phone) == 11 && (strings.HasPrefix(phone, "7") || strings.HasPrefix(phone, "8")) {
		phone = phone[1:]
	}
	if !phoneRe.MatchString(phone) {
		return ""
	}
	return phone
}

func canSkipMedia(draft Draft) bool {
	if draft.CategoryID == 10 {
		return true
	}

	name := strings.ToLower(strings.TrimSpace(draft.CategoryName))
	return strings.Contains(name, "тишина") && strings.Contains(name, "покой")
}

func totalVideoDurationSeconds(items []maxapi.AttachmentBody) (int, int, error) {
	total := 0
	unknownCount := 0
	for _, item := range items {
		if item.Type != "video" {
			continue
		}
		seconds, found, err := extractVideoDurationSeconds(item.RawPayload)
		if err != nil {
			return 0, 0, err
		}
		if !found {
			unknownCount++
			continue
		}
		total += seconds
	}
	return total, unknownCount, nil
}

func extractVideoDurationSeconds(raw json.RawMessage) (int, bool, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return 0, false, nil
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, false, fmt.Errorf("decode video payload: %w", err)
	}

	seconds, ok := findDurationSeconds(payload, 0)
	if !ok || seconds <= 0 {
		return 0, false, nil
	}
	return seconds, true, nil
}

func findDurationSeconds(value any, depth int) (int, bool) {
	if depth > 6 {
		return 0, false
	}

	switch typed := value.(type) {
	case map[string]any:
		priorityKeys := []string{
			"duration",
			"duration_seconds",
			"video_duration",
			"length_seconds",
			"duration_ms",
			"duration_millis",
			"duration_milliseconds",
		}
		for _, key := range priorityKeys {
			if raw, ok := typed[key]; ok {
				if seconds, ok := durationToSeconds(key, raw); ok {
					return seconds, true
				}
			}
		}

		for key, raw := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "duration") || strings.Contains(lower, "length") {
				if seconds, ok := durationToSeconds(lower, raw); ok {
					return seconds, true
				}
			}
		}

		for _, nested := range typed {
			if seconds, ok := findDurationSeconds(nested, depth+1); ok {
				return seconds, true
			}
		}
	case []any:
		for _, nested := range typed {
			if seconds, ok := findDurationSeconds(nested, depth+1); ok {
				return seconds, true
			}
		}
	}

	return 0, false
}

func durationToSeconds(key string, raw any) (int, bool) {
	value, ok := anyToFloat(raw)
	if !ok || value <= 0 {
		return 0, false
	}

	lower := strings.ToLower(key)
	if strings.Contains(lower, "ms") || strings.Contains(lower, "milli") {
		return int(math.Ceil(value / 1000)), true
	}
	return int(math.Ceil(value)), true
}

func anyToFloat(raw any) (float64, bool) {
	switch value := raw.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case int32:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func digitsOnly(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func attachmentSummary(items []maxapi.AttachmentBody) ([]string, bool) {
	result := make([]string, 0, len(items))
	for _, item := range items {
		switch item.Type {
		case "photo", "image", "video":
			result = append(result, "- "+item.Type)
		default:
			return nil, false
		}
	}
	return result, true
}

func collectMediaAttachments(items []maxapi.AttachmentBody) []reporting.MediaAttachment {
	if len(items) == 0 {
		return nil
	}

	result := make([]reporting.MediaAttachment, 0, len(items))
	for _, item := range items {
		switch item.Type {
		case "photo", "image", "video":
			result = append(result, reporting.MediaAttachment{
				Type:    item.Type,
				Payload: append(json.RawMessage(nil), item.RawPayload...),
			})
		}
	}
	return result
}

func attachmentTypes(items []maxapi.AttachmentBody) []string {
	if len(items) == 0 {
		return nil
	}

	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Type)
	}
	return result
}

func logIncomingMessage(userID int64, text string, attachments []maxapi.AttachmentBody) {
	slog.Info("получено сообщение", "user_id", userID, "text", text, "attachments_count", len(attachments))
	for i, item := range attachments {
		slog.Info("получено вложение", "index", i, "type", item.Type, "raw", strings.TrimSpace(string(item.RawPayload)))
	}
}
