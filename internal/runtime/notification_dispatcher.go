package runtime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"max_bot/internal/maxapi"
	"max_bot/internal/reporting"
)

type NotificationStore interface {
	ListPendingNotifications(context.Context, int) ([]reporting.NotificationItem, error)
	MarkNotificationSent(context.Context, int64) error
	MarkNotificationError(context.Context, int64) error
}

type ClarificationStore interface {
	GetPendingClarification(context.Context, int64) (*reporting.ClarificationPrompt, error)
}

type ConversationStore interface {
	GetConversation(context.Context, int64) (*reporting.ConversationState, error)
	SaveConversation(context.Context, reporting.SaveConversationRequest) (*reporting.ConversationState, error)
}

type NotificationSender interface {
	SendMessage(context.Context, int64, maxapi.NewMessageBody) error
}

type NotificationDispatcherConfig struct {
	Interval  time.Duration
	BatchSize int
}

type NotificationDispatcher struct {
	notifications  NotificationStore
	clarifications ClarificationStore
	conversations  ConversationStore
	sender         NotificationSender
	cfg            NotificationDispatcherConfig
	logger         *slog.Logger
}

func NewNotificationDispatcher(
	notifications NotificationStore,
	clarifications ClarificationStore,
	conversations ConversationStore,
	sender NotificationSender,
	cfg NotificationDispatcherConfig,
	logger *slog.Logger,
) *NotificationDispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = 50
	}

	return &NotificationDispatcher{
		notifications:  notifications,
		clarifications: clarifications,
		conversations:  conversations,
		sender:         sender,
		cfg:            cfg,
		logger:         logger,
	}
}

func (d *NotificationDispatcher) Run(ctx context.Context) error {
	d.logger.Info(
		"диспетчер уведомлений запущен",
		"interval", d.cfg.Interval.String(),
		"batch_size", d.cfg.BatchSize,
	)

	for {
		if err := d.dispatchBatch(ctx); err != nil && !errors.Is(err, context.Canceled) {
			d.logger.Warn("ошибка обработки уведомлений", "error", err.Error())
		}
		if err := sleepWithContext(ctx, d.cfg.Interval); err != nil {
			return err
		}
	}
}

func (d *NotificationDispatcher) dispatchBatch(ctx context.Context) error {
	items, err := d.notifications.ListPendingNotifications(ctx, d.cfg.BatchSize)
	if err != nil {
		return err
	}

	for _, item := range items {
		if err := d.dispatchOne(ctx, item); err != nil && !errors.Is(err, context.Canceled) {
			d.logger.Warn("уведомление обработано с ошибкой", "notification_id", item.ID, "user_id", item.MaxUserID, "error", err.Error())
		}
	}
	return nil
}

func (d *NotificationDispatcher) dispatchOne(ctx context.Context, item reporting.NotificationItem) error {
	attachments, clarificationPrompt, deferNotification, err := d.notificationAttachments(ctx, item)
	if err != nil {
		d.logger.Warn("не удалось определить режим уведомления", "notification_id", item.ID, "user_id", item.MaxUserID, "error", err.Error())
	}
	if deferNotification {
		d.logger.Debug("уведомление отложено до завершения текущего уточнения", "notification_id", item.ID, "user_id", item.MaxUserID)
		return nil
	}

	body := maxapi.NewMessageBody{
		Text:        item.Notification,
		Attachments: attachments,
	}
	if formattedText, ok := highlightNotificationStatuses(item.Notification); ok {
		body.Text = formattedText
		body.Format = "markdown"
	}

	if err := d.sender.SendMessage(ctx, item.MaxUserID, body); err != nil {
		_ = d.markNotificationError(ctx, item.ID, item.MaxUserID, err)
		return err
	}

	if clarificationPrompt != nil {
		if err := d.markUserWaitingClarification(ctx, item.MaxUserID); err != nil {
			d.logger.Warn("не удалось перевести пользователя в waiting_clarification", "notification_id", item.ID, "user_id", item.MaxUserID, "error", err.Error())
		}
	}

	if err := d.notifications.MarkNotificationSent(ctx, item.ID); err != nil {
		return err
	}
	return nil
}

func (d *NotificationDispatcher) notificationAttachments(ctx context.Context, item reporting.NotificationItem) ([]maxapi.AttachmentRequest, *reporting.ClarificationPrompt, bool, error) {
	if d.clarifications == nil {
		return nil, nil, false, nil
	}

	prompt, err := d.clarifications.GetPendingClarification(ctx, item.MaxUserID)
	if err != nil {
		if errors.Is(err, reporting.ErrNotFound) {
			return nil, nil, false, nil
		}
		return nil, nil, false, err
	}
	if prompt.NotificationID != 0 && prompt.NotificationID != item.ID {
		return nil, nil, true, nil
	}

	return clarificationNotificationKeyboard(), prompt, false, nil
}

func (d *NotificationDispatcher) markUserWaitingClarification(ctx context.Context, maxUserID int64) error {
	conversation, err := d.currentConversation(ctx, maxUserID)
	if err != nil {
		return err
	}

	req := reporting.SaveConversationRequest{
		MaxUserID: maxUserID,
		UserStage: reporting.UserStageWaitingClarification,
	}
	if conversation != nil {
		req.PreviousStage = waitingPreviousStage(conversation)
		if conversation.ActiveDraft != nil {
			draft := *conversation.ActiveDraft
			req.ActiveDraft = &draft
		}
	}

	_, err = d.conversations.SaveConversation(ctx, req)
	return err
}

func waitingPreviousStage(conversation *reporting.ConversationState) reporting.UserStage {
	if conversation == nil {
		return reporting.UserStageMainMenu
	}
	if conversation.PreviousStage != "" && conversation.PreviousStage != reporting.UserStageWaitingClarification {
		return conversation.PreviousStage
	}
	if conversation.Stage != reporting.UserStageWaitingClarification {
		return conversation.Stage
	}
	if conversation.ActiveDraft != nil {
		return reporting.UserStageFillingReport
	}
	return reporting.UserStageMainMenu
}

func (d *NotificationDispatcher) currentConversation(ctx context.Context, maxUserID int64) (*reporting.ConversationState, error) {
	if d.conversations == nil {
		return nil, nil
	}
	return d.conversations.GetConversation(ctx, maxUserID)
}

func (d *NotificationDispatcher) markNotificationError(ctx context.Context, notificationID, maxUserID int64, sendErr error) error {
	if err := d.notifications.MarkNotificationError(ctx, notificationID); err != nil {
		d.logger.Warn("не удалось отметить уведомление как error", "notification_id", notificationID, "user_id", maxUserID, "send_error", sendErr.Error(), "error", err.Error())
		return err
	}
	return nil
}

func clarificationNotificationKeyboard() []maxapi.AttachmentRequest {
	return []maxapi.AttachmentRequest{
		{
			Type: "inline_keyboard",
			Payload: maxapi.InlineKeyboardPayload{
				Buttons: [][]maxapi.Button{
					{
						{Type: "callback", Text: "Отклонить ввод ответа", Payload: "clarification:reject"},
					},
					{
						{Type: "callback", Text: "Меню", Payload: "menu:main"},
					},
				},
			},
		},
	}
}

func highlightNotificationStatuses(text string) (string, bool) {
	replacer := strings.NewReplacer(
		"прошло модерацию", "прошло **модерацию**",
		"«модерация»", "«**модерация**»",
		"«в работе»", "«**в работе**»",
		"«отклонено»", "«**отклонено**»",
		"«рассмотрено»", "«**рассмотрено**»",
		"«запрошено уточнение»", "«**запрошено уточнение**»",
		"статусе модерация", "статусе **модерация**",
		"статусе в работе", "статусе **в работе**",
		"статусе отклонено", "статусе **отклонено**",
		"статусе рассмотрено", "статусе **рассмотрено**",
		"статусе запрошено уточнение", "статусе **запрошено уточнение**",
		" отклонено по следующей причине", " **отклонено** по следующей причине",
		" рассмотрено.", " **рассмотрено**.",
		" рассмотрено\n", " **рассмотрено**\n",
	)

	formatted := replacer.Replace(text)
	return formatted, formatted != text
}
