package scenario

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

const (
	stateMainMenu        = "main_menu"
	stateReportCategory  = "report_category"
	stateReportMunicipal = "report_municipality"
	stateReportPhone     = "report_phone"
	stateReportAddress   = "report_address"
	stateReportTime      = "report_time"
	stateReportDesc      = "report_description"
	stateReportMedia     = "report_media"
	stateReportExtra     = "report_extra"
	stateReportConfirm   = "report_confirm"
)

var phoneRe = regexp.MustCompile(`^[78]\d{10}$`)

type Engine struct {
	client        Sender
	refData       reference.Provider
	reportSink    ReportSink
	reportCreator ReportCreator
	mu            sync.Mutex
	sessions      map[int64]*Session
}

type Sender interface {
	SendMessage(ctx context.Context, userID int64, body maxapi.NewMessageBody) error
	AnswerCallback(ctx context.Context, callbackID, notification string) error
}

type Session struct {
	State     string
	StartedAt time.Time
	Draft     Draft
	Trace     []report.DialogStep
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

func New(client Sender, refData reference.Provider, options ...Option) *Engine {
	engine := &Engine{
		client:        client,
		refData:       refData,
		reportSink:    noopReportSink{},
		reportCreator: noopReportCreator{},
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

func (e *Engine) HandleUpdate(ctx context.Context, upd maxapi.Update) error {
	switch upd.UpdateType {
	case "bot_started":
		if upd.User == nil {
			return nil
		}
		slog.Info("бот запущен пользователем", "user_id", upd.User.UserID, "payload", upd.Payload)
		return e.showMainMenu(ctx, upd.User.UserID)
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
	e.appendTrace(userID, report.DialogStep{
		At:      time.Now().UTC(),
		Kind:    "callback",
		State:   session.State,
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
		text, attachments := aboutMessage()
		return e.reply(ctx, userID, text, attachments)
	case "menu:legal":
		text, attachments := legalMessage()
		return e.reply(ctx, userID, text, attachments)
	case "menu:violations":
		categories, err := e.loadCategories(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		text, attachments := violationsMessage(categories)
		return e.reply(ctx, userID, text, attachments)
	case "menu:report":
		text, attachments := consentMessage()
		return e.reply(ctx, userID, text, attachments)
	case "menu:my_reports":
		return e.sendText(ctx, userID, "Раздел \"Мои сообщения\" пока заглушка. Позже сюда подключим бэкенд и реальные сообщения.", backToMenuKeyboard())
	case "report:consent_yes":
		categories, err := e.loadCategories(ctx)
		if err != nil {
			return e.sendReferenceLookupError(ctx, userID)
		}
		e.setState(userID, stateReportCategory)
		return e.sendText(ctx, userID, categoriesPrompt(categories), backToMenuKeyboard())
	case "report:skip_media":
		e.setState(userID, stateReportExtra)
		return e.sendText(ctx, userID, "Напишите дополнительную информацию или нажмите \"Пропустить\".", extraInfoKeyboard())
	case "report:skip_extra":
		e.setState(userID, stateReportConfirm)
		return e.sendDraftSummary(ctx, userID)
	case "report:send":
		return e.finishDraft(ctx, userID)
	case "report:cancel":
		e.resetSession(userID)
		return e.sendText(ctx, userID, "Отправка черновика отменена.", backToMenuKeyboard())
	default:
		return e.sendText(ctx, userID, "Неизвестная кнопка. Возвращаю в меню.", backToMenuKeyboard())
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
	e.appendTrace(userID, report.DialogStep{
		At:              time.Now().UTC(),
		Kind:            "message",
		State:           session.State,
		Text:            text,
		AttachmentTypes: attachmentTypes(attachments),
	})

	if text == "/start" || strings.EqualFold(text, "меню") {
		return e.showMainMenu(ctx, userID)
	}

	switch session.State {
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
		session.State = stateReportMunicipal
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
		session.State = stateReportPhone
		return e.sendText(ctx, userID, "Введите номер телефона начиная с 8/7 или отправьте контакт по кнопке ниже.", phoneKeyboard())
	case stateReportPhone:
		phone := parsePhone(text, attachments)
		if !phoneRe.MatchString(phone) {
			return e.sendText(ctx, userID, "Номер не соответствует формату. Введите 11 цифр, начиная с 8 или 7.", phoneKeyboard())
		}
		session.Draft.Phone = phone
		session.State = stateReportAddress
		return e.sendText(ctx, userID, "Введите адрес или место совершения правонарушения.", backToMenuKeyboard())
	case stateReportAddress:
		if len(text) == 0 || len([]rune(text)) > 1000 {
			return e.sendText(ctx, userID, "Адрес должен быть от 1 до 1000 символов.", backToMenuKeyboard())
		}
		session.Draft.Address = text
		session.State = stateReportTime
		return e.sendText(ctx, userID, "Введите дату и время нарушения по формату день/месяц/год часы:минуты.", backToMenuKeyboard())
	case stateReportTime:
		incidentTime, ok := parseIncidentTime(text)
		if !ok {
			return e.sendText(ctx, userID, "Дата и время должны быть в формате дд/мм/гг чч:мм. Пример: 31/03/26 14:45.", backToMenuKeyboard())
		}
		session.Draft.IncidentTime = incidentTime
		session.State = stateReportDesc
		return e.sendText(ctx, userID, "Опишите суть правонарушения. Максимум 3900 символов.", backToMenuKeyboard())
	case stateReportDesc:
		if len(text) == 0 || len([]rune(text)) > 3900 {
			return e.sendText(ctx, userID, "Описание должно быть от 1 до 3900 символов.", backToMenuKeyboard())
		}
		session.Draft.Description = text
		session.State = stateReportMedia
		return e.sendText(ctx, userID, "Отправьте фото/видео или нажмите \"Пропустить\".", mediaKeyboard())
	case stateReportMedia:
		if len(attachments) == 0 {
			return e.sendText(ctx, userID, "На этом шаге ждём фото/видео или кнопку \"Пропустить\".", mediaKeyboard())
		}
		if len(attachments) > 5 {
			return e.sendText(ctx, userID, "Получено больше 5 вложений. Повторите попытку.", mediaKeyboard())
		}
		logs, ok := attachmentSummary(attachments)
		if !ok {
			return e.sendText(ctx, userID, "Поддерживаются только фото, видео, контакт и геолокация для теста. Попробуйте ещё раз или пропустите шаг.", mediaKeyboard())
		}
		session.Draft.AttachmentLog = append(session.Draft.AttachmentLog, logs...)
		session.State = stateReportExtra
		return e.sendText(ctx, userID, "Вложения получены. Напишите дополнительную информацию или нажмите \"Пропустить\".", extraInfoKeyboard())
	case stateReportExtra:
		if len([]rune(text)) > 3900 {
			return e.sendText(ctx, userID, "Дополнительная информация должна быть не длиннее 3900 символов.", extraInfoKeyboard())
		}
		session.Draft.ExtraInfo = text
		session.State = stateReportConfirm
		return e.sendDraftSummary(ctx, userID)
	default:
		return e.showMainMenu(ctx, userID)
	}
}

func (e *Engine) showMainMenu(ctx context.Context, userID int64) error {
	e.resetSession(userID)
	text, attachments := mainMenuMessage()
	return e.reply(ctx, userID, text, attachments)
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

	slog.Info("черновик принят", "user_id", userID, "report_number", created.ReportNumber, "dedup_key", payload.DedupKey)
	e.resetSession(userID)
	return e.sendText(ctx, userID, "Сообщение принято с номером "+created.ReportNumber+".\n\nТекущий статус: "+created.Status+".", backToMenuKeyboard())
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

func (e *Engine) session(userID int64) *Session {
	e.mu.Lock()
	defer e.mu.Unlock()

	s, ok := e.sessions[userID]
	if !ok {
		s = &Session{
			State:     stateMainMenu,
			StartedAt: time.Now().UTC(),
		}
		e.sessions[userID] = s
	}
	return s
}

func (e *Engine) resetSession(userID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessions[userID] = &Session{
		State:     stateMainMenu,
		StartedAt: time.Now().UTC(),
	}
}

func (e *Engine) setState(userID int64, state string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.sessions[userID]
	if !ok {
		s = &Session{StartedAt: time.Now().UTC()}
		e.sessions[userID] = s
	}
	s.State = state
}

func (e *Engine) appendTrace(userID int64, step report.DialogStep) {
	e.mu.Lock()
	defer e.mu.Unlock()

	s, ok := e.sessions[userID]
	if !ok {
		s = &Session{
			State:     stateMainMenu,
			StartedAt: time.Now().UTC(),
		}
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
		if phone := normalizePhone(payload.VCFPhone); phone != "" {
			return phone
		}
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
		case "contact":
			var payload maxapi.ContactPayload
			if err := json.Unmarshal(item.RawPayload, &payload); err == nil {
				result = append(result, "- контакт: "+payload.VCFPhone)
			} else {
				result = append(result, "- контакт")
			}
		case "location":
			if item.Latitude != nil && item.Longitude != nil {
				result = append(result, fmt.Sprintf("- геопозиция: %.6f, %.6f", *item.Latitude, *item.Longitude))
			} else {
				result = append(result, "- геопозиция")
			}
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
