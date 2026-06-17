package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newProxyEntRepo(t *testing.T) (*proxyRepository, *dbent.Client) {
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

	return newProxyRepositoryWithSQL(client, db), client
}

func TestProxyRepositoryVisibleScopeOnlyIncludesPlatformAndOwnProxies(t *testing.T) {
	repo, client := newProxyEntRepo(t)
	ctx := context.Background()
	ownerA := createProxyOwner(t, ctx, client, "proxy-owner-a@example.com")
	ownerB := createProxyOwner(t, ctx, client, "proxy-owner-b@example.com")

	platform := createProxyForVisibilityTest(t, ctx, repo, &service.Proxy{
		Name:     "platform",
		Protocol: "socks5",
		Host:     "10.0.0.1",
		Port:     1080,
		Status:   service.StatusActive,
	})
	ownedByA := createProxyForVisibilityTest(t, ctx, repo, &service.Proxy{
		Name:        "owned-a",
		Protocol:    "socks5",
		Host:        "10.0.0.2",
		Port:        1080,
		OwnerUserID: &ownerA,
		Status:      service.StatusActive,
	})
	ownedByB := createProxyForVisibilityTest(t, ctx, repo, &service.Proxy{
		Name:        "owned-b",
		Protocol:    "socks5",
		Host:        "10.0.0.3",
		Port:        1080,
		OwnerUserID: &ownerB,
		Status:      service.StatusActive,
	})
	createProxyForVisibilityTest(t, ctx, repo, &service.Proxy{
		Name:        "disabled-owned-a",
		Protocol:    "socks5",
		Host:        "10.0.0.4",
		Port:        1080,
		OwnerUserID: &ownerA,
		Status:      service.StatusDisabled,
	})

	visible, err := repo.ListActiveVisibleWithAccountCount(ctx, ownerA)
	require.NoError(t, err)
	visibleIDs := map[int64]bool{}
	for _, item := range visible {
		visibleIDs[item.ID] = true
	}
	require.True(t, visibleIDs[platform.ID], "platform proxy should be visible")
	require.True(t, visibleIDs[ownedByA.ID], "own proxy should be visible")
	require.False(t, visibleIDs[ownedByB.ID], "other user's proxy must stay hidden")
	require.Len(t, visibleIDs, 2)

	_, err = repo.GetVisibleByID(ctx, ownerA, ownedByB.ID)
	require.ErrorIs(t, err, service.ErrProxyNotFound)
	_, err = repo.FindVisibleActiveByEndpoint(ctx, ownerA, ownedByB.Protocol, ownedByB.Host, ownedByB.Port, ownedByB.Username, ownedByB.Password)
	require.ErrorIs(t, err, service.ErrProxyNotFound)
}

func TestProxyRepositoryFindVisibleActiveByEndpointPrefersOwnProxyOverPlatformDuplicate(t *testing.T) {
	repo, client := newProxyEntRepo(t)
	ctx := context.Background()
	ownerID := createProxyOwner(t, ctx, client, "proxy-owner-duplicate@example.com")

	createProxyForVisibilityTest(t, ctx, repo, &service.Proxy{
		Name:     "platform-duplicate",
		Protocol: "http",
		Host:     "192.168.0.1",
		Port:     8000,
		Username: "user",
		Password: "pass",
		Status:   service.StatusActive,
	})
	owned := createProxyForVisibilityTest(t, ctx, repo, &service.Proxy{
		Name:        "owned-duplicate",
		Protocol:    "http",
		Host:        "192.168.0.1",
		Port:        8000,
		Username:    "user",
		Password:    "pass",
		OwnerUserID: &ownerID,
		Status:      service.StatusActive,
	})

	got, err := repo.FindVisibleActiveByEndpoint(ctx, ownerID, "http", "192.168.0.1", 8000, "user", "pass")
	require.NoError(t, err)
	require.Equal(t, owned.ID, got.ID)
	require.NotNil(t, got.OwnerUserID)
	require.Equal(t, ownerID, *got.OwnerUserID)
}

func createProxyOwner(t *testing.T, ctx context.Context, client *dbent.Client, email string) int64 {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("hash").
		Save(ctx)
	require.NoError(t, err)
	return user.ID
}

func createProxyForVisibilityTest(t *testing.T, ctx context.Context, repo *proxyRepository, proxy *service.Proxy) *service.Proxy {
	t.Helper()
	require.NoError(t, repo.Create(ctx, proxy))
	return proxy
}
