package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type subsiteUsageBillingRepoStub struct {
	calls           int
	lastCmd         *UsageBillingCommand
	reservationRepo *subsiteUsageReservationRepoStub
	leaseRepo       *subsiteUsageLeaseRepoStub
}

func (s *subsiteUsageBillingRepoStub) Apply(_ context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	s.calls++
	s.lastCmd = cmd
	if s.reservationRepo != nil {
		s.reservationRepo.settleCalls++
		s.reservationRepo.settleRequest = cmd.RequestID
		if cmd.SubscriptionCost > cmd.BalanceCost {
			s.reservationRepo.settleCost = cmd.SubscriptionCost
		} else {
			s.reservationRepo.settleCost = cmd.BalanceCost
		}
	}
	if s.leaseRepo != nil {
		s.leaseRepo.incrementCalls++
		s.leaseRepo.incrementLeaseID = cmd.LeaseID
		s.leaseRepo.incrementRequests = cmd.LeaseUsageRequests
		s.leaseRepo.incrementTokens = cmd.LeaseUsageTokens
	}
	return &UsageBillingApplyResult{Applied: true}, nil
}

type subsiteUsageReservationRepoStub struct {
	reservation   *QuotaReservation
	settleCalls   int
	settleRequest string
	settleCost    float64
}

func (s *subsiteUsageReservationRepoStub) Create(_ context.Context, _ *QuotaReservation) error {
	panic("unexpected Create call")
}

func (s *subsiteUsageReservationRepoStub) GetByRequestID(_ context.Context, _ string) (*QuotaReservation, error) {
	panic("unexpected GetByRequestID call")
}

func (s *subsiteUsageReservationRepoStub) GetByReservationID(_ context.Context, _ string) (*QuotaReservation, error) {
	return s.reservation, nil
}

func (s *subsiteUsageReservationRepoStub) Cancel(_ context.Context, _ string) error {
	panic("unexpected Cancel call")
}
func (s *subsiteUsageReservationRepoStub) CancelForSubsite(context.Context, string, string) error {
	panic("unexpected CancelForSubsite call")
}

func (s *subsiteUsageReservationRepoStub) Settle(_ context.Context, _ string, _ float64) error {
	panic("unexpected Settle call")
}

func (s *subsiteUsageReservationRepoStub) ExpireStale(_ context.Context, _ time.Time) (int64, error) {
	panic("unexpected ExpireStale call")
}

type subsiteUsageLeaseRepoStub struct {
	incrementCalls    int
	incrementLeaseID  string
	incrementRequests int64
	incrementTokens   int64
}

func (s *subsiteUsageLeaseRepoStub) Create(context.Context, *AccountLease) error {
	panic("unexpected Create call")
}
func (s *subsiteUsageLeaseRepoStub) GetByLeaseID(context.Context, string) (*AccountLease, error) {
	panic("unexpected GetByLeaseID call")
}
func (s *subsiteUsageLeaseRepoStub) ListBySubsite(context.Context, string) ([]AccountLease, error) {
	panic("unexpected ListBySubsite call")
}
func (s *subsiteUsageLeaseRepoStub) ListBySubsitePaginated(context.Context, string, pagination.PaginationParams) ([]AccountLease, *pagination.PaginationResult, error) {
	panic("unexpected ListBySubsitePaginated call")
}
func (s *subsiteUsageLeaseRepoStub) ListActiveBySubsite(context.Context, string) ([]AccountLease, error) {
	panic("unexpected ListActiveBySubsite call")
}
func (s *subsiteUsageLeaseRepoStub) ListActiveAccountIDsBySubsite(context.Context, string) ([]int64, error) {
	panic("unexpected ListActiveAccountIDsBySubsite call")
}
func (s *subsiteUsageLeaseRepoStub) UpdateLimitsForSubsite(context.Context, string, string, int, int, int64) (*AccountLease, error) {
	panic("unexpected UpdateLimitsForSubsite call")
}
func (s *subsiteUsageLeaseRepoStub) Renew(context.Context, string, time.Time) (*AccountLease, error) {
	panic("unexpected Renew call")
}
func (s *subsiteUsageLeaseRepoStub) RenewForSubsite(context.Context, string, string, time.Time) (*AccountLease, error) {
	panic("unexpected RenewForSubsite call")
}
func (s *subsiteUsageLeaseRepoStub) Release(context.Context, string) (*AccountLease, error) {
	panic("unexpected Release call")
}
func (s *subsiteUsageLeaseRepoStub) ReleaseForSubsite(context.Context, string, string) (*AccountLease, error) {
	panic("unexpected ReleaseForSubsite call")
}
func (s *subsiteUsageLeaseRepoStub) Drain(context.Context, string) (*AccountLease, error) {
	panic("unexpected Drain call")
}
func (s *subsiteUsageLeaseRepoStub) DrainForSubsite(context.Context, string, string) (*AccountLease, error) {
	panic("unexpected DrainForSubsite call")
}
func (s *subsiteUsageLeaseRepoStub) DeleteForSubsite(context.Context, string, string) (*AccountLease, error) {
	panic("unexpected DeleteForSubsite call")
}
func (s *subsiteUsageLeaseRepoStub) IncrementUsage(_ context.Context, leaseID string, requests int64, tokens int64) error {
	s.incrementCalls++
	s.incrementLeaseID = leaseID
	s.incrementRequests = requests
	s.incrementTokens = tokens
	return nil
}
func (s *subsiteUsageLeaseRepoStub) ExpireStale(context.Context, time.Time) (int64, error) {
	panic("unexpected ExpireStale call")
}

type subsiteUsageAPIKeyRepoStub struct {
	apiKey *APIKey
}

func (s *subsiteUsageAPIKeyRepoStub) Create(context.Context, *APIKey) error {
	panic("unexpected Create call")
}
func (s *subsiteUsageAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	return s.apiKey, nil
}
func (s *subsiteUsageAPIKeyRepoStub) GetKeyAndOwnerID(context.Context, int64) (string, int64, error) {
	panic("unexpected GetKeyAndOwnerID call")
}
func (s *subsiteUsageAPIKeyRepoStub) GetByKey(context.Context, string) (*APIKey, error) {
	panic("unexpected GetByKey call")
}
func (s *subsiteUsageAPIKeyRepoStub) GetByKeyForAuth(context.Context, string) (*APIKey, error) {
	panic("unexpected GetByKeyForAuth call")
}
func (s *subsiteUsageAPIKeyRepoStub) Update(context.Context, *APIKey) error {
	panic("unexpected Update call")
}
func (s *subsiteUsageAPIKeyRepoStub) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}
func (s *subsiteUsageAPIKeyRepoStub) ListByUserID(context.Context, int64, pagination.PaginationParams, APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByUserID call")
}
func (s *subsiteUsageAPIKeyRepoStub) VerifyOwnership(context.Context, int64, []int64) ([]int64, error) {
	panic("unexpected VerifyOwnership call")
}
func (s *subsiteUsageAPIKeyRepoStub) CountByUserID(context.Context, int64) (int64, error) {
	panic("unexpected CountByUserID call")
}
func (s *subsiteUsageAPIKeyRepoStub) ExistsByKey(context.Context, string) (bool, error) {
	panic("unexpected ExistsByKey call")
}
func (s *subsiteUsageAPIKeyRepoStub) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}
func (s *subsiteUsageAPIKeyRepoStub) SearchAPIKeys(context.Context, int64, string, int) ([]APIKey, error) {
	panic("unexpected SearchAPIKeys call")
}
func (s *subsiteUsageAPIKeyRepoStub) ClearGroupIDByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected ClearGroupIDByGroupID call")
}
func (s *subsiteUsageAPIKeyRepoStub) UpdateGroupIDByUserAndGroup(context.Context, int64, int64, int64) (int64, error) {
	panic("unexpected UpdateGroupIDByUserAndGroup call")
}
func (s *subsiteUsageAPIKeyRepoStub) CountByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected CountByGroupID call")
}
func (s *subsiteUsageAPIKeyRepoStub) ListKeysByUserID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByUserID call")
}
func (s *subsiteUsageAPIKeyRepoStub) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByGroupID call")
}
func (s *subsiteUsageAPIKeyRepoStub) IncrementQuotaUsed(context.Context, int64, float64) (float64, error) {
	panic("unexpected IncrementQuotaUsed call")
}
func (s *subsiteUsageAPIKeyRepoStub) UpdateLastUsed(context.Context, int64, time.Time) error {
	panic("unexpected UpdateLastUsed call")
}
func (s *subsiteUsageAPIKeyRepoStub) IncrementRateLimitUsage(context.Context, int64, float64) error {
	panic("unexpected IncrementRateLimitUsage call")
}
func (s *subsiteUsageAPIKeyRepoStub) ResetRateLimitWindows(context.Context, int64) error {
	panic("unexpected ResetRateLimitWindows call")
}
func (s *subsiteUsageAPIKeyRepoStub) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	panic("unexpected GetRateLimitData call")
}

type subsiteUsageAccountRepoStub struct {
	account *Account
}

type subsiteUsageSettingRepoStub struct {
	values map[string]string
}

func (s *subsiteUsageSettingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	if s == nil {
		return nil, ErrSettingNotFound
	}
	value, ok := s.values[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: value}, nil
}

func (s *subsiteUsageSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if s == nil {
		return "", ErrSettingNotFound
	}
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (s *subsiteUsageSettingRepoStub) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (s *subsiteUsageSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	if s == nil {
		return out, nil
	}
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *subsiteUsageSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *subsiteUsageSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	out := map[string]string{}
	if s == nil {
		return out, nil
	}
	for key, value := range s.values {
		out[key] = value
	}
	return out, nil
}

func (s *subsiteUsageSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func (s *subsiteUsageAccountRepoStub) Create(context.Context, *Account) error {
	panic("unexpected Create call")
}
func (s *subsiteUsageAccountRepoStub) GetByID(context.Context, int64) (*Account, error) {
	return s.account, nil
}
func (s *subsiteUsageAccountRepoStub) GetByIDs(context.Context, []int64) ([]*Account, error) {
	panic("unexpected GetByIDs call")
}
func (s *subsiteUsageAccountRepoStub) ExistsByID(context.Context, int64) (bool, error) {
	panic("unexpected ExistsByID call")
}
func (s *subsiteUsageAccountRepoStub) GetByCRSAccountID(context.Context, string) (*Account, error) {
	panic("unexpected GetByCRSAccountID call")
}
func (s *subsiteUsageAccountRepoStub) FindByExtraField(context.Context, string, any) ([]Account, error) {
	panic("unexpected FindByExtraField call")
}
func (s *subsiteUsageAccountRepoStub) ListCRSAccountIDs(context.Context) (map[string]int64, error) {
	panic("unexpected ListCRSAccountIDs call")
}
func (s *subsiteUsageAccountRepoStub) Update(context.Context, *Account) error {
	panic("unexpected Update call")
}
func (s *subsiteUsageAccountRepoStub) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}
func (s *subsiteUsageAccountRepoStub) List(context.Context, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (s *subsiteUsageAccountRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, string, int64, int64, string) ([]Account, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (s *subsiteUsageAccountRepoStub) ListByGroup(context.Context, int64) ([]Account, error) {
	panic("unexpected ListByGroup call")
}
func (s *subsiteUsageAccountRepoStub) ListActive(context.Context) ([]Account, error) {
	panic("unexpected ListActive call")
}
func (s *subsiteUsageAccountRepoStub) ListByPlatform(context.Context, string) ([]Account, error) {
	panic("unexpected ListByPlatform call")
}
func (s *subsiteUsageAccountRepoStub) UpdateLastUsed(context.Context, int64) error {
	panic("unexpected UpdateLastUsed call")
}
func (s *subsiteUsageAccountRepoStub) BatchUpdateLastUsed(context.Context, map[int64]time.Time) error {
	panic("unexpected BatchUpdateLastUsed call")
}
func (s *subsiteUsageAccountRepoStub) SetError(context.Context, int64, string) error {
	panic("unexpected SetError call")
}
func (s *subsiteUsageAccountRepoStub) ClearError(context.Context, int64) error {
	panic("unexpected ClearError call")
}
func (s *subsiteUsageAccountRepoStub) SetSchedulable(context.Context, int64, bool) error {
	panic("unexpected SetSchedulable call")
}
func (s *subsiteUsageAccountRepoStub) AutoPauseExpiredAccounts(context.Context, time.Time) (int64, error) {
	panic("unexpected AutoPauseExpiredAccounts call")
}
func (s *subsiteUsageAccountRepoStub) BindGroups(context.Context, int64, []int64) error {
	panic("unexpected BindGroups call")
}
func (s *subsiteUsageAccountRepoStub) ListSchedulable(context.Context) ([]Account, error) {
	panic("unexpected ListSchedulable call")
}
func (s *subsiteUsageAccountRepoStub) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	panic("unexpected ListSchedulableByGroupID call")
}
func (s *subsiteUsageAccountRepoStub) ListSchedulableByPlatform(context.Context, string) ([]Account, error) {
	panic("unexpected ListSchedulableByPlatform call")
}
func (s *subsiteUsageAccountRepoStub) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	panic("unexpected ListSchedulableByGroupIDAndPlatform call")
}
func (s *subsiteUsageAccountRepoStub) ListSchedulableByPlatforms(context.Context, []string) ([]Account, error) {
	panic("unexpected ListSchedulableByPlatforms call")
}
func (s *subsiteUsageAccountRepoStub) ListSchedulableByGroupIDAndPlatforms(context.Context, int64, []string) ([]Account, error) {
	panic("unexpected ListSchedulableByGroupIDAndPlatforms call")
}
func (s *subsiteUsageAccountRepoStub) ListSchedulableUngroupedByPlatform(context.Context, string) ([]Account, error) {
	panic("unexpected ListSchedulableUngroupedByPlatform call")
}
func (s *subsiteUsageAccountRepoStub) ListSchedulableUngroupedByPlatforms(context.Context, []string) ([]Account, error) {
	panic("unexpected ListSchedulableUngroupedByPlatforms call")
}
func (s *subsiteUsageAccountRepoStub) SetRateLimited(context.Context, int64, time.Time) error {
	panic("unexpected SetRateLimited call")
}
func (s *subsiteUsageAccountRepoStub) SetModelRateLimit(context.Context, int64, string, time.Time) error {
	panic("unexpected SetModelRateLimit call")
}
func (s *subsiteUsageAccountRepoStub) SetOverloaded(context.Context, int64, time.Time) error {
	panic("unexpected SetOverloaded call")
}
func (s *subsiteUsageAccountRepoStub) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	panic("unexpected SetTempUnschedulable call")
}
func (s *subsiteUsageAccountRepoStub) ClearTempUnschedulable(context.Context, int64) error {
	panic("unexpected ClearTempUnschedulable call")
}
func (s *subsiteUsageAccountRepoStub) ClearRateLimit(context.Context, int64) error {
	panic("unexpected ClearRateLimit call")
}
func (s *subsiteUsageAccountRepoStub) ClearAntigravityQuotaScopes(context.Context, int64) error {
	panic("unexpected ClearAntigravityQuotaScopes call")
}
func (s *subsiteUsageAccountRepoStub) ClearModelRateLimits(context.Context, int64) error {
	panic("unexpected ClearModelRateLimits call")
}
func (s *subsiteUsageAccountRepoStub) UpdateSessionWindow(context.Context, int64, *time.Time, *time.Time, string) error {
	panic("unexpected UpdateSessionWindow call")
}
func (s *subsiteUsageAccountRepoStub) UpdateExtra(context.Context, int64, map[string]any) error {
	panic("unexpected UpdateExtra call")
}
func (s *subsiteUsageAccountRepoStub) BulkUpdate(context.Context, []int64, AccountBulkUpdate) (int64, error) {
	panic("unexpected BulkUpdate call")
}
func (s *subsiteUsageAccountRepoStub) IncrementQuotaUsed(context.Context, int64, float64) error {
	panic("unexpected IncrementQuotaUsed call")
}
func (s *subsiteUsageAccountRepoStub) ResetQuotaUsed(context.Context, int64) error {
	panic("unexpected ResetQuotaUsed call")
}

type subsiteAuthorizeRepoStub struct {
	subsite *Subsite
}

func (s *subsiteAuthorizeRepoStub) Create(context.Context, *Subsite) error {
	panic("unexpected Create call")
}
func (s *subsiteAuthorizeRepoStub) GetBySubsiteID(context.Context, string) (*Subsite, error) {
	return s.subsite, nil
}
func (s *subsiteAuthorizeRepoStub) List(context.Context, pagination.PaginationParams, ListSubsitesFilter) ([]Subsite, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (s *subsiteAuthorizeRepoStub) Update(context.Context, *Subsite) error {
	panic("unexpected Update call")
}
func (s *subsiteAuthorizeRepoStub) UpdateStatus(context.Context, string, string) error {
	panic("unexpected UpdateStatus call")
}
func (s *subsiteAuthorizeRepoStub) UpdateSecret(context.Context, string, string, string) error {
	panic("unexpected UpdateSecret call")
}
func (s *subsiteAuthorizeRepoStub) RecordHeartbeat(context.Context, *SubsiteHeartbeat) error {
	panic("unexpected RecordHeartbeat call")
}
func (s *subsiteAuthorizeRepoStub) MarkHeartbeatTimeouts(context.Context, time.Time) (int64, error) {
	panic("unexpected MarkHeartbeatTimeouts call")
}

type subsiteAuthorizeLeaseRepoStub struct {
	leases        []AccountLease
	createdLeases []*AccountLease
	createErr     error
}

type subsiteAuthorizeGroupRepoStub struct {
	groups map[int64]*Group
}

func (s *subsiteAuthorizeGroupRepoStub) Create(context.Context, *Group) error {
	panic("unexpected Create call")
}
func (s *subsiteAuthorizeGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	if s == nil || s.groups == nil {
		return nil, ErrGroupNotFound
	}
	group := s.groups[id]
	if group == nil {
		return nil, ErrGroupNotFound
	}
	return group, nil
}
func (s *subsiteAuthorizeGroupRepoStub) GetByIDLite(context.Context, int64) (*Group, error) {
	panic("unexpected GetByIDLite call")
}
func (s *subsiteAuthorizeGroupRepoStub) Update(context.Context, *Group) error {
	panic("unexpected Update call")
}
func (s *subsiteAuthorizeGroupRepoStub) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}
func (s *subsiteAuthorizeGroupRepoStub) DeleteCascade(context.Context, int64) ([]int64, error) {
	panic("unexpected DeleteCascade call")
}
func (s *subsiteAuthorizeGroupRepoStub) List(context.Context, pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (s *subsiteAuthorizeGroupRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (s *subsiteAuthorizeGroupRepoStub) ListActive(context.Context) ([]Group, error) {
	panic("unexpected ListActive call")
}
func (s *subsiteAuthorizeGroupRepoStub) ListActiveByPlatform(context.Context, string) ([]Group, error) {
	panic("unexpected ListActiveByPlatform call")
}
func (s *subsiteAuthorizeGroupRepoStub) ExistsByName(context.Context, string) (bool, error) {
	panic("unexpected ExistsByName call")
}
func (s *subsiteAuthorizeGroupRepoStub) GetAccountCount(context.Context, int64) (int64, int64, error) {
	panic("unexpected GetAccountCount call")
}
func (s *subsiteAuthorizeGroupRepoStub) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected DeleteAccountGroupsByGroupID call")
}
func (s *subsiteAuthorizeGroupRepoStub) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	panic("unexpected GetAccountIDsByGroupIDs call")
}
func (s *subsiteAuthorizeGroupRepoStub) BindAccountsToGroup(context.Context, int64, []int64) error {
	panic("unexpected BindAccountsToGroup call")
}
func (s *subsiteAuthorizeGroupRepoStub) UpdateSortOrders(context.Context, []GroupSortOrderUpdate) error {
	panic("unexpected UpdateSortOrders call")
}

func (s *subsiteAuthorizeLeaseRepoStub) Create(_ context.Context, lease *AccountLease) error {
	if s.createErr != nil {
		return s.createErr
	}
	cloned := *lease
	s.createdLeases = append(s.createdLeases, &cloned)
	s.leases = append(s.leases, cloned)
	return nil
}
func (s *subsiteAuthorizeLeaseRepoStub) GetByLeaseID(context.Context, string) (*AccountLease, error) {
	panic("unexpected GetByLeaseID call")
}
func (s *subsiteAuthorizeLeaseRepoStub) ListBySubsite(context.Context, string) ([]AccountLease, error) {
	panic("unexpected ListBySubsite call")
}
func (s *subsiteAuthorizeLeaseRepoStub) ListBySubsitePaginated(context.Context, string, pagination.PaginationParams) ([]AccountLease, *pagination.PaginationResult, error) {
	panic("unexpected ListBySubsitePaginated call")
}
func (s *subsiteAuthorizeLeaseRepoStub) ListActiveBySubsite(context.Context, string) ([]AccountLease, error) {
	return s.leases, nil
}
func (s *subsiteAuthorizeLeaseRepoStub) ListActiveAccountIDsBySubsite(context.Context, string) ([]int64, error) {
	panic("unexpected ListActiveAccountIDsBySubsite call")
}
func (s *subsiteAuthorizeLeaseRepoStub) UpdateLimitsForSubsite(context.Context, string, string, int, int, int64) (*AccountLease, error) {
	panic("unexpected UpdateLimitsForSubsite call")
}
func (s *subsiteAuthorizeLeaseRepoStub) Renew(context.Context, string, time.Time) (*AccountLease, error) {
	panic("unexpected Renew call")
}
func (s *subsiteAuthorizeLeaseRepoStub) RenewForSubsite(context.Context, string, string, time.Time) (*AccountLease, error) {
	panic("unexpected RenewForSubsite call")
}
func (s *subsiteAuthorizeLeaseRepoStub) Release(context.Context, string) (*AccountLease, error) {
	panic("unexpected Release call")
}
func (s *subsiteAuthorizeLeaseRepoStub) ReleaseForSubsite(context.Context, string, string) (*AccountLease, error) {
	panic("unexpected ReleaseForSubsite call")
}
func (s *subsiteAuthorizeLeaseRepoStub) Drain(context.Context, string) (*AccountLease, error) {
	panic("unexpected Drain call")
}
func (s *subsiteAuthorizeLeaseRepoStub) DrainForSubsite(context.Context, string, string) (*AccountLease, error) {
	panic("unexpected DrainForSubsite call")
}
func (s *subsiteAuthorizeLeaseRepoStub) DeleteForSubsite(context.Context, string, string) (*AccountLease, error) {
	panic("unexpected DeleteForSubsite call")
}
func (s *subsiteAuthorizeLeaseRepoStub) IncrementUsage(context.Context, string, int64, int64) error {
	panic("unexpected IncrementUsage call")
}
func (s *subsiteAuthorizeLeaseRepoStub) ExpireStale(context.Context, time.Time) (int64, error) {
	panic("unexpected ExpireStale call")
}

type subsiteAuthorizeReservationRepoStub struct {
	created           *QuotaReservation
	createErr         error
	capacityByLeaseID map[string]error
}

func (s *subsiteAuthorizeReservationRepoStub) Create(_ context.Context, reservation *QuotaReservation) error {
	if s != nil && s.createErr != nil {
		return s.createErr
	}
	if s != nil && s.capacityByLeaseID != nil && reservation != nil {
		if err, ok := s.capacityByLeaseID[reservation.LeaseID]; ok {
			return err
		}
	}
	s.created = reservation
	return nil
}
func (s *subsiteAuthorizeReservationRepoStub) GetByRequestID(context.Context, string) (*QuotaReservation, error) {
	panic("unexpected GetByRequestID call")
}
func (s *subsiteAuthorizeReservationRepoStub) GetByReservationID(context.Context, string) (*QuotaReservation, error) {
	panic("unexpected GetByReservationID call")
}
func (s *subsiteAuthorizeReservationRepoStub) Cancel(context.Context, string) error {
	panic("unexpected Cancel call")
}
func (s *subsiteAuthorizeReservationRepoStub) CancelForSubsite(context.Context, string, string) error {
	panic("unexpected CancelForSubsite call")
}
func (s *subsiteAuthorizeReservationRepoStub) Settle(context.Context, string, float64) error {
	panic("unexpected Settle call")
}
func (s *subsiteAuthorizeReservationRepoStub) ExpireStale(context.Context, time.Time) (int64, error) {
	panic("unexpected ExpireStale call")
}

type subsiteAuthorizeAPIKeyRepoStub struct {
	apiKey *APIKey
}

func (s *subsiteAuthorizeAPIKeyRepoStub) Create(context.Context, *APIKey) error {
	panic("unexpected Create call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	panic("unexpected GetByID call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) GetKeyAndOwnerID(context.Context, int64) (string, int64, error) {
	panic("unexpected GetKeyAndOwnerID call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) GetByKey(context.Context, string) (*APIKey, error) {
	return s.apiKey, nil
}
func (s *subsiteAuthorizeAPIKeyRepoStub) GetByKeyForAuth(context.Context, string) (*APIKey, error) {
	return s.apiKey, nil
}
func (s *subsiteAuthorizeAPIKeyRepoStub) Update(context.Context, *APIKey) error {
	panic("unexpected Update call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) ListByUserID(context.Context, int64, pagination.PaginationParams, APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByUserID call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) VerifyOwnership(context.Context, int64, []int64) ([]int64, error) {
	panic("unexpected VerifyOwnership call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) CountByUserID(context.Context, int64) (int64, error) {
	panic("unexpected CountByUserID call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) ExistsByKey(context.Context, string) (bool, error) {
	panic("unexpected ExistsByKey call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) SearchAPIKeys(context.Context, int64, string, int) ([]APIKey, error) {
	panic("unexpected SearchAPIKeys call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) ClearGroupIDByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected ClearGroupIDByGroupID call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) UpdateGroupIDByUserAndGroup(context.Context, int64, int64, int64) (int64, error) {
	panic("unexpected UpdateGroupIDByUserAndGroup call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) CountByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected CountByGroupID call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) ListKeysByUserID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByUserID call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByGroupID call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) IncrementQuotaUsed(context.Context, int64, float64) (float64, error) {
	panic("unexpected IncrementQuotaUsed call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) UpdateLastUsed(context.Context, int64, time.Time) error {
	panic("unexpected UpdateLastUsed call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) IncrementRateLimitUsage(context.Context, int64, float64) error {
	panic("unexpected IncrementRateLimitUsage call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) ResetRateLimitWindows(context.Context, int64) error {
	panic("unexpected ResetRateLimitWindows call")
}
func (s *subsiteAuthorizeAPIKeyRepoStub) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	panic("unexpected GetRateLimitData call")
}

type subsiteAuthorizeAccountRepoStub struct {
	accounts                      map[int64]*Account
	schedulableByGroupAndPlatform map[int64]map[string][]Account
}

func (s *subsiteAuthorizeAccountRepoStub) Create(context.Context, *Account) error {
	panic("unexpected Create call")
}
func (s *subsiteAuthorizeAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	return s.accounts[id], nil
}
func (s *subsiteAuthorizeAccountRepoStub) GetByIDs(context.Context, []int64) ([]*Account, error) {
	panic("unexpected GetByIDs call")
}
func (s *subsiteAuthorizeAccountRepoStub) ExistsByID(context.Context, int64) (bool, error) {
	panic("unexpected ExistsByID call")
}
func (s *subsiteAuthorizeAccountRepoStub) GetByCRSAccountID(context.Context, string) (*Account, error) {
	panic("unexpected GetByCRSAccountID call")
}
func (s *subsiteAuthorizeAccountRepoStub) FindByExtraField(context.Context, string, any) ([]Account, error) {
	panic("unexpected FindByExtraField call")
}
func (s *subsiteAuthorizeAccountRepoStub) ListCRSAccountIDs(context.Context) (map[string]int64, error) {
	panic("unexpected ListCRSAccountIDs call")
}
func (s *subsiteAuthorizeAccountRepoStub) Update(context.Context, *Account) error {
	panic("unexpected Update call")
}
func (s *subsiteAuthorizeAccountRepoStub) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}
func (s *subsiteAuthorizeAccountRepoStub) List(context.Context, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (s *subsiteAuthorizeAccountRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, string, int64, int64, string) ([]Account, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (s *subsiteAuthorizeAccountRepoStub) ListByGroup(context.Context, int64) ([]Account, error) {
	panic("unexpected ListByGroup call")
}
func (s *subsiteAuthorizeAccountRepoStub) ListActive(context.Context) ([]Account, error) {
	panic("unexpected ListActive call")
}
func (s *subsiteAuthorizeAccountRepoStub) ListByPlatform(context.Context, string) ([]Account, error) {
	panic("unexpected ListByPlatform call")
}
func (s *subsiteAuthorizeAccountRepoStub) UpdateLastUsed(context.Context, int64) error {
	panic("unexpected UpdateLastUsed call")
}
func (s *subsiteAuthorizeAccountRepoStub) BatchUpdateLastUsed(context.Context, map[int64]time.Time) error {
	panic("unexpected BatchUpdateLastUsed call")
}
func (s *subsiteAuthorizeAccountRepoStub) SetError(context.Context, int64, string) error {
	panic("unexpected SetError call")
}
func (s *subsiteAuthorizeAccountRepoStub) ClearError(context.Context, int64) error {
	panic("unexpected ClearError call")
}
func (s *subsiteAuthorizeAccountRepoStub) SetSchedulable(context.Context, int64, bool) error {
	panic("unexpected SetSchedulable call")
}
func (s *subsiteAuthorizeAccountRepoStub) AutoPauseExpiredAccounts(context.Context, time.Time) (int64, error) {
	panic("unexpected AutoPauseExpiredAccounts call")
}
func (s *subsiteAuthorizeAccountRepoStub) BindGroups(context.Context, int64, []int64) error {
	panic("unexpected BindGroups call")
}
func (s *subsiteAuthorizeAccountRepoStub) ListSchedulable(context.Context) ([]Account, error) {
	panic("unexpected ListSchedulable call")
}
func (s *subsiteAuthorizeAccountRepoStub) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	panic("unexpected ListSchedulableByGroupID call")
}
func (s *subsiteAuthorizeAccountRepoStub) ListSchedulableByPlatform(context.Context, string) ([]Account, error) {
	panic("unexpected ListSchedulableByPlatform call")
}
func (s *subsiteAuthorizeAccountRepoStub) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]Account, error) {
	if s == nil || s.schedulableByGroupAndPlatform == nil {
		return nil, nil
	}
	byPlatform := s.schedulableByGroupAndPlatform[groupID]
	if byPlatform == nil {
		return nil, nil
	}
	return append([]Account(nil), byPlatform[platform]...), nil
}
func (s *subsiteAuthorizeAccountRepoStub) ListSchedulableByPlatforms(context.Context, []string) ([]Account, error) {
	panic("unexpected ListSchedulableByPlatforms call")
}
func (s *subsiteAuthorizeAccountRepoStub) ListSchedulableByGroupIDAndPlatforms(_ context.Context, groupID int64, platforms []string) ([]Account, error) {
	if s == nil || s.schedulableByGroupAndPlatform == nil {
		return nil, nil
	}
	byPlatform := s.schedulableByGroupAndPlatform[groupID]
	if byPlatform == nil {
		return nil, nil
	}
	var out []Account
	for _, platform := range platforms {
		out = append(out, byPlatform[platform]...)
	}
	return out, nil
}

type stubSubscriptionService struct {
	activeSub *UserSubscription
	limitErr  error
}

func (s *stubSubscriptionService) GetActiveSubscription(context.Context, int64, int64) (*UserSubscription, error) {
	if s == nil || s.activeSub == nil {
		return nil, ErrSubscriptionNotFound
	}
	cp := *s.activeSub
	return &cp, nil
}

func (s *stubSubscriptionService) CheckUsageLimits(context.Context, *UserSubscription, *Group, float64) error {
	return s.limitErr
}

func (s *stubSubscriptionService) ValidateAndCheckLimits(*UserSubscription, *Group) (bool, error) {
	return false, s.limitErr
}

func (s *stubSubscriptionService) DoWindowMaintenance(*UserSubscription) {}
func (s *subsiteAuthorizeAccountRepoStub) ListSchedulableUngroupedByPlatform(context.Context, string) ([]Account, error) {
	panic("unexpected ListSchedulableUngroupedByPlatform call")
}
func (s *subsiteAuthorizeAccountRepoStub) ListSchedulableUngroupedByPlatforms(context.Context, []string) ([]Account, error) {
	panic("unexpected ListSchedulableUngroupedByPlatforms call")
}
func (s *subsiteAuthorizeAccountRepoStub) SetRateLimited(context.Context, int64, time.Time) error {
	panic("unexpected SetRateLimited call")
}
func (s *subsiteAuthorizeAccountRepoStub) SetModelRateLimit(context.Context, int64, string, time.Time) error {
	panic("unexpected SetModelRateLimit call")
}
func (s *subsiteAuthorizeAccountRepoStub) SetOverloaded(context.Context, int64, time.Time) error {
	panic("unexpected SetOverloaded call")
}
func (s *subsiteAuthorizeAccountRepoStub) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	panic("unexpected SetTempUnschedulable call")
}
func (s *subsiteAuthorizeAccountRepoStub) ClearTempUnschedulable(context.Context, int64) error {
	panic("unexpected ClearTempUnschedulable call")
}
func (s *subsiteAuthorizeAccountRepoStub) ClearRateLimit(context.Context, int64) error {
	panic("unexpected ClearRateLimit call")
}
func (s *subsiteAuthorizeAccountRepoStub) ClearAntigravityQuotaScopes(context.Context, int64) error {
	panic("unexpected ClearAntigravityQuotaScopes call")
}
func (s *subsiteAuthorizeAccountRepoStub) ClearModelRateLimits(context.Context, int64) error {
	panic("unexpected ClearModelRateLimits call")
}
func (s *subsiteAuthorizeAccountRepoStub) UpdateSessionWindow(context.Context, int64, *time.Time, *time.Time, string) error {
	panic("unexpected UpdateSessionWindow call")
}
func (s *subsiteAuthorizeAccountRepoStub) UpdateExtra(context.Context, int64, map[string]any) error {
	panic("unexpected UpdateExtra call")
}
func (s *subsiteAuthorizeAccountRepoStub) BulkUpdate(context.Context, []int64, AccountBulkUpdate) (int64, error) {
	panic("unexpected BulkUpdate call")
}
func (s *subsiteAuthorizeAccountRepoStub) IncrementQuotaUsed(context.Context, int64, float64) error {
	panic("unexpected IncrementQuotaUsed call")
}
func (s *subsiteAuthorizeAccountRepoStub) ResetQuotaUsed(context.Context, int64) error {
	panic("unexpected ResetQuotaUsed call")
}

func TestRequestAuthorize_PrefersRequestedLeaseForWebSocketTurns(t *testing.T) {
	now := time.Now()
	groupID := int64(7)
	reservationRepo := &subsiteAuthorizeReservationRepoStub{}
	svc := NewRequestAuthorizeService(
		&subsiteAuthorizeRepoStub{subsite: &Subsite{SubsiteID: "site_1", Status: SubsiteStatusActive}},
		&subsiteAuthorizeLeaseRepoStub{leases: []AccountLease{
			{
				LeaseID:   "lease_other",
				SubsiteID: "site_1",
				AccountID: 101,
				GroupID:   groupID,
				Platform:  PlatformOpenAI,
				Status:    AccountLeaseStatusActive,
				ExpiresAt: now.Add(time.Hour),
			},
			{
				LeaseID:   "lease_ws",
				SubsiteID: "site_1",
				AccountID: 100,
				GroupID:   groupID,
				Platform:  PlatformOpenAI,
				Status:    AccountLeaseStatusActive,
				ExpiresAt: now.Add(time.Hour),
			},
		}},
		reservationRepo,
		NewAPIKeyService(&subsiteAuthorizeAPIKeyRepoStub{apiKey: &APIKey{
			ID:      200,
			Key:     "client-key",
			UserID:  300,
			GroupID: &groupID,
			Status:  StatusActive,
			User:    &User{ID: 300, Status: StatusActive, Balance: 10},
			Group:   &Group{ID: groupID, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 1},
		}}, nil, nil, nil, nil, nil, &config.Config{}),
		nil,
		&subsiteAuthorizeAccountRepoStub{accounts: map[int64]*Account{
			100: {
				ID:          100,
				Type:        AccountTypeAPIKey,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
			},
			101: {
				ID:          101,
				Type:        AccountTypeAPIKey,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
			},
		}},
		NewBillingService(&config.Config{}, nil),
		nil,
	)

	authorization, err := svc.Authorize(context.Background(), AuthorizeSubsiteRequestInput{
		SubsiteID:             "site_1",
		APIKey:                "client-key",
		Platform:              PlatformOpenAI,
		RequestedModel:        "gpt-5.4",
		MappedModel:           "gpt-5.4",
		RequestFingerprint:    "fp_1",
		ClientIP:              "127.0.0.1",
		InboundEndpoint:       "/v1/responses",
		PreferredLeaseID:      "lease_ws",
		PreferredAccountID:    100,
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 1000,
	})

	require.NoError(t, err)
	require.Equal(t, "lease_ws", authorization.LeaseID)
	require.Equal(t, int64(100), authorization.AccountID)
	require.NotNil(t, reservationRepo.created)
	require.Equal(t, "lease_ws", reservationRepo.created.LeaseID)
	require.Equal(t, int64(100), reservationRepo.created.AccountID)
	require.Equal(t, int64(1), reservationRepo.created.ReservedRequests)
	require.Greater(t, reservationRepo.created.ReservedTokens, int64(0))
	require.Equal(t, int64(1), reservationRepo.created.ActiveRequestUnits)
}

func TestRequestAuthorize_EstimatesCostWhenClientOmitsCost(t *testing.T) {
	now := time.Now()
	groupID := int64(7)
	reservationRepo := &subsiteAuthorizeReservationRepoStub{}
	svc := NewRequestAuthorizeService(
		&subsiteAuthorizeRepoStub{subsite: &Subsite{SubsiteID: "site_1", Status: SubsiteStatusActive}},
		&subsiteAuthorizeLeaseRepoStub{leases: []AccountLease{{
			LeaseID:   "lease_1",
			SubsiteID: "site_1",
			AccountID: 100,
			GroupID:   groupID,
			Platform:  PlatformOpenAI,
			Status:    AccountLeaseStatusActive,
			ExpiresAt: now.Add(time.Hour),
		}}},
		reservationRepo,
		NewAPIKeyService(&subsiteAuthorizeAPIKeyRepoStub{apiKey: &APIKey{
			ID:      200,
			Key:     "client-key",
			UserID:  300,
			GroupID: &groupID,
			Status:  StatusActive,
			User:    &User{ID: 300, Status: StatusActive, Balance: 10},
			Group:   &Group{ID: groupID, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 1},
		}}, nil, nil, nil, nil, nil, &config.Config{}),
		nil,
		&subsiteAuthorizeAccountRepoStub{accounts: map[int64]*Account{
			100: {
				ID:          100,
				Type:        AccountTypeAPIKey,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
			},
		}},
		NewBillingService(&config.Config{}, nil),
		nil,
	)

	authorization, err := svc.Authorize(context.Background(), AuthorizeSubsiteRequestInput{
		SubsiteID:                "site_1",
		APIKey:                   "client-key",
		Platform:                 PlatformOpenAI,
		RequestedModel:           "gpt-5.4",
		RequestFingerprint:       "fp_1",
		ClientIP:                 "127.0.0.1",
		InboundEndpoint:          "/v1/responses",
		EstimatedInputTokens:     1000,
		EstimatedOutputTokens:    2000,
		EstimatedCacheReadTokens: 100,
	})

	require.NoError(t, err)
	require.Greater(t, authorization.MaxCost, 0.0)
	require.NotNil(t, reservationRepo.created)
	require.InDelta(t, authorization.MaxCost, reservationRepo.created.EstimatedCost, 1e-12)
}

func TestRequestAuthorize_SkipsExhaustedLeaseLimits(t *testing.T) {
	now := time.Now()
	groupID := int64(7)
	reservationRepo := &subsiteAuthorizeReservationRepoStub{}
	svc := NewRequestAuthorizeService(
		&subsiteAuthorizeRepoStub{subsite: &Subsite{SubsiteID: "site_1", Status: SubsiteStatusActive}},
		&subsiteAuthorizeLeaseRepoStub{leases: []AccountLease{
			{
				LeaseID:      "lease_exhausted",
				SubsiteID:    "site_1",
				AccountID:    100,
				GroupID:      groupID,
				Platform:     PlatformOpenAI,
				Status:       AccountLeaseStatusActive,
				MaxRequests:  1,
				UsedRequests: 1,
				ExpiresAt:    now.Add(time.Hour),
			},
			{
				LeaseID:   "lease_available",
				SubsiteID: "site_1",
				AccountID: 101,
				GroupID:   groupID,
				Platform:  PlatformOpenAI,
				Status:    AccountLeaseStatusActive,
				ExpiresAt: now.Add(time.Hour),
			},
		}},
		reservationRepo,
		NewAPIKeyService(&subsiteAuthorizeAPIKeyRepoStub{apiKey: &APIKey{
			ID:      200,
			Key:     "client-key",
			UserID:  300,
			GroupID: &groupID,
			Status:  StatusActive,
			User:    &User{ID: 300, Status: StatusActive, Balance: 10},
			Group:   &Group{ID: groupID, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 1},
		}}, nil, nil, nil, nil, nil, &config.Config{}),
		nil,
		&subsiteAuthorizeAccountRepoStub{accounts: map[int64]*Account{
			100: {ID: 100, Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true},
			101: {ID: 101, Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true},
		}},
		NewBillingService(&config.Config{}, nil),
		nil,
	)

	authorization, err := svc.Authorize(context.Background(), AuthorizeSubsiteRequestInput{
		SubsiteID:             "site_1",
		APIKey:                "client-key",
		Platform:              PlatformOpenAI,
		RequestedModel:        "gpt-5.4",
		RequestFingerprint:    "fp_1",
		ClientIP:              "127.0.0.1",
		InboundEndpoint:       "/v1/responses",
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 1000,
	})

	require.NoError(t, err)
	require.Equal(t, "lease_available", authorization.LeaseID)
	require.Equal(t, int64(1), reservationRepo.created.ActiveRequestUnits)
}

func TestAccountLeaseServiceCreate_LeavesZeroConcurrencyUnlimited(t *testing.T) {
	groupID := int64(7)
	accountID := int64(100)
	subsiteRepo := &subsiteAuthorizeRepoStub{subsite: &Subsite{SubsiteID: "site_1", Status: SubsiteStatusActive}}
	leaseRepo := &subsiteAuthorizeLeaseRepoStub{}
	svc := NewAccountLeaseService(
		leaseRepo,
		subsiteRepo,
		&subsiteAuthorizeAccountRepoStub{accounts: map[int64]*Account{
			accountID: {ID: accountID, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, GroupIDs: []int64{groupID}},
		}},
		&subsiteAuthorizeGroupRepoStub{groups: map[int64]*Group{
			groupID: {ID: groupID, Platform: PlatformOpenAI, Status: StatusActive},
		}},
	)

	lease, err := svc.Create(context.Background(), CreateAccountLeaseInput{
		SubsiteID:      "site_1",
		GroupID:        groupID,
		AccountID:      accountID,
		MaxConcurrency: 0,
	})

	require.NoError(t, err)
	require.NotNil(t, lease)
	require.Equal(t, 0, lease.MaxConcurrency)
	require.Len(t, leaseRepo.createdLeases, 1)
	require.Equal(t, 0, leaseRepo.createdLeases[0].MaxConcurrency)
}

func TestRequestAuthorize_SkipsLeaseWithNoImmediateCapacity(t *testing.T) {
	now := time.Now()
	groupID := int64(7)
	reservationRepo := &subsiteAuthorizeReservationRepoStub{
		capacityByLeaseID: map[string]error{
			"lease_full": ErrSubsiteLeaseCapacityExceeded,
		},
	}
	svc := NewRequestAuthorizeService(
		&subsiteAuthorizeRepoStub{subsite: &Subsite{SubsiteID: "site_1", Status: SubsiteStatusActive}},
		&subsiteAuthorizeLeaseRepoStub{leases: []AccountLease{
			{
				LeaseID:        "lease_full",
				SubsiteID:      "site_1",
				AccountID:      100,
				GroupID:        groupID,
				Platform:       PlatformOpenAI,
				Status:         AccountLeaseStatusActive,
				MaxConcurrency: 1,
				ExpiresAt:      now.Add(time.Hour),
			},
			{
				LeaseID:        "lease_open",
				SubsiteID:      "site_1",
				AccountID:      101,
				GroupID:        groupID,
				Platform:       PlatformOpenAI,
				Status:         AccountLeaseStatusActive,
				MaxConcurrency: 1,
				ExpiresAt:      now.Add(time.Hour),
			},
		}},
		reservationRepo,
		NewAPIKeyService(&subsiteAuthorizeAPIKeyRepoStub{apiKey: &APIKey{
			ID:      200,
			Key:     "client-key",
			UserID:  300,
			GroupID: &groupID,
			Status:  StatusActive,
			User:    &User{ID: 300, Status: StatusActive, Balance: 10},
			Group:   &Group{ID: groupID, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 1},
		}}, nil, nil, nil, nil, nil, &config.Config{}),
		nil,
		&subsiteAuthorizeAccountRepoStub{accounts: map[int64]*Account{
			100: {ID: 100, Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true},
			101: {ID: 101, Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true},
		}},
		NewBillingService(&config.Config{}, nil),
		nil,
	)

	authorization, err := svc.Authorize(context.Background(), AuthorizeSubsiteRequestInput{
		SubsiteID:             "site_1",
		APIKey:                "client-key",
		Platform:              PlatformOpenAI,
		RequestedModel:        "gpt-5.4",
		RequestFingerprint:    "fp_capacity_skip",
		ClientIP:              "127.0.0.1",
		InboundEndpoint:       "/v1/responses",
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 1000,
	})

	require.NoError(t, err)
	require.NotNil(t, authorization)
	require.Equal(t, "lease_open", authorization.LeaseID)
	require.Equal(t, int64(101), authorization.AccountID)
	require.NotNil(t, reservationRepo.created)
	require.Equal(t, "lease_open", reservationRepo.created.LeaseID)
}

func TestRequestAuthorize_RejectsLeaseGroupMismatch(t *testing.T) {
	now := time.Now()
	apiKeyGroupID := int64(7)
	leaseGroupID := int64(8)
	reservationRepo := &subsiteAuthorizeReservationRepoStub{}
	svc := NewRequestAuthorizeService(
		&subsiteAuthorizeRepoStub{subsite: &Subsite{SubsiteID: "site_1", Status: SubsiteStatusActive}},
		&subsiteAuthorizeLeaseRepoStub{leases: []AccountLease{{
			LeaseID:   "lease_1",
			SubsiteID: "site_1",
			AccountID: 100,
			GroupID:   leaseGroupID,
			Platform:  PlatformOpenAI,
			Status:    AccountLeaseStatusActive,
			ExpiresAt: now.Add(time.Hour),
		}}},
		reservationRepo,
		NewAPIKeyService(&subsiteAuthorizeAPIKeyRepoStub{apiKey: &APIKey{
			ID:      200,
			Key:     "client-key",
			UserID:  300,
			GroupID: &apiKeyGroupID,
			Status:  StatusActive,
			User:    &User{ID: 300, Status: StatusActive, Balance: 10},
			Group:   &Group{ID: apiKeyGroupID, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 1},
		}}, nil, nil, nil, nil, nil, &config.Config{}),
		nil,
		&subsiteAuthorizeAccountRepoStub{accounts: map[int64]*Account{
			100: {ID: 100, Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true},
		}},
		NewBillingService(&config.Config{}, nil),
		nil,
	)

	authorization, err := svc.Authorize(context.Background(), AuthorizeSubsiteRequestInput{
		SubsiteID:             "site_1",
		APIKey:                "client-key",
		Platform:              PlatformOpenAI,
		RequestedModel:        "gpt-5.4",
		RequestFingerprint:    "fp_1",
		ClientIP:              "127.0.0.1",
		InboundEndpoint:       "/v1/responses",
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 1000,
	})

	require.Nil(t, authorization)
	require.ErrorIs(t, err, ErrSubsiteAuthorizeNoLease)
	require.Nil(t, reservationRepo.created)
}

func TestRequestAuthorize_AutoCreatesLeaseForPrivateSubscriptionGroup(t *testing.T) {
	now := time.Now()
	groupID := int64(77)
	userID := int64(300)
	reservationRepo := &subsiteAuthorizeReservationRepoStub{}
	leaseRepo := &subsiteAuthorizeLeaseRepoStub{}
	svc := NewRequestAuthorizeService(
		&subsiteAuthorizeRepoStub{subsite: &Subsite{SubsiteID: "site_1", Status: SubsiteStatusActive}},
		leaseRepo,
		reservationRepo,
		NewAPIKeyService(&subsiteAuthorizeAPIKeyRepoStub{apiKey: &APIKey{
			ID:      200,
			Key:     "client-key",
			UserID:  userID,
			GroupID: &groupID,
			Status:  StatusActive,
			User:    &User{ID: userID, Status: StatusActive, Balance: 10},
			Group:   &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscription, Scope: GroupScopeUserPrivate, OwnerUserID: &userID, RateMultiplier: 1},
		}}, nil, nil, nil, nil, nil, &config.Config{}),
		&stubSubscriptionService{
			activeSub: &UserSubscription{ID: 900, UserID: userID, GroupID: groupID, Status: SubscriptionStatusActive, ExpiresAt: now.Add(24 * time.Hour)},
		},
		&subsiteAuthorizeAccountRepoStub{
			accounts: map[int64]*Account{
				501: {
					ID:          501,
					Type:        AccountTypeOAuth,
					Platform:    PlatformOpenAI,
					Status:      StatusActive,
					Schedulable: true,
					OwnerUserID: &userID,
					ShareMode:   AccountShareModePrivate,
					Concurrency: 2,
				},
			},
			schedulableByGroupAndPlatform: map[int64]map[string][]Account{
				groupID: {
					PlatformOpenAI: {{
						ID:          501,
						Type:        AccountTypeOAuth,
						Platform:    PlatformOpenAI,
						Status:      StatusActive,
						Schedulable: true,
						OwnerUserID: &userID,
						ShareMode:   AccountShareModePrivate,
						Concurrency: 2,
					}},
				},
			},
		},
		NewBillingService(&config.Config{}, nil),
		nil,
	)

	authorization, err := svc.Authorize(context.Background(), AuthorizeSubsiteRequestInput{
		SubsiteID:             "site_1",
		APIKey:                "client-key",
		Platform:              PlatformOpenAI,
		RequestedModel:        "gpt-5.4",
		RequestFingerprint:    "fp_private_1",
		ClientIP:              "127.0.0.1",
		InboundEndpoint:       "/v1/responses",
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 1000,
	})

	require.NoError(t, err)
	require.NotNil(t, authorization)
	require.Equal(t, int64(501), authorization.AccountID)
	require.Len(t, leaseRepo.createdLeases, 1)
	require.Equal(t, int64(501), leaseRepo.createdLeases[0].AccountID)
	require.Equal(t, groupID, leaseRepo.createdLeases[0].GroupID)
	require.NotNil(t, authorization.SubscriptionID)
	require.Equal(t, int64(900), *authorization.SubscriptionID)
	require.Equal(t, BillingTypeSubscription, authorization.BillingType)
	require.NotNil(t, reservationRepo.created)
	require.Equal(t, leaseRepo.createdLeases[0].LeaseID, reservationRepo.created.LeaseID)
}

func TestIsPrivateSubsiteAutoLeaseEligible(t *testing.T) {
	groupID := int64(77)
	userID := int64(300)
	apiKey := &APIKey{
		ID:      200,
		Key:     "client-key",
		UserID:  userID,
		GroupID: &groupID,
		Status:  StatusActive,
		User:    &User{ID: userID, Status: StatusActive, Balance: 10},
		Group:   &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscription, Scope: GroupScopeUserPrivate, OwnerUserID: &userID, RateMultiplier: 1},
	}
	require.True(t, isPrivateSubsiteAutoLeaseEligible(apiKey))
}

func TestEnsurePrivateLeaseCreatesLeaseForOwnedPrivateAccount(t *testing.T) {
	groupID := int64(77)
	userID := int64(300)
	leaseRepo := &subsiteAuthorizeLeaseRepoStub{}
	svc := &RequestAuthorizeService{
		leaseRepo: leaseRepo,
		accountRepo: &subsiteAuthorizeAccountRepoStub{
			accounts: map[int64]*Account{
				501: {
					ID:          501,
					Type:        AccountTypeOAuth,
					Platform:    PlatformOpenAI,
					Status:      StatusActive,
					Schedulable: true,
					OwnerUserID: &userID,
					ShareMode:   AccountShareModePrivate,
					Concurrency: 2,
				},
			},
			schedulableByGroupAndPlatform: map[int64]map[string][]Account{
				groupID: {
					PlatformOpenAI: {{
						ID:          501,
						Type:        AccountTypeOAuth,
						Platform:    PlatformOpenAI,
						Status:      StatusActive,
						Schedulable: true,
						OwnerUserID: &userID,
						ShareMode:   AccountShareModePrivate,
						Concurrency: 2,
					}},
				},
			},
		},
	}

	lease, account, err := svc.ensurePrivateLease(context.Background(), AuthorizeSubsiteRequestInput{
		SubsiteID: "site_1",
		Platform:  PlatformOpenAI,
	}, &APIKey{
		User:  &User{ID: userID},
		Group: &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscription, Scope: GroupScopeUserPrivate, OwnerUserID: &userID},
	}, groupID)
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.NotNil(t, account)
	require.Equal(t, int64(501), account.ID)
	require.Len(t, leaseRepo.createdLeases, 1)
}

func TestRequestAuthorize_SelectLeaseAutoCreatesForPrivateSubscriptionGroup(t *testing.T) {
	groupID := int64(77)
	userID := int64(300)
	leaseRepo := &subsiteAuthorizeLeaseRepoStub{}
	svc := NewRequestAuthorizeService(
		&subsiteAuthorizeRepoStub{subsite: &Subsite{SubsiteID: "site_1", Status: SubsiteStatusActive}},
		leaseRepo,
		&subsiteAuthorizeReservationRepoStub{},
		NewAPIKeyService(&subsiteAuthorizeAPIKeyRepoStub{apiKey: &APIKey{
			ID:      200,
			Key:     "client-key",
			UserID:  userID,
			GroupID: &groupID,
			Status:  StatusActive,
			User:    &User{ID: userID, Status: StatusActive, Balance: 10},
			Group:   &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscription, Scope: GroupScopeUserPrivate, OwnerUserID: &userID, RateMultiplier: 1},
		}}, nil, nil, nil, nil, nil, &config.Config{}),
		&stubSubscriptionService{},
		&subsiteAuthorizeAccountRepoStub{
			accounts: map[int64]*Account{
				501: {
					ID:          501,
					Type:        AccountTypeOAuth,
					Platform:    PlatformOpenAI,
					Status:      StatusActive,
					Schedulable: true,
					OwnerUserID: &userID,
					ShareMode:   AccountShareModePrivate,
					Concurrency: 2,
				},
			},
			schedulableByGroupAndPlatform: map[int64]map[string][]Account{
				groupID: {
					PlatformOpenAI: {{
						ID:          501,
						Type:        AccountTypeOAuth,
						Platform:    PlatformOpenAI,
						Status:      StatusActive,
						Schedulable: true,
						OwnerUserID: &userID,
						ShareMode:   AccountShareModePrivate,
						Concurrency: 2,
					}},
				},
			},
		},
		NewBillingService(&config.Config{}, nil),
		nil,
	)

	apiKey, err := svc.apiKeyService.GetByKey(context.Background(), "client-key")
	require.NoError(t, err)
	require.NotNil(t, apiKey.Group)
	require.True(t, isPrivateSubsiteAutoLeaseEligible(apiKey))

	lease, account, err := svc.selectLease(context.Background(), AuthorizeSubsiteRequestInput{
		SubsiteID:          "site_1",
		Platform:           PlatformOpenAI,
		RequestFingerprint: "fp_private_1",
	}, apiKey)
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.NotNil(t, account)
	require.Equal(t, int64(501), account.ID)
	require.Len(t, leaseRepo.createdLeases, 1)
}

func TestRequestAuthorize_PrivateSubscriptionGroupDoesNotUsePublicSharedAccount(t *testing.T) {
	now := time.Now()
	groupID := int64(77)
	userID := int64(300)
	reservationRepo := &subsiteAuthorizeReservationRepoStub{}
	leaseRepo := &subsiteAuthorizeLeaseRepoStub{}
	svc := NewRequestAuthorizeService(
		&subsiteAuthorizeRepoStub{subsite: &Subsite{SubsiteID: "site_1", Status: SubsiteStatusActive}},
		leaseRepo,
		reservationRepo,
		NewAPIKeyService(&subsiteAuthorizeAPIKeyRepoStub{apiKey: &APIKey{
			ID:      200,
			Key:     "client-key",
			UserID:  userID,
			GroupID: &groupID,
			Status:  StatusActive,
			User:    &User{ID: userID, Status: StatusActive, Balance: 10},
			Group:   &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscription, Scope: GroupScopeUserPrivate, OwnerUserID: &userID, RateMultiplier: 1},
		}}, nil, nil, nil, nil, nil, &config.Config{}),
		&stubSubscriptionService{
			activeSub: &UserSubscription{ID: 900, UserID: userID, GroupID: groupID, Status: SubscriptionStatusActive, ExpiresAt: now.Add(24 * time.Hour)},
		},
		&subsiteAuthorizeAccountRepoStub{
			accounts: map[int64]*Account{},
			schedulableByGroupAndPlatform: map[int64]map[string][]Account{
				groupID: {
					PlatformOpenAI: {{
						ID:          502,
						Type:        AccountTypeOAuth,
						Platform:    PlatformOpenAI,
						Status:      StatusActive,
						Schedulable: true,
						OwnerUserID: &userID,
						ShareMode:   AccountShareModePublic,
						Concurrency: 2,
					}},
				},
			},
		},
		NewBillingService(&config.Config{}, nil),
		nil,
	)

	authorization, err := svc.Authorize(context.Background(), AuthorizeSubsiteRequestInput{
		SubsiteID:             "site_1",
		APIKey:                "client-key",
		Platform:              PlatformOpenAI,
		RequestedModel:        "gpt-5.4",
		RequestFingerprint:    "fp_private_2",
		ClientIP:              "127.0.0.1",
		InboundEndpoint:       "/v1/responses",
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 1000,
	})

	require.Nil(t, authorization)
	require.ErrorIs(t, err, ErrSubsiteAuthorizeNoLease)
	require.Empty(t, leaseRepo.createdLeases)
	require.Nil(t, reservationRepo.created)
}

func TestRequestAuthorize_SkipsTempUnschedulableLeaseAccount(t *testing.T) {
	now := time.Now()
	groupID := int64(7)
	future := now.Add(10 * time.Minute)
	reservationRepo := &subsiteAuthorizeReservationRepoStub{}
	svc := NewRequestAuthorizeService(
		&subsiteAuthorizeRepoStub{subsite: &Subsite{SubsiteID: "site_1", Status: SubsiteStatusActive}},
		&subsiteAuthorizeLeaseRepoStub{leases: []AccountLease{
			{
				LeaseID:   "lease_bad",
				SubsiteID: "site_1",
				AccountID: 101,
				GroupID:   groupID,
				Platform:  PlatformOpenAI,
				Status:    AccountLeaseStatusActive,
				ExpiresAt: now.Add(time.Hour),
			},
			{
				LeaseID:   "lease_good",
				SubsiteID: "site_1",
				AccountID: 102,
				GroupID:   groupID,
				Platform:  PlatformOpenAI,
				Status:    AccountLeaseStatusActive,
				ExpiresAt: now.Add(time.Hour),
			},
		}},
		reservationRepo,
		NewAPIKeyService(&subsiteAuthorizeAPIKeyRepoStub{apiKey: &APIKey{
			ID:      200,
			Key:     "client-key",
			UserID:  300,
			GroupID: &groupID,
			Status:  StatusActive,
			User:    &User{ID: 300, Status: StatusActive, Balance: 10},
			Group:   &Group{ID: groupID, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 1},
		}}, nil, nil, nil, nil, nil, &config.Config{}),
		nil,
		&subsiteAuthorizeAccountRepoStub{accounts: map[int64]*Account{
			101: {
				ID:                     101,
				Type:                   AccountTypeAPIKey,
				Platform:               PlatformOpenAI,
				Status:                 StatusActive,
				Schedulable:            true,
				TempUnschedulableUntil: &future,
			},
			102: {
				ID:          102,
				Type:        AccountTypeAPIKey,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
			},
		}},
		NewBillingService(&config.Config{}, nil),
		nil,
	)

	authorization, err := svc.Authorize(context.Background(), AuthorizeSubsiteRequestInput{
		SubsiteID:             "site_1",
		APIKey:                "client-key",
		Platform:              PlatformOpenAI,
		RequestedModel:        "gpt-5.4",
		RequestFingerprint:    "fp_temp_unsched_1",
		ClientIP:              "127.0.0.1",
		InboundEndpoint:       "/v1/responses",
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 1000,
	})

	require.NoError(t, err)
	require.NotNil(t, authorization)
	require.Equal(t, int64(102), authorization.AccountID)
	require.NotNil(t, reservationRepo.created)
	require.Equal(t, "lease_good", reservationRepo.created.LeaseID)
}

func TestUsageIngest_RecalculatesCostOnMaster(t *testing.T) {
	groupID := int64(7)
	accountMultiplier := 0.5
	reservation := &QuotaReservation{
		ReservationID:      "qres_1",
		RequestID:          "subreq_1",
		SubsiteID:          "site_1",
		AccountID:          100,
		APIKeyID:           200,
		UserID:             300,
		GroupID:            &groupID,
		Platform:           PlatformAnthropic,
		RequestedModel:     "claude-sonnet-4",
		MappedModel:        "claude-sonnet-4",
		EstimatedCost:      1.0,
		ReservedRequests:   1,
		ReservedTokens:     4096,
		ActiveRequestUnits: 1,
		LeaseID:            "lease_1",
		BillingType:        BillingTypeBalance,
		Status:             QuotaReservationStatusReserved,
		RequestFingerprint: "fp_1",
	}
	reservationRepo := &subsiteUsageReservationRepoStub{reservation: reservation}
	leaseRepo := &subsiteUsageLeaseRepoStub{}
	billingRepo := &subsiteUsageBillingRepoStub{reservationRepo: reservationRepo, leaseRepo: leaseRepo}
	apiKeyRepo := &subsiteUsageAPIKeyRepoStub{apiKey: &APIKey{
		ID:      200,
		UserID:  300,
		GroupID: &groupID,
		Status:  StatusAPIKeyActive,
		User:    &User{ID: 300, Status: StatusActive},
		Group:   &Group{ID: groupID, RateMultiplier: 2},
	}}
	accountRepo := &subsiteUsageAccountRepoStub{account: &Account{
		ID:             100,
		Type:           AccountTypeAPIKey,
		Platform:       PlatformAnthropic,
		Status:         StatusActive,
		RateMultiplier: &accountMultiplier,
	}}
	billingService := NewBillingService(&config.Config{}, nil)
	svc := NewUsageIngestService(
		billingRepo,
		reservationRepo,
		billingService,
		nil,
		NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, nil, &config.Config{}),
		nil,
		accountRepo,
	)

	result, err := svc.Ingest(context.Background(), UsageIngestBatch{
		SubsiteID: "site_1",
		Items: []UsageIngestItem{{
			RequestID:          "subreq_1",
			ReservationID:      "qres_1",
			APIKeyID:           200,
			UserID:             300,
			AccountID:          100,
			GroupID:            &groupID,
			BillingType:        BillingTypeBalance,
			InputTokens:        1000,
			OutputTokens:       500,
			BalanceCost:        0.99,
			RequestFingerprint: "fp_1",
		}},
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Accepted)
	require.Equal(t, 1, result.Applied)
	require.Zero(t, result.Duplicate)
	require.Zero(t, result.Failed)
	require.Len(t, result.Items, 1)
	require.True(t, result.Items[0].Applied)
	require.Equal(t, 1, billingRepo.calls)
	require.Equal(t, 1, reservationRepo.settleCalls)
	require.Equal(t, "subreq_1", reservationRepo.settleRequest)
	require.InDelta(t, 0.021, reservationRepo.settleCost, 1e-12)
	require.Equal(t, 1, leaseRepo.incrementCalls)
	require.Equal(t, "lease_1", leaseRepo.incrementLeaseID)
	require.Equal(t, int64(1), leaseRepo.incrementRequests)
	require.Equal(t, int64(1500), leaseRepo.incrementTokens)

	cmd := billingRepo.lastCmd
	require.NotNil(t, cmd)
	require.InDelta(t, 0.021, cmd.BalanceCost, 1e-12)
	require.Zero(t, cmd.SubscriptionCost)
	require.InDelta(t, 0.021, cmd.APIKeyQuotaCost, 1e-12)
	require.InDelta(t, 0.021, cmd.APIKeyRateLimitCost, 1e-12)
	require.InDelta(t, 0.00525, cmd.AccountQuotaCost, 1e-12)
	require.Equal(t, "qres_1", cmd.QuotaReservationID)
	require.Equal(t, "lease_1", cmd.LeaseID)
	require.Equal(t, int64(1), cmd.LeaseUsageRequests)
	require.Equal(t, int64(1500), cmd.LeaseUsageTokens)
	require.NotNil(t, cmd.UsageLog)
	require.InDelta(t, 0.0105, cmd.UsageLog.TotalCost, 1e-12)
	require.InDelta(t, 2.0, cmd.UsageLog.RateMultiplier, 1e-12)
	require.NotNil(t, cmd.UsageLog.AccountRateMultiplier)
	require.InDelta(t, 0.5, *cmd.UsageLog.AccountRateMultiplier, 1e-12)
}

func TestUsageIngest_RejectsReservationIdentityMismatch(t *testing.T) {
	groupID := int64(7)
	billingRepo := &subsiteUsageBillingRepoStub{}
	svc := NewUsageIngestService(
		billingRepo,
		&subsiteUsageReservationRepoStub{reservation: &QuotaReservation{
			ReservationID:      "qres_1",
			RequestID:          "subreq_1",
			SubsiteID:          "site_1",
			AccountID:          100,
			APIKeyID:           200,
			UserID:             300,
			GroupID:            &groupID,
			EstimatedCost:      1,
			ReservedRequests:   1,
			ReservedTokens:     4096,
			ActiveRequestUnits: 1,
			BillingType:        BillingTypeBalance,
			Status:             QuotaReservationStatusReserved,
			RequestFingerprint: "fp_1",
		}},
		NewBillingService(&config.Config{}, nil),
		nil,
		nil,
		nil,
		nil,
	)

	result, err := svc.Ingest(context.Background(), UsageIngestBatch{
		SubsiteID: "site_1",
		Items: []UsageIngestItem{{
			RequestID:          "subreq_1",
			ReservationID:      "qres_1",
			APIKeyID:           200,
			UserID:             301,
			AccountID:          100,
			GroupID:            &groupID,
			BillingType:        BillingTypeBalance,
			RequestFingerprint: "fp_1",
		}},
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Accepted)
	require.Equal(t, 1, result.Failed)
	require.Len(t, result.Items, 1)
	require.Equal(t, "SUBSITE_USAGE_RESERVATION_MISMATCH", result.Items[0].Error)
	require.Zero(t, billingRepo.calls)
}

func TestRequestAuthorize_IgnoresClientEstimatedCost(t *testing.T) {
	now := time.Now()
	groupID := int64(7)
	reservationRepo := &subsiteAuthorizeReservationRepoStub{}
	svc := NewRequestAuthorizeService(
		&subsiteAuthorizeRepoStub{subsite: &Subsite{SubsiteID: "site_1", Status: SubsiteStatusActive}},
		&subsiteAuthorizeLeaseRepoStub{leases: []AccountLease{{
			LeaseID:   "lease_1",
			SubsiteID: "site_1",
			AccountID: 100,
			GroupID:   groupID,
			Platform:  PlatformOpenAI,
			Status:    AccountLeaseStatusActive,
			ExpiresAt: now.Add(time.Hour),
		}}},
		reservationRepo,
		NewAPIKeyService(&subsiteAuthorizeAPIKeyRepoStub{apiKey: &APIKey{
			ID:      200,
			Key:     "client-key",
			UserID:  300,
			GroupID: &groupID,
			Status:  StatusActive,
			User:    &User{ID: 300, Status: StatusActive, Balance: 10},
			Group:   &Group{ID: groupID, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 1},
		}}, nil, nil, nil, nil, nil, &config.Config{}),
		nil,
		&subsiteAuthorizeAccountRepoStub{accounts: map[int64]*Account{
			100: {
				ID:          100,
				Type:        AccountTypeAPIKey,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
			},
		}},
		NewBillingService(&config.Config{}, nil),
		nil,
	)

	authorization, err := svc.Authorize(context.Background(), AuthorizeSubsiteRequestInput{
		SubsiteID:             "site_1",
		APIKey:                "client-key",
		Platform:              PlatformOpenAI,
		RequestedModel:        "gpt-5.4",
		RequestFingerprint:    "fp_1",
		ClientIP:              "127.0.0.1",
		InboundEndpoint:       "/v1/responses",
		EstimatedCost:         0.0000001,
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 1000,
	})

	require.NoError(t, err)
	require.NotNil(t, reservationRepo.created)
	require.Greater(t, reservationRepo.created.EstimatedCost, 0.0000001)
	require.InDelta(t, authorization.MaxCost, reservationRepo.created.EstimatedCost, 1e-12)
}

func TestUsageIngest_AppliesPrivateGroupCommissionForSubscription(t *testing.T) {
	groupID := int64(7)
	accountMultiplier := 0.5
	reservation := &QuotaReservation{
		ReservationID:      "qres_private_1",
		RequestID:          "subreq_private_1",
		SubsiteID:          "site_1",
		AccountID:          100,
		APIKeyID:           200,
		UserID:             300,
		GroupID:            &groupID,
		SubscriptionID:     int64PtrSubsite(900),
		Platform:           PlatformAnthropic,
		RequestedModel:     "claude-sonnet-4",
		MappedModel:        "claude-sonnet-4",
		EstimatedCost:      1.0,
		ReservedRequests:   1,
		ReservedTokens:     4096,
		ActiveRequestUnits: 1,
		LeaseID:            "lease_1",
		BillingType:        BillingTypeSubscription,
		Status:             QuotaReservationStatusReserved,
		RequestFingerprint: "fp_private_1",
	}
	reservationRepo := &subsiteUsageReservationRepoStub{reservation: reservation}
	leaseRepo := &subsiteUsageLeaseRepoStub{}
	billingRepo := &subsiteUsageBillingRepoStub{reservationRepo: reservationRepo, leaseRepo: leaseRepo}
	apiKeyRepo := &subsiteUsageAPIKeyRepoStub{apiKey: &APIKey{
		ID:      200,
		UserID:  300,
		GroupID: &groupID,
		Status:  StatusAPIKeyActive,
		User:    &User{ID: 300, Status: StatusActive},
		Group: &Group{
			ID:               groupID,
			Scope:            GroupScopeUserPrivate,
			SubscriptionType: SubscriptionTypeStandard,
			RateMultiplier:   2,
		},
	}}
	accountRepo := &subsiteUsageAccountRepoStub{account: &Account{
		ID:             100,
		Type:           AccountTypeAPIKey,
		Platform:       PlatformAnthropic,
		Status:         StatusActive,
		RateMultiplier: &accountMultiplier,
	}}
	billingService := NewBillingService(&config.Config{}, nil)
	settingService := NewSettingService(&subsiteUsageSettingRepoStub{values: map[string]string{
		SettingKeyUserPrivateGroupCommissionRate: "0.2",
	}}, &config.Config{})
	svc := NewUsageIngestService(
		billingRepo,
		reservationRepo,
		billingService,
		nil,
		NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, nil, &config.Config{}),
		settingService,
		accountRepo,
	)

	result, err := svc.Ingest(context.Background(), UsageIngestBatch{
		SubsiteID: "site_1",
		Items: []UsageIngestItem{{
			RequestID:          "subreq_private_1",
			ReservationID:      "qres_private_1",
			APIKeyID:           200,
			UserID:             300,
			AccountID:          100,
			GroupID:            &groupID,
			SubscriptionID:     int64PtrSubsite(900),
			BillingType:        BillingTypeSubscription,
			InputTokens:        1000,
			OutputTokens:       500,
			RequestFingerprint: "fp_private_1",
		}},
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Applied)
	require.Equal(t, 1, billingRepo.calls)
	cmd := billingRepo.lastCmd
	require.NotNil(t, cmd)
	require.Zero(t, cmd.BalanceCost)
	require.InDelta(t, 0.021, cmd.SubscriptionCost, 1e-12)
	require.InDelta(t, 0.0042, cmd.PrivateGroupCommissionCost, 1e-12)
}

func int64PtrSubsite(v int64) *int64 {
	return &v
}
