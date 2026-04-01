package scenario

import (
	"fmt"
	"strings"
	"time"

	"max_bot/internal/maxapi"
	"max_bot/internal/reference"
	"max_bot/internal/reporting"
)

var userFacingLocation = loadUserFacingLocation()

func mainMenuMessage() (string, []maxapi.AttachmentRequest) {
	text := strings.Join([]string{
		"Главное меню.",
		"Выберите действие:",
		"1. Посмотреть список нарушений.",
		"2. Сообщить о нарушении.",
		"3. Открыть юридическую информацию.",
		"4. Посмотреть, как работают кнопки и сценарии.",
	}, "\n")

	return text, mainMenuKeyboard()
}

func welcomeMessage() (string, []maxapi.AttachmentRequest) {
	return "Данный бот создан для оперативного сбора информации об административных правонарушениях, предусмотренных региональным законодательством (в сфере благоустройства территорий муниципальных образований, обращения с домашними животными, выпаса и прогона сельскохозяйственных животных, торговли вне установленных мест, тишины и покоя граждан и др.).", nil
}

func aboutMessage() (string, []maxapi.AttachmentRequest) {
	return "Данный бот создан для оперативного сбора информации об административных правонарушениях, предусмотренных региональным законодательством (в сфере благоустройства территорий муниципальных образований, обращения с домашними животными, выпаса и прогона сельскохозяйственных животных, торговли вне установленных мест, тишины и покоя граждан и др.).", nil
}

func unsupportedInputMessage() (string, []maxapi.AttachmentRequest) {
	return "Не могу распознать вашу команду, вы будете перенаправлены в главное меню.", nil
}

func legalAndConsentText() string {
	return strings.Join([]string{
		"Продолжая использование бота, вы выражаете согласие с требованиями к сообщениям и следующими документами:",
		"1. Федеральный закон от 27.07.2006 № 152-ФЗ \"О персональных данных\";",
		"2. Закон Волгоградской области от 11.06.2008 № 1693-ОД \"Кодекс Волгоградской области об административной ответственности\";",
		"3. Закон Волгоградской области от 02.12.2008 № 1789-ОД \"Об административных комиссиях\";",
		"4. Закон Волгоградской области от 02.12.2008 № 1792-ОД \"О наделении органов местного самоуправления муниципальных образований в Волгоградской области государственными полномочиями по организационному обеспечению деятельности территориальных административных комиссий\";",
		"5. Пользовательское соглашение при использовании чат-бота по аккумулированию информации об административных правонарушениях в мессенджере \"MAX\";",
		"6. Соглашение о конфиденциальности при использовании чат-бота по аккумулированию информации об административных правонарушениях в мессенджере \"MAX\".",
		"",
		"Требования к сообщениям",
		"В сообщении гражданин излагает суть сообщения. Сообщение должно быть адресовано в конкретное муниципальное образование с указанием вида нарушения. К сообщению приобщаются фото и видео файлы.",
		"В сообщении не должно содержаться персональных данных, не должны нарушаться права и свободы других лиц, а также не должно содержаться нецензурных либо оскорбительных выражений.",
		"Требования к прикладываемым файлам:",
		"- принимаются файлы типа фото и видео;",
		"- максимальное количество фото/видео: 5 шт;",
		"- максимальная суммарная длина видео: 2 минуты.",
	}, "\n")
}

func legalMessage() (string, []maxapi.AttachmentRequest) {
	return legalAndConsentText(), []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Вернуться в начало", "menu:main")),
			row(cb("Сообщить о нарушении", "menu:report")),
		),
	}
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
	return legalAndConsentText(), []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Вернуться в начало", "menu:main")),
			row(cb("Подтвердить и продолжить", "report:consent_yes")),
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

func mainMenuKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("О боте", "menu:about")),
			row(cb("Юридическая информация", "menu:legal")),
			row(cb("Список нарушений", "menu:violations")),
			row(cb("Сообщить о нарушении", "menu:report")),
			row(cb("Мои сообщения", "menu:my_reports")),
		),
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
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, formatReportMoment(item.CreatedAt, item.SendedAt)))
		lines = append(lines, fmt.Sprintf("   Статус: %s", humanizeReportStatus(item.Status)))
	}
	lines = append(lines, "", "Отправьте номер обращения из списка, чтобы посмотреть подробности.")
	return strings.Join(lines, "\n")
}

func myReportDetailMessage(item *reporting.ReportDetail) string {
	lines := []string{
		fmt.Sprintf("Обращение №%s", item.ReportNumber),
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
	lines = append(lines, "", "Чтобы открыть другое обращение, отправьте его номер из списка.")
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
		return sendedAt.In(userFacingLocation).Format("02.01.2006 15:04")
	}
	if createdAt.IsZero() {
		return "-"
	}
	return createdAt.In(userFacingLocation).Format("02.01.2006 15:04")
}

func loadUserFacingLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("Europe/Moscow", 3*60*60)
	}
	return loc
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
