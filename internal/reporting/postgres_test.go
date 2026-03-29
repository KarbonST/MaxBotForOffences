package reporting

import (
	"context"
	"database/sql"
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

	store := &PostgresStore{db: db}

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
		INSERT INTO users (max_id, stage)
		VALUES ($1, 'main_menu')
		ON CONFLICT (max_id) DO UPDATE
		SET stage = 'main_menu',
		    updated_at = NOW()
		RETURNING id
	`)).
		WithArgs(int64(777)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))

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
		INSERT INTO messages_history (
			message_id,
			admin_id,
			event_type,
			new_value,
			comments
		)
		VALUES ($1, NULL, 'status', 'moderation', 'created by max bot')
	`)).
		WithArgs(int64(15)).
		WillReturnResult(sqlmock.NewResult(1, 1))

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

func TestPostgresStoreGetReportByIDNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	store := &PostgresStore{db: db}
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
