package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestSupportUnreadCountUsesOneQuery(t *testing.T) {
	ctx := context.Background()
	var trackQueries bool
	var queryCount int

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	driver := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(
		dbent.Driver(driver),
		dbent.Debug(),
		dbent.Log(func(...any) {
			if trackQueries {
				queryCount++
			}
		}),
	))
	t.Cleanup(func() { _ = client.Close() })

	admin := client.User.Create().SetEmail("admin@example.com").SetPasswordHash("hash").SetRole(RoleAdmin).SaveX(ctx)
	userOne := client.User.Create().SetEmail("one@example.com").SetPasswordHash("hash").SaveX(ctx)
	userTwo := client.User.Create().SetEmail("two@example.com").SetPasswordHash("hash").SaveX(ctx)

	ticketOne := client.SupportTicket.Create().
		SetTicketNo("T-1").SetUserID(userOne.ID).SetTitle("one").SetCategory("support").SaveX(ctx)
	ticketTwo := client.SupportTicket.Create().
		SetTicketNo("T-2").SetUserID(userTwo.ID).SetTitle("two").SetCategory("support").SaveX(ctx)

	userOneFirst := client.SupportTicketMessage.Create().
		SetTicketID(ticketOne.ID).SetSenderID(userOne.ID).SetSenderRole(RoleUser).SetContent("first").SaveX(ctx)
	client.SupportTicketMessage.Create().
		SetTicketID(ticketOne.ID).SetSenderID(admin.ID).SetSenderRole(RoleAdmin).SetContent("reply").SaveX(ctx)
	client.SupportTicketMessage.Create().
		SetTicketID(ticketOne.ID).SetSenderID(userOne.ID).SetSenderRole(RoleUser).SetContent("unread by admin").SaveX(ctx)
	client.SupportTicketMessage.Create().
		SetTicketID(ticketOne.ID).SetSenderID(admin.ID).SetSenderRole(RoleAdmin).SetKind("internal").SetContent("hidden").SaveX(ctx)
	client.SupportTicketMessage.Create().
		SetTicketID(ticketTwo.ID).SetSenderID(userTwo.ID).SetSenderRole(RoleUser).SetContent("unread by admin").SaveX(ctx)
	client.SupportTicketMessage.Create().
		SetTicketID(ticketTwo.ID).SetSenderID(admin.ID).SetSenderRole(RoleAdmin).SetContent("reply").SaveX(ctx)

	client.SupportTicketRead.Create().
		SetTicketID(ticketOne.ID).SetReaderID(admin.ID).SetLastReadMessageID(userOneFirst.ID).SaveX(ctx)
	client.SupportTicketRead.Create().
		SetTicketID(ticketOne.ID).SetReaderID(userOne.ID).SetLastReadMessageID(userOneFirst.ID).SaveX(ctx)

	service := NewSupportService(client, nil, nil)
	trackQueries = true

	assertUnread := func(readerID int64, isAdmin bool, want int) {
		t.Helper()
		queryCount = 0
		got, err := service.UnreadCount(ctx, readerID, isAdmin)
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, 1, queryCount, "unread count must remain a single aggregate query")
	}

	assertUnread(admin.ID, true, 2)
	assertUnread(userOne.ID, false, 1)
	assertUnread(userTwo.ID, false, 1)
}
