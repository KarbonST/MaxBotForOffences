package reporting

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotFound       = errors.New("report not found")
	ErrInvalidRequest = errors.New("invalid report request")
)

var phoneRe = regexp.MustCompile(`^\d{10}$`)

type CreateReportRequest struct {
	DialogDedupKey string   `json:"dialog_dedup_key,omitempty"`
	MaxUserID      int64    `json:"max_user_id"`
	CategoryID     int      `json:"category_id"`
	MunicipalityID int      `json:"municipality_id"`
	Phone          string   `json:"phone"`
	Address        string   `json:"address"`
	IncidentTime   string   `json:"incident_time"`
	Description    string   `json:"description"`
	AdditionalInfo string   `json:"additional_info,omitempty"`
	AttachmentLog  []string `json:"attachment_log,omitempty"`
}

type CreatedReport struct {
	ID           int64      `json:"id"`
	ReportNumber string     `json:"report_number"`
	Status       string     `json:"status"`
	Stage        string     `json:"stage"`
	UserID       int64      `json:"user_id"`
	MaxUserID    int64      `json:"max_user_id"`
	CreatedAt    time.Time  `json:"created_at"`
	SendedAt     *time.Time `json:"sended_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type ReportSummary struct {
	ID               int64      `json:"id"`
	ReportNumber     string     `json:"report_number"`
	UserID           int64      `json:"user_id"`
	MaxUserID        int64      `json:"max_user_id"`
	CategoryID       int        `json:"category_id"`
	CategoryName     string     `json:"category_name"`
	MunicipalityID   int        `json:"municipality_id"`
	MunicipalityName string     `json:"municipality_name"`
	Status           string     `json:"status"`
	Stage            string     `json:"stage"`
	Description      string     `json:"description"`
	Address          string     `json:"address"`
	CreatedAt        time.Time  `json:"created_at"`
	SendedAt         *time.Time `json:"sended_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type ReportDetail struct {
	ReportSummary
	Phone          string `json:"phone"`
	IncidentTime   string `json:"incident_time"`
	AdditionalInfo string `json:"additional_info,omitempty"`
	Answer         string `json:"answer,omitempty"`
}

type ListReportsFilter struct {
	MaxUserID      *int64
	Status         string
	CategoryID     int
	MunicipalityID int
	Search         string
	Limit          int
	Offset         int
}

func (r *CreateReportRequest) Normalize() {
	r.DialogDedupKey = strings.TrimSpace(r.DialogDedupKey)
	r.Phone = normalizePhone(r.Phone)
	r.Address = strings.TrimSpace(r.Address)
	r.IncidentTime = strings.TrimSpace(r.IncidentTime)
	r.Description = strings.TrimSpace(r.Description)
	r.AdditionalInfo = strings.TrimSpace(r.AdditionalInfo)
}

func (r CreateReportRequest) Validate() error {
	if r.MaxUserID <= 0 {
		return fmt.Errorf("%w: max_user_id must be positive", ErrInvalidRequest)
	}
	if r.CategoryID <= 0 {
		return fmt.Errorf("%w: category_id must be positive", ErrInvalidRequest)
	}
	if r.MunicipalityID <= 0 {
		return fmt.Errorf("%w: municipality_id must be positive", ErrInvalidRequest)
	}
	if !phoneRe.MatchString(r.Phone) {
		return fmt.Errorf("%w: phone must contain exactly 10 digits", ErrInvalidRequest)
	}
	if length := len([]rune(r.Address)); length < 1 || length > 1000 {
		return fmt.Errorf("%w: address length must be between 1 and 1000", ErrInvalidRequest)
	}
	if length := len([]rune(r.IncidentTime)); length < 1 || length > 100 {
		return fmt.Errorf("%w: incident_time length must be between 1 and 100", ErrInvalidRequest)
	}
	if length := len([]rune(r.Description)); length < 1 || length > 3900 {
		return fmt.Errorf("%w: description length must be between 1 and 3900", ErrInvalidRequest)
	}
	if len([]rune(r.AdditionalInfo)) > 3900 {
		return fmt.Errorf("%w: additional_info length must be <= 3900", ErrInvalidRequest)
	}
	return nil
}

func normalizeFilter(filter ListReportsFilter) ListReportsFilter {
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Search = strings.TrimSpace(filter.Search)
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
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

func normalizePhone(value string) string {
	phone := digitsOnly(value)
	if len(phone) == 11 && (strings.HasPrefix(phone, "7") || strings.HasPrefix(phone, "8")) {
		return phone[1:]
	}
	return phone
}
