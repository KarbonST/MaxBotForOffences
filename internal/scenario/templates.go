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

const (
	personalDataLawURL = "https://ams.volgograd.ru/other/territorialnye-administrativnye-komissii-volgogradskoy-oblasti/152-FZ.docx"
	regionalCodeURL    = "https://ams.volgograd.ru/other/territorialnye-administrativnye-komissii-volgogradskoy-oblasti/kodeks_VO.docx"
	commissionsLawURL  = "https://ams.volgograd.ru/other/territorialnye-administrativnye-komissii-volgogradskoy-oblasti/Ob_administrativnykh_komissiyakh.docx"
	powersLawURL       = "https://ams.volgograd.ru/other/territorialnye-administrativnye-komissii-volgogradskoy-oblasti/Zakon_1792-OD_VO.docx"
	userAgreementURL   = "https://ams.volgograd.ru/other/territorialnye-administrativnye-komissii-volgogradskoy-oblasti/Polzovatelskoe_soglashenie.docx"
	privacyPolicyURL   = "https://ams.volgograd.ru/other/territorialnye-administrativnye-komissii-volgogradskoy-oblasti/Soglashenie_o_konfidentsialnosti.docx"
)

func mainMenuMessage() (string, []maxapi.AttachmentRequest) {
	return "Для просмотра списка нарушений, ответственность за совершение которых предусмотрена региональным законодательством, нажмите кнопку «Список нарушений». Чтобы оставить сообщение о правонарушении, нажмите «Сообщить о нарушении». Перед использованием бота ознакомьтесь с юридической информацией, для этого нажмите на кнопку «Юридическая информация».", mainMenuKeyboard()
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
		"1. " + mdLink("Федеральный закон от 27.07.2006 № 152-ФЗ \"О персональных данных\"", personalDataLawURL) + ";",
		"2. " + mdLink("Закон Волгоградской области от 11.06.2008 № 1693-ОД \"Кодекс Волгоградской области об административной ответственности\"", regionalCodeURL) + ";",
		"3. " + mdLink("Закон Волгоградской области от 02.12.2008 № 1789-ОД \"Об административных комиссиях\"", commissionsLawURL) + ";",
		"4. " + mdLink("Закон Волгоградской области от 02.12.2008 № 1792-ОД \"О наделении органов местного самоуправления муниципальных образований в Волгоградской области государственными полномочиями по организационному обеспечению деятельности территориальных административных комиссий\"", powersLawURL) + ";",
		"5. " + mdLink("Пользовательское соглашение при использовании чат-бота по аккумулированию информации об административных правонарушениях в мессенджере \"MAX\"", userAgreementURL) + ";",
		"6. " + mdLink("Соглашение о конфиденциальности при использовании чат-бота по аккумулированию информации об административных правонарушениях в мессенджере \"MAX\"", privacyPolicyURL) + ".",
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

func mdLink(text, url string) string {
	return "[" + text + "](<" + url + ">)"
}

func legalMessage() (string, []maxapi.AttachmentRequest) {
	return legalAndConsentText(), legalInfoKeyboard()
}

func violationsMessage(items []reference.Item) (string, []maxapi.AttachmentRequest) {
	lines := []string{"Выберите нарушение или просто посмотрите справочник:"}
	for i, item := range items {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, item.Name))
	}
	return strings.Join(lines, "\n"), violationsKeyboard()
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
			row(cb("Вернуться в начало", "menu:main")),
		),
	}
}

func extraInfoKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Пропустить", "report:skip_extra")),
			row(cb("Вернуться в начало", "menu:main")),
		),
	}
}

func mediaKeyboard(allowSkip bool) []maxapi.AttachmentRequest {
	if allowSkip {
		return []maxapi.AttachmentRequest{
			inlineKeyboard(
				row(cb("Пропустить", "report:skip_media")),
				row(cb("Вернуться в начало", "menu:main")),
			),
		}
	}

	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Вернуться в начало", "menu:main")),
		),
	}
}

func confirmKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Отправить", "report:send"), cb("Отменить", "report:cancel")),
		),
	}
}

func legalInfoKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Вернуться в начало", "menu:main")),
			row(cb("Сообщить о нарушении", "menu:report")),
		),
	}
}

func violationsKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Вернуться в начало", "menu:main")),
			row(cb("Сообщить о нарушении", "menu:report")),
		),
	}
}

func clarificationKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Отклонить ввод ответа", "clarification:reject")),
			row(cb("Меню", "menu:main")),
		),
	}
}

func backToMenuKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(row(cb("Вернуться в начало", "menu:main"))),
	}
}

func backToStartKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(row(cb("Вернуться в начало", "menu:main"))),
	}
}

func mainMenuKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Список нарушений", "menu:violations")),
			row(cb("Сообщить о нарушении", "menu:report")),
			row(cb("Юридическая информация", "menu:legal")),
			row(cb("Мои сообщения", "menu:my_reports")),
			row(cb("О боте", "menu:about")),
		),
	}
}

func myReportsKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Вернуться в начало", "menu:main")),
		),
	}
}

func myReportDetailKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		inlineKeyboard(
			row(cb("Вернуться к списку сообщений", "reports:back")),
			row(cb("Вернуться в начало", "menu:main")),
		),
	}
}

func myReportsListMessage(items []reporting.ReportSummary) string {
	lines := []string{
		"Список сообщений, написанных заявителем (номер, дата, категория, статус)",
		"",
	}
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("№%s", item.ReportNumber))
		lines = append(lines, fmt.Sprintf("Дата: %s", formatReportMoment(item.CreatedAt, item.SendedAt)))
		if value := strings.TrimSpace(item.CategoryName); value != "" {
			lines = append(lines, fmt.Sprintf("Категория: %s", value))
		}
		lines = append(lines, fmt.Sprintf("Статус: %s", humanizeReportStatus(item.Status)))
		lines = append(lines, "")
	}
	lines = append(lines, "Для просмотра детальной информации по сообщению отправьте номер сообщения в чат.", "", "Сообщения хранятся не более 30 дней.")
	return strings.Join(lines, "\n")
}

func acceptedReportMessage(item *reporting.CreatedReport) string {
	return strings.Join([]string{
		fmt.Sprintf("Сообщение о правонарушении принято с номером %s от %s.", item.ReportNumber, formatReportMoment(item.CreatedAt, item.SendedAt)),
		"",
		"Статусы рассмотрения сообщения:",
		"• модерация",
		"• в работе/отклонено",
		"• запрошена дополнительная информация (при необходимости)",
		"• рассмотрено",
		"",
		"При изменении статуса сообщения вам поступит уведомление.",
		"Сообщение будет храниться не более 30 дней.",
	}, "\n")
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
