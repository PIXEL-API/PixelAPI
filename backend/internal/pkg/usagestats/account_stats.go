package usagestats

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	AccountStatsDefaultDays = 7
	AccountStatsMaxDays     = 31
)

// ResolveAccountStatsDateRange resolves the inclusive calendar-date range used by
// account statistics. startDate and endDate must be provided together. daysRaw
// remains supported for older clients, but is subject to the same 31-day limit.
func ResolveAccountStatsDateRange(startDate, endDate, daysRaw string, now time.Time) (time.Time, time.Time, error) {
	startDate = strings.TrimSpace(startDate)
	endDate = strings.TrimSpace(endDate)
	daysRaw = strings.TrimSpace(daysRaw)

	if startDate != "" || endDate != "" {
		if startDate == "" || endDate == "" {
			return time.Time{}, time.Time{}, fmt.Errorf("start_date and end_date must be provided together")
		}

		start, err := timezone.ParseInLocation("2006-01-02", startDate)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start_date format, use YYYY-MM-DD")
		}
		end, err := timezone.ParseInLocation("2006-01-02", endDate)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end_date format, use YYYY-MM-DD")
		}
		if end.Before(start) {
			return time.Time{}, time.Time{}, fmt.Errorf("end_date must not be before start_date")
		}

		today := timezone.StartOfDay(now)
		if end.After(today) {
			return time.Time{}, time.Time{}, fmt.Errorf("end_date must not be in the future")
		}

		inclusiveDays := 0
		for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
			inclusiveDays++
			if inclusiveDays > AccountStatsMaxDays {
				return time.Time{}, time.Time{}, fmt.Errorf("date range must not exceed %d days", AccountStatsMaxDays)
			}
		}
		return start, end.AddDate(0, 0, 1), nil
	}

	days := AccountStatsDefaultDays
	if daysRaw != "" {
		parsedDays, err := strconv.Atoi(daysRaw)
		if err != nil || parsedDays <= 0 {
			return time.Time{}, time.Time{}, fmt.Errorf("days must be a positive integer")
		}
		if parsedDays > AccountStatsMaxDays {
			return time.Time{}, time.Time{}, fmt.Errorf("days must not exceed %d", AccountStatsMaxDays)
		}
		days = parsedDays
	}

	today := timezone.StartOfDay(now)
	return today.AddDate(0, 0, -days+1), today.AddDate(0, 0, 1), nil
}

// AccountStats 账号使用统计
//
// cost: 账号口径费用（使用 total_cost * account_rate_multiplier）
// standard_cost: 标准费用（使用 total_cost，不含倍率）
// user_cost: 用户/API Key 口径费用（使用 actual_cost，受分组倍率影响）
type AccountStats struct {
	Requests     int64   `json:"requests"`
	Tokens       int64   `json:"tokens"`
	Cost         float64 `json:"cost"`
	StandardCost float64 `json:"standard_cost"`
	UserCost     float64 `json:"user_cost"`
}
