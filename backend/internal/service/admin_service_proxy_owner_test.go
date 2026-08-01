//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type proxyRepoStubForOwner struct {
	proxyRepoStub
	proxies map[int64]*Proxy
	// ownerAssignmentErr 模拟事务内守卫拒绝（他人账号仍绑在该代理上）。
	ownerAssignmentErr error
	created            *Proxy
	updated            *Proxy
	ownerAssigned      *Proxy
}

func (s *proxyRepoStubForOwner) Create(_ context.Context, proxy *Proxy) error {
	s.created = proxy
	return nil
}

func (s *proxyRepoStubForOwner) GetByID(_ context.Context, id int64) (*Proxy, error) {
	if proxy, ok := s.proxies[id]; ok {
		copied := *proxy
		return &copied, nil
	}
	return nil, ErrProxyNotFound
}

func (s *proxyRepoStubForOwner) Update(_ context.Context, proxy *Proxy) error {
	s.updated = proxy
	return nil
}

func (s *proxyRepoStubForOwner) UpdateWithOwnerAssignment(_ context.Context, proxy *Proxy) error {
	if s.ownerAssignmentErr != nil {
		return s.ownerAssignmentErr
	}
	s.ownerAssigned = proxy
	return nil
}

type userRepoStubForProxyOwner struct {
	userRepoStub
	users map[int64]*User
}

func (s *userRepoStubForProxyOwner) GetByID(_ context.Context, id int64) (*User, error) {
	if user, ok := s.users[id]; ok {
		return user, nil
	}
	return nil, ErrUserNotFound
}

func TestAdminService_CreateProxy_WithOwnerUser(t *testing.T) {
	ownerID := int64(42)
	proxyRepo := &proxyRepoStubForOwner{}
	userRepo := &userRepoStubForProxyOwner{users: map[int64]*User{ownerID: {ID: ownerID}}}
	svc := &adminServiceImpl{proxyRepo: proxyRepo, userRepo: userRepo}

	created, err := svc.CreateProxy(context.Background(), &CreateProxyInput{
		Name:        "exclusive",
		Protocol:    "http",
		Host:        "127.0.0.1",
		Port:        1080,
		OwnerUserID: ownerID,
	})

	require.NoError(t, err)
	require.NotNil(t, created.OwnerUserID)
	require.Equal(t, ownerID, *created.OwnerUserID)
	require.NotNil(t, proxyRepo.created.OwnerUserID)
	require.Equal(t, ownerID, *proxyRepo.created.OwnerUserID)
}

func TestAdminService_CreateProxy_WithoutOwnerIsPlatform(t *testing.T) {
	proxyRepo := &proxyRepoStubForOwner{}
	svc := &adminServiceImpl{proxyRepo: proxyRepo, userRepo: &userRepoStubForProxyOwner{}}

	created, err := svc.CreateProxy(context.Background(), &CreateProxyInput{
		Name:     "platform",
		Protocol: "http",
		Host:     "127.0.0.1",
		Port:     1080,
	})

	require.NoError(t, err)
	require.Nil(t, created.OwnerUserID)
}

func TestAdminService_CreateProxy_OwnerNotFound(t *testing.T) {
	proxyRepo := &proxyRepoStubForOwner{}
	svc := &adminServiceImpl{proxyRepo: proxyRepo, userRepo: &userRepoStubForProxyOwner{}}

	created, err := svc.CreateProxy(context.Background(), &CreateProxyInput{
		Name:        "exclusive",
		Protocol:    "http",
		Host:        "127.0.0.1",
		Port:        1080,
		OwnerUserID: 999,
	})

	require.Nil(t, created)
	require.ErrorIs(t, err, ErrProxyOwnerNotFound)
	require.Nil(t, proxyRepo.created)
}

func TestAdminService_UpdateProxy_AssignOwnerUsesGuardedWrite(t *testing.T) {
	proxyID := int64(7)
	ownerID := int64(42)
	proxyRepo := &proxyRepoStubForOwner{
		proxies: map[int64]*Proxy{proxyID: {ID: proxyID, Name: "p"}},
	}
	userRepo := &userRepoStubForProxyOwner{users: map[int64]*User{ownerID: {ID: ownerID}}}
	svc := &adminServiceImpl{proxyRepo: proxyRepo, userRepo: userRepo}

	updated, err := svc.UpdateProxy(context.Background(), proxyID, &UpdateProxyInput{
		OwnerUserID: &ownerID,
	})

	require.NoError(t, err)
	require.NotNil(t, updated.OwnerUserID)
	require.Equal(t, ownerID, *updated.OwnerUserID)
	// 归属变更必须走带行锁的事务写入，不能走普通 Update。
	require.NotNil(t, proxyRepo.ownerAssigned)
	require.Nil(t, proxyRepo.updated)
}

func TestAdminService_UpdateProxy_AssignOwnerBlockedByOtherUsersAccounts(t *testing.T) {
	proxyID := int64(7)
	ownerID := int64(42)
	proxyRepo := &proxyRepoStubForOwner{
		proxies:            map[int64]*Proxy{proxyID: {ID: proxyID, Name: "p"}},
		ownerAssignmentErr: ErrProxyOwnerConflict,
	}
	userRepo := &userRepoStubForProxyOwner{users: map[int64]*User{ownerID: {ID: ownerID}}}
	svc := &adminServiceImpl{proxyRepo: proxyRepo, userRepo: userRepo}

	updated, err := svc.UpdateProxy(context.Background(), proxyID, &UpdateProxyInput{
		OwnerUserID: &ownerID,
	})

	require.Nil(t, updated)
	require.ErrorIs(t, err, ErrProxyOwnerConflict)
	require.Nil(t, proxyRepo.updated)
}

func TestAdminService_UpdateProxy_ClearOwner(t *testing.T) {
	proxyID := int64(7)
	oldOwner := int64(42)
	clearOwner := int64(0)
	proxyRepo := &proxyRepoStubForOwner{
		proxies: map[int64]*Proxy{proxyID: {ID: proxyID, Name: "p", OwnerUserID: &oldOwner}},
	}
	svc := &adminServiceImpl{proxyRepo: proxyRepo, userRepo: &userRepoStubForProxyOwner{}}

	updated, err := svc.UpdateProxy(context.Background(), proxyID, &UpdateProxyInput{
		OwnerUserID: &clearOwner,
	})

	require.NoError(t, err)
	require.Nil(t, updated.OwnerUserID)
	require.NotNil(t, proxyRepo.ownerAssigned)
	require.Nil(t, proxyRepo.ownerAssigned.OwnerUserID)
}

// 归属未变化时不得重跑归属校验：否则归属用户已注销的历史代理会被锁死，
// 连改名这种无关编辑都做不了。
func TestAdminService_UpdateProxy_UnchangedOwnerSkipsValidation(t *testing.T) {
	proxyID := int64(7)
	deletedOwner := int64(42)
	sameOwner := deletedOwner
	name := "renamed"
	proxyRepo := &proxyRepoStubForOwner{
		proxies: map[int64]*Proxy{proxyID: {ID: proxyID, Name: "p", OwnerUserID: &deletedOwner}},
	}
	// userRepo 里查不到 42（用户已注销）。
	svc := &adminServiceImpl{proxyRepo: proxyRepo, userRepo: &userRepoStubForProxyOwner{}}

	updated, err := svc.UpdateProxy(context.Background(), proxyID, &UpdateProxyInput{
		Name:        name,
		OwnerUserID: &sameOwner,
	})

	require.NoError(t, err)
	require.Equal(t, name, updated.Name)
	require.NotNil(t, updated.OwnerUserID)
	require.Equal(t, deletedOwner, *updated.OwnerUserID)
	require.NotNil(t, proxyRepo.updated)
	require.Nil(t, proxyRepo.ownerAssigned)
}

func TestAdminService_UpdateProxy_OwnerUntouchedWhenNil(t *testing.T) {
	proxyID := int64(7)
	oldOwner := int64(42)
	name := "renamed"
	proxyRepo := &proxyRepoStubForOwner{
		proxies: map[int64]*Proxy{proxyID: {ID: proxyID, Name: "p", OwnerUserID: &oldOwner}},
	}
	svc := &adminServiceImpl{proxyRepo: proxyRepo, userRepo: &userRepoStubForProxyOwner{}}

	updated, err := svc.UpdateProxy(context.Background(), proxyID, &UpdateProxyInput{
		Name: name,
	})

	require.NoError(t, err)
	require.NotNil(t, updated.OwnerUserID)
	require.Equal(t, oldOwner, *updated.OwnerUserID)
	require.NotNil(t, proxyRepo.updated)
	require.Nil(t, proxyRepo.ownerAssigned)
}

func TestAdminService_UpdateProxy_AssignOwnerNotFound(t *testing.T) {
	proxyID := int64(7)
	ownerID := int64(999)
	proxyRepo := &proxyRepoStubForOwner{
		proxies: map[int64]*Proxy{proxyID: {ID: proxyID, Name: "p"}},
	}
	svc := &adminServiceImpl{proxyRepo: proxyRepo, userRepo: &userRepoStubForProxyOwner{}}

	updated, err := svc.UpdateProxy(context.Background(), proxyID, &UpdateProxyInput{
		OwnerUserID: &ownerID,
	})

	require.Nil(t, updated)
	require.ErrorIs(t, err, ErrProxyOwnerNotFound)
	require.Nil(t, proxyRepo.updated)
	require.Nil(t, proxyRepo.ownerAssigned)
}

func TestProxyOwnerAllowsAccountOwner(t *testing.T) {
	owner := int64(42)
	other := int64(43)

	platform := &Proxy{ID: 1}
	exclusive := &Proxy{ID: 2, OwnerUserID: &owner}

	require.True(t, proxyOwnerAllowsAccountOwner(platform, nil))
	require.True(t, proxyOwnerAllowsAccountOwner(platform, &other))
	require.True(t, proxyOwnerAllowsAccountOwner(exclusive, &owner))
	require.False(t, proxyOwnerAllowsAccountOwner(exclusive, &other))
	// 管理员账号（无归属）同样不能占用某个用户的专属出口。
	require.False(t, proxyOwnerAllowsAccountOwner(exclusive, nil))
}

func TestAdminService_EnsureProxyOwnerAllowsAccount(t *testing.T) {
	owner := int64(42)
	other := int64(43)
	proxyRepo := &proxyRepoStubForOwner{
		proxies: map[int64]*Proxy{
			1: {ID: 1},
			2: {ID: 2, OwnerUserID: &owner},
		},
	}
	svc := &adminServiceImpl{proxyRepo: proxyRepo, userRepo: &userRepoStubForProxyOwner{}}
	ctx := context.Background()

	require.NoError(t, svc.ensureProxyOwnerAllowsAccount(ctx, 1, &other))
	require.NoError(t, svc.ensureProxyOwnerAllowsAccount(ctx, 2, &owner))
	require.ErrorIs(t, svc.ensureProxyOwnerAllowsAccount(ctx, 2, &other), ErrProxyOwnerConflict)
	require.ErrorIs(t, svc.ensureProxyOwnerAllowsAccount(ctx, 2, nil), ErrProxyOwnerConflict)
	// proxyID <= 0 表示不绑定代理，直接放行。
	require.NoError(t, svc.ensureProxyOwnerAllowsAccount(ctx, 0, &other))
}
