package reporting

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresStoreCreateReport(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	store := &PostgresStore{
		db:                  db,
		hasDialogReports:    true,
		hasDraftAttachments: true,
		historyValueExpr:    "new_value",
		historyCommentCol:   "comments",
		historyEnabled:      true,
		historyStrict:       true,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT message_id
		FROM dialog_reports
		WHERE dedup_key = $1
		FOR UPDATE
	`)).
		WithArgs("dlg-1").
		WillReturnRows(sqlmock.NewRows([]string{"message_id"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO users (max_id, stage, previous_stage)
		VALUES ($1, 'main_menu', NULL)
		ON CONFLICT (max_id) DO UPDATE
		SET stage = 'main_menu',
		    updated_at = NOW()
		RETURNING id
	`)).
		WithArgs(int64(777)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id
		FROM messages
		WHERE user_id = $1 AND status = 'draft'
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
		FOR UPDATE
	`)).
		WithArgs(int64(11)).
		WillReturnError(sql.ErrNoRows)

	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(`
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
	`)).
		WithArgs(int64(11), 1, 2, "9991234567", "ул. Мира", "ночь", "Описание", "Доп").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "stage", "created_at", "sended_at", "updated_at"}).
			AddRow(int64(15), "moderation", "sended", now, now, now))

	mock.ExpectExec(regexp.QuoteMeta(`
			UPDATE dialog_reports
			SET message_id = $1, normalized_at = NOW()
			WHERE dedup_key = $2 AND message_id IS NULL
		`)).
		WithArgs(int64(15), "dlg-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, err := store.CreateReport(context.Background(), CreateReportRequest{
		DialogDedupKey: "dlg-1",
		MaxUserID:      777,
		CategoryID:     1,
		MunicipalityID: 2,
		Phone:          "9991234567",
		Address:        "ул. Мира",
		IncidentTime:   "ночь",
		Description:    "Описание",
		AdditionalInfo: "Доп",
	})
	if err != nil {
		t.Fatalf("CreateReport() error = %v", err)
	}
	if result.ID != 15 || result.ReportNumber != "15" {
		t.Fatalf("unexpected create result: %+v", result)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresStoreCreateReportIgnoresHistoryErrorWhenNotStrict(t *testing.T) {
	t.Skip("история статусов больше не пишется вручную: поведение закрыто БД-триггерами")

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	store := &PostgresStore{
		db:                  db,
		hasDialogReports:    true,
		hasDraftAttachments: true,
		historyValueExpr:    "new_value",
		historyCommentCol:   "comments",
		historyEnabled:      true,
		historyStrict:       false,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT message_id
		FROM dialog_reports
		WHERE dedup_key = $1
		FOR UPDATE
	`)).
		WithArgs("dlg-1").
		WillReturnRows(sqlmock.NewRows([]string{"message_id"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO users (max_id, stage, previous_stage)
		VALUES ($1, 'main_menu', NULL)
		ON CONFLICT (max_id) DO UPDATE
		SET stage = 'main_menu',
		    updated_at = NOW()
		RETURNING id
	`)).
		WithArgs(int64(777)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id
		FROM messages
		WHERE user_id = $1 AND status = 'draft'
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
		FOR UPDATE
	`)).
		WithArgs(int64(11)).
		WillReturnError(sql.ErrNoRows)

	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(`
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
	`)).
		WithArgs(int64(11), 1, 2, "9991234567", "ул. Мира", "ночь", "Описание", "Доп").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "stage", "created_at", "sended_at", "updated_at"}).
			AddRow(int64(15), "moderation", "sended", now, now, now))

	mock.ExpectExec(regexp.QuoteMeta(`
			UPDATE dialog_reports
			SET message_id = $1, normalized_at = NOW()
			WHERE dedup_key = $2 AND message_id IS NULL
		`)).
		WithArgs(int64(15), "dlg-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO messages_history (
			message_id,
			admin_id,
			event_type,
			new_value,
			comments
		)
		VALUES ($1, NULL, 'status', $2, $3)
	`)).
		WithArgs(int64(15), "moderation", "created by max bot").
		WillReturnError(fmt.Errorf("history insert failed"))

	result, err := store.CreateReport(context.Background(), CreateReportRequest{
		DialogDedupKey: "dlg-1",
		MaxUserID:      777,
		CategoryID:     1,
		MunicipalityID: 2,
		Phone:          "9991234567",
		Address:        "ул. Мира",
		IncidentTime:   "ночь",
		Description:    "Описание",
		AdditionalInfo: "Доп",
	})
	if err != nil {
		t.Fatalf("CreateReport() error = %v", err)
	}
	if result.ID != 15 || result.ReportNumber != "15" {
		t.Fatalf("unexpected create result: %+v", result)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresStoreGetReportByIDNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	store := &PostgresStore{
		db:                  db,
		hasDraftAttachments: true,
		historyValueExpr:    "new_value",
		historyCommentCol:   "comments",
		historyEnabled:      true,
	}
	mock.ExpectQuery(regexp.QuoteMeta(`
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
	`)).
		WithArgs(int64(99)).
		WillReturnError(sql.ErrNoRows)

	_, err = store.GetReportByID(context.Background(), 99)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPostgresStoreSaveConversationCreatesDraft(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	store := &PostgresStore{
		db:                  db,
		hasDraftAttachments: true,
		historyValueExpr:    "new_value",
		historyCommentCol:   "comments",
		historyEnabled:      true,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO users (max_id, stage, previous_stage)
		VALUES ($1, $2, NULLIF($3, '')::user_stage)
		ON CONFLICT (max_id) DO UPDATE
		SET stage = EXCLUDED.stage,
		    previous_stage = EXCLUDED.previous_stage,
		    updated_at = NOW()
		RETURNING id
	`)).
		WithArgs(int64(777), UserStageFillingReport, UserStage("")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id
		FROM messages
		WHERE user_id = $1 AND status = 'draft'
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
		FOR UPDATE
	`)).
		WithArgs(int64(11)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`
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
			`)).
		WithArgs(int64(11), 1, nil, nil, nil, nil, nil, nil, MessageStageCategory).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(20)))
	mock.ExpectExec(regexp.QuoteMeta(`
			DELETE FROM draft_attachments
			WHERE message_id = $1
		`)).
		WithArgs(int64(20)).
		WillReturnResult(sqlmock.NewResult(1, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, max_id, stage::text, COALESCE(previous_stage::text, '')
		FROM users
		WHERE max_id = $1
	`)).
		WithArgs(int64(777)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "max_id", "stage", "previous_stage"}).
			AddRow(int64(11), int64(777), string(UserStageFillingReport), ""))
	mock.ExpectQuery(regexp.QuoteMeta(`
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
	`)).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "stage", "category_id", "municipality_id", "phone", "address", "incident_time", "description", "additional_info"}).
			AddRow(int64(20), string(MessageStatusDraft), string(MessageStageCategory), 1, 0, "", "", "", "", ""))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT attachment_log, attachments
		FROM draft_attachments
		WHERE message_id = $1
	`)).
		WithArgs(int64(20)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectCommit()

	state, err := store.SaveConversation(context.Background(), SaveConversationRequest{
		MaxUserID: 777,
		UserStage: UserStageFillingReport,
		ActiveDraft: &DraftMessage{
			Stage:      MessageStageCategory,
			CategoryID: 1,
		},
	})
	if err != nil {
		t.Fatalf("SaveConversation() error = %v", err)
	}
	if state.ActiveDraft == nil || state.ActiveDraft.Stage != MessageStageCategory {
		t.Fatalf("unexpected state: %+v", state)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresStoreSaveConversationPersistsDraftAttachments(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	store := &PostgresStore{
		db:                  db,
		hasDraftAttachments: true,
		historyValueExpr:    "new_value",
		historyCommentCol:   "comments",
		historyEnabled:      true,
	}

	rawPayload, err := json.Marshal(map[string]any{"url": "https://example.com/photo.webp"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO users (max_id, stage, previous_stage)
		VALUES ($1, $2, NULLIF($3, '')::user_stage)
		ON CONFLICT (max_id) DO UPDATE
		SET stage = EXCLUDED.stage,
		    previous_stage = EXCLUDED.previous_stage,
		    updated_at = NOW()
		RETURNING id
	`)).
		WithArgs(int64(555), UserStageFillingReport, UserStage("")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id
		FROM messages
		WHERE user_id = $1 AND status = 'draft'
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
		FOR UPDATE
	`)).
		WithArgs(int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`
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
			`)).
		WithArgs(int64(42), 11, 21, "9991234567", "ул. Мира, 1", "ночью", "Описание", nil, MessageStageFiles).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(77)))
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO draft_attachments (message_id, attachment_log, attachments)
		VALUES ($1, $2, $3)
		ON CONFLICT (message_id) DO UPDATE
		SET attachment_log = EXCLUDED.attachment_log,
		    attachments = EXCLUDED.attachments,
		    updated_at = NOW()
	`)).
		WithArgs(int64(77), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, max_id, stage::text, COALESCE(previous_stage::text, '')
		FROM users
		WHERE max_id = $1
	`)).
		WithArgs(int64(555)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "max_id", "stage", "previous_stage"}).
			AddRow(int64(42), int64(555), string(UserStageFillingReport), ""))
	mock.ExpectQuery(regexp.QuoteMeta(`
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
	`)).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "stage", "category_id", "municipality_id", "phone", "address", "incident_time", "description", "additional_info"}).
			AddRow(int64(77), string(MessageStatusDraft), string(MessageStageFiles), 11, 21, "9991234567", "ул. Мира, 1", "ночью", "Описание", ""))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT attachment_log, attachments
		FROM draft_attachments
		WHERE message_id = $1
	`)).
		WithArgs(int64(77)).
		WillReturnRows(sqlmock.NewRows([]string{"attachment_log", "attachments"}).
			AddRow(`["- photo"]`, fmt.Sprintf(`[{"type":"photo","payload":%s,"file_name":"test.webp","mime_type":"image/webp"}]`, string(rawPayload))))
	mock.ExpectCommit()

	state, err := store.SaveConversation(context.Background(), SaveConversationRequest{
		MaxUserID: 555,
		UserStage: UserStageFillingReport,
		ActiveDraft: &DraftMessage{
			Stage:          MessageStageFiles,
			CategoryID:     11,
			MunicipalityID: 21,
			Phone:          "9991234567",
			Address:        "ул. Мира, 1",
			IncidentTime:   "ночью",
			Description:    "Описание",
			AttachmentLog:  []string{"- photo"},
			Attachments: []MediaAttachment{
				{
					Type:     "photo",
					Payload:  rawPayload,
					FileName: "test.webp",
					MIMEType: "image/webp",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("SaveConversation() error = %v", err)
	}
	if state.ActiveDraft == nil || len(state.ActiveDraft.Attachments) != 1 {
		t.Fatalf("expected restored draft attachment, got %+v", state)
	}
	if state.ActiveDraft.Attachments[0].Type != "photo" {
		t.Fatalf("unexpected attachment: %+v", state.ActiveDraft.Attachments[0])
	}
	if len(state.ActiveDraft.AttachmentLog) != 1 || state.ActiveDraft.AttachmentLog[0] != "- photo" {
		t.Fatalf("unexpected attachment log: %+v", state.ActiveDraft.AttachmentLog)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresStoreInsertStatusHistoryValueJSON(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	store := &PostgresStore{
		db:                db,
		historyValueExpr:  "value",
		historyCommentCol: "comment",
		historyEnabled:    true,
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
			INSERT INTO messages_history (
				message_id,
				admin_id,
				event_type,
				value,
				comment
			)
			VALUES ($1, NULL, 'status', jsonb_build_object('old_value', NULL::text, 'new_value', $2::text), $3)
		`)).
		WithArgs(int64(15), "moderation", "created by max bot").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("db.BeginTx() error = %v", err)
	}

	if err := store.insertStatusHistory(context.Background(), tx, 15, "moderation", "created by max bot"); err != nil {
		t.Fatalf("insertStatusHistory() error = %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresStoreInsertStatusHistoryDisabled(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	store := &PostgresStore{
		db:             db,
		historyEnabled: false,
	}

	mock.ExpectBegin()
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("db.BeginTx() error = %v", err)
	}

	if err := store.insertStatusHistory(context.Background(), tx, 15, "moderation", "created by max bot"); err != nil {
		t.Fatalf("insertStatusHistory() error = %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestHistoryFlagsDefaults(t *testing.T) {
	t.Setenv("REPORT_WRITE_HISTORY", "")
	t.Setenv("REPORT_HISTORY_STRICT", "")

	if !envBoolDefaultTrue("REPORT_WRITE_HISTORY") {
		t.Fatalf("REPORT_WRITE_HISTORY default must be true")
	}
	if envBoolDefaultFalse("REPORT_HISTORY_STRICT") {
		t.Fatalf("REPORT_HISTORY_STRICT default must be false")
	}

	t.Setenv("REPORT_WRITE_HISTORY", "false")
	if envBoolDefaultTrue("REPORT_WRITE_HISTORY") {
		t.Fatalf("REPORT_WRITE_HISTORY=false must disable history writes")
	}
}
