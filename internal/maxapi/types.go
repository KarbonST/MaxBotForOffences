package maxapi

import "encoding/json"

type BotInfo struct {
	UserID   int64  `json:"user_id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	IsBot    bool   `json:"is_bot"`
}

type UpdateList struct {
	Updates []Update `json:"updates"`
	Marker  *int64   `json:"marker"`
}

type Update struct {
	UpdateType string    `json:"update_type"`
	Timestamp  int64     `json:"timestamp"`
	Message    *Message  `json:"message,omitempty"`
	Callback   *Callback `json:"callback,omitempty"`
	ChatID     int64     `json:"chat_id,omitempty"`
	User       *User     `json:"user,omitempty"`
	Payload    string    `json:"payload,omitempty"`
	UserLocale string    `json:"user_locale,omitempty"`
}

type Callback struct {
	Timestamp  int64  `json:"timestamp"`
	CallbackID string `json:"callback_id"`
	Payload    string `json:"payload"`
	User       User   `json:"user"`
}

type Message struct {
	Sender    *User       `json:"sender,omitempty"`
	Recipient Recipient   `json:"recipient"`
	Timestamp int64       `json:"timestamp"`
	Body      MessageBody `json:"body"`
}

type Recipient struct {
	ChatID   *int64 `json:"chat_id"`
	ChatType string `json:"chat_type"`
	UserID   *int64 `json:"user_id"`
}

type User struct {
	UserID   int64  `json:"user_id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
}

type MessageBody struct {
	MID         string           `json:"mid"`
	Seq         int64            `json:"seq"`
	Text        string           `json:"text"`
	Attachments []AttachmentBody `json:"attachments"`
}

type AttachmentBody struct {
	Type       string          `json:"type"`
	RawPayload json.RawMessage `json:"payload,omitempty"`
	Latitude   *float64        `json:"latitude,omitempty"`
	Longitude  *float64        `json:"longitude,omitempty"`
}

type ContactPayload struct {
	Name     string `json:"name"`
	VCFPhone string `json:"vcf_phone"`
	VCFInfo  string `json:"vcf_info"`
}

type NewMessageBody struct {
	Text        string              `json:"text"`
	Attachments []AttachmentRequest `json:"attachments"`
	Format      string              `json:"format,omitempty"`
}

type AttachmentRequest struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

type InlineKeyboardPayload struct {
	Buttons [][]Button `json:"buttons"`
}

type Button struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Payload string `json:"payload,omitempty"`
	Intent  string `json:"intent,omitempty"`
}

type CallbackAnswer struct {
	Notification string `json:"notification,omitempty"`
}

type SendMessageResult struct {
	Message Message `json:"message"`
}
