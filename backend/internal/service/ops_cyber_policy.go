package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const CyberPolicyRequestExportMaxRows = 1000

type CyberPolicyRequestFilter struct {
	StartTime  *time.Time
	EndTime    *time.Time
	GroupQuery string
	UserQuery  string
	Model      string
	Endpoint   string
	Page       int
	PageSize   int
}

type CyberPolicyRequest struct {
	ID                      int64     `json:"id"`
	CreatedAt               time.Time `json:"created_at"`
	RequestID               string    `json:"request_id"`
	UserID                  *int64    `json:"user_id,omitempty"`
	UserName                string    `json:"user_name"`
	UserEmail               string    `json:"user_email"`
	GroupID                 *int64    `json:"group_id,omitempty"`
	GroupName               string    `json:"group_name"`
	APIKeyID                *int64    `json:"api_key_id,omitempty"`
	APIKeyName              string    `json:"api_key_name"`
	AccountID               *int64    `json:"account_id,omitempty"`
	AccountName             string    `json:"account_name"`
	RequestedModel          string    `json:"requested_model"`
	UpstreamModel           string    `json:"upstream_model"`
	InboundEndpoint         string    `json:"inbound_endpoint"`
	UpstreamEndpoint        string    `json:"upstream_endpoint"`
	StatusCode              int       `json:"status_code"`
	UpstreamStatusCode      *int      `json:"upstream_status_code,omitempty"`
	ProviderErrorCode       string    `json:"provider_error_code"`
	UpstreamErrorMessage    string    `json:"upstream_error_message"`
	RequestContentPreview   string    `json:"request_content_preview"`
	RequestContentTruncated bool      `json:"request_content_truncated"`
	RequestContentBytes     *int      `json:"request_content_bytes,omitempty"`
}

type CyberPolicyRequestDetail struct {
	CyberPolicyRequest
	RequestContent      string `json:"request_content"`
	UpstreamErrorDetail string `json:"upstream_error_detail"`
	UpstreamErrors      string `json:"upstream_errors"`
}

type CyberPolicyRequestList struct {
	Items    []*CyberPolicyRequest `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

type OpsCyberPolicyRepository interface {
	ListCyberPolicyRequests(ctx context.Context, filter CyberPolicyRequestFilter) (*CyberPolicyRequestList, error)
	GetCyberPolicyRequestByID(ctx context.Context, id int64) (*CyberPolicyRequestDetail, error)
	ListCyberPolicyRequestsForExport(ctx context.Context, filter CyberPolicyRequestFilter, limit int) ([]*CyberPolicyRequestDetail, error)
}

func (s *OpsService) ListCyberPolicyRequests(ctx context.Context, filter CyberPolicyRequestFilter) (*CyberPolicyRequestList, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	repo, ok := s.opsRepo.(OpsCyberPolicyRepository)
	if !ok {
		return nil, infraerrors.ServiceUnavailable("OPS_CYBER_POLICY_REPOSITORY_UNAVAILABLE", "Cyber Policy request repository is unavailable")
	}
	filter = normalizeCyberPolicyRequestFilter(filter)
	result, err := repo.ListCyberPolicyRequests(ctx, filter)
	if err != nil {
		return nil, infraerrors.InternalServer("OPS_CYBER_POLICY_LIST_FAILED", "Failed to load Cyber Policy requests").WithCause(err)
	}
	return result, nil
}

func (s *OpsService) GetCyberPolicyRequestByID(ctx context.Context, id int64) (*CyberPolicyRequestDetail, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, infraerrors.BadRequest("OPS_CYBER_POLICY_INVALID_ID", "Invalid Cyber Policy request id")
	}
	repo, ok := s.opsRepo.(OpsCyberPolicyRepository)
	if !ok {
		return nil, infraerrors.ServiceUnavailable("OPS_CYBER_POLICY_REPOSITORY_UNAVAILABLE", "Cyber Policy request repository is unavailable")
	}
	detail, err := repo.GetCyberPolicyRequestByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, infraerrors.NotFound("OPS_CYBER_POLICY_REQUEST_NOT_FOUND", "Cyber Policy request not found")
		}
		return nil, infraerrors.InternalServer("OPS_CYBER_POLICY_LOAD_FAILED", "Failed to load Cyber Policy request").WithCause(err)
	}
	return detail, nil
}

func (s *OpsService) ExportCyberPolicyRequests(ctx context.Context, filter CyberPolicyRequestFilter) ([]*CyberPolicyRequestDetail, bool, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, false, err
	}
	repo, ok := s.opsRepo.(OpsCyberPolicyRepository)
	if !ok {
		return nil, false, infraerrors.ServiceUnavailable("OPS_CYBER_POLICY_REPOSITORY_UNAVAILABLE", "Cyber Policy request repository is unavailable")
	}
	filter = normalizeCyberPolicyRequestFilter(filter)
	items, err := repo.ListCyberPolicyRequestsForExport(ctx, filter, CyberPolicyRequestExportMaxRows+1)
	if err != nil {
		return nil, false, infraerrors.InternalServer("OPS_CYBER_POLICY_EXPORT_FAILED", "Failed to export Cyber Policy requests").WithCause(err)
	}
	truncated := len(items) > CyberPolicyRequestExportMaxRows
	if truncated {
		items = items[:CyberPolicyRequestExportMaxRows]
	}
	return items, truncated, nil
}

func normalizeCyberPolicyRequestFilter(filter CyberPolicyRequestFilter) CyberPolicyRequestFilter {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	filter.GroupQuery = strings.TrimSpace(filter.GroupQuery)
	filter.UserQuery = strings.TrimSpace(filter.UserQuery)
	filter.Model = strings.TrimSpace(filter.Model)
	filter.Endpoint = strings.TrimSpace(filter.Endpoint)
	return filter
}
