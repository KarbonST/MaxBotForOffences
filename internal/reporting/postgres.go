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
	if draftID > 0 {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM draft_attachments
			WHERE message_id = $1
		`, result.ID); err != nil {
			return nil, fmt.Errorf("delete draft attachments: %w", err)
		}
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
					stage
				)
				VALUES ($1, $2, $3, 'draft', $4, $5, $6, $7, $8, $9)
				RETURNING id
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
			).Scan(&draftID); err != nil {
				return nil, fmt.Errorf("insert draft message: %w", err)
			}
		}
		if err := saveDraftAttachments(ctx, tx, draftID, req.ActiveDraft.AttachmentLog, req.ActiveDraft.Attachments); err != nil {
			return nil, err
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

func (s *PostgresStore) ListPendingNotifications(ctx context.Context, limit int) ([]NotificationItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			n.id,
			n.user_id,
			u.max_id,
			n.notification,
			n.status::text,
			n.created_at,
			n.sended_at
		FROM user_notifications n
		JOIN users u ON u.id = n.user_id
		WHERE n.status = 'new'
		ORDER BY n.created_at ASC, n.id ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending notifications: %w", err)
	}
	defer rows.Close()

	items := make([]NotificationItem, 0)
	for rows.Next() {
		var item NotificationItem
		var sendedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.MaxUserID,
			&item.Notification,
			&item.Status,
			&item.CreatedAt,
			&sendedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending notification: %w", err)
		}
		if sendedAt.Valid {
			value := sendedAt.Time
			item.SendedAt = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending notifications: %w", err)
	}

	return items, nil
}

func (s *PostgresStore) MarkNotificationSent(ctx context.Context, notificationID int64) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE user_notifications
		SET status = 'sended',
		    sended_at = NOW()
		WHERE id = $1
	`, notificationID)
	if err != nil {
		return fmt.Errorf("mark notification sent: %w", err)
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) MarkNotificationError(ctx context.Context, notificationID int64) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE user_notifications
		SET status = 'error'
		WHERE id = $1
	`, notificationID)
	if err != nil {
		return fmt.Errorf("mark notification error: %w", err)
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) GetPendingClarification(ctx context.Context, maxUserID int64) (*ClarificationPrompt, error) {
	var prompt ClarificationPrompt
	var notificationID sql.NullInt64
	var notificationText sql.NullString
	var updatedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, `
		SELECT
			cr.id,
			cr.message_id,
			notification_link.notification_id,
			notification_link.notification_text,
			cr.status::text,
			cr.created_at,
			cr.updated_at
		FROM clarification_requests cr
		JOIN messages m ON m.id = cr.message_id
		JOIN users u ON u.id = m.user_id
		LEFT JOIN LATERAL (
			SELECT
				mh.notification_id,
				un.notification AS notification_text
			FROM messages_history mh
			JOIN user_notifications un ON un.id = mh.notification_id
			WHERE mh.message_id = m.id
			  AND mh.event_type = 'status'
			  AND mh.new_value = 'clarification_requested'
			  AND mh.notification_id IS NOT NULL
			ORDER BY mh.date DESC, mh.id DESC
			LIMIT 1
		) AS notification_link ON TRUE
		WHERE u.max_id = $1
		  AND cr.status = 'new'
		  AND m.status = 'clarification_requested'
		ORDER BY cr.created_at ASC, cr.id ASC
		LIMIT 1
	`, maxUserID).Scan(
		&prompt.ID,
		&prompt.MessageID,
		&notificationID,
		&notificationText,
		&prompt.Status,
		&prompt.CreatedAt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("select pending clarification: %w", err)
	}

	prompt.ReportNumber = fmt.Sprintf("%d", prompt.MessageID)
	if notificationID.Valid {
		prompt.NotificationID = notificationID.Int64
	}
	prompt.NotificationText = strings.TrimSpace(notificationText.String)
	if prompt.NotificationText == "" {
		prompt.NotificationText = fmt.Sprintf("По сообщению №%s требуется уточнение администратора.", prompt.ReportNumber)
	}
	if updatedAt.Valid {
		value := updatedAt.Time
		prompt.UpdatedAt = &value
	}

	return &prompt, nil
}

func (s *PostgresStore) AnswerClarification(ctx context.Context, req ClarificationAnswerRequest) error {
	return s.completeClarification(ctx, req.ClarificationID, req.MaxUserID, nullableString(req.Answer), RequestStatusAnswered, "clarification answered by user")
}

func (s *PostgresStore) RejectClarification(ctx context.Context, req ClarificationRejectRequest) error {
	return s.completeClarification(ctx, req.ClarificationID, req.MaxUserID, nil, RequestStatusRejected, "clarification rejected by user")
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

func (s *PostgresStore) completeClarification(ctx context.Context, clarificationID, maxUserID int64, answer any, status RequestStatus, historyComment string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var messageID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT cr.message_id
		FROM clarification_requests cr
		JOIN messages m ON m.id = cr.message_id
		JOIN users u ON u.id = m.user_id
		WHERE cr.id = $1
		  AND u.max_id = $2
		  AND cr.status = 'new'
		FOR UPDATE
	`, clarificationID, maxUserID).Scan(&messageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("select clarification for completion: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE clarification_requests
		SET answer = $2,
		    status = $3,
		    updated_at = NOW()
		WHERE id = $1
	`, clarificationID, answer, status); err != nil {
		return fmt.Errorf("update clarification: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE messages
		SET status = 'in_progress',
		    updated_at = NOW()
		WHERE id = $1
	`, messageID); err != nil {
		return fmt.Errorf("update message status after clarification: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages_history (
			message_id,
			admin_id,
			event_type,
			new_value,
			comments
		)
		VALUES ($1, NULL, 'status', 'in_progress', $2)
	`, messageID, historyComment); err != nil {
		return fmt.Errorf("insert clarification history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit clarification completion: %w", err)
	}
	return nil
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
	attachmentLog, attachments, err := loadDraftAttachments(ctx, q, draft.ID)
	if err != nil {
		return nil, err
	}
	draft.AttachmentLog = attachmentLog
	draft.Attachments = attachments
	return &draft, nil
}

func saveDraftAttachments(ctx context.Context, tx *sql.Tx, draftID int64, attachmentLog []string, attachments []MediaAttachment) error {
	if draftID <= 0 {
		return nil
	}
	if len(attachmentLog) == 0 && len(attachments) == 0 {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM draft_attachments
			WHERE message_id = $1
		`, draftID); err != nil {
			return fmt.Errorf("delete empty draft attachments: %w", err)
		}
		return nil
	}

	attachmentLogRaw, err := json.Marshal(attachmentLog)
	if err != nil {
		return fmt.Errorf("marshal draft attachment log: %w", err)
	}
	attachmentsRaw, err := json.Marshal(attachments)
	if err != nil {
		return fmt.Errorf("marshal draft attachments: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO draft_attachments (message_id, attachment_log, attachments)
		VALUES ($1, $2, $3)
		ON CONFLICT (message_id) DO UPDATE
		SET attachment_log = EXCLUDED.attachment_log,
		    attachments = EXCLUDED.attachments,
		    updated_at = NOW()
	`, draftID, attachmentLogRaw, attachmentsRaw); err != nil {
		return fmt.Errorf("upsert draft attachments: %w", err)
	}

	return nil
}

func loadDraftAttachments(ctx context.Context, q rowQuerier, draftID int64) ([]string, []MediaAttachment, error) {
	var attachmentLogRaw []byte
	var attachmentsRaw []byte
	err := q.QueryRowContext(ctx, `
		SELECT attachment_log, attachments
		FROM draft_attachments
		WHERE message_id = $1
	`, draftID).Scan(&attachmentLogRaw, &attachmentsRaw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("select draft attachments: %w", err)
	}

	var attachmentLog []string
	if len(attachmentLogRaw) > 0 {
		if err := json.Unmarshal(attachmentLogRaw, &attachmentLog); err != nil {
			return nil, nil, fmt.Errorf("decode draft attachment log: %w", err)
		}
	}

	var attachments []MediaAttachment
	if len(attachmentsRaw) > 0 {
		if err := json.Unmarshal(attachmentsRaw, &attachments); err != nil {
			return nil, nil, fmt.Errorf("decode draft attachments: %w", err)
		}
	}

	return attachmentLog, attachments, nil
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
