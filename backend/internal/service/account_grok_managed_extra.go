package service

import (
	"encoding/json"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrOwnedAccountGrokManagedExtraNotAllowed = infraerrors.BadRequest(
		"OWNED_ACCOUNT_GROK_MANAGED_EXTRA_NOT_ALLOWED",
		"user accounts cannot modify server-managed Grok media eligibility metadata",
	)
	ErrGrokBillingSnapshotManaged = infraerrors.BadRequest(
		"GROK_BILLING_SNAPSHOT_MANAGED",
		"grok_billing_snapshot is server-managed and cannot be set manually",
	)
	ErrGrokMediaEligibilityOverrideInvalid = infraerrors.BadRequest(
		"GROK_MEDIA_ELIGIBILITY_OVERRIDE_INVALID",
		"grok_media_eligible must be a boolean or null",
	)
)

var ownedAccountGrokManagedExtraKeys = [...]string{
	GrokMediaEligibleExtraKey,
	grokBillingExtraKey,
}

// rejectOwnedAccountGrokManagedExtra prevents user-owned account APIs from
// forging operator overrides or provider observations used by the scheduler.
func rejectOwnedAccountGrokManagedExtra(extra map[string]any) error {
	for _, key := range ownedAccountGrokManagedExtraKeys {
		if _, exists := extra[key]; exists {
			return ErrOwnedAccountGrokManagedExtraNotAllowed.WithMetadata(map[string]string{"field": key})
		}
	}
	return nil
}

// preserveOwnedAccountGrokManagedExtra keeps server-managed values on a
// replacement-style user update. Echoing the unchanged value is accepted so
// clients may safely round-trip an account response, while mutations and
// explicit deletion attempts fail closed.
func preserveOwnedAccountGrokManagedExtra(current, next map[string]any) error {
	for _, key := range ownedAccountGrokManagedExtraKeys {
		requested, provided := next[key]
		stored, exists := current[key]
		if provided && (!exists || !sameAccountJSONValue(stored, requested)) {
			return ErrOwnedAccountGrokManagedExtraNotAllowed.WithMetadata(map[string]string{"field": key})
		}
		preserveMapKey(current, next, key)
	}
	return nil
}

// validateAdminGrokManagedExtra allows administrators to set only the
// documented boolean eligibility override. Billing observations are written
// exclusively by the quota probe and cannot enter through generic CRUD APIs.
func validateAdminGrokManagedExtra(extra map[string]any) (mediaOverrideProvided bool, err error) {
	if _, exists := extra[grokBillingExtraKey]; exists {
		return false, ErrGrokBillingSnapshotManaged
	}
	raw, exists := extra[GrokMediaEligibleExtraKey]
	if !exists {
		return false, nil
	}
	if raw != nil {
		if _, ok := raw.(bool); !ok {
			return false, ErrGrokMediaEligibilityOverrideInvalid
		}
	}
	return true, nil
}

func sameAccountJSONValue(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	if leftErr != nil {
		return false
	}
	rightJSON, rightErr := json.Marshal(right)
	if rightErr != nil {
		return false
	}
	return string(leftJSON) == string(rightJSON)
}
