package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func newOpsCyberPolicyService(repo OpsRepository) *OpsService {
	return NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func TestOpsServiceListCyberPolicyRequestsNormalizesFilter(t *testing.T) {
	var captured CyberPolicyRequestFilter
	repo := &opsRepoMock{ListCyberPolicyRequestsFn: func(_ context.Context, filter CyberPolicyRequestFilter) (*CyberPolicyRequestList, error) {
		captured = filter
		return &CyberPolicyRequestList{Items: []*CyberPolicyRequest{}, Total: 3, Page: filter.Page, PageSize: filter.PageSize}, nil
	}}

	result, err := newOpsCyberPolicyService(repo).ListCyberPolicyRequests(context.Background(), CyberPolicyRequestFilter{
		GroupQuery: " 研发组 ",
		UserQuery:  " user@example.com ",
		Model:      " gpt-5 ",
		Endpoint:   " /v1/responses ",
		Page:       -1,
		PageSize:   101,
	})

	require.NoError(t, err)
	require.Equal(t, 1, captured.Page)
	require.Equal(t, 100, captured.PageSize)
	require.Equal(t, "研发组", captured.GroupQuery)
	require.Equal(t, "user@example.com", captured.UserQuery)
	require.Equal(t, "gpt-5", captured.Model)
	require.Equal(t, "/v1/responses", captured.Endpoint)
	require.Equal(t, int64(3), result.Total)
}

func TestOpsServiceCyberPolicyRepositoryUnavailable(t *testing.T) {
	_, err := newOpsCyberPolicyService(nil).ListCyberPolicyRequests(context.Background(), CyberPolicyRequestFilter{})
	require.Error(t, err)
	require.Equal(t, "OPS_CYBER_POLICY_REPOSITORY_UNAVAILABLE", infraerrors.Reason(err))
	require.True(t, infraerrors.IsServiceUnavailable(err))
}

func TestOpsServiceGetCyberPolicyRequestValidationAndNotFound(t *testing.T) {
	svc := newOpsCyberPolicyService(&opsRepoMock{GetCyberPolicyRequestByIDFn: func(_ context.Context, _ int64) (*CyberPolicyRequestDetail, error) {
		return nil, sql.ErrNoRows
	}})

	_, err := svc.GetCyberPolicyRequestByID(context.Background(), 0)
	require.True(t, infraerrors.IsBadRequest(err))
	require.Equal(t, "OPS_CYBER_POLICY_INVALID_ID", infraerrors.Reason(err))

	_, err = svc.GetCyberPolicyRequestByID(context.Background(), 7)
	require.True(t, infraerrors.IsNotFound(err))
	require.Equal(t, "OPS_CYBER_POLICY_REQUEST_NOT_FOUND", infraerrors.Reason(err))
}

func TestOpsServiceCyberPolicyRepositoryErrorsAreMapped(t *testing.T) {
	repoErr := errors.New("query failed")
	repo := &opsRepoMock{
		ListCyberPolicyRequestsFn: func(context.Context, CyberPolicyRequestFilter) (*CyberPolicyRequestList, error) {
			return nil, repoErr
		},
		ListCyberPolicyExportFn: func(context.Context, CyberPolicyRequestFilter, int) ([]*CyberPolicyRequestDetail, error) {
			return nil, repoErr
		},
	}
	svc := newOpsCyberPolicyService(repo)

	_, err := svc.ListCyberPolicyRequests(context.Background(), CyberPolicyRequestFilter{})
	require.True(t, infraerrors.IsInternalServer(err))
	require.Equal(t, "OPS_CYBER_POLICY_LIST_FAILED", infraerrors.Reason(err))
	require.ErrorIs(t, err, repoErr)

	_, _, err = svc.ExportCyberPolicyRequests(context.Background(), CyberPolicyRequestFilter{})
	require.True(t, infraerrors.IsInternalServer(err))
	require.Equal(t, "OPS_CYBER_POLICY_EXPORT_FAILED", infraerrors.Reason(err))
	require.ErrorIs(t, err, repoErr)
}

func TestOpsServiceExportCyberPolicyRequestsEnforcesLimit(t *testing.T) {
	items := make([]*CyberPolicyRequestDetail, CyberPolicyRequestExportMaxRows+1)
	for i := range items {
		items[i] = &CyberPolicyRequestDetail{CyberPolicyRequest: CyberPolicyRequest{ID: int64(i + 1)}}
	}
	var capturedLimit int
	repo := &opsRepoMock{ListCyberPolicyExportFn: func(_ context.Context, filter CyberPolicyRequestFilter, limit int) ([]*CyberPolicyRequestDetail, error) {
		capturedLimit = limit
		require.Equal(t, 1, filter.Page)
		require.Equal(t, 20, filter.PageSize)
		return items, nil
	}}

	got, truncated, err := newOpsCyberPolicyService(repo).ExportCyberPolicyRequests(context.Background(), CyberPolicyRequestFilter{})

	require.NoError(t, err)
	require.Equal(t, CyberPolicyRequestExportMaxRows+1, capturedLimit)
	require.True(t, truncated)
	require.Len(t, got, CyberPolicyRequestExportMaxRows)
	require.Equal(t, int64(CyberPolicyRequestExportMaxRows), got[len(got)-1].ID)
}

func TestOpsServiceExportCyberPolicyRequestsDoesNotTruncateAtLimit(t *testing.T) {
	items := make([]*CyberPolicyRequestDetail, CyberPolicyRequestExportMaxRows)
	repo := &opsRepoMock{ListCyberPolicyExportFn: func(context.Context, CyberPolicyRequestFilter, int) ([]*CyberPolicyRequestDetail, error) {
		return items, nil
	}}

	got, truncated, err := newOpsCyberPolicyService(repo).ExportCyberPolicyRequests(context.Background(), CyberPolicyRequestFilter{})

	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, got, CyberPolicyRequestExportMaxRows)
}
