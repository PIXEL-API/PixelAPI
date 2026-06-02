package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

type MasterClient struct {
	baseURL    string
	subsiteID  string
	secret     string
	httpClient *http.Client
}

func NewMasterClient(baseURL, subsiteID, secret string) *MasterClient {
	return &MasterClient{
		baseURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		subsiteID: strings.TrimSpace(subsiteID),
		secret:    strings.TrimSpace(secret),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *MasterClient) Heartbeat(ctx context.Context, input service.SubsiteHeartbeatInput) error {
	return c.post(ctx, "/api/internal/subsites/heartbeat", input, nil)
}

func (c *MasterClient) Config(ctx context.Context, out any) error {
	return c.do(ctx, http.MethodGet, "/api/internal/subsites/config", nil, out)
}

func (c *MasterClient) Authorize(ctx context.Context, input service.AuthorizeSubsiteRequestInput) (*service.AuthorizeSubsiteResponse, error) {
	var out service.AuthorizeSubsiteResponse
	if err := c.post(ctx, "/api/internal/requests/authorize", input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *MasterClient) UsageBatch(ctx context.Context, input service.UsageIngestBatch) (*service.UsageIngestResult, error) {
	var out service.UsageIngestResult
	if err := c.post(ctx, "/api/internal/usage/batch", input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *MasterClient) PostRaw(ctx context.Context, path string, input any, out any) error {
	return c.post(ctx, path, input, out)
}

func (c *MasterClient) post(ctx context.Context, path string, input any, out any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal master request: %w", err)
	}
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *MasterClient) do(ctx context.Context, method, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create master request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.sign(req, path, body)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call master: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read master response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("master returned %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if out == nil {
		return nil
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("parse master envelope: %w", err)
	}
	if envelope.Code != 0 {
		return fmt.Errorf("master error: %s", envelope.Message)
	}
	if len(envelope.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("parse master data: %w", err)
	}
	return nil
}

func (c *MasterClient) sign(req *http.Request, path string, body []byte) {
	sum := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(sum[:])
	timestamp := time.Now().UTC().Format(time.RFC3339)
	nonce := uuid.NewString()
	req.Header.Set(service.SubsiteAuthHeaderID, c.subsiteID)
	req.Header.Set(service.SubsiteAuthHeaderTimestamp, timestamp)
	req.Header.Set(service.SubsiteAuthHeaderNonce, nonce)
	req.Header.Set(service.SubsiteAuthHeaderBodySHA, bodyHash)
	req.Header.Set(service.SubsiteAuthHeaderSignature, service.SignSubsiteRequest(c.secret, req.Method, path, timestamp, nonce, bodyHash))
}
