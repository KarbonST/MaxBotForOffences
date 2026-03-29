package reporting

import "context"

type Store interface {
	CreateReport(context.Context, CreateReportRequest) (*CreatedReport, error)
	ListReports(context.Context, ListReportsFilter) ([]ReportSummary, error)
	ListReportsByMaxUserID(context.Context, int64) ([]ReportSummary, error)
	GetReportByID(context.Context, int64) (*ReportDetail, error)
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
