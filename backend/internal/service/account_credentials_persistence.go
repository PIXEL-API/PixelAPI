package service

import (
	"context"
	"strconv"
)

type accountCredentialsUpdater interface {
	UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error
}

func persistAccountCredentials(ctx context.Context, repo AccountRepository, account *Account, credentials map[string]any) error {
	if account == nil {
		return ErrAccountNilInput
	}
	if repo == nil {
		return ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{
			"operation": "update_account_credentials",
			"stage":     "repository_unavailable",
		})
	}
	updater, ok := any(repo).(accountCredentialsUpdater)
	if !ok {
		return ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{
			"account_id": strconv.FormatInt(account.ID, 10),
			"operation":  "update_account_credentials",
			"stage":      "missing_narrow_capability",
		})
	}

	nextCredentials := cloneCredentials(credentials)
	if err := updater.UpdateCredentials(ctx, account.ID, nextCredentials); err != nil {
		return err
	}
	account.Credentials = nextCredentials
	return nil
}

func cloneCredentials(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
