package report

import (
	"fmt"
	"strings"
	"time"
)

const CurrentSchemaVersion = 1

type DialogPayload struct {
	SchemaVersion int          `json:"schema_version"`
	DialogID      string       `json:"dialog_id"`
	DedupKey      string       `json:"dedup_key"`
	Source        string       `json:"source"`
	UserID        int64        `json:"user_id"`
	ReportNumber  string       `json:"report_number"`
	CreatedAt     time.Time    `json:"created_at"`
	CompletedAt   time.Time    `json:"completed_at"`
	Draft         DialogDraft  `json:"draft"`
	Steps         []DialogStep `json:"steps,omitempty"`
}

type DialogDraft struct {
	CategoryID       int      `json:"category_id"`
	CategoryName     string   `json:"category_name"`
	MunicipalityID   int      `json:"municipality_id"`
	MunicipalityName string   `json:"municipality_name"`
	Phone            string   `json:"phone"`
	Address          string   `json:"address"`
	IncidentTime     string   `json:"incident_time"`
	Description      string   `json:"description"`
	ExtraInfo        string   `json:"extra_info,omitempty"`
	AttachmentLog    []string `json:"attachment_log,omitempty"`
}

type DialogStep struct {
	At              time.Time `json:"at"`
	Kind            string    `json:"kind"`
	State           string    `json:"state"`
	Text            string    `json:"text,omitempty"`
	Payload         string    `json:"payload,omitempty"`
	AttachmentTypes []string  `json:"attachment_types,omitempty"`
}

func (p *DialogPayload) Normalize(now time.Time) {
	if p.SchemaVersion <= 0 {
		p.SchemaVersion = CurrentSchemaVersion
	}
	if p.Source == "" {
		p.Source = "max_bot"
	}

	if p.CompletedAt.IsZero() {
		p.CompletedAt = now.UTC()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = p.CompletedAt
	}
	p.CompletedAt = p.CompletedAt.UTC()
	p.CreatedAt = p.CreatedAt.UTC()

	if p.DialogID == "" {
		p.DialogID = fmt.Sprintf("dlg-%d-%d", p.UserID, p.CompletedAt.UnixNano())
	}
	if p.DedupKey == "" {
		p.DedupKey = strings.TrimSpace(fmt.Sprintf("%d:%s", p.UserID, p.ReportNumber))
	}
}
