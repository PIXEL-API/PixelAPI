package service

import (
	"context"
	"errors"
	"testing"
)

func TestEnsureAccountExternalPlacementIdleFailsClosedWithoutConcurrencyService(t *testing.T) {
	err := ensureAccountExternalPlacementIdle(context.Background(), nil, &Account{ID: 1})
	if !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("expected service unavailable, got %v", err)
	}
}

func TestEnsureAccountExternalPlacementIdleRejectsInvalidAccount(t *testing.T) {
	err := ensureAccountExternalPlacementIdle(context.Background(), nil, nil)
	if !errors.Is(err, ErrAccountExternalPlacementInvalid) {
		t.Fatalf("expected invalid placement account, got %v", err)
	}
}
