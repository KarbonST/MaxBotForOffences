package scenario

import (
	"fmt"
	"strings"

	"max_bot/internal/maxapi"
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

func violationsMessage() (string, []maxapi.AttachmentRequest) {
	lines := []string{"Выберите нарушение или просто посмотрите справочник:"}
	for i, item := range categories {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, item))
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

func categoriesPrompt() string {
	lines := []string{"Список категорий. Отправьте номер категории сообщением в чат."}
	for i, item := range categories {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, item))
	}
	return strings.Join(lines, "\n")
}

func municipalitiesPrompt() string {
	lines := []string{"Список муниципалитетов. Отправьте номер муниципалитета сообщением в чат."}
	for i, item := range municipalities {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, item))
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
