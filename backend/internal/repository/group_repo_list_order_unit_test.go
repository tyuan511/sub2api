package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newGroupEntRepo(t *testing.T) (*groupRepository, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(10)

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return newGroupRepositoryWithSQL(client, db), client
}

func seedGroup(t *testing.T, client *dbent.Client, name, platform string, rate float64, sortOrder int) {
	t.Helper()
	err := client.Group.Create().
		SetName(name).
		SetPlatform(platform).
		SetRateMultiplier(rate).
		SetSortOrder(sortOrder).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		Exec(context.Background())
	require.NoError(t, err)
}

func listGroupNames(t *testing.T, repo *groupRepository, params pagination.PaginationParams) []string {
	t.Helper()
	groups, _, err := repo.ListWithFilters(context.Background(), params, "", "", "", nil)
	require.NoError(t, err)
	names := make([]string, 0, len(groups))
	for i := range groups {
		names = append(names, groups[i].Name)
	}
	return names
}

// 分组列表默认视图：手动排序优先，同一 sort_order 内按平台定制顺序、倍率从低到高。
func TestGroupListDefaultOrderGroupsByPlatformThenRate(t *testing.T) {
	repo, client := newGroupEntRepo(t)
	seedGroup(t, client, "gpt-050", "openai", 0.5, 0)
	seedGroup(t, client, "gpt-010", "openai", 0.1, 0)
	seedGroup(t, client, "gpt-025", "openai", 0.25, 0)
	seedGroup(t, client, "claude-04", "anthropic", 0.4, 0)
	seedGroup(t, client, "claude-03", "anthropic", 0.3, 0)
	seedGroup(t, client, "grok-heavy", "grok", 0.1, 0)
	seedGroup(t, client, "gemini-low", "gemini", 0.05, 0)
	// 不在定制顺序里的平台排到末尾，按平台名字母序（aaatest 字母序最前但仍在定制组之后）。
	seedGroup(t, client, "aaa-other", "aaatest", 1, 0)
	// 手动排到最后的分组不受平台影响，仍然排在末尾。
	seedGroup(t, client, "manual-last", "openai", 0.2, 99)

	expected := []string{
		"gpt-010", "gpt-025", "gpt-050",
		"claude-03", "claude-04",
		"grok-heavy",
		"gemini-low",
		"aaa-other",
		"manual-last",
	}
	for _, sortBy := range []string{"", "sort_order"} {
		names := listGroupNames(t, repo, pagination.PaginationParams{Page: 1, PageSize: 50, SortBy: sortBy})
		require.Equal(t, expected, names, "sortBy=%q", sortBy)
	}

	// 下拉候选清单走 ListActive，顺序必须一致。
	active, err := repo.ListActive(context.Background())
	require.NoError(t, err)
	activeNames := make([]string, 0, len(active))
	for i := range active {
		activeNames = append(activeNames, active[i].Name)
	}
	require.Equal(t, expected, activeNames)
}

// 定制顺序优先于字母序：openai 在 anthropic 前，不在列表里的平台按字母序排到末尾。
func TestGroupListPlatformRankBeatsAlphabeticalOrder(t *testing.T) {
	repo, client := newGroupEntRepo(t)
	seedGroup(t, client, "z-zhipu", "zhipu", 0.5, 0)
	seedGroup(t, client, "a-antigravity", "antigravity", 0.5, 0)
	seedGroup(t, client, "grok-1", "grok", 9, 0)
	seedGroup(t, client, "openai-1", "openai", 9, 0)
	seedGroup(t, client, "anthropic-1", "anthropic", 9, 0)
	seedGroup(t, client, "gemini-1", "gemini", 9, 0)
	seedGroup(t, client, "b-antigravity", "antigravity", 0.2, 0)

	require.Equal(t, []string{
		"openai-1", "anthropic-1", "grok-1", "gemini-1",
		"b-antigravity", "a-antigravity", "z-zhipu",
	}, listGroupNames(t, repo, pagination.PaginationParams{Page: 1, PageSize: 50}))
}

// 点“平台”列：平台定制顺序优先于手动排序，同平台内再按倍率升序。
func TestGroupListPlatformSortTiesBreakOnRate(t *testing.T) {
	repo, client := newGroupEntRepo(t)
	seedGroup(t, client, "grok-heavy", "grok", 0.1, 7)
	seedGroup(t, client, "gpt-050", "openai", 0.5, 0)
	seedGroup(t, client, "gpt-010", "openai", 0.1, 0)
	seedGroup(t, client, "claude-03", "anthropic", 0.3, 0)

	require.Equal(t, []string{"gpt-010", "gpt-050", "claude-03", "grok-heavy"},
		listGroupNames(t, repo, pagination.PaginationParams{Page: 1, PageSize: 50, SortBy: "platform", SortOrder: pagination.SortOrderAsc}))

	require.Equal(t, []string{"grok-heavy", "claude-03", "gpt-050", "gpt-010"},
		listGroupNames(t, repo, pagination.PaginationParams{Page: 1, PageSize: 50, SortBy: "platform", SortOrder: pagination.SortOrderDesc}))
}

// 单列排序（倍率 / 名称）行为保持不变。
func TestGroupListSingleColumnSortUnchanged(t *testing.T) {
	repo, client := newGroupEntRepo(t)
	seedGroup(t, client, "b-openai", "openai", 0.5, 0)
	seedGroup(t, client, "a-grok", "grok", 0.1, 0)
	seedGroup(t, client, "c-anthropic", "anthropic", 0.9, 0)

	require.Equal(t, []string{"a-grok", "b-openai", "c-anthropic"},
		listGroupNames(t, repo, pagination.PaginationParams{Page: 1, PageSize: 50, SortBy: "rate_multiplier", SortOrder: pagination.SortOrderAsc}))
	require.Equal(t, []string{"c-anthropic", "b-openai", "a-grok"},
		listGroupNames(t, repo, pagination.PaginationParams{Page: 1, PageSize: 50, SortBy: "rate_multiplier", SortOrder: pagination.SortOrderDesc}))
	require.Equal(t, []string{"a-grok", "b-openai", "c-anthropic"},
		listGroupNames(t, repo, pagination.PaginationParams{Page: 1, PageSize: 50, SortBy: "name", SortOrder: pagination.SortOrderAsc}))
}
