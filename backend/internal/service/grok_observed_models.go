package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/tidwall/gjson"
)

const (
	grokObservedModelsExtraKey = "grok_observed_models"
	grokObservedModelsTTL      = 6 * time.Hour
	grokObservedModelsTimeout  = 15 * time.Second
)

type grokObservedModelsSnapshot struct {
	Models    []string `json:"models"`
	FetchedAt string   `json:"fetched_at"`
	Source    string   `json:"source,omitempty"`
}

var grokObservedModelsFlight sync.Map

func (s *GrokQuotaService) scheduleGrokObservedModelsSync(account *Account) {
	if s == nil || account == nil || !account.IsGrokOAuth() || s.accountRepo == nil || account.ID <= 0 {
		return
	}
	if _, loaded := grokObservedModelsFlight.LoadOrStore(account.ID, struct{}{}); loaded {
		return
	}
	copyAccount := *account
	copyAccount.Credentials = cloneCredentials(account.Credentials)
	copyAccount.Extra = cloneCredentials(account.Extra)
	go func() {
		defer grokObservedModelsFlight.Delete(copyAccount.ID)
		ctx, cancel := context.WithTimeout(context.Background(), grokObservedModelsTimeout)
		defer cancel()
		if err := s.syncGrokObservedModels(ctx, &copyAccount); err != nil {
			slog.Debug("grok_observed_models_sync_failed", "account_id", copyAccount.ID, "error", err)
		}
	}()
}

func (s *GrokQuotaService) syncGrokObservedModels(ctx context.Context, account *Account) error {
	if s == nil || account == nil || !account.IsGrokOAuth() {
		return nil
	}
	if snapshot := parseGrokObservedModels(account.Extra); snapshot != nil {
		if fetchedAt, err := time.Parse(time.RFC3339, snapshot.FetchedAt); err == nil && time.Since(fetchedAt) < grokObservedModelsTTL {
			return nil
		}
	}
	if s.httpUpstream == nil || s.accountRepo == nil {
		return fmt.Errorf("grok observed model sync is not configured")
	}
	token := strings.TrimSpace(account.GetGrokAccessToken())
	if token == "" && s.tokenProvider != nil {
		accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
		if err != nil {
			return err
		}
		token = strings.TrimSpace(accessToken)
	}
	if token == "" {
		return fmt.Errorf("grok access token is empty")
	}
	baseURL := strings.TrimSpace(account.GetGrokBaseURL())
	if baseURL == "" {
		baseURL = xai.DefaultCLIBaseURL
	}
	validator, err := grokBaseURLValidator(ctx, account, s.cfg, s.settingService)
	if err != nil {
		return err
	}
	validatedBaseURL, err := validator(baseURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), http.MethodGet, buildOpenAIModelsURL(validatedBaseURL), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	applyGrokCLIHeaders(req.Header)
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	} else if account.ProxyID != nil && s.proxyRepo != nil {
		proxy, proxyErr := s.proxyRepo.GetByID(ctx, *account.ProxyID)
		if proxyErr != nil {
			return proxyErr
		}
		if proxy == nil {
			return fmt.Errorf("grok account proxy not found")
		}
		proxyURL = proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("grok models endpoint returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	models := extractGrokModelIDsFromModelsBody(body)
	if len(models) == 0 {
		return fmt.Errorf("grok models endpoint returned no model ids")
	}
	snapshot := grokObservedModelsSnapshot{
		Models: models, FetchedAt: time.Now().UTC().Format(time.RFC3339), Source: "upstream_v1_models",
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	var stored map[string]any
	if err := json.Unmarshal(encoded, &stored); err != nil {
		return err
	}
	return s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{grokObservedModelsExtraKey: stored})
}

func extractGrokModelIDsFromModelsBody(body []byte) []string {
	data := gjson.GetBytes(body, "data")
	if !data.IsArray() {
		data = gjson.ParseBytes(body)
	}
	seen := make(map[string]struct{})
	models := make([]string, 0)
	data.ForEach(func(_, value gjson.Result) bool {
		id := strings.TrimSpace(value.Get("id").String())
		if id == "" && value.Type == gjson.String {
			id = strings.TrimSpace(value.String())
		}
		if id == "" {
			return true
		}
		if _, exists := seen[id]; exists {
			return true
		}
		seen[id] = struct{}{}
		models = append(models, id)
		return true
	})
	sort.Strings(models)
	return models
}

func parseGrokObservedModels(extra map[string]any) *grokObservedModelsSnapshot {
	if extra == nil {
		return nil
	}
	raw, exists := extra[grokObservedModelsExtraKey]
	if !exists || raw == nil {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var snapshot grokObservedModelsSnapshot
	if json.Unmarshal(encoded, &snapshot) != nil || len(snapshot.Models) == 0 {
		return nil
	}
	return &snapshot
}
