package repository

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type probeKeyArgument struct{}

func (probeKeyArgument) Match(value driver.Value) bool {
	key, ok := value.(string)
	return ok && strings.HasPrefix(key, ProbeInternalKeyPrefix) && len(key) == len(ProbeInternalKeyPrefix)+64
}

func TestProbeInternalKeyHiddenFromUserKeyQueries(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "probe-hidden@example.com")
	internal := &service.APIKey{UserID: user.ID, Key: ProbeInternalKeyPrefix + "test", Name: "分组检测", Status: service.StatusDisabled}
	public := &service.APIKey{UserID: user.ID, Key: "sk-normal-probe-test", Name: "Normal", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, internal))
	require.NoError(t, repo.Create(ctx, public))
	keys, page, err := repo.ListByUserID(ctx, user.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, public.ID, keys[0].ID)
	require.EqualValues(t, 1, page.Total)
	count, err := repo.CountByUserID(ctx, user.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	_, err = repo.GetByID(ctx, internal.ID)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	_, _, err = repo.GetKeyAndOwnerID(ctx, internal.ID)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	_, err = repo.GetByKeyForAuth(ctx, internal.Key)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	entity, err := client.APIKey.Get(ctx, internal.ID)
	require.NoError(t, err)
	require.Empty(t, apiKeyEntityToService(entity).Key)
}

func TestProbeInternalKeyGetOrCreate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	r := NewProbeInternalKeyRepository(db)
	mock.ExpectQuery(`(?s)INSERT INTO api_keys.*ON CONFLICT.*RETURNING id`).
		WithArgs(int64(7), int64(9), probeKeyArgument{}).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
	id, err := r.GetOrCreate(context.Background(), 7, 9)
	require.NoError(t, err)
	require.EqualValues(t, 42, id)
	_, err = r.GetOrCreate(context.Background(), 0, 9)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
