//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scheduledTestPlanRepoStub struct {
	created *ScheduledTestPlan
	updated *ScheduledTestPlan
}

func (r *scheduledTestPlanRepoStub) Create(_ context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	r.created = plan
	return plan, nil
}

func (r *scheduledTestPlanRepoStub) GetByID(_ context.Context, _ int64) (*ScheduledTestPlan, error) {
	return nil, nil
}

func (r *scheduledTestPlanRepoStub) ListByAccountID(_ context.Context, _ int64) ([]*ScheduledTestPlan, error) {
	return nil, nil
}

func (r *scheduledTestPlanRepoStub) ListDue(_ context.Context, _ time.Time) ([]*ScheduledTestPlan, error) {
	return nil, nil
}

func (r *scheduledTestPlanRepoStub) Update(_ context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	r.updated = plan
	return plan, nil
}

func (r *scheduledTestPlanRepoStub) Delete(_ context.Context, _ int64) error {
	return nil
}

func (r *scheduledTestPlanRepoStub) UpdateAfterRun(_ context.Context, _ int64, _ time.Time, _ time.Time) error {
	return nil
}

func newValidateModelTestService(catalog PricedModelCatalog, account *Account) *AccountTestService {
	repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	svc := &AccountTestService{accountRepo: repo}
	svc.SetModelResolver(NewAccountTestModelResolver(catalog))
	return svc
}

func TestValidateTestModel_ModelAvailable(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"grok-4.5", "grok-4.6"}, nil
	}}
	account := ownedGrokAccount(map[string]any{"grok-4.5": "grok-4.5", "grok-4.6": "grok-4.6"})
	svc := newValidateModelTestService(catalog, account)

	err := svc.ValidateTestModel(context.Background(), account.ID, "grok-4.5")

	require.NoError(t, err)
}

func TestValidateTestModel_ModelNotAvailable(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"grok-4.5"}, nil
	}}
	account := ownedGrokAccount(map[string]any{"grok-4.5": "grok-4.5"})
	svc := newValidateModelTestService(catalog, account)

	err := svc.ValidateTestModel(context.Background(), account.ID, "grok-9.9")

	require.ErrorIs(t, err, ErrAccountTestModelNotAvailable)
}

func TestValidateTestModel_NilResolver(t *testing.T) {
	svc := &AccountTestService{accountRepo: &mockAccountRepoForGemini{}}

	err := svc.ValidateTestModel(context.Background(), 1, "grok-4.5")

	require.ErrorIs(t, err, ErrOwnedAccountModelCatalogUnavailable)
}

func TestScheduledTestService_CreatePlan_ValidatesModel(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"grok-4.5"}, nil
	}}
	account := ownedGrokAccount(map[string]any{"grok-4.5": "grok-4.5"})
	validator := newValidateModelTestService(catalog, account)

	planRepo := &scheduledTestPlanRepoStub{}
	svc := &ScheduledTestService{planRepo: planRepo}
	svc.SetModelValidator(validator)

	// 非法模型被拒绝，不落库
	_, err := svc.CreatePlan(context.Background(), &ScheduledTestPlan{
		AccountID:      account.ID,
		ModelID:        "grok-9.9",
		CronExpression: "*/5 * * * *",
	})
	require.ErrorIs(t, err, ErrAccountTestModelNotAvailable)
	require.Nil(t, planRepo.created)

	// 合法模型通过
	_, err = svc.CreatePlan(context.Background(), &ScheduledTestPlan{
		AccountID:      account.ID,
		ModelID:        "grok-4.5",
		CronExpression: "*/5 * * * *",
	})
	require.NoError(t, err)
	require.NotNil(t, planRepo.created)
}

func TestScheduledTestService_CreatePlan_NoValidatorSkips(t *testing.T) {
	planRepo := &scheduledTestPlanRepoStub{}
	svc := &ScheduledTestService{planRepo: planRepo}

	_, err := svc.CreatePlan(context.Background(), &ScheduledTestPlan{
		AccountID:      1,
		ModelID:        "grok-4.5",
		CronExpression: "*/5 * * * *",
	})

	require.NoError(t, err)
	require.NotNil(t, planRepo.created)
}

func TestScheduledTestService_UpdatePlan_ValidatesModel(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"grok-4.5"}, nil
	}}
	account := ownedGrokAccount(map[string]any{"grok-4.5": "grok-4.5"})
	validator := newValidateModelTestService(catalog, account)

	planRepo := &scheduledTestPlanRepoStub{}
	svc := &ScheduledTestService{planRepo: planRepo}
	svc.SetModelValidator(validator)

	_, err := svc.UpdatePlan(context.Background(), &ScheduledTestPlan{
		AccountID:      account.ID,
		ModelID:        "grok-9.9",
		CronExpression: "*/5 * * * *",
	})
	require.ErrorIs(t, err, ErrAccountTestModelNotAvailable)
	require.Nil(t, planRepo.updated)
}
