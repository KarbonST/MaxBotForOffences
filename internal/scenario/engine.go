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

var phoneRe = regexp.MustCompile(`^\d{10}$`)

type Engine struct {
	client   Sender
	mu       sync.Mutex
	sessions map[int64]*Session
}

type Sender interface {
	SendMessage(ctx context.Context, userID int64, body maxapi.NewMessageBody) error
	AnswerCallback(ctx context.Context, callbackID, notification string) error
}

type Session struct {
	State string
	Draft Draft
}

type Draft struct {
	Category      int
	Municipality  int
	Phone         string
	Address       string
	IncidentTime  string
	Description   string
	ExtraInfo     string
	AttachmentLog []string
}

func New(client Sender) *Engine {
	return &Engine{
		client:   client,
		sessions: make(map[int64]*Session),
	}
}

func (e *Engine) HandleUpdate(ctx context.Context, upd maxapi.Update) error {
	switch upd.UpdateType {
	case "bot_started":
		if upd.User == nil {
			return nil
		}
		slog.Info("bot started", "user_id", upd.User.UserID, "payload", upd.Payload)
		return e.showMainMenu(ctx, upd.User.UserID)
	case "message_callback":
		return e.handleCallback(ctx, upd)
	case "message_created":
		return e.handleMessage(ctx, upd)
	default:
		slog.Debug("skip update", "type", upd.UpdateType)
		return nil
	}
}

func (e *Engine) handleCallback(ctx context.Context, upd maxapi.Update) error {
	if upd.Callback == nil {
		return nil
	}

	userID := upd.Callback.User.UserID
	payload := upd.Callback.Payload
	slog.Info("callback received", "user_id", userID, "payload", payload)

	if err := e.client.AnswerCallback(ctx, upd.Callback.CallbackID, "Принято"); err != nil {
		slog.Warn("callback answer failed", "error", err.Error())
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
		text, attachments := violationsMessage()
		return e.reply(ctx, userID, text, attachments)
	case "menu:report":
		text, attachments := consentMessage()
		return e.reply(ctx, userID, text, attachments)
	case "menu:my_reports":
		return e.sendText(ctx, userID, "Раздел \"Мои сообщения\" пока заглушка. Позже сюда подключим backend и реальные сообщения.", backToMenuKeyboard())
	case "report:consent_yes":
		e.setState(userID, stateReportCategory)
		return e.sendText(ctx, userID, categoriesPrompt(), backToMenuKeyboard())
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

	if text == "/start" || strings.EqualFold(text, "меню") {
		return e.showMainMenu(ctx, userID)
	}

	session := e.session(userID)
	switch session.State {
	case stateReportCategory:
		index, ok := parseChoice(text, len(categories))
		if !ok {
			return e.sendText(ctx, userID, "Категория не найдена, отправьте номер от 1 до 12.", backToMenuKeyboard())
		}
		session.Draft.Category = index
		session.State = stateReportMunicipal
		return e.sendText(ctx, userID, municipalitiesPrompt(), backToMenuKeyboard())
	case stateReportMunicipal:
		index, ok := parseChoice(text, len(municipalities))
		if !ok {
			return e.sendText(ctx, userID, "Муниципалитет не найден, отправьте номер из списка.", backToMenuKeyboard())
		}
		session.Draft.Municipality = index
		session.State = stateReportPhone
		return e.sendText(ctx, userID, "Введите номер телефона в формате 10 цифр без +7/8 или отправьте контакт кнопкой ниже.", phoneKeyboard())
	case stateReportPhone:
		phone := parsePhone(text, attachments)
		if !phoneRe.MatchString(phone) {
			return e.sendText(ctx, userID, "Номер не соответствует формату. Нужны ровно 10 цифр.", phoneKeyboard())
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
		return e.sendText(ctx, userID, "Введите время или период совершения правонарушения.", backToMenuKeyboard())
	case stateReportTime:
		if len(text) == 0 || len([]rune(text)) > 100 {
			return e.sendText(ctx, userID, "Время/период должен быть от 1 до 100 символов.", backToMenuKeyboard())
		}
		session.Draft.IncidentTime = text
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
	reportNumber := fmt.Sprintf("DEV-%d", time.Now().Unix()%100000)
	slog.Info("draft accepted", "user_id", userID, "report_number", reportNumber, "draft", fmt.Sprintf("%+v", session.Draft))
	e.resetSession(userID)
	return e.sendText(ctx, userID, "Сообщение принято в dev-режиме с номером "+reportNumber+".\n\nСейчас это демонстрационный приём без backend, но все введённые шаги и кнопки уже прошли через FSM и попали в лог терминала.", backToMenuKeyboard())
}

func (e *Engine) sendDraftSummary(ctx context.Context, userID int64) error {
	session := e.session(userID)
	lines := []string{
		"Черновик сообщения готов.",
		fmt.Sprintf("Категория: %s", categories[session.Draft.Category-1]),
		fmt.Sprintf("Муниципалитет: %s", municipalities[session.Draft.Municipality-1]),
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

func (e *Engine) session(userID int64) *Session {
	e.mu.Lock()
	defer e.mu.Unlock()

	s, ok := e.sessions[userID]
	if !ok {
		s = &Session{State: stateMainMenu}
		e.sessions[userID] = s
	}
	return s
}

func (e *Engine) resetSession(userID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessions[userID] = &Session{State: stateMainMenu}
}

func (e *Engine) setState(userID int64, state string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.sessions[userID]
	if !ok {
		s = &Session{}
		e.sessions[userID] = s
	}
	s.State = state
}

func parseChoice(text string, max int) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || n < 1 || n > max {
		return 0, false
	}
	return n, true
}

func parsePhone(text string, attachments []maxapi.AttachmentBody) string {
	if phone := digitsOnly(text); phoneRe.MatchString(phone) {
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
		if phone := digitsOnly(payload.VCFPhone); len(phone) >= 10 {
			return phone[len(phone)-10:]
		}
	}

	return ""
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
				result = append(result, "- contact: "+payload.VCFPhone)
			} else {
				result = append(result, "- contact")
			}
		case "location":
			if item.Latitude != nil && item.Longitude != nil {
				result = append(result, fmt.Sprintf("- location: %.6f, %.6f", *item.Latitude, *item.Longitude))
			} else {
				result = append(result, "- location")
			}
		default:
			return nil, false
		}
	}
	return result, true
}

func logIncomingMessage(userID int64, text string, attachments []maxapi.AttachmentBody) {
	slog.Info("message received", "user_id", userID, "text", text, "attachments_count", len(attachments))
	for i, item := range attachments {
		slog.Info("attachment received", "index", i, "type", item.Type, "raw", strings.TrimSpace(string(item.RawPayload)))
	}
}
