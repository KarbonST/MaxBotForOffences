package scenario

import (
	"fmt"
	"strings"
	"time"

	"max_bot/internal/maxapi"
	"max_bot/internal/reference"
	"max_bot/internal/reporting"
)

func mainMenuMessage() (string, []maxapi.AttachmentRequest) {
	text := strings.Join([]string{
		"Главное меню.",
		"Выберите действие:",
		"1. Посмотреть список нарушений.",
		"2. Сообщить о нарушении.",
		"3. Открыть юридическую информацию.",
		"4. Посмотреть, как работают кнопки и сценарии.",
	}, "\n")

	return text, []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("О боте", "menu:about")),
			row(cb("Юридическая информация", "menu:legal")),
			row(cb("Список нарушений", "menu:violations")),
			row(cb("Сообщить о нарушении", "menu:report")),
			row(cb("Мои сообщения", "menu:my_reports")),
		),
	}
}

func aboutMessage() (string, []maxapi.AttachmentRequest) {
	return "Этот бот нужен для оперативного сбора сообщений об административных правонарушениях. Сейчас запущен dev-каркас: он показывает кнопки, пишет входящие события в терминал и ведёт по базовому сценарию.", backToMenuKeyboard()
}

func legalMessage() (string, []maxapi.AttachmentRequest) {
	return "Юридическая информация:\n- политика обработки персональных данных;\n- пользовательское соглашение.\n\nВ каркасе это заглушка, но переходы и кнопки уже работают.", backToMenuKeyboard()
}

func violationsMessage(items []reference.Item) (string, []maxapi.AttachmentRequest) {
	lines := []string{"Выберите нарушение или просто посмотрите справочник:"}
	for i, item := range items {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, item.Name))
	}
	return strings.Join(lines, "\n"), []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Сообщить о нарушении", "menu:report")),
			row(cb("Вернуться в меню", "menu:main")),
		),
	}
}

func consentMessage() (string, []maxapi.AttachmentRequest) {
	text := strings.Join([]string{
		"Перед продолжением подтвердите согласие с правилами использования бота.",
		"Требования к вложениям:",
		"- фото и/или видео;",
		"- не более 5 файлов;",
		"- видео суммарно не более 2 минут.",
	}, "\n")
	return text, []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Подтвердить и продолжить", "report:consent_yes")),
			row(cb("Вернуться в меню", "menu:main")),
		),
	}
}

func categoriesPrompt(items []reference.Item) string {
	lines := []string{"Список категорий. Отправьте номер категории сообщением в чат."}
	for i, item := range items {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, item.Name))
	}
	return strings.Join(lines, "\n")
}

func municipalitiesPrompt(items []reference.Item) string {
	lines := []string{"Список муниципалитетов. Отправьте номер муниципалитета сообщением в чат."}
	for i, item := range items {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, item.Name))
	}
	return strings.Join(lines, "\n")
}

func phoneKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(contactBtn("Отправить мой контакт")),
			row(cb("Вернуться в меню", "menu:main")),
		),
	}
}

func extraInfoKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Пропустить", "report:skip_extra")),
			row(cb("Вернуться в меню", "menu:main")),
		),
	}
}

func mediaKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Пропустить", "report:skip_media")),
			row(cb("Вернуться в меню", "menu:main")),
		),
	}
}

func confirmKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Отправить", "report:send"), cb("Отменить", "report:cancel")),
			row(cb("Вернуться в меню", "menu:main")),
		),
	}
}

func backToMenuKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(row(cb("Вернуться в меню", "menu:main"))),
	}
}

func myReportsKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Обновить список", "menu:my_reports")),
			row(cb("Вернуться в меню", "menu:main")),
		),
	}
}

func myReportsListMessage(items []reporting.ReportSummary) string {
	lines := []string{
		"Ваши обращения:",
	}
	for i, item := range items {
		lines = append(lines, fmt.Sprintf("%d. №%s | %s", i+1, item.ReportNumber, humanizeReportStatus(item.Status)))
		lines = append(lines, fmt.Sprintf("   %s", formatReportMoment(item.CreatedAt, item.SendedAt)))
		if value := strings.TrimSpace(item.CategoryName); value != "" {
			lines = append(lines, fmt.Sprintf("   Категория: %s", value))
		}
		if value := strings.TrimSpace(item.Address); value != "" {
			lines = append(lines, fmt.Sprintf("   Адрес: %s", value))
		}
		if value := truncateRunes(strings.TrimSpace(item.Description), 90); value != "" {
			lines = append(lines, fmt.Sprintf("   Суть: %s", value))
		}
	}
	lines = append(lines, "", "Отправьте номер обращения из списка, чтобы посмотреть подробности.")
	return strings.Join(lines, "\n")
}

func myReportDetailMessage(index int, item *reporting.ReportDetail) string {
	lines := []string{
		fmt.Sprintf("Обращение %d: №%s", index, item.ReportNumber),
		fmt.Sprintf("Статус: %s", humanizeReportStatus(item.Status)),
		fmt.Sprintf("Дата: %s", formatReportMoment(item.CreatedAt, item.SendedAt)),
	}
	if value := strings.TrimSpace(item.CategoryName); value != "" {
		lines = append(lines, fmt.Sprintf("Категория: %s", value))
	}
	if value := strings.TrimSpace(item.MunicipalityName); value != "" {
		lines = append(lines, fmt.Sprintf("Муниципалитет: %s", value))
	}
	if value := strings.TrimSpace(item.Address); value != "" {
		lines = append(lines, fmt.Sprintf("Адрес: %s", value))
	}
	if value := strings.TrimSpace(item.IncidentTime); value != "" {
		lines = append(lines, fmt.Sprintf("Когда: %s", value))
	}
	if value := strings.TrimSpace(item.Description); value != "" {
		lines = append(lines, fmt.Sprintf("Описание: %s", value))
	}
	if value := strings.TrimSpace(item.AdditionalInfo); value != "" {
		lines = append(lines, fmt.Sprintf("Доп. информация: %s", value))
	}
	if value := strings.TrimSpace(item.Answer); value != "" {
		lines = append(lines, fmt.Sprintf("Ответ: %s", value))
	}
	lines = append(lines, "", "Можно отправить номер другого обращения или нажать кнопку ниже.")
	return strings.Join(lines, "\n")
}

func humanizeReportStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "draft":
		return "Черновик"
	case "moderation":
		return "На модерации"
	case "in_progress":
		return "В работе"
	case "clarification_requested":
		return "Запрошено уточнение"
	case "rejected":
		return "Отклонено"
	case "resolved":
		return "Рассмотрено"
	default:
		return status
	}
}

func formatReportMoment(createdAt time.Time, sendedAt *time.Time) string {
	if sendedAt != nil && !sendedAt.IsZero() {
		return sendedAt.Format("02.01.2006 15:04")
	}
	if createdAt.IsZero() {
		return "-"
	}
	return createdAt.Format("02.01.2006 15:04")
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func inlineKeyboard(rows ...[]maxapi.Button) maxapi.AttachmentRequest {
	return maxapi.AttachmentRequest{
		Type: "inline_keyboard",
		Payload: maxapi.InlineKeyboardPayload{
			Buttons: rows,
		},
	}
}

func row(buttons ...maxapi.Button) []maxapi.Button {
	return buttons
}

func cb(text, payload string) maxapi.Button {
	return maxapi.Button{
		Type:    "callback",
		Text:    text,
		Payload: payload,
	}
}

func contactBtn(text string) maxapi.Button {
	return maxapi.Button{
		Type: "request_contact",
		Text: text,
	}
}
