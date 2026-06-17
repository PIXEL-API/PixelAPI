package service

import (
	"context"
	"fmt"
)

const (
	UserBalanceLedgerReasonRedeemCode      = "redeem_code"
	UserBalanceLedgerReasonAdminAdjustment = "admin_adjustment"
)

type UserBalanceLedgerDeltaInput struct {
	UserID               int64
	Delta                float64
	Reason               string
	RefType              string
	RefID                *int64
	Metadata             map[string]any
	ClampZero            bool
	AllowNegativeBalance bool
	TrackTotalRecharged  bool
}

type UserBalanceLedgerAdjustmentResult struct {
	BalanceBefore float64
	BalanceAfter  float64
	Delta         float64
}

type UserBalanceLedgerRepository interface {
	LockUserBalanceForUpdate(ctx context.Context, userID int64) (float64, error)
	ApplyBalanceLedgerDelta(ctx context.Context, input UserBalanceLedgerDeltaInput) (*UserBalanceLedgerAdjustmentResult, error)
}

func requireUserBalanceLedgerRepository(repo UserRepository) (UserBalanceLedgerRepository, error) {
	if repo == nil {
		return nil, fmt.Errorf("user repository is not configured")
	}
	ledgerRepo, ok := repo.(UserBalanceLedgerRepository)
	if !ok {
		return nil, fmt.Errorf("user repository does not support balance ledger writes")
	}
	return ledgerRepo, nil
}
