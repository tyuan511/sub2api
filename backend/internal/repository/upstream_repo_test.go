package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpstreamBalanceNotificationTransaction(t *testing.T) {
	for _, test := range []struct {
		name                                   string
		balance                                *float64
		already, notify, enqueue, wantNotified bool
	}{
		{"low balance", floatPointer(8), false, true, true, true},
		{"repeat low balance", floatPointer(8), true, true, false, true},
		{"recovered at threshold", floatPointer(10), true, true, false, false},
		{"Telegram disabled", floatPointer(8), false, false, false, false},
		{"query failed", nil, true, true, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := NewUpstreamRepository(db)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT low_balance_notified.*FOR UPDATE").WithArgs(int64(1), int64(2)).WillReturnRows(sqlmock.NewRows([]string{"notified"}).AddRow(test.already))
			if test.enqueue {
				mock.ExpectExec("INSERT INTO support_notification_outbox").WithArgs(service.UpstreamLowBalanceEvent, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
			}
			if test.balance == nil {
				mock.ExpectExec("UPDATE upstreams SET last_error").WithArgs(int64(1), "HTTP 401").WillReturnResult(sqlmock.NewResult(0, 1))
			} else {
				mock.ExpectExec("UPDATE upstreams SET balance").WithArgs(int64(1), *test.balance, test.wantNotified).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectCommit()
			require.NoError(t, repo.RecordBalance(context.Background(), &service.Upstream{ID: 1, Revision: 2, BalanceThreshold: 10}, test.balance, "HTTP 401", test.notify))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
func floatPointer(v float64) *float64 { return &v }

func TestUpstreamStaleQueryCannotOverwriteEditedConfiguration(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT low_balance_notified.*FOR UPDATE").WithArgs(int64(1), int64(2)).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	require.NoError(t, NewUpstreamRepository(db).RecordBalance(context.Background(), &service.Upstream{ID: 1, Revision: 2}, floatPointer(8), "", true))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpstreamNoTelegramBindingsDoesNotConsumeAlert(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT low_balance_notified.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"notified"}).AddRow(false))
	mock.ExpectExec("INSERT INTO support_notification_outbox").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE upstreams SET balance").WithArgs(int64(1), 8.0, false).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, NewUpstreamRepository(db).RecordBalance(context.Background(), &service.Upstream{ID: 1, Revision: 2, BalanceThreshold: 10}, floatPointer(8), "", true))
	require.NoError(t, mock.ExpectationsWereMet())
}
