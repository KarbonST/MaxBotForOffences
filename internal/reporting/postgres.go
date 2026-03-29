package reporting

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(dsn string) (*PostgresStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("reporting postgres dsn is empty")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) CreateReport(ctx context.Context, req CreateReportRequest) (*CreatedReport, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if req.DialogDedupKey != "" {
		existing, err := s.findCreatedReportByDialogKey(ctx, tx, req.DialogDedupKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if err := tx.Commit(); err != nil {
				return nil, fmt.Errorf("commit tx after dedup hit: %w", err)
			}
			return existing, nil
		}
	}

	var userID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO users (max_id, stage)
		VALUES ($1, 'main_menu')
		ON CONFLICT (max_id) DO UPDATE
		SET stage = 'main_menu',
		    updated_at = NOW()
		RETURNING id
	`, req.MaxUserID).Scan(&userID); err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}

	result := &CreatedReport{}
	var sendedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO messages (
			user_id,
			category_id,
			municipality_id,
			status,
			phone,
			address,
			incident_time,
			description,
			additional_info,
			stage,
			sended_at
		)
		VALUES ($1, $2, $3, 'moderation', $4, $5, $6, $7, $8, 'sended', NOW())
		RETURNING id, status::text, stage::text, created_at, sended_at, updated_at
	`,
		userID,
		req.CategoryID,
		req.MunicipalityID,
		req.Phone,
		req.Address,
		req.IncidentTime,
		req.Description,
		nullableString(req.AdditionalInfo),
	).Scan(
		&result.ID,
		&result.Status,
		&result.Stage,
		&result.CreatedAt,
		&sendedAt,
		&result.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}
	if sendedAt.Valid {
		value := sendedAt.Time
		result.SendedAt = &value
	}
	result.UserID = userID
	result.MaxUserID = req.MaxUserID
	result.ReportNumber = fmt.Sprintf("%d", result.ID)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages_history (
			message_id,
			admin_id,
			event_type,
			new_value,
			comments
		)
		VALUES ($1, NULL, 'status', 'moderation', 'created by max bot')
	`, result.ID); err != nil {
		return nil, fmt.Errorf("insert message history: %w", err)
	}

	if req.DialogDedupKey != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE dialog_reports
			SET message_id = $1, normalized_at = NOW()
			WHERE dedup_key = $2 AND message_id IS NULL
		`, result.ID, req.DialogDedupKey); err != nil {
			return nil, fmt.Errorf("update dialog_reports link: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return result, nil
}

func (s *PostgresStore) ListReportsByMaxUserID(ctx context.Context, maxUserID int64) ([]ReportSummary, error) {
	return s.listReports(ctx, normalizeFilter(ListReportsFilter{MaxUserID: &maxUserID, Limit: 100}))
}

func (s *PostgresStore) ListReports(ctx context.Context, filter ListReportsFilter) ([]ReportSummary, error) {
	return s.listReports(ctx, normalizeFilter(filter))
}

func (s *PostgresStore) GetReportByID(ctx context.Context, id int64) (*ReportDetail, error) {
	var detail ReportDetail
	var sendedAt sql.NullTime
	var answer sql.NullString
	var additionalInfo sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT
			m.id,
			u.id,
			u.max_id,
			c.id,
			COALESCE(c.name, ''),
			mn.id,
			COALESCE(mn.name, ''),
			m.status::text,
			m.stage::text,
			m.description,
			m.address,
			m.created_at,
			m.sended_at,
			m.updated_at,
			m.phone,
			m.incident_time,
			m.additional_info,
			m.answer
		FROM messages m
		JOIN users u ON u.id = m.user_id
		LEFT JOIN categories c ON c.id = m.category_id
		LEFT JOIN municipalities mn ON mn.id = m.municipality_id
		WHERE m.id = $1
	`, id).Scan(
		&detail.ID,
		&detail.UserID,
		&detail.MaxUserID,
		&detail.CategoryID,
		&detail.CategoryName,
		&detail.MunicipalityID,
		&detail.MunicipalityName,
		&detail.Status,
		&detail.Stage,
		&detail.Description,
		&detail.Address,
		&detail.CreatedAt,
		&sendedAt,
		&detail.UpdatedAt,
		&detail.Phone,
		&detail.IncidentTime,
		&additionalInfo,
		&answer,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("select report by id: %w", err)
	}

	detail.ReportNumber = fmt.Sprintf("%d", detail.ID)
	if sendedAt.Valid {
		value := sendedAt.Time
		detail.SendedAt = &value
	}
	detail.AdditionalInfo = additionalInfo.String
	detail.Answer = answer.String
	return &detail, nil
}

func (s *PostgresStore) listReports(ctx context.Context, filter ListReportsFilter) ([]ReportSummary, error) {
	base := `
		SELECT
			m.id,
			u.id,
			u.max_id,
			c.id,
			COALESCE(c.name, ''),
			mn.id,
			COALESCE(mn.name, ''),
			m.status::text,
			m.stage::text,
			m.description,
			m.address,
			m.created_at,
			m.sended_at,
			m.updated_at
		FROM messages m
		JOIN users u ON u.id = m.user_id
		LEFT JOIN categories c ON c.id = m.category_id
		LEFT JOIN municipalities mn ON mn.id = m.municipality_id
	`

	var where []string
	var args []any
	nextArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	if filter.MaxUserID != nil {
		where = append(where, "u.max_id = "+nextArg(*filter.MaxUserID))
	}
	if filter.Status != "" {
		where = append(where, "m.status::text = "+nextArg(filter.Status))
	}
	if filter.CategoryID > 0 {
		where = append(where, "m.category_id = "+nextArg(filter.CategoryID))
	}
	if filter.MunicipalityID > 0 {
		where = append(where, "m.municipality_id = "+nextArg(filter.MunicipalityID))
	}
	if filter.Search != "" {
		pattern := "%" + filter.Search + "%"
		placeholder := nextArg(pattern)
		where = append(where, fmt.Sprintf("(m.description ILIKE %s OR m.address ILIKE %s OR CAST(m.id AS TEXT) ILIKE %s)", placeholder, placeholder, placeholder))
	}

	query := base
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY COALESCE(m.sended_at, m.created_at) DESC"
	query += " LIMIT " + nextArg(filter.Limit)
	query += " OFFSET " + nextArg(filter.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	defer rows.Close()

	var items []ReportSummary
	for rows.Next() {
		var item ReportSummary
		var sendedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.MaxUserID,
			&item.CategoryID,
			&item.CategoryName,
			&item.MunicipalityID,
			&item.MunicipalityName,
			&item.Status,
			&item.Stage,
			&item.Description,
			&item.Address,
			&item.CreatedAt,
			&sendedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan report summary: %w", err)
		}
		item.ReportNumber = fmt.Sprintf("%d", item.ID)
		if sendedAt.Valid {
			value := sendedAt.Time
			item.SendedAt = &value
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate report summaries: %w", err)
	}

	return items, nil
}

func (s *PostgresStore) findCreatedReportByDialogKey(ctx context.Context, tx *sql.Tx, dedupKey string) (*CreatedReport, error) {
	var existingID sql.NullInt64
	err := tx.QueryRowContext(ctx, `
		SELECT message_id
		FROM dialog_reports
		WHERE dedup_key = $1
		FOR UPDATE
	`, dedupKey).Scan(&existingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select dialog_reports by dedup key: %w", err)
	}
	if !existingID.Valid {
		return nil, nil
	}

	var result CreatedReport
	var sendedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT
			m.id,
			u.id,
			u.max_id,
			m.status::text,
			m.stage::text,
			m.created_at,
			m.sended_at,
			m.updated_at
		FROM messages m
		JOIN users u ON u.id = m.user_id
		WHERE m.id = $1
	`, existingID.Int64).Scan(
		&result.ID,
		&result.UserID,
		&result.MaxUserID,
		&result.Status,
		&result.Stage,
		&result.CreatedAt,
		&sendedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select existing report by dialog link: %w", err)
	}

	result.ReportNumber = fmt.Sprintf("%d", result.ID)
	if sendedAt.Valid {
		value := sendedAt.Time
		result.SendedAt = &value
	}
	return &result, nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
