//go:build unit

package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func adminOwnedTestOwnerID() *int64 {
	v := int64(7)
	return &v
}

func adminOwnedGrokCatalog() *ownedPricedModelCatalogStub {
	return &ownedPricedModelCatalogStub{modelsByPlatform: map[string][]string{PlatformGrok: {"grok-4.5"}}}
}

func TestValidateAdminOwnedModelMapping_MissingMapping(t *testing.T) {
	svc := &adminServiceImpl{pricedModelCatalog: adminOwnedGrokCatalog()}

	err := svc.validateAdminOwnedModelMapping(context.Background(), PlatformGrok, map[string]any{})

	require.ErrorIs(t, err, ErrOwnedAccountModelMappingInvalid)
}

func TestValidateAdminOwnedModelMapping_ExplicitEmptyMapping(t *testing.T) {
	svc := &adminServiceImpl{pricedModelCatalog: adminOwnedGrokCatalog()}

	err := svc.validateAdminOwnedModelMapping(context.Background(), PlatformGrok, map[string]any{
		"model_mapping": map[string]any{},
	})

	require.ErrorIs(t, err, ErrOwnedAccountModelMappingInvalid)
}

func TestValidateAdminOwnedModelMapping_NilCatalog(t *testing.T) {
	svc := &adminServiceImpl{}

	err := svc.validateAdminOwnedModelMapping(context.Background(), PlatformGrok, map[string]any{
		"model_mapping": map[string]any{"grok-4.5": "grok-4.5"},
	})

	require.ErrorIs(t, err, ErrOwnedAccountModelCatalogUnavailable)
}

func TestValidateAdminOwnedModelMapping_NotSelectable(t *testing.T) {
	svc := &adminServiceImpl{pricedModelCatalog: adminOwnedGrokCatalog()}

	err := svc.validateAdminOwnedModelMapping(context.Background(), PlatformGrok, map[string]any{
		"model_mapping": map[string]any{"grok-9.9": "grok-9.9"},
	})

	require.ErrorIs(t, err, ErrOwnedAccountModelNotSelectable)
}

func TestValidateAdminOwnedModelMapping_Valid(t *testing.T) {
	svc := &adminServiceImpl{pricedModelCatalog: adminOwnedGrokCatalog()}
	credentials := map[string]any{"model_mapping": map[string]any{"grok-4.5": "grok-4.5"}}

	err := svc.validateAdminOwnedModelMapping(context.Background(), PlatformGrok, credentials)

	require.NoError(t, err)
	require.Equal(t, map[string]any{"grok-4.5": "grok-4.5"}, credentials["model_mapping"])
}

func TestAdminService_CreateOwnedAccount_EmptyWhitelist(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{}
	svc := &adminServiceImpl{
		accountRepo:        repo,
		pricedModelCatalog: adminOwnedGrokCatalog(),
	}

	_, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "owned-grok",
		Platform:             PlatformGrok,
		Type:                 AccountTypeOAuth,
		OwnerUserID:          adminOwnedTestOwnerID(),
		Credentials:          map[string]any{},
		SkipDefaultGroupBind: true,
	})

	require.ErrorIs(t, err, ErrOwnedAccountModelMappingInvalid)
}

func TestAdminService_CreateOwnedAccount_ValidWhitelist(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{}
	svc := &adminServiceImpl{
		accountRepo:        repo,
		pricedModelCatalog: adminOwnedGrokCatalog(),
	}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "owned-grok",
		Platform:             PlatformGrok,
		Type:                 AccountTypeOAuth,
		OwnerUserID:          adminOwnedTestOwnerID(),
		Credentials:          map[string]any{"model_mapping": map[string]any{"grok-4.5": "grok-4.5"}},
		SkipDefaultGroupBind: true,
	})

	require.NoError(t, err)
	require.NotNil(t, account)
	require.NotNil(t, account.OwnerUserID)
}

func TestAdminService_CreatePlatformAccount_SkipsValidation(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{}
	svc := &adminServiceImpl{
		accountRepo:        repo,
		pricedModelCatalog: adminOwnedGrokCatalog(),
	}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "platform-grok",
		Platform:             PlatformGrok,
		Type:                 AccountTypeOAuth,
		SkipDefaultGroupBind: true,
	})

	require.NoError(t, err)
	require.NotNil(t, account)
	require.Nil(t, account.OwnerUserID)
}

func TestAdminService_UpdatePlatformToOwned_EmptyWhitelist(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{
		1: {ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive},
	}}
	svc := &adminServiceImpl{
		accountRepo:        repo,
		pricedModelCatalog: adminOwnedGrokCatalog(),
	}

	_, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		OwnerUserID: adminOwnedTestOwnerID(),
	})

	require.ErrorIs(t, err, ErrOwnedAccountModelMappingInvalid)
}

func TestAdminService_UpdatePlatformToOwned_ValidWhitelist(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{
		1: {ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{}},
	}}
	svc := &adminServiceImpl{
		accountRepo:        repo,
		pricedModelCatalog: adminOwnedGrokCatalog(),
	}

	updated, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		OwnerUserID: adminOwnedTestOwnerID(),
		Credentials: map[string]any{"model_mapping": map[string]any{"grok-4.5": "grok-4.5"}},
	})

	require.NoError(t, err)
	require.NotNil(t, updated.OwnerUserID)
}

func TestAdminService_UpdateExistingOwnedOpencode_RejectsNonCanonicalModelMapping(t *testing.T) {
	ownerID := int64(7)
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{
		1: {
			ID:          1,
			Platform:    PlatformOpencode,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			OwnerUserID: &ownerID,
			Credentials: map[string]any{"model_mapping": map[string]any{"hy3": "hy3"}},
		},
	}}
	svc := &adminServiceImpl{
		accountRepo: repo,
		pricedModelCatalog: &ownedPricedModelCatalogStub{modelsByPlatform: map[string][]string{
			PlatformOpencode: {"hy3", "deepseek-v4-flash-vision-exp"},
		}},
	}

	_, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		Credentials: map[string]any{
			"model_mapping": map[string]any{"Hy3": "Hy3"},
		},
	})

	require.Error(t, err)
	require.Equal(t, "OWNED_ACCOUNT_MODEL_MAPPING_INVALID", infraerrors.Reason(err))
	require.Equal(t, "Hy3", infraerrors.FromError(err).Metadata["model"])
	require.Equal(t, "hy3", infraerrors.FromError(err).Metadata["canonical_model"])
	require.Nil(t, repo.updatedAccount)
}

func TestAdminService_UpdateExistingOwnedOpencode_UnrelatedEditSkipsLegacyMappingValidation(t *testing.T) {
	ownerID := int64(7)
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{
		1: {
			ID:          1,
			Platform:    PlatformOpencode,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			OwnerUserID: &ownerID,
			Credentials: map[string]any{"model_mapping": map[string]any{"Hy3": "Hy3"}},
		},
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	updated, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Name: "renamed"})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, "renamed", updated.Name)
	require.NotNil(t, repo.updatedAccount)
}

func TestAdminService_BulkUpdateExistingOwnedOpencode_RejectsNonCanonicalModelMapping(t *testing.T) {
	ownerID := int64(7)
	repo := &accountRepoStubForBulkUpdate{
		getByIDsAccounts: []*Account{{
			ID:          1,
			Platform:    PlatformOpencode,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			OwnerUserID: &ownerID,
			Credentials: map[string]any{"model_mapping": map[string]any{"hy3": "hy3"}},
		}},
	}
	svc := &adminServiceImpl{
		accountRepo: repo,
		pricedModelCatalog: &ownedPricedModelCatalogStub{modelsByPlatform: map[string][]string{
			PlatformOpencode: {"hy3", "deepseek-v4-flash-vision-exp"},
		}},
	}

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1},
		Credentials: map[string]any{
			"model_mapping": map[string]any{"Hy3": "Hy3"},
		},
	})

	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, "OWNED_ACCOUNT_MODEL_MAPPING_INVALID", infraerrors.Reason(err))
	require.Equal(t, "hy3", infraerrors.FromError(err).Metadata["canonical_model"])
	require.Empty(t, repo.bulkUpdateIDs)
}

func TestAdminService_UpdatePlatformAccountOwnerUnchanged_SkipsValidation(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{
		1: {ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive},
	}}
	svc := &adminServiceImpl{
		accountRepo:        repo,
		pricedModelCatalog: adminOwnedGrokCatalog(),
	}

	updated, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		Name: "renamed-platform-account",
	})

	require.NoError(t, err)
	require.Nil(t, updated.OwnerUserID)
}

func TestAdminService_UpdateOwnedToPlatform_KeepsMapping(t *testing.T) {
	ownerID := int64(7)
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{
		1: {
			ID:          1,
			Platform:    PlatformGrok,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			OwnerUserID: &ownerID,
			Credentials: map[string]any{"model_mapping": map[string]any{"grok-4.5": "grok-4.5"}},
		},
	}}
	svc := &adminServiceImpl{
		accountRepo:        repo,
		pricedModelCatalog: adminOwnedGrokCatalog(),
	}
	clearOwner := int64(0)

	updated, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		OwnerUserID: &clearOwner,
	})

	require.NoError(t, err)
	require.Nil(t, updated.OwnerUserID)
}
