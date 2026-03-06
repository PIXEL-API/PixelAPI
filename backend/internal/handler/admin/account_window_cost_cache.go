package admin

import (
	"strconv"
	"strings"
	"time"
)

var accountWindowCostCache = newSnapshotCache(30 * time.Second)

func buildWindowCostCacheKey(accountIDs []int64) string {
	if len(accountIDs) == 0 {
		return "accounts_window_cost_empty"
	}
	var b strings.Builder
	b.Grow(len(accountIDs) * 6)
	_, _ = b.WriteString("accounts_window_cost:")
	for i, id := range accountIDs {
		if i > 0 {
			_ = b.WriteByte(',')
		}
		_, _ = b.WriteString(strconv.FormatInt(id, 10))
	}
	return b.String()
}
