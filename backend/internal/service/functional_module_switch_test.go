package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestSettingServiceFunctionalSwitchDefaults(t *testing.T) {
	svc := newModuleSwitchSettingService(map[string]string{})
	ctx := context.Background()

	require.False(t, svc.IsInvoiceManagementEnabled(ctx))
	require.True(t, svc.IsWithdrawalManagementEnabled(ctx))
}

func TestWithdrawalServiceListMineRejectsWhenDisabled(t *testing.T) {
	settingSvc := newModuleSwitchSettingService(map[string]string{
		SettingKeyWithdrawalManagementEnabled: "false",
	})
	svc := NewWithdrawalService(moduleSwitchWithdrawalRepo{}, nil, nil, nil, nil, settingSvc)

	items, total, err := svc.ListMine(context.Background(), 1, 1, 20)

	require.Nil(t, items)
	require.Zero(t, total)
	require.Error(t, err)
	require.Equal(t, "WITHDRAWAL_MANAGEMENT_DISABLED", infraerrors.Reason(err))
}

func TestInvoiceServiceCreateRequestRejectsWhenDisabled(t *testing.T) {
	repo := &moduleSwitchInvoiceRepo{}
	settingSvc := newModuleSwitchSettingService(map[string]string{
		SettingKeyInvoiceManagementEnabled: "false",
	})
	svc := NewInvoiceService(repo, settingSvc)

	req, err := svc.CreateRequest(context.Background(), 1, moduleSwitchValidInvoiceRequest(
		InvoiceSourceRef{SourceType: InvoiceSourceTypePaymentOrder, SourceID: 1},
	))

	require.Nil(t, req)
	require.Error(t, err)
	require.Equal(t, "INVOICE_MANAGEMENT_DISABLED", infraerrors.Reason(err))
	require.False(t, repo.createRequestCalled)
}

func TestNormalizeInvoiceRequestAllowsRedeemCodeSource(t *testing.T) {
	input := moduleSwitchValidInvoiceRequest(
		InvoiceSourceRef{SourceType: InvoiceSourceTypeRedeemCode, SourceID: 9},
	)

	got, err := normalizeInvoiceRequestInput(input)

	require.NoError(t, err)
	require.Len(t, got.SourceRefs, 1)
	require.Equal(t, InvoiceSourceTypeRedeemCode, got.SourceRefs[0].SourceType)
	require.EqualValues(t, 9, got.SourceRefs[0].SourceID)
}

func TestNormalizeInvoiceRequestRejectsDuplicateSources(t *testing.T) {
	input := moduleSwitchValidInvoiceRequest(
		InvoiceSourceRef{SourceType: InvoiceSourceTypePaymentOrder, SourceID: 7},
		InvoiceSourceRef{SourceType: InvoiceSourceTypePaymentOrder, SourceID: 7},
	)

	_, err := normalizeInvoiceRequestInput(input)

	require.Error(t, err)
	require.Equal(t, "INVOICE_SOURCE_INVALID", infraerrors.Reason(err))
}

func TestNormalizeInvoiceRequestRequiresSpecialInvoiceFields(t *testing.T) {
	input := moduleSwitchValidInvoiceRequest(
		InvoiceSourceRef{SourceType: InvoiceSourceTypePaymentOrder, SourceID: 7},
	)
	input.InvoiceType = InvoiceTypeEnterpriseSpecial
	input.TaxID = "91310000MA1K000000"
	input.RegisteredAddress = ""
	input.RegisteredPhone = "021-12345678"
	input.BankName = "Test Bank"
	input.BankAccount = "6222000000000000"

	_, err := normalizeInvoiceRequestInput(input)

	require.Error(t, err)
	require.Equal(t, "INVOICE_FIELD_REQUIRED", infraerrors.Reason(err))
}

func moduleSwitchValidInvoiceRequest(refs ...InvoiceSourceRef) InvoiceRequestInput {
	return InvoiceRequestInput{
		InvoiceType:    InvoiceTypePersonalNormal,
		TitleName:      "Test Buyer",
		RecipientEmail: "invoice@example.com",
		SourceRefs:     refs,
	}
}

func newModuleSwitchSettingService(values map[string]string) *SettingService {
	return NewSettingService(&moduleSwitchSettingRepo{values: values}, &config.Config{})
}

type moduleSwitchSettingRepo struct {
	values map[string]string
}

func (r *moduleSwitchSettingRepo) Get(_ context.Context, key string) (*Setting, error) {
	value, err := r.GetValue(context.Background(), key)
	if err != nil {
		return nil, err
	}
	return &Setting{Key: key, Value: value}, nil
}

func (r *moduleSwitchSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (r *moduleSwitchSettingRepo) Set(_ context.Context, key, value string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value
	return nil
}

func (r *moduleSwitchSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = r.values[key]
	}
	return out, nil
}

func (r *moduleSwitchSettingRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range settings {
		r.values[key] = value
	}
	return nil
}

func (r *moduleSwitchSettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *moduleSwitchSettingRepo) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

var errUnexpectedModuleSwitchRepoCall = errors.New("unexpected module switch repository call")

type moduleSwitchWithdrawalRepo struct{}

func (moduleSwitchWithdrawalRepo) Submit(context.Context, WithdrawalSubmitInput) (*WithdrawalRequest, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (moduleSwitchWithdrawalRepo) Cancel(context.Context, int64, int64, string) (*WithdrawalRequest, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (moduleSwitchWithdrawalRepo) GetByID(context.Context, int64) (*WithdrawalRequest, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (moduleSwitchWithdrawalRepo) ListByUser(context.Context, int64, int, int) ([]WithdrawalRequest, int64, error) {
	return nil, 0, errUnexpectedModuleSwitchRepoCall
}
func (moduleSwitchWithdrawalRepo) ListAdmin(context.Context, WithdrawalListParams) ([]WithdrawalRequest, int64, error) {
	return nil, 0, errUnexpectedModuleSwitchRepoCall
}
func (moduleSwitchWithdrawalRepo) Settle(context.Context, int64, int64, string) (*WithdrawalRequest, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (moduleSwitchWithdrawalRepo) Reject(context.Context, int64, int64, string) (*WithdrawalRequest, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (moduleSwitchWithdrawalRepo) ReceiptCodeInUse(context.Context, string) (bool, error) {
	return false, errUnexpectedModuleSwitchRepoCall
}

type moduleSwitchInvoiceRepo struct {
	createRequestCalled bool
}

func (r *moduleSwitchInvoiceRepo) ListProfiles(context.Context, int64) ([]InvoiceProfile, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) CreateProfile(context.Context, int64, InvoiceProfileInput) (*InvoiceProfile, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) UpdateProfile(context.Context, int64, int64, InvoiceProfileInput) (*InvoiceProfile, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) DeleteProfile(context.Context, int64, int64) error {
	return errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) SetDefaultProfile(context.Context, int64, int64) (*InvoiceProfile, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) ListEligibleSources(context.Context, int64, int, int) ([]InvoiceEligibleSource, int64, error) {
	return nil, 0, errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) CreateRequest(context.Context, int64, InvoiceRequestInput, string) (*InvoiceRequest, error) {
	r.createRequestCalled = true
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) ListRequestsByUser(context.Context, int64, InvoiceRequestListParams) ([]InvoiceRequest, int64, error) {
	return nil, 0, errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) GetRequestByUser(context.Context, int64, int64) (*InvoiceRequest, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) CancelRequest(context.Context, int64, int64) (*InvoiceRequest, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) ListRequestsAdmin(context.Context, InvoiceRequestListParams) ([]InvoiceRequest, int64, error) {
	return nil, 0, errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) GetRequestByID(context.Context, int64) (*InvoiceRequest, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) IssueRequest(context.Context, int64, int64, InvoiceIssueInput) (*InvoiceRequest, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
func (r *moduleSwitchInvoiceRepo) RejectRequest(context.Context, int64, int64, string, string) (*InvoiceRequest, error) {
	return nil, errUnexpectedModuleSwitchRepoCall
}
