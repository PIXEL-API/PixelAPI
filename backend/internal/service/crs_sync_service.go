package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const (
	crsPreviewTokenVersion     = 1
	crsPreviewTokenTTL         = 5 * time.Minute
	crsPreviewTokenDomain      = "sub2api.crs-sync.preview.v1"
	crsConnectionHashDomain    = "sub2api.crs-sync.connection.v1"
	crsExportHashDomain        = "sub2api.crs-sync.export.v1"
	crsLocalSnapshotHashDomain = "sub2api.crs-sync.local-snapshot.v1"
	crsCapacityProbeDomain     = "sub2api.crs-sync.response-capacity.v1"
	crsPreviewTokenMaxLength   = 4096
	crsUnknownProxyIDForGuard  = int64(0)
	crsSyncItemErrorMaxBytes   = 2048
)

var (
	ErrCRSPreviewActorRequired = infraerrors.BadRequest(
		"CRS_PREVIEW_ACTOR_REQUIRED",
		"an authenticated administrator is required for CRS preview",
	)
	ErrCRSPreviewTokenRequired = infraerrors.BadRequest(
		"CRS_PREVIEW_TOKEN_REQUIRED",
		"a fresh CRS preview token is required before synchronization",
	)
	ErrCRSPreviewTokenInvalid = infraerrors.BadRequest(
		"CRS_PREVIEW_TOKEN_INVALID",
		"the CRS preview token is invalid",
	)
	ErrCRSPreviewTokenExpired = infraerrors.Conflict(
		"CRS_PREVIEW_TOKEN_EXPIRED",
		"the CRS preview expired; preview again before synchronizing",
	)
	ErrCRSPreviewContextConflict = infraerrors.Conflict(
		"CRS_PREVIEW_CONTEXT_CONFLICT",
		"the CRS preview no longer matches the synchronization request",
	)
	ErrCRSPreviewSigningUnavailable = infraerrors.Conflict(
		"CRS_PREVIEW_SIGNING_UNAVAILABLE",
		"CRS preview signing is unavailable",
	)
	ErrCRSExportInvalid = infraerrors.BadRequest(
		"CRS_EXPORT_INVALID",
		"the CRS export contains invalid or duplicate account identifiers",
	)
)

type CRSSyncService struct {
	accountRepo        AccountRepository
	proxyRepo          ProxyRepository
	oauthService       *OAuthService
	openaiOAuthService *OpenAIOAuthService
	geminiOAuthService *GeminiOAuthService
	cfg                *config.Config
	now                func() time.Time
}

func NewCRSSyncService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	oauthService *OAuthService,
	openaiOAuthService *OpenAIOAuthService,
	geminiOAuthService *GeminiOAuthService,
	cfg *config.Config,
) *CRSSyncService {
	return &CRSSyncService{
		accountRepo:        accountRepo,
		proxyRepo:          proxyRepo,
		oauthService:       oauthService,
		openaiOAuthService: openaiOAuthService,
		geminiOAuthService: geminiOAuthService,
		cfg:                cfg,
		now:                time.Now,
	}
}

type SyncFromCRSInput struct {
	BaseURL            string
	Username           string
	Password           string
	SyncProxies        bool
	SelectedAccountIDs []string // if non-empty, only create new accounts with these CRS IDs
	ActorAdminID       int64
	ForceActiveEdit    bool
	Confirmed          bool
	Reason             string
	ExpectedVersion    *int64
	ExpectedVersions   map[int64]int64
	OperationID        string
	PreviewToken       string
	// ValidateResponseCapacity is injected by the HTTP idempotency boundary.
	// It must run before any account or proxy write so a response that cannot
	// be replayed is rejected without producing partial state.
	ValidateResponseCapacity func(any) error
}

type SyncFromCRSItemResult struct {
	CRSAccountID string `json:"crs_account_id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Action       string `json:"action"` // created/updated/failed/skipped
	Error        string `json:"error,omitempty"`
}

type SyncFromCRSResult struct {
	Created int                     `json:"created"`
	Updated int                     `json:"updated"`
	Skipped int                     `json:"skipped"`
	Failed  int                     `json:"failed"`
	Items   []SyncFromCRSItemResult `json:"items"`
}

type crsLoginResponse struct {
	Success  bool   `json:"success"`
	Token    string `json:"token"`
	Message  string `json:"message"`
	Error    string `json:"error"`
	Username string `json:"username"`
}

type crsExportResponse struct {
	Success bool          `json:"success"`
	Error   string        `json:"error"`
	Message string        `json:"message"`
	Data    crsExportData `json:"data"`
}

type crsExportData struct {
	ExportedAt              string                      `json:"exportedAt"`
	ClaudeAccounts          []crsClaudeAccount          `json:"claudeAccounts"`
	ClaudeConsoleAccounts   []crsConsoleAccount         `json:"claudeConsoleAccounts"`
	OpenAIOAuthAccounts     []crsOpenAIOAuthAccount     `json:"openaiOAuthAccounts"`
	OpenAIResponsesAccounts []crsOpenAIResponsesAccount `json:"openaiResponsesAccounts"`
	GeminiOAuthAccounts     []crsGeminiOAuthAccount     `json:"geminiOAuthAccounts"`
	GeminiAPIKeyAccounts    []crsGeminiAPIKeyAccount    `json:"geminiApiKeyAccounts"`
}

type normalizedCRSConnection struct {
	BaseURL  string
	Username string
	Password string
}

type crsPreviewTokenPayload struct {
	Version           int    `json:"version"`
	ActorAdminID      int64  `json:"actor_admin_id"`
	ConnectionHash    string `json:"connection_sha256"`
	ExportHash        string `json:"export_sha256"`
	LocalSnapshotHash string `json:"local_snapshot_sha256"`
	ExpiresAt         int64  `json:"expires_at"`
}

type crsProxy struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type crsProxyPlan struct {
	resolvedID *int64
	pending    *Proxy
}

type crsClaudeAccount struct {
	Kind        string         `json:"kind"`
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Platform    string         `json:"platform"`
	AuthType    string         `json:"authType"` // oauth/setup-token
	IsActive    bool           `json:"isActive"`
	Schedulable bool           `json:"schedulable"`
	Priority    int            `json:"priority"`
	Status      string         `json:"status"`
	Proxy       *crsProxy      `json:"proxy"`
	Credentials map[string]any `json:"credentials"`
	Extra       map[string]any `json:"extra"`
}

type crsConsoleAccount struct {
	Kind               string         `json:"kind"`
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	Platform           string         `json:"platform"`
	IsActive           bool           `json:"isActive"`
	Schedulable        bool           `json:"schedulable"`
	Priority           int            `json:"priority"`
	Status             string         `json:"status"`
	MaxConcurrentTasks int            `json:"maxConcurrentTasks"`
	Proxy              *crsProxy      `json:"proxy"`
	Credentials        map[string]any `json:"credentials"`
}

type crsOpenAIResponsesAccount struct {
	Kind        string         `json:"kind"`
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Platform    string         `json:"platform"`
	IsActive    bool           `json:"isActive"`
	Schedulable bool           `json:"schedulable"`
	Priority    int            `json:"priority"`
	Status      string         `json:"status"`
	Proxy       *crsProxy      `json:"proxy"`
	Credentials map[string]any `json:"credentials"`
}

type crsOpenAIOAuthAccount struct {
	Kind        string         `json:"kind"`
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Platform    string         `json:"platform"`
	AuthType    string         `json:"authType"` // oauth
	IsActive    bool           `json:"isActive"`
	Schedulable bool           `json:"schedulable"`
	Priority    int            `json:"priority"`
	Status      string         `json:"status"`
	Proxy       *crsProxy      `json:"proxy"`
	Credentials map[string]any `json:"credentials"`
	Extra       map[string]any `json:"extra"`
}

type crsGeminiOAuthAccount struct {
	Kind        string         `json:"kind"`
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Platform    string         `json:"platform"`
	AuthType    string         `json:"authType"` // oauth
	IsActive    bool           `json:"isActive"`
	Schedulable bool           `json:"schedulable"`
	Priority    int            `json:"priority"`
	Status      string         `json:"status"`
	Proxy       *crsProxy      `json:"proxy"`
	Credentials map[string]any `json:"credentials"`
	Extra       map[string]any `json:"extra"`
}

type crsGeminiAPIKeyAccount struct {
	Kind        string         `json:"kind"`
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Platform    string         `json:"platform"`
	IsActive    bool           `json:"isActive"`
	Schedulable bool           `json:"schedulable"`
	Priority    int            `json:"priority"`
	Status      string         `json:"status"`
	Proxy       *crsProxy      `json:"proxy"`
	Credentials map[string]any `json:"credentials"`
	Extra       map[string]any `json:"extra"`
}

func (s *CRSSyncService) normalizeCRSConnection(
	baseURL,
	username,
	password string,
) (normalizedCRSConnection, error) {
	if s.cfg == nil {
		return normalizedCRSConnection{}, errors.New("config is not available")
	}
	normalizedURL := strings.TrimSpace(baseURL)
	if s.cfg.Security.URLAllowlist.Enabled {
		normalized, err := normalizeBaseURL(normalizedURL, s.cfg.Security.URLAllowlist.CRSHosts, s.cfg.Security.URLAllowlist.AllowPrivateHosts)
		if err != nil {
			return normalizedCRSConnection{}, err
		}
		normalizedURL = normalized
	} else {
		normalized, err := urlvalidator.ValidateURLFormat(normalizedURL, s.cfg.Security.URLAllowlist.AllowInsecureHTTP)
		if err != nil {
			return normalizedCRSConnection{}, fmt.Errorf("invalid base_url: %w", err)
		}
		normalizedURL = normalized
	}
	normalizedUsername := strings.TrimSpace(username)
	if normalizedUsername == "" || strings.TrimSpace(password) == "" {
		return normalizedCRSConnection{}, errors.New("username and password are required")
	}

	return normalizedCRSConnection{
		BaseURL:  normalizedURL,
		Username: normalizedUsername,
		Password: password,
	}, nil
}

// fetchCRSExport authenticates with CRS and returns the exported accounts for
// an already validated connection.
func (s *CRSSyncService) fetchCRSExport(
	ctx context.Context,
	connection normalizedCRSConnection,
) (*crsExportResponse, error) {
	client, err := httpclient.GetClient(httpclient.Options{
		Timeout:            20 * time.Second,
		ValidateResolvedIP: s.cfg.Security.URLAllowlist.Enabled,
		AllowPrivateHosts:  s.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return nil, fmt.Errorf("create http client failed: %w", err)
	}

	adminToken, err := crsLogin(
		ctx,
		client,
		connection.BaseURL,
		connection.Username,
		connection.Password,
	)
	if err != nil {
		return nil, err
	}

	return crsExportAccounts(ctx, client, connection.BaseURL, adminToken)
}

func hashCRSPreviewValue(domain string, value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	_, _ = io.WriteString(hasher, domain)
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write(encoded)
	return base64.RawURLEncoding.EncodeToString(hasher.Sum(nil)), nil
}

func hashCRSConnection(connection normalizedCRSConnection) (string, error) {
	return hashCRSPreviewValue(crsConnectionHashDomain, struct {
		BaseURL  string `json:"base_url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}{
		BaseURL:  connection.BaseURL,
		Username: connection.Username,
		Password: connection.Password,
	})
}

func normalizeCRSExportAccounts[T any](
	accounts []T,
	category string,
	kindAndID func(T) (string, string),
	seen map[string]string,
) ([]T, error) {
	normalized := append([]T(nil), accounts...)
	for _, account := range normalized {
		_, id := kindAndID(account)
		trimmedID := strings.TrimSpace(id)
		if trimmedID == "" || trimmedID != id {
			return nil, ErrCRSExportInvalid.WithMetadata(map[string]string{
				"category": category,
				"stage":    "invalid_account_id",
			})
		}
		if existingCategory, exists := seen[id]; exists {
			return nil, ErrCRSExportInvalid.WithMetadata(map[string]string{
				"category":          category,
				"crs_account_id":    id,
				"existing_category": existingCategory,
				"stage":             "duplicate_account_id",
			})
		}
		seen[id] = category
	}
	sort.Slice(normalized, func(i, j int) bool {
		leftKind, leftID := kindAndID(normalized[i])
		rightKind, rightID := kindAndID(normalized[j])
		if leftKind == rightKind {
			return leftID < rightID
		}
		return leftKind < rightKind
	})
	return normalized, nil
}

func normalizeCRSExportData(exported *crsExportResponse) (crsExportData, error) {
	if exported == nil {
		return crsExportData{}, ErrCRSExportInvalid.WithMetadata(map[string]string{
			"stage": "missing_export",
		})
	}
	stableData := exported.Data
	stableData.ExportedAt = ""
	seen := make(map[string]string)
	var err error
	stableData.ClaudeAccounts, err = normalizeCRSExportAccounts(
		exported.Data.ClaudeAccounts,
		"claude",
		func(account crsClaudeAccount) (string, string) { return account.Kind, account.ID },
		seen,
	)
	if err != nil {
		return crsExportData{}, err
	}
	stableData.ClaudeConsoleAccounts, err = normalizeCRSExportAccounts(
		exported.Data.ClaudeConsoleAccounts,
		"claude_console",
		func(account crsConsoleAccount) (string, string) { return account.Kind, account.ID },
		seen,
	)
	if err != nil {
		return crsExportData{}, err
	}
	stableData.OpenAIOAuthAccounts, err = normalizeCRSExportAccounts(
		exported.Data.OpenAIOAuthAccounts,
		"openai_oauth",
		func(account crsOpenAIOAuthAccount) (string, string) { return account.Kind, account.ID },
		seen,
	)
	if err != nil {
		return crsExportData{}, err
	}
	stableData.OpenAIResponsesAccounts, err = normalizeCRSExportAccounts(
		exported.Data.OpenAIResponsesAccounts,
		"openai_responses",
		func(account crsOpenAIResponsesAccount) (string, string) { return account.Kind, account.ID },
		seen,
	)
	if err != nil {
		return crsExportData{}, err
	}
	stableData.GeminiOAuthAccounts, err = normalizeCRSExportAccounts(
		exported.Data.GeminiOAuthAccounts,
		"gemini_oauth",
		func(account crsGeminiOAuthAccount) (string, string) { return account.Kind, account.ID },
		seen,
	)
	if err != nil {
		return crsExportData{}, err
	}
	stableData.GeminiAPIKeyAccounts, err = normalizeCRSExportAccounts(
		exported.Data.GeminiAPIKeyAccounts,
		"gemini_apikey",
		func(account crsGeminiAPIKeyAccount) (string, string) { return account.Kind, account.ID },
		seen,
	)
	if err != nil {
		return crsExportData{}, err
	}
	return stableData, nil
}

func hashCRSExportAccounts(exported *crsExportResponse) (string, error) {
	stableData, err := normalizeCRSExportData(exported)
	if err != nil {
		return "", err
	}
	return hashCRSPreviewValue(crsExportHashDomain, stableData)
}

func hashesMatch(expected, actual string) bool {
	return hmac.Equal([]byte(expected), []byte(actual))
}

func boundedCRSSyncItemError(message string) string {
	message = logredact.RedactText(message)
	if len(message) <= crsSyncItemErrorMaxBytes {
		return message
	}
	end := crsSyncItemErrorMaxBytes - len("...")
	for end > 0 && (message[end]&0xc0) == 0x80 {
		end--
	}
	return message[:end] + "..."
}

func buildCRSSyncCapacityProbeError(seed string) string {
	var output strings.Builder
	output.Grow(crsSyncItemErrorMaxBytes)
	for counter := 0; output.Len() < crsSyncItemErrorMaxBytes; counter++ {
		hasher := sha256.New()
		_, _ = io.WriteString(hasher, crsCapacityProbeDomain)
		_, _ = hasher.Write([]byte{0})
		_, _ = io.WriteString(hasher, seed)
		_, _ = hasher.Write([]byte{0})
		_, _ = io.WriteString(hasher, strconv.Itoa(counter))
		output.WriteString(base64.RawURLEncoding.EncodeToString(hasher.Sum(nil)))
	}
	return output.String()[:crsSyncItemErrorMaxBytes]
}

func buildCRSSyncResponseCapacityProbe(exported *crsExportResponse) *SyncFromCRSResult {
	if exported == nil {
		return &SyncFromCRSResult{Items: make([]SyncFromCRSItemResult, 0)}
	}
	total := len(exported.Data.ClaudeAccounts) +
		len(exported.Data.ClaudeConsoleAccounts) +
		len(exported.Data.OpenAIOAuthAccounts) +
		len(exported.Data.OpenAIResponsesAccounts) +
		len(exported.Data.GeminiOAuthAccounts) +
		len(exported.Data.GeminiAPIKeyAccounts)
	probe := &SyncFromCRSResult{
		Created: total,
		Updated: total,
		Skipped: total,
		Failed:  total,
		Items:   make([]SyncFromCRSItemResult, 0, total),
	}
	appendItem := func(crsAccountID, kind, name string) {
		itemIndex := len(probe.Items)
		probe.Items = append(probe.Items, SyncFromCRSItemResult{
			CRSAccountID: crsAccountID,
			Kind:         kind,
			Name:         name,
			Action:       "updated",
			Error: buildCRSSyncCapacityProbeError(
				strconv.Itoa(itemIndex) + "\x00" + crsAccountID + "\x00" + kind + "\x00" + name,
			),
		})
	}
	for _, src := range exported.Data.ClaudeAccounts {
		appendItem(src.ID, src.Kind, src.Name)
	}
	for _, src := range exported.Data.ClaudeConsoleAccounts {
		appendItem(src.ID, src.Kind, src.Name)
	}
	for _, src := range exported.Data.OpenAIOAuthAccounts {
		appendItem(src.ID, src.Kind, src.Name)
	}
	for _, src := range exported.Data.OpenAIResponsesAccounts {
		appendItem(src.ID, src.Kind, src.Name)
	}
	for _, src := range exported.Data.GeminiOAuthAccounts {
		appendItem(src.ID, src.Kind, src.Name)
	}
	for _, src := range exported.Data.GeminiAPIKeyAccounts {
		appendItem(src.ID, src.Kind, src.Name)
	}
	return probe
}

func (s *CRSSyncService) crsPreviewSigningSecret() ([]byte, error) {
	if s.cfg == nil || strings.TrimSpace(s.cfg.JWT.Secret) == "" {
		return nil, ErrCRSPreviewSigningUnavailable
	}
	return []byte(s.cfg.JWT.Secret), nil
}

func (s *CRSSyncService) signCRSPreviewToken(payload crsPreviewTokenPayload) (string, error) {
	secret, err := s.crsPreviewSigningSecret()
	if err != nil {
		return "", err
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return "", ErrCRSPreviewSigningUnavailable.WithMetadata(map[string]string{
			"stage": "payload_encode",
		}).WithCause(err)
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(encodedPayload)
	mac := hmac.New(sha256.New, secret)
	_, _ = io.WriteString(mac, crsPreviewTokenDomain)
	_, _ = mac.Write([]byte{0})
	_, _ = io.WriteString(mac, payloadPart)
	signaturePart := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payloadPart + "." + signaturePart, nil
}

func (s *CRSSyncService) verifyCRSPreviewToken(rawToken string) (crsPreviewTokenPayload, error) {
	var payload crsPreviewTokenPayload
	token := strings.TrimSpace(rawToken)
	if token == "" {
		return payload, ErrCRSPreviewTokenRequired
	}
	if len(token) > crsPreviewTokenMaxLength {
		return payload, ErrCRSPreviewTokenInvalid
	}
	secret, err := s.crsPreviewSigningSecret()
	if err != nil {
		return payload, err
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return payload, ErrCRSPreviewTokenInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil ||
		len(signature) != sha256.Size ||
		base64.RawURLEncoding.EncodeToString(signature) != parts[1] {
		return payload, ErrCRSPreviewTokenInvalid
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = io.WriteString(mac, crsPreviewTokenDomain)
	_, _ = mac.Write([]byte{0})
	_, _ = io.WriteString(mac, parts[0])
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return payload, ErrCRSPreviewTokenInvalid
	}
	encodedPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || base64.RawURLEncoding.EncodeToString(encodedPayload) != parts[0] {
		return payload, ErrCRSPreviewTokenInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(encodedPayload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return crsPreviewTokenPayload{}, ErrCRSPreviewTokenInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return crsPreviewTokenPayload{}, ErrCRSPreviewTokenInvalid
	}
	if payload.Version != crsPreviewTokenVersion ||
		payload.ActorAdminID <= 0 ||
		payload.ConnectionHash == "" ||
		payload.ExportHash == "" ||
		payload.LocalSnapshotHash == "" ||
		payload.ExpiresAt <= 0 {
		return crsPreviewTokenPayload{}, ErrCRSPreviewTokenInvalid
	}
	return payload, nil
}

func (s *CRSSyncService) loadValidatedCRSPreviewSnapshots(
	ctx context.Context,
) ([]CRSAccountPreviewSnapshot, map[string]CRSAccountPreviewSnapshot, error) {
	snapshotRepo, ok := s.accountRepo.(CRSPreviewSnapshotRepository)
	if !ok || snapshotRepo == nil {
		return nil, nil, ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
			"stage": "repository_capability",
		})
	}
	localSnapshots, err := snapshotRepo.ListCRSAccountPreviewSnapshots(ctx)
	if err != nil {
		return nil, nil, ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
			"stage": "repository_snapshot",
		}).WithCause(err)
	}
	snapshots := append([]CRSAccountPreviewSnapshot(nil), localSnapshots...)
	for index := range snapshots {
		snapshot := &snapshots[index]
		if strings.TrimSpace(snapshot.CRSAccountID) == "" || snapshot.LocalAccountID <= 0 {
			return nil, nil, ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
				"stage": "invalid_account_snapshot",
			})
		}
		snapshot.RoomBindings = append(
			[]CRSAccountRoomBindingSnapshot(nil),
			snapshot.RoomBindings...,
		)
		sort.Slice(snapshot.RoomBindings, func(i, j int) bool {
			if snapshot.RoomBindings[i].ListingID == snapshot.RoomBindings[j].ListingID {
				return snapshot.RoomBindings[i].RowVersion < snapshot.RoomBindings[j].RowVersion
			}
			return snapshot.RoomBindings[i].ListingID < snapshot.RoomBindings[j].ListingID
		})
		for bindingIndex, binding := range snapshot.RoomBindings {
			if binding.ListingID <= 0 ||
				binding.RowVersion <= 0 ||
				(bindingIndex > 0 &&
					snapshot.RoomBindings[bindingIndex-1].ListingID == binding.ListingID) {
				return nil, nil, ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
					"account_id": strconv.FormatInt(snapshot.LocalAccountID, 10),
					"stage":      "invalid_room_snapshot",
				})
			}
		}
	}
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].CRSAccountID == snapshots[j].CRSAccountID {
			return snapshots[i].LocalAccountID < snapshots[j].LocalAccountID
		}
		return snapshots[i].CRSAccountID < snapshots[j].CRSAccountID
	})
	existingByCRSID := make(map[string]CRSAccountPreviewSnapshot, len(snapshots))
	localAccountIDs := make(map[int64]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		if _, exists := existingByCRSID[snapshot.CRSAccountID]; exists {
			return nil, nil, ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
				"crs_account_id": snapshot.CRSAccountID,
				"stage":          "duplicate_crs_account_id",
			})
		}
		if _, exists := localAccountIDs[snapshot.LocalAccountID]; exists {
			return nil, nil, ErrCRSPreviewSnapshotUnavailable.WithMetadata(map[string]string{
				"account_id": strconv.FormatInt(snapshot.LocalAccountID, 10),
				"stage":      "duplicate_local_account_id",
			})
		}
		existingByCRSID[snapshot.CRSAccountID] = snapshot
		localAccountIDs[snapshot.LocalAccountID] = struct{}{}
	}
	return snapshots, existingByCRSID, nil
}

func (s *CRSSyncService) SyncFromCRS(ctx context.Context, input SyncFromCRSInput) (*SyncFromCRSResult, error) {
	connection, err := s.normalizeCRSConnection(input.BaseURL, input.Username, input.Password)
	if err != nil {
		return nil, err
	}
	tokenPayload, err := s.verifyCRSPreviewToken(input.PreviewToken)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if now.Unix() >= tokenPayload.ExpiresAt {
		return nil, ErrCRSPreviewTokenExpired
	}
	if input.ActorAdminID <= 0 || tokenPayload.ActorAdminID != input.ActorAdminID {
		return nil, ErrCRSPreviewContextConflict.WithMetadata(map[string]string{
			"stage": "actor",
		})
	}
	connectionHash, err := hashCRSConnection(connection)
	if err != nil {
		return nil, ErrCRSPreviewSigningUnavailable.WithMetadata(map[string]string{
			"stage": "connection_hash",
		}).WithCause(err)
	}
	if !hashesMatch(tokenPayload.ConnectionHash, connectionHash) {
		return nil, ErrCRSPreviewContextConflict.WithMetadata(map[string]string{
			"stage": "connection",
		})
	}
	exported, err := s.fetchCRSExport(ctx, connection)
	if err != nil {
		return nil, err
	}
	localSnapshots, _, err := s.loadValidatedCRSPreviewSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	exportHash, err := hashCRSExportAccounts(exported)
	if err != nil {
		if errors.Is(err, ErrCRSExportInvalid) {
			return nil, err
		}
		return nil, ErrCRSPreviewSigningUnavailable.WithMetadata(map[string]string{
			"stage": "export_hash",
		}).WithCause(err)
	}
	if !hashesMatch(tokenPayload.ExportHash, exportHash) {
		return nil, ErrCRSPreviewContextConflict.WithMetadata(map[string]string{
			"stage": "remote_export",
		})
	}
	localSnapshotHash, err := hashCRSPreviewValue(crsLocalSnapshotHashDomain, localSnapshots)
	if err != nil {
		return nil, ErrCRSPreviewSigningUnavailable.WithMetadata(map[string]string{
			"stage": "local_snapshot_hash",
		}).WithCause(err)
	}
	if !hashesMatch(tokenPayload.LocalSnapshotHash, localSnapshotHash) {
		return nil, ErrCRSPreviewContextConflict.WithMetadata(map[string]string{
			"stage": "local_snapshot",
		})
	}
	if input.ValidateResponseCapacity != nil {
		if err := input.ValidateResponseCapacity(buildCRSSyncResponseCapacityProbe(exported)); err != nil {
			return nil, err
		}
	}

	syncedAt := now.Format(time.RFC3339)

	result := &SyncFromCRSResult{
		Items: make(
			[]SyncFromCRSItemResult,
			0,
			len(exported.Data.ClaudeAccounts)+len(exported.Data.ClaudeConsoleAccounts)+len(exported.Data.OpenAIOAuthAccounts)+len(exported.Data.OpenAIResponsesAccounts)+len(exported.Data.GeminiOAuthAccounts)+len(exported.Data.GeminiAPIKeyAccounts),
		),
	}

	selectedSet := buildSelectedSet(input.SelectedAccountIDs)

	var proxies []Proxy
	if input.SyncProxies {
		if s.proxyRepo == nil {
			return nil, errors.New("proxy repository is not available")
		}
		proxies, err = s.proxyRepo.ListActive(ctx)
		if err != nil {
			return nil, fmt.Errorf("list active proxies failed: %w", err)
		}
	}

	// Claude OAuth / Setup Token -> sub2api anthropic oauth/setup-token
	for _, src := range exported.Data.ClaudeAccounts {
		item := SyncFromCRSItemResult{
			CRSAccountID: src.ID,
			Kind:         src.Kind,
			Name:         src.Name,
		}

		targetType := strings.TrimSpace(src.AuthType)
		if targetType == "" {
			targetType = "oauth"
		}
		if targetType != AccountTypeOAuth && targetType != AccountTypeSetupToken {
			item.Action = "skipped"
			item.Error = boundedCRSSyncItemError("unsupported authType: " + targetType)
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}

		accessToken, _ := src.Credentials["access_token"].(string)
		if strings.TrimSpace(accessToken) == "" {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("missing access_token")
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		credentials := sanitizeCredentialsMap(src.Credentials)
		// 🔧 Remove /v1 suffix from base_url for Claude accounts
		cleanBaseURL(credentials, "/v1")
		// 🔧 Convert expires_at from ISO string to Unix timestamp
		if expiresAtStr, ok := credentials["expires_at"].(string); ok && expiresAtStr != "" {
			if t, err := time.Parse(time.RFC3339, expiresAtStr); err == nil {
				credentials["expires_at"] = t.Unix()
			}
		}
		// 🔧 Add intercept_warmup_requests if not present (defaults to false)
		if _, exists := credentials["intercept_warmup_requests"]; !exists {
			credentials["intercept_warmup_requests"] = false
		}
		priority := clampPriority(src.Priority)
		concurrency := 3
		status := mapCRSStatus(src.IsActive, src.Status)

		// 🔧 Preserve all CRS extra fields and add sync metadata
		extra := make(map[string]any)
		if src.Extra != nil {
			for k, v := range src.Extra {
				extra[k] = v
			}
		}
		extra["crs_account_id"] = src.ID
		extra["crs_kind"] = src.Kind
		extra["crs_synced_at"] = syncedAt
		// Extract org_uuid and account_uuid from CRS credentials to extra
		if orgUUID, ok := src.Credentials["org_uuid"]; ok {
			extra["org_uuid"] = orgUUID
		}
		if accountUUID, ok := src.Credentials["account_uuid"]; ok {
			extra["account_uuid"] = accountUUID
		}

		existing, err := s.accountRepo.GetByCRSAccountID(ctx, src.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("db lookup failed: " + err.Error())
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		proxyPlan := planCRSProxy(
			input.SyncProxies,
			proxies,
			src.Proxy,
			fmt.Sprintf("crs-%s", src.Name),
		)

		if existing == nil {
			if !shouldCreateAccount(src.ID, selectedSet) {
				item.Action = "skipped"
				item.Error = boundedCRSSyncItemError("not selected")
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			proxyID, err := s.resolveCRSProxyPlan(ctx, &proxies, proxyPlan)
			if err != nil {
				item.Action = "failed"
				item.Error = boundedCRSSyncItemError("proxy sync failed: " + err.Error())
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			account := &Account{
				Name:        defaultName(src.Name, src.ID),
				Platform:    PlatformAnthropic,
				Type:        targetType,
				Credentials: credentials,
				Extra:       extra,
				ProxyID:     proxyID,
				Concurrency: concurrency,
				Priority:    priority,
				Status:      status,
				Schedulable: src.Schedulable,
			}
			if err := s.accountRepo.Create(ctx, account); err != nil {
				item.Action = "failed"
				item.Error = boundedCRSSyncItemError("create failed: " + err.Error())
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			// 🔄 Refresh OAuth token after creation
			if targetType == AccountTypeOAuth {
				if refreshedCreds := s.refreshOAuthToken(ctx, account); refreshedCreds != nil {
					_ = persistAccountCredentials(ctx, s.accountRepo, account, refreshedCreds)
				}
			}
			item.Action = "created"
			result.Created++
			result.Items = append(result.Items, item)
			continue
		}

		// Update existing
		existing.Extra = mergeMap(existing.Extra, extra)
		existing.Name = defaultName(src.Name, src.ID)
		existing.Platform = PlatformAnthropic
		existing.Type = targetType
		existing.Credentials = mergeMap(existing.Credentials, credentials)
		existing.Concurrency = concurrency
		existing.Priority = priority
		existing.Status = status
		existing.Schedulable = src.Schedulable

		if err := s.updateExistingAccountWithProxy(ctx, input, existing, &proxies, proxyPlan); err != nil {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("update failed: " + err.Error())
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		// 🔄 Refresh OAuth token after update
		if targetType == AccountTypeOAuth {
			if refreshedCreds := s.refreshOAuthToken(ctx, existing); refreshedCreds != nil {
				_ = persistAccountCredentials(ctx, s.accountRepo, existing, refreshedCreds)
			}
		}

		item.Action = "updated"
		result.Updated++
		result.Items = append(result.Items, item)
	}

	// Claude Console API Key -> sub2api anthropic apikey
	for _, src := range exported.Data.ClaudeConsoleAccounts {
		item := SyncFromCRSItemResult{
			CRSAccountID: src.ID,
			Kind:         src.Kind,
			Name:         src.Name,
		}

		apiKey, _ := src.Credentials["api_key"].(string)
		if strings.TrimSpace(apiKey) == "" {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("missing api_key")
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		credentials := sanitizeCredentialsMap(src.Credentials)
		priority := clampPriority(src.Priority)
		concurrency := 3
		if src.MaxConcurrentTasks > 0 {
			concurrency = src.MaxConcurrentTasks
		}
		status := mapCRSStatus(src.IsActive, src.Status)

		extra := map[string]any{
			"crs_account_id": src.ID,
			"crs_kind":       src.Kind,
			"crs_synced_at":  syncedAt,
		}

		existing, err := s.accountRepo.GetByCRSAccountID(ctx, src.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("db lookup failed: " + err.Error())
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		proxyPlan := planCRSProxy(
			input.SyncProxies,
			proxies,
			src.Proxy,
			fmt.Sprintf("crs-%s", src.Name),
		)

		if existing == nil {
			if !shouldCreateAccount(src.ID, selectedSet) {
				item.Action = "skipped"
				item.Error = boundedCRSSyncItemError("not selected")
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			proxyID, err := s.resolveCRSProxyPlan(ctx, &proxies, proxyPlan)
			if err != nil {
				item.Action = "failed"
				item.Error = boundedCRSSyncItemError("proxy sync failed: " + err.Error())
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			account := &Account{
				Name:        defaultName(src.Name, src.ID),
				Platform:    PlatformAnthropic,
				Type:        AccountTypeAPIKey,
				Credentials: credentials,
				Extra:       extra,
				ProxyID:     proxyID,
				Concurrency: concurrency,
				Priority:    priority,
				Status:      status,
				Schedulable: src.Schedulable,
			}
			if err := s.accountRepo.Create(ctx, account); err != nil {
				item.Action = "failed"
				item.Error = boundedCRSSyncItemError("create failed: " + err.Error())
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			item.Action = "created"
			result.Created++
			result.Items = append(result.Items, item)
			continue
		}

		existing.Extra = mergeMap(existing.Extra, extra)
		existing.Name = defaultName(src.Name, src.ID)
		existing.Platform = PlatformAnthropic
		existing.Type = AccountTypeAPIKey
		existing.Credentials = mergeMap(existing.Credentials, credentials)
		existing.Concurrency = concurrency
		existing.Priority = priority
		existing.Status = status
		existing.Schedulable = src.Schedulable

		if err := s.updateExistingAccountWithProxy(ctx, input, existing, &proxies, proxyPlan); err != nil {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("update failed: " + err.Error())
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		item.Action = "updated"
		result.Updated++
		result.Items = append(result.Items, item)
	}

	// OpenAI OAuth -> sub2api openai oauth
	for _, src := range exported.Data.OpenAIOAuthAccounts {
		item := SyncFromCRSItemResult{
			CRSAccountID: src.ID,
			Kind:         src.Kind,
			Name:         src.Name,
		}

		accessToken, _ := src.Credentials["access_token"].(string)
		if strings.TrimSpace(accessToken) == "" {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("missing access_token")
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		credentials := sanitizeCredentialsMap(src.Credentials)
		// Normalize token_type
		if v, ok := credentials["token_type"].(string); !ok || strings.TrimSpace(v) == "" {
			credentials["token_type"] = "Bearer"
		}
		// 🔧 Convert expires_at from ISO string to Unix timestamp
		if expiresAtStr, ok := credentials["expires_at"].(string); ok && expiresAtStr != "" {
			if t, err := time.Parse(time.RFC3339, expiresAtStr); err == nil {
				credentials["expires_at"] = t.Unix()
			}
		}
		priority := clampPriority(src.Priority)
		concurrency := 3
		status := mapCRSStatus(src.IsActive, src.Status)

		// 🔧 Preserve all CRS extra fields and add sync metadata
		extra := make(map[string]any)
		if src.Extra != nil {
			for k, v := range src.Extra {
				extra[k] = v
			}
		}
		extra["crs_account_id"] = src.ID
		extra["crs_kind"] = src.Kind
		extra["crs_synced_at"] = syncedAt
		// Extract email from CRS extra (crs_email -> email)
		if crsEmail, ok := src.Extra["crs_email"]; ok {
			extra["email"] = crsEmail
		}

		existing, err := s.accountRepo.GetByCRSAccountID(ctx, src.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("db lookup failed: " + err.Error())
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		proxyPlan := planCRSProxy(
			input.SyncProxies,
			proxies,
			src.Proxy,
			fmt.Sprintf("crs-%s", src.Name),
		)

		if existing == nil {
			if !shouldCreateAccount(src.ID, selectedSet) {
				item.Action = "skipped"
				item.Error = boundedCRSSyncItemError("not selected")
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			proxyID, err := s.resolveCRSProxyPlan(ctx, &proxies, proxyPlan)
			if err != nil {
				item.Action = "failed"
				item.Error = boundedCRSSyncItemError("proxy sync failed: " + err.Error())
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			account := &Account{
				Name:        defaultName(src.Name, src.ID),
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Credentials: credentials,
				Extra:       extra,
				ProxyID:     proxyID,
				Concurrency: concurrency,
				Priority:    priority,
				Status:      status,
				Schedulable: src.Schedulable,
			}
			if err := s.accountRepo.Create(ctx, account); err != nil {
				item.Action = "failed"
				item.Error = boundedCRSSyncItemError("create failed: " + err.Error())
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			// 🔄 Refresh OAuth token after creation
			if refreshedCreds := s.refreshOAuthToken(ctx, account); refreshedCreds != nil {
				_ = persistAccountCredentials(ctx, s.accountRepo, account, refreshedCreds)
			}
			item.Action = "created"
			result.Created++
			result.Items = append(result.Items, item)
			continue
		}

		existing.Extra = mergeMap(existing.Extra, extra)
		existing.Name = defaultName(src.Name, src.ID)
		existing.Platform = PlatformOpenAI
		existing.Type = AccountTypeOAuth
		existing.Credentials = mergeMap(existing.Credentials, credentials)
		existing.Concurrency = concurrency
		existing.Priority = priority
		existing.Status = status
		existing.Schedulable = src.Schedulable

		if err := s.updateExistingAccountWithProxy(ctx, input, existing, &proxies, proxyPlan); err != nil {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("update failed: " + err.Error())
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		// 🔄 Refresh OAuth token after update
		if refreshedCreds := s.refreshOAuthToken(ctx, existing); refreshedCreds != nil {
			_ = persistAccountCredentials(ctx, s.accountRepo, existing, refreshedCreds)
		}

		item.Action = "updated"
		result.Updated++
		result.Items = append(result.Items, item)
	}

	// OpenAI Responses API Key -> sub2api openai apikey
	for _, src := range exported.Data.OpenAIResponsesAccounts {
		item := SyncFromCRSItemResult{
			CRSAccountID: src.ID,
			Kind:         src.Kind,
			Name:         src.Name,
		}

		apiKey, _ := src.Credentials["api_key"].(string)
		if strings.TrimSpace(apiKey) == "" {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("missing api_key")
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		if baseURL, ok := src.Credentials["base_url"].(string); !ok || strings.TrimSpace(baseURL) == "" {
			src.Credentials["base_url"] = "https://api.openai.com"
		}
		// 🔧 Remove /v1 suffix from base_url for OpenAI accounts
		cleanBaseURL(src.Credentials, "/v1")

		credentials := sanitizeCredentialsMap(src.Credentials)
		priority := clampPriority(src.Priority)
		concurrency := 3
		status := mapCRSStatus(src.IsActive, src.Status)

		extra := map[string]any{
			"crs_account_id": src.ID,
			"crs_kind":       src.Kind,
			"crs_synced_at":  syncedAt,
		}

		existing, err := s.accountRepo.GetByCRSAccountID(ctx, src.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("db lookup failed: " + err.Error())
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		proxyPlan := planCRSProxy(
			input.SyncProxies,
			proxies,
			src.Proxy,
			fmt.Sprintf("crs-%s", src.Name),
		)

		if existing == nil {
			if !shouldCreateAccount(src.ID, selectedSet) {
				item.Action = "skipped"
				item.Error = boundedCRSSyncItemError("not selected")
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			proxyID, err := s.resolveCRSProxyPlan(ctx, &proxies, proxyPlan)
			if err != nil {
				item.Action = "failed"
				item.Error = boundedCRSSyncItemError("proxy sync failed: " + err.Error())
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			account := &Account{
				Name:        defaultName(src.Name, src.ID),
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Credentials: credentials,
				Extra:       extra,
				ProxyID:     proxyID,
				Concurrency: concurrency,
				Priority:    priority,
				Status:      status,
				Schedulable: src.Schedulable,
			}
			if err := s.accountRepo.Create(ctx, account); err != nil {
				item.Action = "failed"
				item.Error = boundedCRSSyncItemError("create failed: " + err.Error())
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			item.Action = "created"
			result.Created++
			result.Items = append(result.Items, item)
			continue
		}

		existing.Extra = mergeMap(existing.Extra, extra)
		existing.Name = defaultName(src.Name, src.ID)
		existing.Platform = PlatformOpenAI
		existing.Type = AccountTypeAPIKey
		existing.Credentials = mergeMap(existing.Credentials, credentials)
		existing.Concurrency = concurrency
		existing.Priority = priority
		existing.Status = status
		existing.Schedulable = src.Schedulable

		if err := s.updateExistingAccountWithProxy(ctx, input, existing, &proxies, proxyPlan); err != nil {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("update failed: " + err.Error())
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		item.Action = "updated"
		result.Updated++
		result.Items = append(result.Items, item)
	}

	// Gemini OAuth -> sub2api gemini oauth
	for _, src := range exported.Data.GeminiOAuthAccounts {
		item := SyncFromCRSItemResult{
			CRSAccountID: src.ID,
			Kind:         src.Kind,
			Name:         src.Name,
		}

		refreshToken, _ := src.Credentials["refresh_token"].(string)
		if strings.TrimSpace(refreshToken) == "" {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("missing refresh_token")
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		credentials := sanitizeCredentialsMap(src.Credentials)
		if v, ok := credentials["token_type"].(string); !ok || strings.TrimSpace(v) == "" {
			credentials["token_type"] = "Bearer"
		}
		// Convert expires_at from RFC3339 to Unix seconds string (recommended to keep consistent with GetCredential())
		if expiresAtStr, ok := credentials["expires_at"].(string); ok && strings.TrimSpace(expiresAtStr) != "" {
			if t, err := time.Parse(time.RFC3339, expiresAtStr); err == nil {
				credentials["expires_at"] = strconv.FormatInt(t.Unix(), 10)
			}
		}

		extra := make(map[string]any)
		if src.Extra != nil {
			for k, v := range src.Extra {
				extra[k] = v
			}
		}
		extra["crs_account_id"] = src.ID
		extra["crs_kind"] = src.Kind
		extra["crs_synced_at"] = syncedAt

		existing, err := s.accountRepo.GetByCRSAccountID(ctx, src.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("db lookup failed: " + err.Error())
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		proxyPlan := planCRSProxy(
			input.SyncProxies,
			proxies,
			src.Proxy,
			fmt.Sprintf("crs-%s", src.Name),
		)

		if existing == nil {
			if !shouldCreateAccount(src.ID, selectedSet) {
				item.Action = "skipped"
				item.Error = boundedCRSSyncItemError("not selected")
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			proxyID, err := s.resolveCRSProxyPlan(ctx, &proxies, proxyPlan)
			if err != nil {
				item.Action = "failed"
				item.Error = boundedCRSSyncItemError("proxy sync failed: " + err.Error())
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			account := &Account{
				Name:        defaultName(src.Name, src.ID),
				Platform:    PlatformGemini,
				Type:        AccountTypeOAuth,
				Credentials: credentials,
				Extra:       extra,
				ProxyID:     proxyID,
				Concurrency: 3,
				Priority:    clampPriority(src.Priority),
				Status:      mapCRSStatus(src.IsActive, src.Status),
				Schedulable: src.Schedulable,
			}
			if err := s.accountRepo.Create(ctx, account); err != nil {
				item.Action = "failed"
				item.Error = boundedCRSSyncItemError("create failed: " + err.Error())
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			if refreshedCreds := s.refreshOAuthToken(ctx, account); refreshedCreds != nil {
				_ = persistAccountCredentials(ctx, s.accountRepo, account, refreshedCreds)
			}
			item.Action = "created"
			result.Created++
			result.Items = append(result.Items, item)
			continue
		}

		existing.Extra = mergeMap(existing.Extra, extra)
		existing.Name = defaultName(src.Name, src.ID)
		existing.Platform = PlatformGemini
		existing.Type = AccountTypeOAuth
		existing.Credentials = mergeMap(existing.Credentials, credentials)
		existing.Concurrency = 3
		existing.Priority = clampPriority(src.Priority)
		existing.Status = mapCRSStatus(src.IsActive, src.Status)
		existing.Schedulable = src.Schedulable

		if err := s.updateExistingAccountWithProxy(ctx, input, existing, &proxies, proxyPlan); err != nil {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("update failed: " + err.Error())
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		if refreshedCreds := s.refreshOAuthToken(ctx, existing); refreshedCreds != nil {
			_ = persistAccountCredentials(ctx, s.accountRepo, existing, refreshedCreds)
		}

		item.Action = "updated"
		result.Updated++
		result.Items = append(result.Items, item)
	}

	// Gemini API Key -> sub2api gemini apikey
	for _, src := range exported.Data.GeminiAPIKeyAccounts {
		item := SyncFromCRSItemResult{
			CRSAccountID: src.ID,
			Kind:         src.Kind,
			Name:         src.Name,
		}

		apiKey, _ := src.Credentials["api_key"].(string)
		if strings.TrimSpace(apiKey) == "" {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("missing api_key")
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		credentials := sanitizeCredentialsMap(src.Credentials)
		if baseURL, ok := credentials["base_url"].(string); !ok || strings.TrimSpace(baseURL) == "" {
			credentials["base_url"] = "https://generativelanguage.googleapis.com"
		}

		extra := make(map[string]any)
		if src.Extra != nil {
			for k, v := range src.Extra {
				extra[k] = v
			}
		}
		extra["crs_account_id"] = src.ID
		extra["crs_kind"] = src.Kind
		extra["crs_synced_at"] = syncedAt

		existing, err := s.accountRepo.GetByCRSAccountID(ctx, src.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("db lookup failed: " + err.Error())
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		proxyPlan := planCRSProxy(
			input.SyncProxies,
			proxies,
			src.Proxy,
			fmt.Sprintf("crs-%s", src.Name),
		)

		if existing == nil {
			if !shouldCreateAccount(src.ID, selectedSet) {
				item.Action = "skipped"
				item.Error = boundedCRSSyncItemError("not selected")
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			proxyID, err := s.resolveCRSProxyPlan(ctx, &proxies, proxyPlan)
			if err != nil {
				item.Action = "failed"
				item.Error = boundedCRSSyncItemError("proxy sync failed: " + err.Error())
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			account := &Account{
				Name:        defaultName(src.Name, src.ID),
				Platform:    PlatformGemini,
				Type:        AccountTypeAPIKey,
				Credentials: credentials,
				Extra:       extra,
				ProxyID:     proxyID,
				Concurrency: 3,
				Priority:    clampPriority(src.Priority),
				Status:      mapCRSStatus(src.IsActive, src.Status),
				Schedulable: src.Schedulable,
			}
			if err := s.accountRepo.Create(ctx, account); err != nil {
				item.Action = "failed"
				item.Error = boundedCRSSyncItemError("create failed: " + err.Error())
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			item.Action = "created"
			result.Created++
			result.Items = append(result.Items, item)
			continue
		}

		existing.Extra = mergeMap(existing.Extra, extra)
		existing.Name = defaultName(src.Name, src.ID)
		existing.Platform = PlatformGemini
		existing.Type = AccountTypeAPIKey
		existing.Credentials = mergeMap(existing.Credentials, credentials)
		existing.Concurrency = 3
		existing.Priority = clampPriority(src.Priority)
		existing.Status = mapCRSStatus(src.IsActive, src.Status)
		existing.Schedulable = src.Schedulable

		if err := s.updateExistingAccountWithProxy(ctx, input, existing, &proxies, proxyPlan); err != nil {
			item.Action = "failed"
			item.Error = boundedCRSSyncItemError("update failed: " + err.Error())
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		item.Action = "updated"
		result.Updated++
		result.Items = append(result.Items, item)
	}

	return result, nil
}

func (s *CRSSyncService) updateExistingAccount(ctx context.Context, input SyncFromCRSInput, account *Account) error {
	return s.updateExistingAccountWithProxy(ctx, input, account, nil, nil)
}

func (s *CRSSyncService) updateExistingAccountWithProxy(
	ctx context.Context,
	input SyncFromCRSInput,
	account *Account,
	cachedProxies *[]Proxy,
	proxyPlan *crsProxyPlan,
) error {
	if account == nil {
		return ErrAccountNilInput
	}
	guardSnapshot := account
	if proxyPlan != nil {
		snapshot := *account
		if proxyPlan.resolvedID != nil {
			proxyID := *proxyPlan.resolvedID
			snapshot.ProxyID = &proxyID
		} else {
			pendingProxyID := crsUnknownProxyIDForGuard
			snapshot.ProxyID = &pendingProxyID
		}
		guardSnapshot = &snapshot
	}
	request := AccountMutationGuardRequest{
		Targets: []AccountMutationGuardTarget{{
			AccountID:         account.ID,
			ExpectedUpdatedAt: account.UpdatedAt,
			After:             guardSnapshot,
			GroupIDs:          append([]int64(nil), account.GroupIDs...),
		}},
		ActorUserID:             input.ActorAdminID,
		ActorIsAdmin:            input.ActorAdminID > 0,
		Intent:                  AccountMutationIntentAdmin,
		ForceActiveEdit:         input.ForceActiveEdit,
		Confirmed:               input.Confirmed,
		Reason:                  input.Reason,
		ExpectedListingVersion:  input.ExpectedVersion,
		ExpectedListingVersions: input.ExpectedVersions,
		OperationID:             input.OperationID,
	}
	mutate := func(mutationCtx context.Context) error {
		if proxyPlan != nil {
			proxyID, err := s.resolveCRSProxyPlan(mutationCtx, cachedProxies, proxyPlan)
			if err != nil {
				return fmt.Errorf("proxy sync failed: %w", err)
			}
			if proxyID != nil {
				account.ProxyID = proxyID
			}
		}
		return s.accountRepo.Update(mutationCtx, account)
	}
	if repo, ok := s.accountRepo.(AccountMutationGuardRepository); ok && repo != nil {
		return repo.WithAccountMutationGuard(ctx, request, mutate)
	}
	if account.AccountShareModeListingID != nil ||
		(account.ExternalPlacement != nil && account.ExternalPlacement.Target == AccountExternalPlacementRoom) {
		return ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{
			"account_id": strconv.FormatInt(account.ID, 10),
		})
	}
	// Lightweight test/legacy repositories cannot contain the SQL room
	// projection. Production accountRepository always implements the guard.
	return mutate(ctx)
}

func mergeMap(existing map[string]any, updates map[string]any) map[string]any {
	out := make(map[string]any, len(existing)+len(updates))
	for k, v := range existing {
		out[k] = v
	}
	for k, v := range updates {
		out[k] = v
	}
	return out
}

func planCRSProxy(enabled bool, cached []Proxy, src *crsProxy, defaultName string) *crsProxyPlan {
	if !enabled || src == nil {
		return nil
	}
	protocol := strings.ToLower(strings.TrimSpace(src.Protocol))
	switch protocol {
	case "socks":
		protocol = "socks5"
	case "socks5h":
		protocol = "socks5"
	}
	host := strings.TrimSpace(src.Host)
	port := src.Port
	username := strings.TrimSpace(src.Username)
	password := strings.TrimSpace(src.Password)

	if protocol == "" || host == "" || port <= 0 {
		return nil
	}
	if protocol != "http" && protocol != "https" && protocol != "socks5" {
		return nil
	}

	// Find existing proxy (active only).
	for _, p := range cached {
		if strings.EqualFold(p.Protocol, protocol) &&
			p.Host == host &&
			p.Port == port &&
			p.Username == username &&
			p.Password == password {
			id := p.ID
			return &crsProxyPlan{resolvedID: &id}
		}
	}

	return &crsProxyPlan{
		pending: &Proxy{
			Name:     defaultProxyName(defaultName, protocol, host, port),
			Protocol: protocol,
			Host:     host,
			Port:     port,
			Username: username,
			Password: password,
			Status:   StatusActive,
		},
	}
}

func (s *CRSSyncService) resolveCRSProxyPlan(
	ctx context.Context,
	cached *[]Proxy,
	plan *crsProxyPlan,
) (*int64, error) {
	if plan == nil {
		return nil, nil
	}
	if plan.resolvedID != nil {
		id := *plan.resolvedID
		return &id, nil
	}
	if s.proxyRepo == nil || cached == nil || plan.pending == nil {
		return nil, errors.New("proxy repository is not available")
	}
	if err := s.proxyRepo.Create(ctx, plan.pending); err != nil {
		return nil, err
	}
	*cached = append(*cached, *plan.pending)
	id := plan.pending.ID
	plan.resolvedID = &id
	plan.pending = nil
	return &id, nil
}

func defaultProxyName(base, protocol, host string, port int) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "crs"
	}
	return fmt.Sprintf("%s (%s://%s:%d)", base, protocol, host, port)
}

func defaultName(name, id string) string {
	if strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return "CRS " + id
}

func clampPriority(priority int) int {
	if priority < 1 || priority > 100 {
		return 50
	}
	return priority
}

func sanitizeCredentialsMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		// Avoid nil values to keep JSONB cleaner
		if v != nil {
			out[k] = v
		}
	}
	return out
}

func mapCRSStatus(isActive bool, status string) string {
	if !isActive {
		return "inactive"
	}
	if strings.EqualFold(strings.TrimSpace(status), "error") {
		return "error"
	}
	return "active"
}

func normalizeBaseURL(raw string, allowlist []string, allowPrivate bool) (string, error) {
	// 当 allowlist 为空时，不强制要求白名单（只进行基本的 URL 和 SSRF 验证）
	requireAllowlist := len(allowlist) > 0
	normalized, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     allowlist,
		RequireAllowlist: requireAllowlist,
		AllowPrivate:     allowPrivate,
	})
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	return normalized, nil
}

// cleanBaseURL removes trailing suffix from base_url in credentials
// Used for both Claude and OpenAI accounts to remove /v1
func cleanBaseURL(credentials map[string]any, suffixToRemove string) {
	if baseURL, ok := credentials["base_url"].(string); ok && baseURL != "" {
		trimmed := strings.TrimSpace(baseURL)
		if strings.HasSuffix(trimmed, suffixToRemove) {
			credentials["base_url"] = strings.TrimSuffix(trimmed, suffixToRemove)
		}
	}
}

func crsLogin(ctx context.Context, client *http.Client, baseURL, username, password string) (string, error) {
	payload := map[string]any{
		"username": username,
		"password": password,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/web/auth/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("crs login failed: status=%d body=%s", resp.StatusCode, string(raw))
	}

	var parsed crsLoginResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("crs login parse failed: %w", err)
	}
	if !parsed.Success || strings.TrimSpace(parsed.Token) == "" {
		msg := parsed.Message
		if msg == "" {
			msg = parsed.Error
		}
		if msg == "" {
			msg = "unknown error"
		}
		return "", errors.New("crs login failed: " + msg)
	}
	return parsed.Token, nil
}

func crsExportAccounts(ctx context.Context, client *http.Client, baseURL, adminToken string) (*crsExportResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/admin/sync/export-accounts?include_secrets=true", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+adminToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("crs export failed: status=%d body=%s", resp.StatusCode, string(raw))
	}

	var parsed crsExportResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("crs export parse failed: %w", err)
	}
	if !parsed.Success {
		msg := parsed.Message
		if msg == "" {
			msg = parsed.Error
		}
		if msg == "" {
			msg = "unknown error"
		}
		return nil, errors.New("crs export failed: " + msg)
	}
	return &parsed, nil
}

// refreshOAuthToken attempts to refresh OAuth token for a synced account
// Returns updated credentials or nil if refresh failed/not applicable
func (s *CRSSyncService) refreshOAuthToken(ctx context.Context, account *Account) map[string]any {
	if account.Type != AccountTypeOAuth {
		return nil
	}

	var newCredentials map[string]any
	var err error

	switch account.Platform {
	case PlatformAnthropic:
		if s.oauthService == nil {
			return nil
		}
		tokenInfo, refreshErr := s.oauthService.RefreshAccountToken(ctx, account)
		if refreshErr != nil {
			err = refreshErr
		} else {
			// Preserve existing credentials
			newCredentials = make(map[string]any)
			for k, v := range account.Credentials {
				newCredentials[k] = v
			}
			// Update token fields
			newCredentials["access_token"] = tokenInfo.AccessToken
			newCredentials["token_type"] = tokenInfo.TokenType
			newCredentials["expires_in"] = tokenInfo.ExpiresIn
			newCredentials["expires_at"] = tokenInfo.ExpiresAt
			if tokenInfo.RefreshToken != "" {
				newCredentials["refresh_token"] = tokenInfo.RefreshToken
			}
			if tokenInfo.Scope != "" {
				newCredentials["scope"] = tokenInfo.Scope
			}
		}
	case PlatformOpenAI:
		if s.openaiOAuthService == nil {
			return nil
		}
		tokenInfo, refreshErr := s.openaiOAuthService.RefreshAccountToken(ctx, account)
		if refreshErr != nil {
			err = refreshErr
		} else {
			newCredentials = s.openaiOAuthService.BuildAccountCredentials(tokenInfo)
			// Preserve non-token settings from existing credentials
			for k, v := range account.Credentials {
				if _, exists := newCredentials[k]; !exists {
					newCredentials[k] = v
				}
			}
			newCredentials = NormalizeOpenAIPersonalAccessTokenCredentials(account, tokenInfo, newCredentials)
		}
	case PlatformGemini:
		if s.geminiOAuthService == nil {
			return nil
		}
		tokenInfo, refreshErr := s.geminiOAuthService.RefreshAccountToken(ctx, account)
		if refreshErr != nil {
			err = refreshErr
		} else {
			newCredentials = s.geminiOAuthService.BuildAccountCredentials(tokenInfo)
			for k, v := range account.Credentials {
				if _, exists := newCredentials[k]; !exists {
					newCredentials[k] = v
				}
			}
		}
	default:
		return nil
	}

	if err != nil {
		// Log but don't fail the sync - token might still be valid or refreshable later
		return nil
	}

	return newCredentials
}

// buildSelectedSet converts a slice of selected CRS account IDs to a set for O(1) lookup.
// Returns nil if ids is nil (field not sent → backward compatible: create all).
// Returns an empty map if ids is non-nil but empty (user selected none → create none).
func buildSelectedSet(ids []string) map[string]struct{} {
	if ids == nil {
		return nil
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

// shouldCreateAccount checks if a new CRS account should be created based on user selection.
// Returns true if selectedSet is nil (backward compatible: create all) or if crsID is in the set.
func shouldCreateAccount(crsID string, selectedSet map[string]struct{}) bool {
	if selectedSet == nil {
		return true
	}
	_, ok := selectedSet[crsID]
	return ok
}

// PreviewFromCRSResult contains the preview of accounts from CRS before sync.
type PreviewFromCRSResult struct {
	NewAccounts      []CRSPreviewAccount `json:"new_accounts"`
	ExistingAccounts []CRSPreviewAccount `json:"existing_accounts"`
	PreviewToken     string              `json:"preview_token"`
	ExpiresAt        int64               `json:"expires_at"`
}

// CRSPreviewAccount represents a single account in the preview result.
type CRSPreviewAccount struct {
	CRSAccountID            string                          `json:"crs_account_id"`
	LocalAccountID          int64                           `json:"local_account_id,omitempty"`
	Kind                    string                          `json:"kind"`
	Name                    string                          `json:"name"`
	Platform                string                          `json:"platform"`
	Type                    string                          `json:"type"`
	RequiresForceActiveEdit bool                            `json:"requires_force_active_edit"`
	RoomBindings            []CRSAccountRoomBindingSnapshot `json:"room_bindings"`
}

// PreviewFromCRS connects to CRS, fetches all accounts, and classifies them
// as new or existing by batch-querying local crs_account_id mappings.
func (s *CRSSyncService) PreviewFromCRS(ctx context.Context, input SyncFromCRSInput) (*PreviewFromCRSResult, error) {
	if input.ActorAdminID <= 0 {
		return nil, ErrCRSPreviewActorRequired
	}
	localSnapshots, existingByCRSID, err := s.loadValidatedCRSPreviewSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.crsPreviewSigningSecret(); err != nil {
		return nil, err
	}
	connection, err := s.normalizeCRSConnection(input.BaseURL, input.Username, input.Password)
	if err != nil {
		return nil, err
	}
	exported, err := s.fetchCRSExport(ctx, connection)
	if err != nil {
		return nil, err
	}

	result := &PreviewFromCRSResult{
		NewAccounts:      make([]CRSPreviewAccount, 0),
		ExistingAccounts: make([]CRSPreviewAccount, 0),
	}

	classify := func(crsID, kind, name, platform, accountType string) {
		preview := CRSPreviewAccount{
			CRSAccountID: crsID,
			Kind:         kind,
			Name:         defaultName(name, crsID),
			Platform:     platform,
			Type:         accountType,
			RoomBindings: make([]CRSAccountRoomBindingSnapshot, 0),
		}
		if snapshot, exists := existingByCRSID[crsID]; exists {
			preview.LocalAccountID = snapshot.LocalAccountID
			preview.RoomBindings = append(preview.RoomBindings, snapshot.RoomBindings...)
			preview.RequiresForceActiveEdit = len(preview.RoomBindings) > 0
			result.ExistingAccounts = append(result.ExistingAccounts, preview)
		} else {
			result.NewAccounts = append(result.NewAccounts, preview)
		}
	}

	for _, src := range exported.Data.ClaudeAccounts {
		authType := strings.TrimSpace(src.AuthType)
		if authType == "" {
			authType = AccountTypeOAuth
		}
		classify(src.ID, src.Kind, src.Name, PlatformAnthropic, authType)
	}
	for _, src := range exported.Data.ClaudeConsoleAccounts {
		classify(src.ID, src.Kind, src.Name, PlatformAnthropic, AccountTypeAPIKey)
	}
	for _, src := range exported.Data.OpenAIOAuthAccounts {
		classify(src.ID, src.Kind, src.Name, PlatformOpenAI, AccountTypeOAuth)
	}
	for _, src := range exported.Data.OpenAIResponsesAccounts {
		classify(src.ID, src.Kind, src.Name, PlatformOpenAI, AccountTypeAPIKey)
	}
	for _, src := range exported.Data.GeminiOAuthAccounts {
		classify(src.ID, src.Kind, src.Name, PlatformGemini, AccountTypeOAuth)
	}
	for _, src := range exported.Data.GeminiAPIKeyAccounts {
		classify(src.ID, src.Kind, src.Name, PlatformGemini, AccountTypeAPIKey)
	}

	sort.SliceStable(result.ExistingAccounts, func(i, j int) bool {
		if result.ExistingAccounts[i].LocalAccountID == result.ExistingAccounts[j].LocalAccountID {
			return result.ExistingAccounts[i].CRSAccountID < result.ExistingAccounts[j].CRSAccountID
		}
		return result.ExistingAccounts[i].LocalAccountID < result.ExistingAccounts[j].LocalAccountID
	})
	sort.SliceStable(result.NewAccounts, func(i, j int) bool {
		return result.NewAccounts[i].CRSAccountID < result.NewAccounts[j].CRSAccountID
	})
	connectionHash, err := hashCRSConnection(connection)
	if err != nil {
		return nil, ErrCRSPreviewSigningUnavailable.WithMetadata(map[string]string{
			"stage": "connection_hash",
		}).WithCause(err)
	}
	exportHash, err := hashCRSExportAccounts(exported)
	if err != nil {
		if errors.Is(err, ErrCRSExportInvalid) {
			return nil, err
		}
		return nil, ErrCRSPreviewSigningUnavailable.WithMetadata(map[string]string{
			"stage": "export_hash",
		}).WithCause(err)
	}
	localSnapshotHash, err := hashCRSPreviewValue(crsLocalSnapshotHashDomain, localSnapshots)
	if err != nil {
		return nil, ErrCRSPreviewSigningUnavailable.WithMetadata(map[string]string{
			"stage": "local_snapshot_hash",
		}).WithCause(err)
	}
	expiresAt := s.now().UTC().Add(crsPreviewTokenTTL).Unix()
	result.PreviewToken, err = s.signCRSPreviewToken(crsPreviewTokenPayload{
		Version:           crsPreviewTokenVersion,
		ActorAdminID:      input.ActorAdminID,
		ConnectionHash:    connectionHash,
		ExportHash:        exportHash,
		LocalSnapshotHash: localSnapshotHash,
		ExpiresAt:         expiresAt,
	})
	if err != nil {
		return nil, err
	}
	result.ExpiresAt = expiresAt
	return result, nil
}
