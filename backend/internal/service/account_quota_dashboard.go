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
	GeneratedAt time.Time             `json:"generated_at"`
	Summaries   []AccountQuotaSummary `json:"summaries"`
	Totals      AccountQuotaSummary   `json:"totals"`
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
	platform := newAccountQuotaDashboardBuilder(generatedAt)

	if err := visitAccountQuotaDashboardAccounts(ctx, s.accountRepo, func(account Account) {
		if account.OwnerUserID != nil && *account.OwnerUserID == ownerUserID {
			mine.addAccount(account)
		}
		if isPlatformQuotaPoolAccount(account) {
			platform.addAccount(account)
		}
	}); err != nil {
		return nil, err
	}

	return &UserAccountQuotaPoolDashboard{
		GeneratedAt: generatedAt,
		Mine:        mine.finalize(),
		Platform:    platform.finalize(),
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

func isPlatformQuotaPoolAccount(account Account) bool {
	if account.OwnerUserID == nil {
		return true
	}
	return (&account).IsPublicShareApproved()
}

func (a *accountQuotaSummaryAccumulator) addAccount(account Account, now time.Time) {
	a.summary.AccountCount++
	if account.Status == StatusActive {
		a.summary.ActiveAccountCount++
	}
	if account.IsSchedulable() {
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
