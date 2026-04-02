package scenario

import (
	"context"
	"encoding/json"
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

var phoneRe = regexp.MustCompile(`^[78]\d{10}$`)

const maxTotalVideoDurationSeconds = 120

type Engine struct {
	client        Sender
	refData       reference.Provider
	reportSink    ReportSink
	reportCreator ReportCreator
	reportReader  ReportReader
	conversations ConversationStore
	mu            sync.Mutex
	sessions      map[int64]*Session
}

type Sender interface {
	SendMessage(ctx context.Context, userID int64, body maxapi.NewMessageBody) error
	AnswerCallback(ctx context.Context, callbackID, notification string) error
}

type Session struct {
	State        BotState
	UserStage    reporting.UserStage
	MessageStage reporting.MessageStage
	StartedAt    time.Time
	Draft        Draft
	Trace        []report.DialogStep
	Reports      []reporting.ReportSummary
	Loaded       bool
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

func New(client Sender, refData reference.Provider, options ...Option) *Engine {
	engine := &Engine{
		client:        client,
		refData:       refData,
		reportSink:    noopReportSink{},
		reportCreator: noopReportCreator{},
		reportReader:  noopReportReader{},
		conversations: noopConversationStore{},
		sessions:      make(map[int64]*Session),
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
		MaxUserID: req.MaxUserID,
		Stage:     req.UserStage,
	}
	if req.ActiveDraft != nil {
		copied := *req.ActiveDraft
		state.ActiveDraft = &copied
	}
	return state, nil
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

	switch payload {
	case "menu:main":
		return e.showMainMenu(ctx, userID)
	case "menu:about":
		return e.showAbout(ctx, userID)
	case "menu:legal":
		e.setState(userID, stateLegalInfo)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		text, attachments := legalMessage()
		return e.reply(ctx, userID, text, attachments)
	case "menu:violations":
		categories, err := e.loadCategories(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		e.setState(userID, stateViolationsList)
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
		return e.reply(ctx, userID, text, attachments)
	case "menu:my_reports":
		items, err := e.reportReader.ListReportsByMaxUserID(ctx, userID)
		if err != nil {
			slog.Error("не удалось загрузить обращения пользователя", "user_id", userID, "error", err.Error())
			return e.sendText(ctx, userID, "Не удалось загрузить ваши обращения. Попробуйте ещё раз немного позже.", backToMenuKeyboard())
		}
		if len(items) == 0 {
			e.setState(userID, stateMyReportsList)
			if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
				return err
			}
			return e.sendText(ctx, userID, "У вас пока нет отправленных обращений.", backToMenuKeyboard())
		}
		session.Reports = append([]reporting.ReportSummary(nil), items...)
		applyState(session, stateMyReportsList)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendText(ctx, userID, myReportsListMessage(items), myReportsKeyboard())
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
	e.appendTrace(userID, report.DialogStep{
		At:              time.Now().UTC(),
		Kind:            "message",
		State:           string(session.State),
		Text:            text,
		AttachmentTypes: attachmentTypes(attachments),
	})

	if text == "/start" {
		return e.showWelcome(ctx, userID)
	}
	if strings.EqualFold(text, "меню") {
		return e.showMainMenu(ctx, userID)
	}

	switch session.State {
	case stateLegalInfo:
		text, attachments := legalMessage()
		return e.sendText(ctx, userID, "В этом разделе используйте кнопки ниже.\n\n"+text, attachments)
	case stateViolationsList:
		categories, err := e.loadCategories(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		text, attachments := violationsMessage(categories)
		return e.sendText(ctx, userID, "В этом разделе используйте кнопки ниже.\n\n"+text, attachments)
	case stateReportConsent:
		text, attachments := consentMessage()
		return e.sendText(ctx, userID, "Для продолжения используйте кнопки ниже.\n\n"+text, attachments)
	case stateMyReportsList:
		if len(session.Reports) == 0 {
			return e.sendText(ctx, userID, "Список обращений устарел. Нажмите \"Обновить список\" или вернитесь в меню.", myReportsKeyboard())
		}
		index, ok := parseChoice(text, len(session.Reports))
		if !ok {
			return e.sendText(ctx, userID, "Отправьте номер обращения из списка, чтобы посмотреть подробности.", myReportsKeyboard())
		}
		selected := session.Reports[index-1]
		detail, err := e.reportReader.GetReportByID(ctx, selected.ID)
		if err != nil {
			slog.Error("не удалось загрузить карточку обращения", "user_id", userID, "report_id", selected.ID, "error", err.Error())
			return e.sendText(ctx, userID, "Не удалось загрузить подробную информацию по обращению. Попробуйте ещё раз.", myReportsKeyboard())
		}
		applyState(session, stateMyReportDetail)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendText(ctx, userID, myReportDetailMessage(detail), myReportsKeyboard())
	case stateReportCategory:
		categories, err := e.loadCategories(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		index, ok := parseChoice(text, len(categories))
		if !ok {
			return e.sendText(ctx, userID, "Категория не найдена, отправьте номер из списка.", backToMenuKeyboard())
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
			return e.sendText(ctx, userID, "Муниципалитет не найден, отправьте номер из списка.", backToMenuKeyboard())
		}
		selected := municipalities[index-1]
		session.Draft.MunicipalityID = selected.ID
		session.Draft.MunicipalityName = selected.Name
		applyState(session, stateReportPhone)
		if err := e.persistStateOrReply(ctx, userID, session, false); err != nil {
			return err
		}
		return e.sendText(ctx, userID, "Введите номер телефона начиная с 8/7 или отправьте контакт по кнопке ниже.", phoneKeyboard())
	case stateReportPhone:
		phone := parsePhone(text, attachments)
		if !phoneRe.MatchString(phone) {
			return e.sendText(ctx, userID, "Номер не соответствует формату. Введите 11 цифр, начиная с 8 или 7.", phoneKeyboard())
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
		return e.sendText(ctx, userID, "Введите дату и время нарушения по формату день/месяц/год часы:минуты.", backToMenuKeyboard())
	case stateReportTime:
		incidentTime, ok := parseIncidentTime(text)
		if !ok {
			return e.sendText(ctx, userID, "Дата и время должны быть в формате дд/мм/гг чч:мм. Пример: 31/03/26 14:45.", backToMenuKeyboard())
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
		totalVideoDuration, err := totalVideoDurationSeconds(attachments)
		if err != nil {
			return e.sendText(ctx, userID, "Не удалось определить длительность видео. Проверьте вложения и повторите попытку.", mediaKeyboard(allowSkip))
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
		session.Draft.AttachmentLog = append(session.Draft.AttachmentLog, logs...)
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

func (e *Engine) showMainMenu(ctx context.Context, userID int64) error {
	e.resetSession(userID)
	session := e.session(userID)
	if err := e.persistStateOrReply(ctx, userID, session, true); err != nil {
		return err
	}
	text, attachments := mainMenuMessage()
	return e.reply(ctx, userID, text, attachments)
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
	e.resetSession(userID)
	session := e.session(userID)
	e.setState(userID, stateAbout)
	session = e.session(userID)
	if err := e.persistStateOrReply(ctx, userID, session, true); err != nil {
		return err
	}
	text, attachments := aboutMessage()
	if err := e.reply(ctx, userID, text, attachments); err != nil {
		return err
	}
	return e.showMainMenu(ctx, userID)
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

func (e *Engine) finishDraft(ctx context.Context, userID int64) error {
	session := e.session(userID)

	completedAt := time.Now().UTC()
	payload := e.buildDialogPayload(userID, fmt.Sprintf("RAW-%d", completedAt.UnixNano()), completedAt, session)
	if err := e.reportSink.Store(ctx, payload); err != nil {
		slog.Error("не удалось сохранить диалог в outbox", "user_id", userID, "dedup_key", payload.DedupKey, "error", err.Error())
		return e.sendText(ctx, userID, "Не удалось сохранить сообщение. Попробуйте отправить ещё раз немного позже.", confirmKeyboard())
	}

	created, err := e.reportCreator.CreateReport(ctx, e.buildCreateReportRequest(userID, payload.DedupKey, session))
	if err != nil {
		slog.Error("не удалось создать обращение в основной БД", "user_id", userID, "dedup_key", payload.DedupKey, "error", err.Error())
		return e.sendText(ctx, userID, "Не удалось создать обращение. Черновик сохранён, попробуйте ещё раз немного позже.", confirmKeyboard())
	}

	payload.ReportNumber = created.ReportNumber
	payload.MessageID = &created.ID
	normalizedAt := time.Now().UTC()
	payload.NormalizedAt = &normalizedAt
	if err := e.reportSink.Store(ctx, payload); err != nil {
		slog.Warn("не удалось обновить raw-слепок диалога ссылкой на сообщение", "user_id", userID, "report_number", created.ReportNumber, "dedup_key", payload.DedupKey, "error", err.Error())
	}

	slog.Info("черновик принят", "user_id", userID, "report_number", created.ReportNumber, "dedup_key", payload.DedupKey)
	e.resetSession(userID)
	reset := e.session(userID)
	if err := e.persistStateOrReply(ctx, userID, reset, true); err != nil {
		return err
	}
	return e.sendText(ctx, userID, "Сообщение принято с номером "+created.ReportNumber+".\n\nТекущий статус: "+humanizeReportStatus(created.Status)+".", backToMenuKeyboard())
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
	return e.sendText(ctx, userID, text, attachments)
}

func (e *Engine) sendText(ctx context.Context, userID int64, text string, attachments []maxapi.AttachmentRequest) error {
	return e.client.SendMessage(ctx, userID, maxapi.NewMessageBody{
		Text:        text,
		Attachments: attachments,
	})
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
		applyState(session, stateFromConversation(conversation.Stage, draftStage(conversation)))
		if conversation.ActiveDraft != nil {
			session.Draft.CategoryID = conversation.ActiveDraft.CategoryID
			session.Draft.MunicipalityID = conversation.ActiveDraft.MunicipalityID
			session.Draft.Phone = conversation.ActiveDraft.Phone
			session.Draft.Address = conversation.ActiveDraft.Address
			session.Draft.IncidentTime = conversation.ActiveDraft.IncidentTime
			session.Draft.Description = conversation.ActiveDraft.Description
			session.Draft.ExtraInfo = conversation.ActiveDraft.AdditionalInfo
		}
		if err := e.hydrateDraftNames(ctx, session); err != nil {
			slog.Warn("не удалось восстановить названия справочников для черновика", "user_id", userID, "error", err.Error())
		}
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
		MaxUserID:   userID,
		UserStage:   session.UserStage,
		DeleteDraft: deleteDraft,
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
		}
	}
	_, err := e.conversations.SaveConversation(ctx, req)
	if err == nil {
		session.Loaded = true
	}
	return err
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
	}
}

func parseChoice(text string, max int) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || n < 1 || n > max {
		return 0, false
	}
	return n, true
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
	if !phoneRe.MatchString(phone) {
		return ""
	}
	return phone
}

func parseIncidentTime(value string) (string, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", false
	}

	const layout = "02/01/06 15:04"
	parsed, err := time.Parse(layout, raw)
	if err != nil {
		return "", false
	}
	return parsed.Format(layout), true
}

func canSkipMedia(draft Draft) bool {
	if draft.CategoryID == 10 {
		return true
	}

	name := strings.ToLower(strings.TrimSpace(draft.CategoryName))
	return strings.Contains(name, "тишина") && strings.Contains(name, "покой")
}

func totalVideoDurationSeconds(items []maxapi.AttachmentBody) (int, error) {
	total := 0
	for _, item := range items {
		if item.Type != "video" {
			continue
		}
		seconds, err := extractVideoDurationSeconds(item.RawPayload)
		if err != nil {
			return 0, err
		}
		total += seconds
	}
	return total, nil
}

func extractVideoDurationSeconds(raw json.RawMessage) (int, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return 0, fmt.Errorf("empty video payload")
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, fmt.Errorf("decode video payload: %w", err)
	}

	seconds, ok := findDurationSeconds(payload, 0)
	if !ok || seconds <= 0 {
		return 0, fmt.Errorf("video duration not found")
	}
	return seconds, nil
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
