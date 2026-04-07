package reporting

import "context"

type Store interface {
	CreateReport(context.Context, CreateReportRequest) (*CreatedReport, error)
	ListReports(context.Context, ListReportsFilter) ([]ReportSummary, error)
	ListReportsByMaxUserID(context.Context, int64) ([]ReportSummary, error)
	GetReportByID(context.Context, int64) (*ReportDetail, error)
	GetConversation(context.Context, int64) (*ConversationState, error)
	SaveConversation(context.Context, SaveConversationRequest) (*ConversationState, error)
	ListPendingNotifications(context.Context, int) ([]NotificationItem, error)
	MarkNotificationSent(context.Context, int64) error
	MarkNotificationError(context.Context, int64) error
	GetPendingClarification(context.Context, int64) (*ClarificationPrompt, error)
	AnswerClarification(context.Context, ClarificationAnswerRequest) error
	RejectClarification(context.Context, ClarificationRejectRequest) error
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) CreateReport(ctx context.Context, req CreateReportRequest) (*CreatedReport, error) {
	req.Normalize()
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return s.store.CreateReport(ctx, req)
}

func (s *Service) ListReports(ctx context.Context, filter ListReportsFilter) ([]ReportSummary, error) {
	return s.store.ListReports(ctx, normalizeFilter(filter))
}

func (s *Service) ListReportsByMaxUserID(ctx context.Context, maxUserID int64) ([]ReportSummary, error) {
	return s.store.ListReportsByMaxUserID(ctx, maxUserID)
}

func (s *Service) GetReportByID(ctx context.Context, id int64) (*ReportDetail, error) {
	return s.store.GetReportByID(ctx, id)
}

func (s *Service) GetConversation(ctx context.Context, maxUserID int64) (*ConversationState, error) {
	if maxUserID <= 0 {
		return nil, ErrInvalidRequest
	}
	return s.store.GetConversation(ctx, maxUserID)
}

func (s *Service) SaveConversation(ctx context.Context, req SaveConversationRequest) (*ConversationState, error) {
	req.Normalize()
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return s.store.SaveConversation(ctx, req)
}

func (s *Service) ListPendingNotifications(ctx context.Context, limit int) ([]NotificationItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.store.ListPendingNotifications(ctx, limit)
}

func (s *Service) MarkNotificationSent(ctx context.Context, notificationID int64) error {
	if notificationID <= 0 {
		return ErrInvalidRequest
	}
	return s.store.MarkNotificationSent(ctx, notificationID)
}

func (s *Service) MarkNotificationError(ctx context.Context, notificationID int64) error {
	if notificationID <= 0 {
		return ErrInvalidRequest
	}
	return s.store.MarkNotificationError(ctx, notificationID)
}

func (s *Service) GetPendingClarification(ctx context.Context, maxUserID int64) (*ClarificationPrompt, error) {
	if maxUserID <= 0 {
		return nil, ErrInvalidRequest
	}
	return s.store.GetPendingClarification(ctx, maxUserID)
}

func (s *Service) AnswerClarification(ctx context.Context, req ClarificationAnswerRequest) error {
	req.Normalize()
	if err := req.Validate(); err != nil {
		return err
	}
	return s.store.AnswerClarification(ctx, req)
}

func (s *Service) RejectClarification(ctx context.Context, req ClarificationRejectRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	return s.store.RejectClarification(ctx, req)
}
