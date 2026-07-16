package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/imroc/req/v3"
)

const (
	chatGPTUsageURL            = "https://chatgpt.com/backend-api/wham/usage"
	chatGPTRateLimitCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	chatGPTRateLimitResetURL   = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"
	openaiQuotaUpstreamTimeout = 20 * time.Second
	openaiQuotaCodexBeta       = "codex-1"
	openaiQuotaCodexOriginator = "Codex Desktop"
	openaiQuotaLanguageTag     = "zh-CN"
)

type OpenAIRateLimitWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

type OpenAIRateLimit struct {
	Allowed         bool                   `json:"allowed"`
	LimitReached    bool                   `json:"limit_reached"`
	PrimaryWindow   *OpenAIRateLimitWindow `json:"primary_window,omitempty"`
	SecondaryWindow *OpenAIRateLimitWindow `json:"secondary_window,omitempty"`
}

type OpenAIAdditionalRateLimit struct {
	LimitName      string           `json:"limit_name"`
	MeteredFeature string           `json:"metered_feature"`
	RateLimit      *OpenAIRateLimit `json:"rate_limit,omitempty"`
}

// OpenAIRateLimitResetCreditDetail is the sanitized projection exposed for an
// available credit. Upstream identifiers and other sensitive fields are never
// copied into the public quota response.
type OpenAIRateLimitResetCreditDetail struct {
	ExpiresAt string `json:"expires_at,omitempty"`
}

type OpenAIRateLimitResetCredits struct {
	AvailableCount int                                `json:"available_count"`
	Credits        []OpenAIRateLimitResetCreditDetail `json:"credits,omitempty"`
}

type OpenAIQuotaUsage struct {
	UserID                string                       `json:"user_id,omitempty"`
	AccountID             string                       `json:"account_id,omitempty"`
	Email                 string                       `json:"email,omitempty"`
	PlanType              string                       `json:"plan_type,omitempty"`
	RateLimit             *OpenAIRateLimit             `json:"rate_limit,omitempty"`
	AdditionalRateLimits  []OpenAIAdditionalRateLimit  `json:"additional_rate_limits,omitempty"`
	RateLimitResetCredits *OpenAIRateLimitResetCredits `json:"rate_limit_reset_credits,omitempty"`
	FetchedAt             int64                        `json:"fetched_at"`
}

type OpenAIQuotaResetCredit struct {
	ID              string `json:"id,omitempty"`
	ResetType       string `json:"reset_type,omitempty"`
	Status          string `json:"status,omitempty"`
	GrantedAt       string `json:"granted_at,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	RedeemStartedAt string `json:"redeem_started_at,omitempty"`
	RedeemedAt      string `json:"redeemed_at,omitempty"`
}

type OpenAIQuotaResetResult struct {
	Code         string                  `json:"code"`
	Credit       *OpenAIQuotaResetCredit `json:"credit,omitempty"`
	WindowsReset int                     `json:"windows_reset"`
}

type OpenAIQuotaService struct {
	accountRepo                AccountRepository
	proxyRepo                  ProxyRepository
	tokenProvider              *OpenAITokenProvider
	privacyClientFactory       PrivacyClientFactory
	agentIdentityTaskMu        sync.Mutex
	agentIdentityWSInvalidator agentIdentityWSConnectionInvalidator
}

func (s *OpenAIQuotaService) SetAgentIdentityWSInvalidator(invalidator *AgentIdentityWSInvalidatorProxy) {
	if s == nil || invalidator == nil {
		panic("OpenAIQuotaService requires an Agent Identity WS invalidator")
	}
	s.agentIdentityWSInvalidator = invalidator
}

func (s *OpenAIQuotaService) buildAuthenticationHeaders(ctx context.Context, account *Account, token string) (http.Header, error) {
	if account != nil && account.IsOpenAIAgentIdentity() {
		if s.agentIdentityWSInvalidator == nil {
			return nil, fmt.Errorf("Agent Identity WS invalidator is not configured")
		}
		return buildAgentIdentityAuthenticationHeaders(ctx, s.accountRepo, s.agentIdentityWSInvalidator, &s.agentIdentityTaskMu, account)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("OpenAI authentication token is missing")
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	return headers, nil
}

func NewOpenAIQuotaService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	tokenProvider *OpenAITokenProvider,
	privacyClientFactory PrivacyClientFactory,
) *OpenAIQuotaService {
	return &OpenAIQuotaService{
		accountRepo:          accountRepo,
		proxyRepo:            proxyRepo,
		tokenProvider:        tokenProvider,
		privacyClientFactory: privacyClientFactory,
	}
}

func (s *OpenAIQuotaService) QueryUsage(ctx context.Context, accountID int64) (*OpenAIQuotaUsage, error) {
	account, accessToken, chatGPTAccountID, proxyURL, err := s.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		return nil, err
	}

	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()

	agentIdentityTaskRecoveryTried := false
	for {
		authHeaders, err := s.buildAuthenticationHeaders(callCtx, account, accessToken)
		if err != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_AUTH_FAILED", "failed to build authentication: %v", err)
		}
		expectedAgentIdentityTaskID := strings.TrimSpace(account.GetCredential("task_id"))
		var payload OpenAIQuotaUsage
		resp, err := client.R().
			SetContext(callCtx).
			SetHeaders(buildOpenAIQuotaHeaders(authHeaders.Get("Authorization"), chatGPTAccountID)).
			SetSuccessResult(&payload).
			Get(chatGPTUsageURL)
		if err != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_REQUEST_FAILED", "upstream request failed: %v", err)
		}
		if !resp.IsSuccessState() {
			status := resp.StatusCode
			rawBody := []byte(resp.String())
			if account.IsOpenAIAgentIdentity() && !agentIdentityTaskRecoveryTried &&
				isAgentIdentityTaskInvalidHTTPResponse(status, rawBody) {
				if err := ensureAgentIdentityTaskForAccount(callCtx, s.accountRepo, s.agentIdentityWSInvalidator, &s.agentIdentityTaskMu, account, expectedAgentIdentityTaskID); err != nil {
					return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_TASK_RECOVERY_FAILED", "failed to recover Agent Identity task: %v", err)
				}
				callCtx = withAgentIdentitySensitiveValues(callCtx, expectedAgentIdentityTaskID)
				agentIdentityTaskRecoveryTried = true
				continue
			}
			rawBody = redactAgentIdentitySensitiveBodyForAccount(account, rawBody, agentIdentitySensitiveValuesFromContext(callCtx)...)
			body := truncate(string(rawBody), 240)
			slog.Warn("openai_quota_query_failed", "account_id", accountID, "status", status, "body", body)
			return nil, infraerrors.Newf(mapOpenAIQuotaUpstreamStatus(status), "OPENAI_QUOTA_UPSTREAM_ERROR", "upstream returned %d: %s", status, body)
		}

		payload.FetchedAt = time.Now().Unix()
		details := s.queryResetCreditDetails(callCtx, client, account, accessToken, chatGPTAccountID, accountID)
		if details != nil {
			if payload.RateLimitResetCredits == nil {
				payload.RateLimitResetCredits = &OpenAIRateLimitResetCredits{}
			}
			if details.CreditListPresent {
				payload.RateLimitResetCredits.Credits = details.Credits
			}
			switch {
			case details.AvailableCount != nil:
				payload.RateLimitResetCredits.AvailableCount = *details.AvailableCount
			case details.CreditListPresent:
				payload.RateLimitResetCredits.AvailableCount = details.AvailableCreditCount
			}
		}
		return &payload, nil
	}
}

func (s *OpenAIQuotaService) queryResetCreditDetails(
	ctx context.Context,
	client *req.Client,
	account *Account,
	accessToken string,
	chatGPTAccountID string,
	accountID int64,
) *openAIRateLimitResetCreditDetails {
	authHeaders, err := s.buildAuthenticationHeaders(ctx, account, accessToken)
	if err != nil {
		slog.Warn("openai_quota_reset_credit_details_auth_failed", "account_id", accountID, "error", err)
		return nil
	}
	resp, err := client.R().
		SetContext(ctx).
		SetHeaders(buildOpenAIQuotaHeaders(authHeaders.Get("Authorization"), chatGPTAccountID)).
		Get(chatGPTRateLimitCreditsURL)
	if err != nil {
		slog.Warn("openai_quota_reset_credit_details_failed", "account_id", accountID, "error", err)
		return nil
	}
	if !resp.IsSuccessState() {
		slog.Warn("openai_quota_reset_credit_details_failed", "account_id", accountID, "status", resp.StatusCode)
		return nil
	}

	details, err := parseOpenAIRateLimitResetCreditDetails(resp.Bytes())
	if err != nil {
		slog.Warn("openai_quota_reset_credit_details_parse_failed", "account_id", accountID, "error", err)
		if details.AvailableCount == nil {
			return nil
		}
	}
	if details.AvailableCount == nil && !details.CreditListPresent {
		return nil
	}
	return &details
}

func (s *OpenAIQuotaService) ResetCredit(ctx context.Context, accountID int64) (*OpenAIQuotaResetResult, error) {
	account, err := s.loadQuotaAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.IsOpenAIAgentIdentity() {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_RESET_UNSUPPORTED", "reset credits are not supported for Agent Identity accounts")
	}
	accessToken, chatGPTAccountID, proxyURL, err := s.prepareUpstreamCallForAccount(ctx, account)
	if err != nil {
		return nil, err
	}

	redeemRequestID, err := generateOpenAIQuotaRedeemRequestID()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_QUOTA_REDEEM_ID_FAILED", "failed to generate redeem id: %v", err)
	}

	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()

	authHeaders, err := s.buildAuthenticationHeaders(callCtx, account, accessToken)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_AUTH_FAILED", "failed to build authentication: %v", err)
	}
	headers := buildOpenAIQuotaHeaders(authHeaders.Get("Authorization"), chatGPTAccountID)
	headers["content-type"] = "application/json"

	var payload OpenAIQuotaResetResult
	resp, err := client.R().
		SetContext(callCtx).
		SetHeaders(headers).
		SetBody(map[string]string{"redeem_request_id": redeemRequestID}).
		SetSuccessResult(&payload).
		Post(chatGPTRateLimitResetURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_RESET_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		status := resp.StatusCode
		body := truncate(resp.String(), 240)
		slog.Warn("openai_quota_reset_failed", "account_id", accountID, "status", status, "body", body)
		return nil, infraerrors.Newf(mapOpenAIQuotaUpstreamStatus(status), "OPENAI_QUOTA_RESET_UPSTREAM_ERROR", "upstream returned %d: %s", status, body)
	}

	slog.Info("openai_quota_reset_success", "account_id", accountID, "code", payload.Code, "windows_reset", payload.WindowsReset)
	return &payload, nil
}

func (s *OpenAIQuotaService) prepareUpstreamCall(ctx context.Context, accountID int64) (account *Account, accessToken, chatGPTAccountID, proxyURL string, err error) {
	if s == nil || s.accountRepo == nil || s.tokenProvider == nil || s.privacyClientFactory == nil {
		return nil, "", "", "", infraerrors.New(http.StatusInternalServerError, "OPENAI_QUOTA_NOT_CONFIGURED", "openai quota service is not configured")
	}

	account, err = s.loadQuotaAccount(ctx, accountID)
	if err != nil {
		return nil, "", "", "", err
	}
	accessToken, chatGPTAccountID, proxyURL, err = s.prepareUpstreamCallForAccount(ctx, account)
	if err != nil {
		return nil, "", "", "", err
	}
	return account, accessToken, chatGPTAccountID, proxyURL, nil
}

func (s *OpenAIQuotaService) loadQuotaAccount(ctx context.Context, accountID int64) (*Account, error) {
	if s == nil || s.accountRepo == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_QUOTA_NOT_CONFIGURED", "openai quota service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusNotFound, "OPENAI_QUOTA_ACCOUNT_NOT_FOUND", "account not found: %v", err)
	}
	if account == nil {
		return nil, infraerrors.New(http.StatusNotFound, "OPENAI_QUOTA_ACCOUNT_NOT_FOUND", "account not found")
	}
	if account.Platform != PlatformOpenAI {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_INVALID_PLATFORM", "account is not an OpenAI account")
	}
	if account.Type != AccountTypeOAuth {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_INVALID_TYPE", "account is not an OAuth account")
	}
	return account, nil
}

func (s *OpenAIQuotaService) prepareUpstreamCallForAccount(ctx context.Context, account *Account) (accessToken, chatGPTAccountID, proxyURL string, err error) {
	if s == nil || s.tokenProvider == nil || s.privacyClientFactory == nil {
		return "", "", "", infraerrors.New(http.StatusInternalServerError, "OPENAI_QUOTA_NOT_CONFIGURED", "openai quota service is not configured")
	}

	chatGPTAccountID = strings.TrimSpace(account.GetCredential("chatgpt_account_id"))
	if chatGPTAccountID == "" {
		chatGPTAccountID = strings.TrimSpace(account.GetCredential("organization_id"))
	}
	if chatGPTAccountID == "" {
		return "", "", "", infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_MISSING_ACCOUNT_ID", "chatgpt_account_id is missing; please re-authorize this account")
	}

	accessToken, err = s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return "", "", "", infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_TOKEN_UNAVAILABLE", "failed to acquire access token: %v", err)
	}
	if strings.TrimSpace(accessToken) == "" && !account.IsOpenAIAgentIdentity() {
		return "", "", "", infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_TOKEN_UNAVAILABLE", "access token is empty")
	}

	if account.ProxyID != nil {
		switch {
		case account.Proxy != nil:
			proxyURL = account.Proxy.URL()
		case s.proxyRepo != nil:
			if proxy, perr := s.proxyRepo.GetByID(ctx, *account.ProxyID); perr == nil && proxy != nil {
				proxyURL = proxy.URL()
			}
		}
	}

	return accessToken, chatGPTAccountID, proxyURL, nil
}

func buildOpenAIQuotaHeaders(authorization, chatGPTAccountID string) map[string]string {
	return map[string]string{
		"authorization":      authorization,
		"chatgpt-account-id": chatGPTAccountID,
		"openai-beta":        openaiQuotaCodexBeta,
		"oai-language":       openaiQuotaLanguageTag,
		"originator":         openaiQuotaCodexOriginator,
		"accept":             "application/json",
		"sec-fetch-site":     "none",
		"sec-fetch-mode":     "no-cors",
		"sec-fetch-dest":     "empty",
		"priority":           "u=4, i",
	}
}

func generateOpenAIQuotaRedeemRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hexStr := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:]), nil
}

func mapOpenAIQuotaUpstreamStatus(status int) int {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return status
	case status == http.StatusTooManyRequests:
		return http.StatusTooManyRequests
	case status >= 400:
		return http.StatusBadGateway
	default:
		return http.StatusBadGateway
	}
}
