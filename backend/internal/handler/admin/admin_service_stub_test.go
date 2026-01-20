package admin

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type stubAdminService struct {
	users       []service.User
	apiKeys     []service.APIKey
	groups      []service.Group
	accounts    []service.Account
	proxies     []service.Proxy
	proxyCounts []service.ProxyWithAccountCount
	redeems     []service.RedeemCode
}

func newStubAdminService() *stubAdminService {
	now := time.Now().UTC()
	user := service.User{
		ID:        1,
		Email:     "user@example.com",
		Role:      service.RoleUser,
		Status:    service.StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	apiKey := service.APIKey{
		ID:        10,
		UserID:    user.ID,
		Key:       "sk-test",
		Name:      "test",
		Status:    service.StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	group := service.Group{
		ID:        2,
		Name:      "group",
		Platform:  service.PlatformAnthropic,
		Status:    service.StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	account := service.Account{
		ID:        3,
		Name:      "account",
		Platform:  service.PlatformAnthropic,
		Type:      service.AccountTypeOAuth,
		Status:    service.StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	proxy := service.Proxy{
		ID:        4,
		Name:      "proxy",
		Protocol:  "http",
		Host:      "127.0.0.1",
		Port:      8080,
		Status:    service.StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	redeem := service.RedeemCode{
		ID:        5,
		Code:      "R-TEST",
		Type:      service.RedeemTypeBalance,
		Value:     10,
		Status:    service.StatusUnused,
		CreatedAt: now,
	}
	return &stubAdminService{
		users:       []service.User{user},
		apiKeys:     []service.APIKey{apiKey},
		groups:      []service.Group{group},
		accounts:    []service.Account{account},
		proxies:     []service.Proxy{proxy},
		proxyCounts: []service.ProxyWithAccountCount{{Proxy: proxy, AccountCount: 1}},
		redeems:     []service.RedeemCode{redeem},
	}
}

func (s *stubAdminService) ListUsers(ctx context.Context, page, pageSize int, filters service.UserListFilters) ([]service.User, int64, error) {
	return s.users, int64(len(s.users)), nil
}

func (s *stubAdminService) GetUser(ctx context.Context, id int64) (*service.User, error) {
	for i := range s.users {
		if s.users[i].ID == id {
			return &s.users[i], nil
		}
	}
	user := service.User{ID: id, Email: "user@example.com", Status: service.StatusActive}
	return &user, nil
}

func (s *stubAdminService) CreateUser(ctx context.Context, input *service.CreateUserInput) (*service.User, error) {
	user := service.User{ID: 100, Email: input.Email, Status: service.StatusActive}
	return &user, nil
}

func (s *stubAdminService) UpdateUser(ctx context.Context, id int64, input *service.UpdateUserInput) (*service.User, error) {
	user := service.User{ID: id, Email: "updated@example.com", Status: service.StatusActive}
	return &user, nil
}

func (s *stubAdminService) DeleteUser(ctx context.Context, id int64) error {
	return nil
}

func (s *stubAdminService) UpdateUserBalance(ctx context.Context, userID int64, balance float64, operation string, notes string) (*service.User, error) {
	user := service.User{ID: userID, Balance: balance, Status: service.StatusActive}
	return &user, nil
}

func (s *stubAdminService) GetUserAPIKeys(ctx context.Context, userID int64, page, pageSize int) ([]service.APIKey, int64, error) {
	return s.apiKeys, int64(len(s.apiKeys)), nil
}

func (s *stubAdminService) GetUserUsageStats(ctx context.Context, userID int64, period string) (any, error) {
	return map[string]any{"user_id": userID}, nil
}

func (s *stubAdminService) ListGroups(ctx context.Context, page, pageSize int, platform, status, search string, isExclusive *bool) ([]service.Group, int64, error) {
	return s.groups, int64(len(s.groups)), nil
}

func (s *stubAdminService) GetAllGroups(ctx context.Context) ([]service.Group, error) {
	return s.groups, nil
}

func (s *stubAdminService) GetAllGroupsByPlatform(ctx context.Context, platform string) ([]service.Group, error) {
	return s.groups, nil
}

func (s *stubAdminService) GetGroup(ctx context.Context, id int64) (*service.Group, error) {
	group := service.Group{ID: id, Name: "group", Status: service.StatusActive}
	return &group, nil
}

func (s *stubAdminService) CreateGroup(ctx context.Context, input *service.CreateGroupInput) (*service.Group, error) {
	group := service.Group{ID: 200, Name: input.Name, Status: service.StatusActive}
	return &group, nil
}

func (s *stubAdminService) UpdateGroup(ctx context.Context, id int64, input *service.UpdateGroupInput) (*service.Group, error) {
	group := service.Group{ID: id, Name: input.Name, Status: service.StatusActive}
	return &group, nil
}

func (s *stubAdminService) DeleteGroup(ctx context.Context, id int64) error {
	return nil
}

func (s *stubAdminService) GetGroupAPIKeys(ctx context.Context, groupID int64, page, pageSize int) ([]service.APIKey, int64, error) {
	return s.apiKeys, int64(len(s.apiKeys)), nil
}

func (s *stubAdminService) ListAccounts(ctx context.Context, page, pageSize int, platform, accountType, status, search string) ([]service.Account, int64, error) {
	return s.accounts, int64(len(s.accounts)), nil
}

func (s *stubAdminService) GetAccount(ctx context.Context, id int64) (*service.Account, error) {
	account := service.Account{ID: id, Name: "account", Status: service.StatusActive}
	return &account, nil
}

func (s *stubAdminService) GetAccountsByIDs(ctx context.Context, ids []int64) ([]*service.Account, error) {
	out := make([]*service.Account, 0, len(ids))
	for _, id := range ids {
		account := service.Account{ID: id, Name: "account", Status: service.StatusActive}
		out = append(out, &account)
	}
	return out, nil
}

func (s *stubAdminService) CreateAccount(ctx context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	account := service.Account{ID: 300, Name: input.Name, Status: service.StatusActive}
	return &account, nil
}

func (s *stubAdminService) UpdateAccount(ctx context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	account := service.Account{ID: id, Name: input.Name, Status: service.StatusActive}
	return &account, nil
}

func (s *stubAdminService) DeleteAccount(ctx context.Context, id int64) error {
	return nil
}

func (s *stubAdminService) RefreshAccountCredentials(ctx context.Context, id int64) (*service.Account, error) {
	account := service.Account{ID: id, Name: "account", Status: service.StatusActive}
	return &account, nil
}

func (s *stubAdminService) ClearAccountError(ctx context.Context, id int64) (*service.Account, error) {
	account := service.Account{ID: id, Name: "account", Status: service.StatusActive}
	return &account, nil
}

func (s *stubAdminService) SetAccountError(ctx context.Context, id int64, errorMsg string) error {
	return nil
}

func (s *stubAdminService) SetAccountSchedulable(ctx context.Context, id int64, schedulable bool) (*service.Account, error) {
	account := service.Account{ID: id, Name: "account", Status: service.StatusActive, Schedulable: schedulable}
	return &account, nil
}

func (s *stubAdminService) BulkUpdateAccounts(ctx context.Context, input *service.BulkUpdateAccountsInput) (*service.BulkUpdateAccountsResult, error) {
	return &service.BulkUpdateAccountsResult{Success: 1, Failed: 0, SuccessIDs: []int64{1}}, nil
}

func (s *stubAdminService) ListProxies(ctx context.Context, page, pageSize int, protocol, status, search string) ([]service.Proxy, int64, error) {
	return s.proxies, int64(len(s.proxies)), nil
}

func (s *stubAdminService) ListProxiesWithAccountCount(ctx context.Context, page, pageSize int, protocol, status, search string) ([]service.ProxyWithAccountCount, int64, error) {
	return s.proxyCounts, int64(len(s.proxyCounts)), nil
}

func (s *stubAdminService) GetAllProxies(ctx context.Context) ([]service.Proxy, error) {
	return s.proxies, nil
}

func (s *stubAdminService) GetAllProxiesWithAccountCount(ctx context.Context) ([]service.ProxyWithAccountCount, error) {
	return s.proxyCounts, nil
}

func (s *stubAdminService) GetProxy(ctx context.Context, id int64) (*service.Proxy, error) {
	proxy := service.Proxy{ID: id, Name: "proxy", Status: service.StatusActive}
	return &proxy, nil
}

func (s *stubAdminService) CreateProxy(ctx context.Context, input *service.CreateProxyInput) (*service.Proxy, error) {
	proxy := service.Proxy{ID: 400, Name: input.Name, Status: service.StatusActive}
	return &proxy, nil
}

func (s *stubAdminService) UpdateProxy(ctx context.Context, id int64, input *service.UpdateProxyInput) (*service.Proxy, error) {
	proxy := service.Proxy{ID: id, Name: input.Name, Status: service.StatusActive}
	return &proxy, nil
}

func (s *stubAdminService) DeleteProxy(ctx context.Context, id int64) error {
	return nil
}

func (s *stubAdminService) BatchDeleteProxies(ctx context.Context, ids []int64) (*service.ProxyBatchDeleteResult, error) {
	return &service.ProxyBatchDeleteResult{DeletedIDs: ids}, nil
}

func (s *stubAdminService) GetProxyAccounts(ctx context.Context, proxyID int64) ([]service.ProxyAccountSummary, error) {
	return []service.ProxyAccountSummary{{ID: 1, Name: "account"}}, nil
}

func (s *stubAdminService) CheckProxyExists(ctx context.Context, host string, port int, username, password string) (bool, error) {
	return false, nil
}

func (s *stubAdminService) TestProxy(ctx context.Context, id int64) (*service.ProxyTestResult, error) {
	return &service.ProxyTestResult{Success: true, Message: "ok"}, nil
}

func (s *stubAdminService) ListRedeemCodes(ctx context.Context, page, pageSize int, codeType, status, search string) ([]service.RedeemCode, int64, error) {
	return s.redeems, int64(len(s.redeems)), nil
}

func (s *stubAdminService) GetRedeemCode(ctx context.Context, id int64) (*service.RedeemCode, error) {
	code := service.RedeemCode{ID: id, Code: "R-TEST", Status: service.StatusUnused}
	return &code, nil
}

func (s *stubAdminService) GenerateRedeemCodes(ctx context.Context, input *service.GenerateRedeemCodesInput) ([]service.RedeemCode, error) {
	return s.redeems, nil
}

func (s *stubAdminService) DeleteRedeemCode(ctx context.Context, id int64) error {
	return nil
}

func (s *stubAdminService) BatchDeleteRedeemCodes(ctx context.Context, ids []int64) (int64, error) {
	return int64(len(ids)), nil
}

func (s *stubAdminService) ExpireRedeemCode(ctx context.Context, id int64) (*service.RedeemCode, error) {
	code := service.RedeemCode{ID: id, Code: "R-TEST", Status: service.StatusUsed}
	return &code, nil
}

// Ensure stub implements interface.
var _ service.AdminService = (*stubAdminService)(nil)
