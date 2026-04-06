package reporting

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresStore struct {
	db           *sql.DB
	mediaRootDir string
	mediaFetcher *mediaFetcher
}

const defaultMediaRootDir = "/var/www/violations-upload"

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

	mediaRootDir := strings.TrimSpace(os.Getenv("MEDIA_UPLOAD_ROOT"))
	if mediaRootDir == "" {
		mediaRootDir = defaultMediaRootDir
	}

	return &PostgresStore{
		db:           db,
		mediaRootDir: mediaRootDir,
		mediaFetcher: newMediaFetcherFromEnv(),
	}, nil
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
		INSERT INTO users (max_id, stage, previous_stage)
		VALUES ($1, 'main_menu', NULL)
		ON CONFLICT (max_id) DO UPDATE
		SET stage = 'main_menu',
		    updated_at = NOW()
		RETURNING id
	`, req.MaxUserID).Scan(&userID); err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}

	result := &CreatedReport{}
	var sendedAt sql.NullTime
	draftID, err := findActiveDraftID(ctx, tx, userID)
	if err != nil {
		return nil, err
	}

	if draftID > 0 {
		if err := tx.QueryRowContext(ctx, `
			UPDATE messages
			SET category_id = $2,
			    municipality_id = $3,
			    status = 'moderation',
			    phone = $4,
			    address = $5,
			    incident_time = $6,
			    description = $7,
			    additional_info = $8,
			    stage = 'sended',
			    sended_at = NOW(),
			    updated_at = NOW()
			WHERE id = $1
			RETURNING id, status::text, stage::text, created_at, sended_at, updated_at
		`,
			draftID,
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
			return nil, fmt.Errorf("update draft message: %w", err)
		}
	} else {
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

	if err := s.storeMediaFiles(ctx, tx, result.ID, req.Attachments); err != nil {
		return nil, err
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

func (s *PostgresStore) storeMediaFiles(ctx context.Context, tx *sql.Tx, messageID int64, items []MediaAttachment) error {
	if len(items) == 0 {
		return nil
	}

	directoryDisk := filepath.Join(s.mediaRootDir, strconv.FormatInt(messageID, 10))
	directoryDB := path.Join(s.mediaRootDir, strconv.FormatInt(messageID, 10))
	if err := os.MkdirAll(directoryDisk, 0o755); err != nil {
		return fmt.Errorf("create media directory: %w", err)
	}

	for index, item := range items {
		content, fileName, mimeType, ext, err := s.materializeAttachment(ctx, item, messageID, index+1)
		if err != nil {
			return err
		}
		filePathDisk := filepath.Join(directoryDisk, fileName)
		if err := os.WriteFile(filePathDisk, content, 0o644); err != nil {
			return fmt.Errorf("write media file %q: %w", filePathDisk, err)
		}

		filePathDB := path.Join(directoryDB, fileName)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO files (message_id, path, file_name, file_size, mime_type)
			VALUES ($1, $2, $3, $4, $5)
		`, messageID, filePathDB, fileName, len(content), pickMIME(mimeType, ext)); err != nil {
			return fmt.Errorf("insert media file row: %w", err)
		}
	}

	return nil
}

func (s *PostgresStore) materializeAttachment(ctx context.Context, item MediaAttachment, messageID int64, position int) ([]byte, string, string, string, error) {
	ext := pickExt(item.Type)

	if decoded, ok := decodeEmbeddedBase64(item.Payload); ok {
		fileName := buildMediaFileName(item.FileName, messageID, position, ext)
		return decoded, fileName, item.MIMEType, ext, nil
	}

	if s.mediaFetcher != nil {
		content, mimeType, downloadedExt, err := s.mediaFetcher.fetch(ctx, item)
		if err == nil && len(content) > 0 {
			if downloadedExt != "" {
				ext = downloadedExt
			}
			fileName := buildMediaFileName(item.FileName, messageID, position, ext)
			return content, fileName, firstNonEmpty(item.MIMEType, mimeType), ext, nil
		}
	}

	trimmedPayload := bytesOrJSON(item.Payload)
	fileName := buildMediaFileName(item.FileName, messageID, position, ext)
	return trimmedPayload, fileName, item.MIMEType, ext, nil
}

func decodeEmbeddedBase64(raw json.RawMessage) ([]byte, bool) {
	if len(raw) == 0 {
		return nil, false
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false
	}

	keys := []string{"data", "file_data", "base64", "content"}
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		encoded, ok := value.(string)
		if !ok {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err == nil && len(decoded) > 0 {
			return decoded, true
		}
	}
	return nil, false
}

func bytesOrJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	return append([]byte(nil), raw...)
}

func buildMediaFileName(fileName string, messageID int64, position int, ext string) string {
	cleanName := filepath.Base(strings.TrimSpace(fileName))
	if cleanName == "" || cleanName == "." {
		return fmt.Sprintf("%d_%02d%s", messageID, position, ext)
	}
	if filepath.Ext(cleanName) == "" && ext != "" {
		return cleanName + ext
	}
	return cleanName
}

func pickExt(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "video":
		return ".mp4"
	default:
		return ".jpg"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func pickMIME(mimeType, ext string) string {
	if strings.TrimSpace(mimeType) != "" {
		return mimeType
	}
	switch ext {
	case ".mp4":
		return "video/mp4"
	default:
		return "image/jpeg"
	}
}

func (s *PostgresStore) GetConversation(ctx context.Context, maxUserID int64) (*ConversationState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, max_id, stage::text, COALESCE(previous_stage::text, '')
		FROM users
		WHERE max_id = $1
	`, maxUserID)

	var state ConversationState
	if err := row.Scan(&state.UserID, &state.MaxUserID, &state.Stage, &state.PreviousStage); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &ConversationState{
				MaxUserID: maxUserID,
				Stage:     UserStageMainMenu,
			}, nil
		}
		return nil, fmt.Errorf("select conversation user: %w", err)
	}

	draft, err := loadActiveDraft(ctx, s.db, state.UserID)
	if err != nil {
		return nil, err
	}
	state.ActiveDraft = draft
	return &state, nil
}

func (s *PostgresStore) SaveConversation(ctx context.Context, req SaveConversationRequest) (*ConversationState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	userID, err := upsertConversationUser(ctx, tx, req.MaxUserID, req.UserStage, req.PreviousStage)
	if err != nil {
		return nil, err
	}

	if req.DeleteDraft {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM messages
			WHERE user_id = $1 AND status = 'draft'
		`, userID); err != nil {
			return nil, fmt.Errorf("delete draft message: %w", err)
		}
	} else if req.ActiveDraft != nil {
		draftID, err := findActiveDraftID(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if draftID > 0 {
			if _, err := tx.ExecContext(ctx, `
				UPDATE messages
				SET category_id = $2,
				    municipality_id = $3,
				    phone = $4,
				    address = $5,
				    incident_time = $6,
				    description = $7,
				    additional_info = $8,
				    stage = $9,
				    updated_at = NOW()
				WHERE id = $1
			`,
				draftID,
				nullableInt(req.ActiveDraft.CategoryID),
				nullableInt(req.ActiveDraft.MunicipalityID),
				nullableString(req.ActiveDraft.Phone),
				nullableString(req.ActiveDraft.Address),
				nullableString(req.ActiveDraft.IncidentTime),
				nullableString(req.ActiveDraft.Description),
				nullableString(req.ActiveDraft.AdditionalInfo),
				req.ActiveDraft.Stage,
			); err != nil {
				return nil, fmt.Errorf("update draft message: %w", err)
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
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
					stage
				)
				VALUES ($1, $2, $3, 'draft', $4, $5, $6, $7, $8, $9)
			`,
				userID,
				nullableInt(req.ActiveDraft.CategoryID),
				nullableInt(req.ActiveDraft.MunicipalityID),
				nullableString(req.ActiveDraft.Phone),
				nullableString(req.ActiveDraft.Address),
				nullableString(req.ActiveDraft.IncidentTime),
				nullableString(req.ActiveDraft.Description),
				nullableString(req.ActiveDraft.AdditionalInfo),
				req.ActiveDraft.Stage,
			); err != nil {
				return nil, fmt.Errorf("insert draft message: %w", err)
			}
		}
	}

	state, err := loadConversationTx(ctx, tx, req.MaxUserID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return state, nil
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

func upsertConversationUser(ctx context.Context, tx *sql.Tx, maxUserID int64, stage UserStage, previousStage UserStage) (int64, error) {
	var userID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO users (max_id, stage, previous_stage)
		VALUES ($1, $2, NULLIF($3, '')::user_stage)
		ON CONFLICT (max_id) DO UPDATE
		SET stage = EXCLUDED.stage,
		    previous_stage = EXCLUDED.previous_stage,
		    updated_at = NOW()
		RETURNING id
	`, maxUserID, stage, previousStage).Scan(&userID); err != nil {
		return 0, fmt.Errorf("upsert user stage: %w", err)
	}
	return userID, nil
}

func findActiveDraftID(ctx context.Context, tx *sql.Tx, userID int64) (int64, error) {
	var draftID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM messages
		WHERE user_id = $1 AND status = 'draft'
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
		FOR UPDATE
	`, userID).Scan(&draftID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("select active draft: %w", err)
	}
	if !draftID.Valid {
		return 0, nil
	}
	return draftID.Int64, nil
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadActiveDraft(ctx context.Context, q rowQuerier, userID int64) (*DraftMessage, error) {
	row := q.QueryRowContext(ctx, `
		SELECT
			id,
			status::text,
			stage::text,
			COALESCE(category_id, 0),
			COALESCE(municipality_id, 0),
			COALESCE(phone, ''),
			COALESCE(address, ''),
			COALESCE(incident_time, ''),
			COALESCE(description, ''),
			COALESCE(additional_info, '')
		FROM messages
		WHERE user_id = $1 AND status = 'draft'
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`, userID)

	var draft DraftMessage
	if err := row.Scan(
		&draft.ID,
		&draft.Status,
		&draft.Stage,
		&draft.CategoryID,
		&draft.MunicipalityID,
		&draft.Phone,
		&draft.Address,
		&draft.IncidentTime,
		&draft.Description,
		&draft.AdditionalInfo,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select active draft: %w", err)
	}
	return &draft, nil
}

func loadConversationTx(ctx context.Context, tx *sql.Tx, maxUserID int64) (*ConversationState, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, max_id, stage::text, COALESCE(previous_stage::text, '')
		FROM users
		WHERE max_id = $1
	`, maxUserID)

	var state ConversationState
	if err := row.Scan(&state.UserID, &state.MaxUserID, &state.Stage, &state.PreviousStage); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &ConversationState{
				MaxUserID: maxUserID,
				Stage:     UserStageMainMenu,
			}, nil
		}
		return nil, fmt.Errorf("select conversation state: %w", err)
	}

	draft, err := loadActiveDraft(ctx, tx, state.UserID)
	if err != nil {
		return nil, err
	}
	state.ActiveDraft = draft
	return &state, nil
}

func nullableInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
