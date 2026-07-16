package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

const grokQuotaSnapshotExtraKey = "grok_usage_snapshot"

const grokQuotaSnapshotSchedulabilityMaxAge = 2 * time.Hour

type GrokQuotaFetcher struct{}

func NewGrokQuotaFetcher() *GrokQuotaFetcher {
	return &GrokQuotaFetcher{}
}

func (f *GrokQuotaFetcher) BuildUsageInfo(account *Account) *UsageInfo {
	now := time.Now()
	usage := &UsageInfo{
		Source:    "passive",
		UpdatedAt: &now,
	}
	if account == nil {
		usage.ErrorCode = "quota_unknown"
		usage.Error = "Grok quota is unknown until the first upstream response includes xAI rate-limit headers or billing is probed"
		return usage
	}

	billing, _ := grokBillingSnapshotFromExtra(account.Extra)
	snapshot, err := grokQuotaSnapshotFromExtra(account.Extra)
	if billing != nil {
		usage.GrokBilling = billing
		if billing.Plan != "" {
			usage.SubscriptionTier = billing.Plan
			usage.SubscriptionTierRaw = billing.Plan
		}
		if parsedAt, parseErr := time.Parse(time.RFC3339, billing.UpdatedAt); parseErr == nil {
			usage.UpdatedAt = &parsedAt
		}
		if billing.FetchedAt != "" {
			usage.GrokLastQuotaProbeAt = billing.FetchedAt
		}
		usage.GrokQuotaSnapshotState = "billing_observed"
		usage.GrokLastStatusCode = billing.StatusCode
		switch billing.StatusCode {
		case http.StatusUnauthorized:
			usage.NeedsReauth = true
			usage.ErrorCode = "unauthenticated"
		case http.StatusForbidden:
			usage.IsForbidden = true
			usage.ForbiddenType = "forbidden"
			usage.ErrorCode = "forbidden"
		case http.StatusTooManyRequests:
			usage.ErrorCode = "rate_limited"
		}
	}

	if err != nil || snapshot == nil {
		applyGrokCredentialUsageFallback(usage, account)
		if billing == nil {
			usage.ErrorCode = "quota_unknown"
			usage.Error = "Grok quota is unknown until the first upstream response includes xAI rate-limit headers or billing is probed"
		}
		return usage
	}

	if parsedAt, parseErr := time.Parse(time.RFC3339, snapshot.UpdatedAt); parseErr == nil {
		if billing == nil || usage.UpdatedAt == nil || parsedAt.After(*usage.UpdatedAt) {
			usage.UpdatedAt = &parsedAt
		}
	}
	usage.GrokRequestQuota = snapshot.Requests
	usage.GrokTokenQuota = snapshot.Tokens
	usage.GrokRetryAfterSeconds = snapshot.RetryAfterSeconds
	if usage.SubscriptionTier == "" {
		usage.SubscriptionTier = snapshot.SubscriptionTier
		usage.SubscriptionTierRaw = snapshot.SubscriptionTier
	}
	if usage.GrokEntitlementStatus == "" {
		usage.GrokEntitlementStatus = snapshot.EntitlementStatus
	}
	if usage.GrokLastQuotaProbeAt == "" {
		usage.GrokLastQuotaProbeAt = snapshot.LastProbeAt
	}
	usage.GrokLastHeadersSeenAt = snapshot.LastHeadersSeenAt
	if snapshot.StatusCode >= http.StatusBadRequest || usage.GrokLastStatusCode == 0 {
		usage.GrokLastStatusCode = snapshot.StatusCode
	}
	if snapshot.HasObservedHeaders() {
		if usage.GrokQuotaSnapshotState == "" {
			usage.GrokQuotaSnapshotState = "observed"
		}
	} else if billing == nil {
		usage.GrokQuotaSnapshotState = "no_headers"
		usage.ErrorCode = "quota_unknown"
		usage.Error = "No xAI quota headers observed on the latest Grok probe"
	}

	if usage.ErrorCode == "" {
		switch snapshot.StatusCode {
		case http.StatusUnauthorized:
			usage.NeedsReauth = true
			usage.ErrorCode = "unauthenticated"
		case http.StatusForbidden:
			usage.IsForbidden = true
			usage.ForbiddenType = "forbidden"
			usage.ErrorCode = "forbidden"
			if usage.GrokEntitlementStatus == "" {
				usage.GrokEntitlementStatus = "forbidden"
			}
		case http.StatusTooManyRequests:
			usage.ErrorCode = "rate_limited"
		}
	}
	applyGrokCredentialUsageFallback(usage, account)
	return usage
}

func applyGrokCredentialUsageFallback(usage *UsageInfo, account *Account) {
	if usage == nil || account == nil {
		return
	}
	if usage.SubscriptionTier == "" {
		tier := strings.TrimSpace(account.GetCredential("subscription_tier"))
		usage.SubscriptionTier = tier
		usage.SubscriptionTierRaw = tier
	}
	if usage.GrokEntitlementStatus == "" {
		usage.GrokEntitlementStatus = strings.TrimSpace(account.GetCredential("entitlement_status"))
	}
}

func grokBillingSnapshotFromExtra(extra map[string]any) (*xai.BillingSummary, error) {
	if extra == nil {
		return nil, nil
	}
	raw, ok := extra[grokBillingExtraKey]
	if !ok || raw == nil {
		return nil, nil
	}
	switch snapshot := raw.(type) {
	case *xai.BillingSummary:
		return snapshot, nil
	case xai.BillingSummary:
		return &snapshot, nil
	case map[string]any:
		data, err := json.Marshal(snapshot)
		if err != nil {
			return nil, err
		}
		var out xai.BillingSummary
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return &out, nil
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("marshal grok billing snapshot: %w", err)
		}
		var out xai.BillingSummary
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
}

func grokQuotaSnapshotFromExtra(extra map[string]any) (*xai.QuotaSnapshot, error) {
	if extra == nil {
		return nil, nil
	}
	raw, ok := extra[grokQuotaSnapshotExtraKey]
	if !ok || raw == nil {
		return nil, nil
	}
	switch snapshot := raw.(type) {
	case *xai.QuotaSnapshot:
		return snapshot, nil
	case xai.QuotaSnapshot:
		return &snapshot, nil
	case map[string]any:
		data, err := json.Marshal(snapshot)
		if err != nil {
			return nil, err
		}
		var out xai.QuotaSnapshot
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return &out, nil
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("marshal grok quota snapshot: %w", err)
		}
		var out xai.QuotaSnapshot
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
}

func shouldAutoPauseGrokAccountByQuota(account *Account) (bool, time.Time) {
	return shouldAutoPauseGrokAccountByQuotaAt(account, time.Now())
}

func shouldAutoPauseGrokAccountByQuotaAt(account *Account, now time.Time) (bool, time.Time) {
	if account == nil || account.Platform != PlatformGrok || account.Type != AccountTypeOAuth {
		return false, time.Time{}
	}
	snapshot, err := grokQuotaSnapshotFromExtra(account.Extra)
	if err != nil || snapshot == nil {
		return false, time.Time{}
	}
	updatedAt, err := time.Parse(time.RFC3339, snapshot.UpdatedAt)
	if err != nil || updatedAt.IsZero() || now.Sub(updatedAt) > grokQuotaSnapshotSchedulabilityMaxAge {
		return false, time.Time{}
	}
	if snapshot.RetryAfterSeconds != nil && *snapshot.RetryAfterSeconds > 0 {
		until := updatedAt.Add(time.Duration(*snapshot.RetryAfterSeconds) * time.Second)
		if now.Before(until) {
			return true, until
		}
	}
	for _, window := range []*xai.QuotaWindow{snapshot.Requests, snapshot.Tokens} {
		if window == nil || window.Remaining == nil || *window.Remaining > 0 || window.ResetUnix == nil {
			continue
		}
		until := time.Unix(*window.ResetUnix, 0)
		if now.Before(until) {
			return true, until
		}
	}
	return false, time.Time{}
}
