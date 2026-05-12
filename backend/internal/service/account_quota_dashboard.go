package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const accountQuotaDashboardPageSize = 1000

type AccountQuotaDashboard struct {
	GeneratedAt    time.Time                  `json:"generated_at"`
	Summaries      []AccountQuotaSummary      `json:"summaries"`
	Totals         AccountQuotaSummary        `json:"totals"`
	GroupSummaries []AccountQuotaGroupSummary `json:"group_summaries,omitempty"`
}

type UserAccountQuotaPoolDashboard struct {
	GeneratedAt time.Time             `json:"generated_at"`
	Mine        AccountQuotaDashboard `json:"mine"`
	Platform    AccountQuotaDashboard `json:"platform"`
}

type AccountQuotaSummary struct {
	Platform                string                       `json:"platform"`
	Type                    string                       `json:"type"`
	AccountCount            int                          `json:"account_count"`
	ActiveAccountCount      int                          `json:"active_account_count"`
	SchedulableAccountCount int                          `json:"schedulable_account_count"`
	QuotaAccountCount       int                          `json:"quota_account_count"`
	UnlimitedAccountCount   int                          `json:"unlimited_account_count"`
	Total                   AccountQuotaDimensionSummary `json:"total"`
	Daily                   AccountQuotaDimensionSummary `json:"daily"`
	Weekly                  AccountQuotaDimensionSummary `json:"weekly"`
	UsageWindows            []AccountUsageWindowSummary  `json:"usage_windows,omitempty"`
}

type AccountQuotaGroupSummary struct {
	GroupID                 *int64                       `json:"group_id"`
	GroupName               string                       `json:"group_name"`
	GroupStatus             string                       `json:"group_status"`
	Platform                string                       `json:"platform"`
	AccountCount            int                          `json:"account_count"`
	ActiveAccountCount      int                          `json:"active_account_count"`
	SchedulableAccountCount int                          `json:"schedulable_account_count"`
	QuotaAccountCount       int                          `json:"quota_account_count"`
	UnlimitedAccountCount   int                          `json:"unlimited_account_count"`
	Total                   AccountQuotaDimensionSummary `json:"total"`
	Daily                   AccountQuotaDimensionSummary `json:"daily"`
	Weekly                  AccountQuotaDimensionSummary `json:"weekly"`
	UsageWindows            []AccountUsageWindowSummary  `json:"usage_windows,omitempty"`
}

type AccountQuotaDimensionSummary struct {
	EnabledAccountCount   int     `json:"enabled_account_count"`
	ExhaustedAccountCount int     `json:"exhausted_account_count"`
	Limit                 float64 `json:"limit"`
	Used                  float64 `json:"used"`
	Remaining             float64 `json:"remaining"`
	Utilization           float64 `json:"utilization"`
}

type AccountUsageWindowSummary struct {
	Window                   string     `json:"window"`
	AccountCount             int        `json:"account_count"`
	KnownAccountCount        int        `json:"known_account_count"`
	AverageUtilization       float64    `json:"average_utilization"`
	RemainingCapacityPercent float64    `json:"remaining_capacity_percent"`
	MinRemainingSeconds      *int       `json:"min_remaining_seconds,omitempty"`
	NextResetAt              *time.Time `json:"next_reset_at,omitempty"`
}

type accountQuotaSummaryAccumulator struct {
	summary    AccountQuotaSummary
	windowAggs map[string]*accountUsageWindowAccumulator
}

type accountUsageWindowAccumulator struct {
	summary        AccountUsageWindowSummary
	utilizationSum float64
}

type accountQuotaDashboardBuilder struct {
	generatedAt  time.Time
	accumulators map[string]*accountQuotaSummaryAccumulator
	total        *accountQuotaSummaryAccumulator
}

type accountQuotaGroupDashboardBuilder struct {
	generatedAt  time.Time
	accumulators map[string]*accountQuotaGroupSummaryAccumulator
}

type accountQuotaGroupSummaryAccumulator struct {
	groupID     *int64
	groupName   string
	groupStatus string
	core        accountQuotaSummaryAccumulator
}

func (s *adminServiceImpl) GetAccountQuotaDashboard(ctx context.Context) (*AccountQuotaDashboard, error) {
	if s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("account repository is unavailable")
	}

	generatedAt := time.Now().UTC()
	builder := newAccountQuotaDashboardBuilder(generatedAt)

	if err := visitAccountQuotaDashboardAccounts(ctx, s.accountRepo, func(account Account) {
		builder.addAccount(account)
	}); err != nil {
		return nil, err
	}

	dashboard := builder.finalize()
	return &dashboard, nil
}

func (s *AccountService) GetQuotaPoolDashboard(ctx context.Context, ownerUserID int64) (*UserAccountQuotaPoolDashboard, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("account repository is unavailable")
	}

	generatedAt := time.Now().UTC()
	mine := newAccountQuotaDashboardBuilder(generatedAt)
	mineGroups := newAccountQuotaGroupDashboardBuilder(generatedAt)
	platform := newAccountQuotaDashboardBuilder(generatedAt)
	platformGroups := newAccountQuotaGroupDashboardBuilder(generatedAt)

	if err := visitAccountQuotaDashboardAccounts(ctx, s.accountRepo, func(account Account) {
		if account.OwnerUserID != nil && *account.OwnerUserID == ownerUserID {
			mine.addAccount(account)
			mineGroups.addAccountWithGroupFilter(account, func(group *Group) bool {
				return isOwnUserPrivateQuotaGroup(group, ownerUserID)
			})
		}
		if isPlatformQuotaPoolAccount(account) {
			platform.addAccount(account)
			platformGroups.addAccountWithGroupFilter(account, isPlatformSharedQuotaGroup)
		}
	}); err != nil {
		return nil, err
	}

	mineDashboard := mine.finalize()
	mineDashboard.GroupSummaries = mineGroups.finalize()

	platformDashboard := platform.finalize()
	platformDashboard.GroupSummaries = platformGroups.finalize()

	return &UserAccountQuotaPoolDashboard{
		GeneratedAt: generatedAt,
		Mine:        mineDashboard,
		Platform:    platformDashboard,
	}, nil
}

func newAccountQuotaDashboardBuilder(generatedAt time.Time) *accountQuotaDashboardBuilder {
	return &accountQuotaDashboardBuilder{
		generatedAt:  generatedAt,
		accumulators: make(map[string]*accountQuotaSummaryAccumulator),
		total: &accountQuotaSummaryAccumulator{
			summary: AccountQuotaSummary{
				Platform: "all",
				Type:     "all",
			},
			windowAggs: make(map[string]*accountUsageWindowAccumulator),
		},
	}
}

func visitAccountQuotaDashboardAccounts(ctx context.Context, repo AccountRepository, visit func(Account)) error {
	if repo == nil {
		return fmt.Errorf("account repository is unavailable")
	}

	for page := 1; ; page++ {
		accounts, result, err := repo.ListWithFilters(
			ctx,
			pagination.PaginationParams{
				Page:      page,
				PageSize:  accountQuotaDashboardPageSize,
				SortBy:    "id",
				SortOrder: "asc",
			},
			"",
			"",
			"",
			"",
			0,
			"",
		)
		if err != nil {
			return err
		}
		if len(accounts) == 0 {
			break
		}

		for i := range accounts {
			visit(accounts[i])
		}

		if result == nil || int64(page*accountQuotaDashboardPageSize) >= result.Total {
			break
		}
	}

	return nil
}

func (b *accountQuotaDashboardBuilder) addAccount(account Account) {
	if b == nil {
		return
	}
	key := account.Platform + "\x00" + account.Type
	acc, ok := b.accumulators[key]
	if !ok {
		acc = &accountQuotaSummaryAccumulator{
			summary: AccountQuotaSummary{
				Platform: account.Platform,
				Type:     account.Type,
			},
			windowAggs: make(map[string]*accountUsageWindowAccumulator),
		}
		b.accumulators[key] = acc
	}
	acc.addAccount(account, b.generatedAt)
	b.total.addAccount(account, b.generatedAt)
}

func (b *accountQuotaDashboardBuilder) finalize() AccountQuotaDashboard {
	if b == nil {
		return AccountQuotaDashboard{}
	}

	summaries := make([]AccountQuotaSummary, 0, len(b.accumulators))
	for _, acc := range b.accumulators {
		summaries = append(summaries, acc.finalize())
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Platform == summaries[j].Platform {
			return summaries[i].Type < summaries[j].Type
		}
		return summaries[i].Platform < summaries[j].Platform
	})

	return AccountQuotaDashboard{
		GeneratedAt: b.generatedAt,
		Summaries:   summaries,
		Totals:      b.total.finalize(),
	}
}

func newAccountQuotaGroupDashboardBuilder(generatedAt time.Time) *accountQuotaGroupDashboardBuilder {
	return &accountQuotaGroupDashboardBuilder{
		generatedAt:  generatedAt,
		accumulators: make(map[string]*accountQuotaGroupSummaryAccumulator),
	}
}

func (b *accountQuotaGroupDashboardBuilder) addAccount(account Account) {
	b.addAccountWithGroupFilter(account, nil)
}

func (b *accountQuotaGroupDashboardBuilder) addAccountWithGroupFilter(account Account, allowGroup func(*Group) bool) {
	if b == nil {
		return
	}
	if len(account.Groups) == 0 {
		b.addAccountToGroup(account, nil, "", StatusActive, account.Platform, "", false, false)
		return
	}
	for _, group := range account.Groups {
		if group == nil || group.ID <= 0 {
			continue
		}
		if allowGroup != nil && !allowGroup(group) {
			continue
		}
		platform := group.Platform
		if platform == "" {
			platform = account.Platform
		}
		b.addAccountToGroup(
			account,
			&group.ID,
			group.Name,
			group.Status,
			platform,
			group.RequiredAccountLevel,
			group.RequireOAuthOnly,
			group.RequirePrivacySet,
		)
	}
}

func isOwnUserPrivateQuotaGroup(group *Group, ownerUserID int64) bool {
	if group == nil || ownerUserID <= 0 || !group.IsUserPrivateScope() || group.OwnerUserID == nil {
		return false
	}
	return *group.OwnerUserID == ownerUserID
}

func isPlatformSharedQuotaGroup(group *Group) bool {
	if group == nil {
		return false
	}
	return group.OwnerUserID == nil && NormalizeGroupScope(group.Scope) == GroupScopePublic
}

func (b *accountQuotaGroupDashboardBuilder) addAccountToGroup(account Account, groupID *int64, groupName, groupStatus, platform, requiredAccountLevel string, requireOAuthOnly, requirePrivacySet bool) {
	key := accountQuotaGroupKey(groupID, platform)
	acc, ok := b.accumulators[key]
	if !ok {
		acc = newAccountQuotaGroupSummaryAccumulator(groupID, groupName, groupStatus, platform)
		b.accumulators[key] = acc
	}
	acc.addAccount(account, b.generatedAt, accountSchedulableInQuotaGroup(account, groupStatus, platform, requiredAccountLevel, requireOAuthOnly, requirePrivacySet))
}

func newAccountQuotaGroupSummaryAccumulator(groupID *int64, groupName, groupStatus, platform string) *accountQuotaGroupSummaryAccumulator {
	var idCopy *int64
	if groupID != nil {
		id := *groupID
		idCopy = &id
	}
	return &accountQuotaGroupSummaryAccumulator{
		groupID:     idCopy,
		groupName:   groupName,
		groupStatus: groupStatus,
		core: accountQuotaSummaryAccumulator{
			summary: AccountQuotaSummary{
				Platform: platform,
				Type:     "all",
			},
			windowAggs: make(map[string]*accountUsageWindowAccumulator),
		},
	}
}

func (a *accountQuotaGroupSummaryAccumulator) addAccount(account Account, now time.Time, schedulable bool) {
	if a == nil {
		return
	}
	a.core.addAccountWithSchedulability(account, now, schedulable)
}

func (a *accountQuotaGroupSummaryAccumulator) finalize() AccountQuotaGroupSummary {
	if a == nil {
		return AccountQuotaGroupSummary{}
	}
	summary := a.core.finalize()
	return AccountQuotaGroupSummary{
		GroupID:                 cloneInt64Ptr(a.groupID),
		GroupName:               a.groupName,
		GroupStatus:             a.groupStatus,
		Platform:                summary.Platform,
		AccountCount:            summary.AccountCount,
		ActiveAccountCount:      summary.ActiveAccountCount,
		SchedulableAccountCount: summary.SchedulableAccountCount,
		QuotaAccountCount:       summary.QuotaAccountCount,
		UnlimitedAccountCount:   summary.UnlimitedAccountCount,
		Total:                   summary.Total,
		Daily:                   summary.Daily,
		Weekly:                  summary.Weekly,
		UsageWindows:            summary.UsageWindows,
	}
}

func (b *accountQuotaGroupDashboardBuilder) finalize() []AccountQuotaGroupSummary {
	if b == nil || len(b.accumulators) == 0 {
		return nil
	}

	out := make([]AccountQuotaGroupSummary, 0, len(b.accumulators))
	for _, acc := range b.accumulators {
		out = append(out, acc.finalize())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Platform != out[j].Platform {
			return out[i].Platform < out[j].Platform
		}
		if out[i].GroupID == nil && out[j].GroupID != nil {
			return false
		}
		if out[i].GroupID != nil && out[j].GroupID == nil {
			return true
		}
		if out[i].GroupName != out[j].GroupName {
			return out[i].GroupName < out[j].GroupName
		}
		return int64PtrValue(out[i].GroupID) < int64PtrValue(out[j].GroupID)
	})
	return out
}

func accountQuotaGroupKey(groupID *int64, platform string) string {
	if groupID == nil {
		return "ungrouped\x00" + platform
	}
	return fmt.Sprintf("%d", *groupID)
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func int64PtrValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func isPlatformQuotaPoolAccount(account Account) bool {
	if account.OwnerUserID == nil {
		return true
	}
	return (&account).IsPublicShareApproved()
}

func (a *accountQuotaSummaryAccumulator) addAccount(account Account, now time.Time) {
	a.addAccountWithSchedulability(account, now, account.IsSchedulable())
}

func (a *accountQuotaSummaryAccumulator) addAccountWithSchedulability(account Account, now time.Time, schedulable bool) {
	a.summary.AccountCount++
	if account.Status == StatusActive {
		a.summary.ActiveAccountCount++
	}
	if schedulable {
		a.summary.SchedulableAccountCount++
	}

	if account.IsAPIKeyOrBedrock() {
		if account.HasAnyQuotaLimit() {
			a.summary.QuotaAccountCount++
		} else {
			a.summary.UnlimitedAccountCount++
		}

		addQuotaDimension(&a.summary.Total, account.GetQuotaLimit(), account.GetQuotaUsed())

		dailyUsed := account.GetQuotaDailyUsed()
		if account.IsDailyQuotaPeriodExpired() {
			dailyUsed = 0
		}
		addQuotaDimension(&a.summary.Daily, account.GetQuotaDailyLimit(), dailyUsed)

		weeklyUsed := account.GetQuotaWeeklyUsed()
		if account.IsWeeklyQuotaPeriodExpired() {
			weeklyUsed = 0
		}
		addQuotaDimension(&a.summary.Weekly, account.GetQuotaWeeklyLimit(), weeklyUsed)
	}

	if account.Platform == PlatformOpenAI && account.Type == AccountTypeOAuth {
		a.addOpenAIUsageWindow(account, "5h", now)
		a.addOpenAIUsageWindow(account, "7d", now)
	}
}

func accountSchedulableInQuotaGroup(account Account, groupStatus, groupPlatform, requiredAccountLevel string, requireOAuthOnly, requirePrivacySet bool) bool {
	if !account.IsSchedulable() {
		return false
	}
	if groupStatus != "" && groupStatus != StatusActive {
		return false
	}
	if groupPlatform != "" && account.Platform != groupPlatform {
		return false
	}
	if groupPlatform == PlatformOpenAI {
		required := NormalizeOpenAISharedPoolRequiredLevel(requiredAccountLevel)
		if required != "" && !CanOpenAIAccountJoinSharedPool(account.AccountLevel, required) {
			return false
		}
	}
	if requireOAuthOnly && requiresOAuthOnlyGroupCheck(account.Type) {
		return false
	}
	if requirePrivacySet && !account.IsPrivacySet() {
		return false
	}
	return true
}

func (a *accountQuotaSummaryAccumulator) addOpenAIUsageWindow(account Account, window string, now time.Time) {
	agg := a.ensureUsageWindow(window)
	agg.summary.AccountCount++

	progress := buildCodexUsageProgressFromExtra(account.Extra, window, now)
	if progress == nil {
		return
	}

	utilization := progress.Utilization
	if utilization < 0 {
		utilization = 0
	}

	agg.summary.KnownAccountCount++
	agg.utilizationSum += utilization
	remaining := 100 - utilization
	if remaining < 0 {
		remaining = 0
	}
	agg.summary.RemainingCapacityPercent += remaining

	if progress.ResetsAt != nil {
		if agg.summary.NextResetAt == nil || progress.ResetsAt.Before(*agg.summary.NextResetAt) {
			resetAt := *progress.ResetsAt
			agg.summary.NextResetAt = &resetAt
		}
		remainingSeconds := progress.RemainingSeconds
		if remainingSeconds < 0 {
			remainingSeconds = 0
		}
		if agg.summary.MinRemainingSeconds == nil || remainingSeconds < *agg.summary.MinRemainingSeconds {
			next := remainingSeconds
			agg.summary.MinRemainingSeconds = &next
		}
	}
}

func (a *accountQuotaSummaryAccumulator) ensureUsageWindow(window string) *accountUsageWindowAccumulator {
	if a.windowAggs == nil {
		a.windowAggs = make(map[string]*accountUsageWindowAccumulator)
	}
	if agg, ok := a.windowAggs[window]; ok {
		return agg
	}
	agg := &accountUsageWindowAccumulator{
		summary: AccountUsageWindowSummary{Window: window},
	}
	a.windowAggs[window] = agg
	return agg
}

func addQuotaDimension(summary *AccountQuotaDimensionSummary, limit, used float64) {
	if summary == nil || limit <= 0 {
		return
	}
	if used < 0 {
		used = 0
	}

	summary.EnabledAccountCount++
	summary.Limit += limit
	summary.Used += used
	if used >= limit {
		summary.ExhaustedAccountCount++
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	summary.Remaining += remaining
}

func (a *accountQuotaSummaryAccumulator) finalize() AccountQuotaSummary {
	out := a.summary
	finalizeQuotaDimension(&out.Total)
	finalizeQuotaDimension(&out.Daily)
	finalizeQuotaDimension(&out.Weekly)

	if len(a.windowAggs) > 0 {
		out.UsageWindows = make([]AccountUsageWindowSummary, 0, len(a.windowAggs))
		for _, agg := range a.windowAggs {
			item := agg.summary
			if item.KnownAccountCount > 0 {
				item.AverageUtilization = agg.utilizationSum / float64(item.KnownAccountCount)
			}
			out.UsageWindows = append(out.UsageWindows, item)
		}
		sort.Slice(out.UsageWindows, func(i, j int) bool {
			return usageWindowSortOrder(out.UsageWindows[i].Window) < usageWindowSortOrder(out.UsageWindows[j].Window)
		})
	}

	return out
}

func finalizeQuotaDimension(summary *AccountQuotaDimensionSummary) {
	if summary == nil || summary.Limit <= 0 {
		return
	}
	summary.Utilization = (summary.Used / summary.Limit) * 100
}

func usageWindowSortOrder(window string) int {
	switch window {
	case "5h":
		return 1
	case "7d":
		return 2
	default:
		return 99
	}
}
