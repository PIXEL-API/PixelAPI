//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type proxyRepoStubForAccountLimit struct {
	proxyRepoStub
	proxies map[int64]*Proxy
	counts  map[int64]int64
	updated *Proxy
	created *Proxy
}

func (s *proxyRepoStubForAccountLimit) Create(_ context.Context, proxy *Proxy) error {
	s.created = proxy
	return nil
}

func (s *proxyRepoStubForAccountLimit) GetByID(_ context.Context, id int64) (*Proxy, error) {
	if proxy, ok := s.proxies[id]; ok {
		return proxy, nil
	}
	return nil, ErrProxyNotFound
}

func (s *proxyRepoStubForAccountLimit) Update(_ context.Context, proxy *Proxy) error {
	s.updated = proxy
	return nil
}

func (s *proxyRepoStubForAccountLimit) CountAccountsByProxyID(_ context.Context, proxyID int64) (int64, error) {
	return s.counts[proxyID], nil
}

type accountRepoStubForProxyLimit struct {
	accountRepoStub
	created          *Account
	updated          *Account
	bulkUpdateCalled bool
	getByIDAccount   *Account
	getByIDsAccounts []*Account
}

func (s *accountRepoStubForProxyLimit) Create(_ context.Context, account *Account) error {
	s.created = account
	account.ID = 100
	return nil
}

func (s *accountRepoStubForProxyLimit) Update(_ context.Context, account *Account) error {
	s.updated = account
	return nil
}

func (s *accountRepoStubForProxyLimit) GetByID(_ context.Context, _ int64) (*Account, error) {
	if s.getByIDAccount == nil {
		return nil, ErrAccountNotFound
	}
	return s.getByIDAccount, nil
}

func (s *accountRepoStubForProxyLimit) GetByIDs(_ context.Context, _ []int64) ([]*Account, error) {
	return s.getByIDsAccounts, nil
}

func (s *accountRepoStubForProxyLimit) BulkUpdate(_ context.Context, _ []int64, _ AccountBulkUpdate) (int64, error) {
	s.bulkUpdateCalled = true
	return 0, nil
}

func TestAdminService_CreateAccount_ProxyLimitExceeded(t *testing.T) {
	proxyID := int64(7)
	accountRepo := &accountRepoStubForProxyLimit{}
	proxyRepo := &proxyRepoStubForAccountLimit{
		proxies: map[int64]*Proxy{
			proxyID: {ID: proxyID, MaxAccounts: 2},
		},
		counts: map[int64]int64{proxyID: 2},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, proxyRepo: proxyRepo}

	created, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "limit-test",
		Platform:             PlatformOpenAI,
		Type:                 AccountTypeOAuth,
		Credentials:          map[string]any{"access_token": "token"},
		ProxyID:              &proxyID,
		Concurrency:          1,
		Priority:             50,
		SkipDefaultGroupBind: true,
	})

	require.Nil(t, created)
	require.ErrorIs(t, err, ErrProxyAccountLimitExceeded)
	require.Nil(t, accountRepo.created)
}

func TestAdminService_UpdateProxy_MaxAccountsBelowCurrentRejected(t *testing.T) {
	proxyID := int64(7)
	maxAccounts := 2
	proxyRepo := &proxyRepoStubForAccountLimit{
		proxies: map[int64]*Proxy{
			proxyID: {ID: proxyID, MaxAccounts: 0},
		},
		counts: map[int64]int64{proxyID: 3},
	}
	svc := &adminServiceImpl{proxyRepo: proxyRepo}

	updated, err := svc.UpdateProxy(context.Background(), proxyID, &UpdateProxyInput{
		MaxAccounts: &maxAccounts,
	})

	require.Nil(t, updated)
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_accounts cannot be lower than current count")
	require.Nil(t, proxyRepo.updated)
}

func TestAdminService_BulkUpdateAccounts_ProxyLimitCountsOnlyNewBindings(t *testing.T) {
	targetProxyID := int64(7)
	existingProxyID := int64(8)
	accountRepo := &accountRepoStubForProxyLimit{
		getByIDsAccounts: []*Account{
			{ID: 1, ProxyID: nil},
			{ID: 2, ProxyID: &targetProxyID},
			{ID: 3, ProxyID: &existingProxyID},
		},
	}
	proxyRepo := &proxyRepoStubForAccountLimit{
		proxies: map[int64]*Proxy{
			targetProxyID: {ID: targetProxyID, MaxAccounts: 5},
		},
		counts: map[int64]int64{targetProxyID: 4},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, proxyRepo: proxyRepo}

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1, 2, 3},
		ProxyID:    &targetProxyID,
	})

	require.Nil(t, result)
	require.ErrorIs(t, err, ErrProxyAccountLimitExceeded)
	require.False(t, accountRepo.bulkUpdateCalled)
}
