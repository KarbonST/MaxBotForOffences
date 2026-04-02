package reporting

type UserStage string

const (
	UserStageMainMenu             UserStage = "main_menu"
	UserStageFillingReport        UserStage = "filling_report"
	UserStageViewingMessages      UserStage = "viewing_messages"
	UserStageWaitingClarification UserStage = "waiting_clarification"
)

type MessageStage string

const (
	MessageStageUnset        MessageStage = ""
	MessageStageCategory     MessageStage = "category"
	MessageStageMunicipality MessageStage = "municipality"
	MessageStagePhone        MessageStage = "phone"
	MessageStageAddress      MessageStage = "address"
	MessageStageTime         MessageStage = "time"
	MessageStageDescription  MessageStage = "description"
	MessageStageFiles        MessageStage = "files"
	MessageStageAdditional   MessageStage = "additional"
	MessageStageSended       MessageStage = "sended"
)

type MessageStatus string

const (
	MessageStatusDraft                 MessageStatus = "draft"
	MessageStatusModeration            MessageStatus = "moderation"
	MessageStatusInProgress            MessageStatus = "in_progress"
	MessageStatusClarificationRequired MessageStatus = "clarification_requested"
	MessageStatusRejected              MessageStatus = "rejected"
	MessageStatusResolved              MessageStatus = "resolved"
)

type RequestStatus string

const (
	RequestStatusNew      RequestStatus = "new"
	RequestStatusAnswered RequestStatus = "answered"
	RequestStatusRejected RequestStatus = "rejected"
)

type NotificationStatus string

const (
	NotificationStatusNew    NotificationStatus = "new"
	NotificationStatusSended NotificationStatus = "sended"
	NotificationStatusError  NotificationStatus = "error"
)
