package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/identityadoptiondecision"
	"github.com/Wei-Shaw/sub2api/ent/pendingauthsession"
	dbpredicate "github.com/Wei-Shaw/sub2api/ent/predicate"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

	entsql "entgo.io/ent/dialect/sql"
)

var (
	ErrPendingAuthSessionNotFound = infraerrors.NotFound("PENDING_AUTH_SESSION_NOT_FOUND", "pending auth session not found")
	ErrPendingAuthSessionExpired  = infraerrors.Unauthorized("PENDING_AUTH_SESSION_EXPIRED", "pending auth session has expired")
	ErrPendingAuthSessionConsumed = infraerrors.Unauthorized("PENDING_AUTH_SESSION_CONSUMED", "pending auth session has already been used")
	ErrPendingAuthCodeInvalid     = infraerrors.Unauthorized("PENDING_AUTH_CODE_INVALID", "pending auth completion code is invalid")
	ErrPendingAuthCodeExpired     = infraerrors.Unauthorized("PENDING_AUTH_CODE_EXPIRED", "pending auth completion code has expired")
	ErrPendingAuthCodeConsumed    = infraerrors.Unauthorized("PENDING_AUTH_CODE_CONSUMED", "pending auth completion code has already been used")
	ErrPendingAuthBrowserMismatch = infraerrors.Unauthorized("PENDING_AUTH_BROWSER_MISMATCH", "pending auth completion code does not match this browser session")
)

const (
	defaultPendingAuthTTL           = 15 * time.Minute
	defaultPendingAuthCompletionTTL = 5 * time.Minute
)

type PendingAuthIdentityKey struct {
	ProviderType    string
	ProviderKey     string
	ProviderSubject string
}

type CreatePendingAuthSessionInput struct {
	SessionToken             string
	Intent                   string
	Identity                 PendingAuthIdentityKey
	TargetUserID             *int64
	RedirectTo               string
	ResolvedEmail            string
	RegistrationPasswordHash string
	BrowserSessionKey        string
	UpstreamIdentityClaims   map[string]any
	LocalFlowState           map[string]any
	ExpiresAt                time.Time
}

type IssuePendingAuthCompletionCodeInput struct {
	PendingAuthSessionID int64
	BrowserSessionKey    string
	TTL                  time.Duration
}

type IssuePendingAuthCompletionCodeResult struct {
	Code      string
	ExpiresAt time.Time
}

type PendingIdentityAdoptionDecisionInput struct {
	PendingAuthSessionID int64
	IdentityID           *int64
	AdoptDisplayName     bool
	AdoptAvatar          bool
}

type AuthPendingIdentityService struct {
	entClient *dbent.Client
}

func NewAuthPendingIdentityService(entClient *dbent.Client) *AuthPendingIdentityService {
	return &AuthPendingIdentityService{entClient: entClient}
}

func (s *AuthPendingIdentityService) CreatePendingSession(ctx context.Context, input CreatePendingAuthSessionInput) (*dbent.PendingAuthSession, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("pending auth ent client is not configured")
	}

	sessionToken := strings.TrimSpace(input.SessionToken)
	if sessionToken == "" {
		var err error
		sessionToken, err = randomOpaqueToken(24)
		if err != nil {
			return nil, err
		}
	}

	expiresAt := input.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(defaultPendingAuthTTL)
	}

	create := s.entClient.PendingAuthSession.Create().
		SetSessionToken(sessionToken).
		SetIntent(strings.TrimSpace(input.Intent)).
		SetProviderType(strings.TrimSpace(input.Identity.ProviderType)).
		SetProviderKey(strings.TrimSpace(input.Identity.ProviderKey)).
		SetProviderSubject(strings.TrimSpace(input.Identity.ProviderSubject)).
		SetRedirectTo(strings.TrimSpace(input.RedirectTo)).
		SetResolvedEmail(strings.TrimSpace(input.ResolvedEmail)).
		SetRegistrationPasswordHash(strings.TrimSpace(input.RegistrationPasswordHash)).
		SetBrowserSessionKey(strings.TrimSpace(input.BrowserSessionKey)).
		SetUpstreamIdentityClaims(copyPendingMap(input.UpstreamIdentityClaims)).
		SetLocalFlowState(copyPendingMap(input.LocalFlowState)).
		SetExpiresAt(expiresAt)
	if input.TargetUserID != nil {
		create = create.SetTargetUserID(*input.TargetUserID)
	}
	return create.Save(ctx)
}

func (s *AuthPendingIdentityService) IssueCompletionCode(ctx context.Context, input IssuePendingAuthCompletionCodeInput) (*IssuePendingAuthCompletionCodeResult, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("pending auth ent client is not configured")
	}

	session, err := s.entClient.PendingAuthSession.Get(ctx, input.PendingAuthSessionID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrPendingAuthSessionNotFound
		}
		return nil, err
	}

	code, err := randomOpaqueToken(24)
	if err != nil {
		return nil, err
	}
	ttl := input.TTL
	if ttl <= 0 {
		ttl = defaultPendingAuthCompletionTTL
	}
	expiresAt := time.Now().UTC().Add(ttl)

	update := s.entClient.PendingAuthSession.UpdateOneID(session.ID).
		SetCompletionCodeHash(hashPendingAuthCode(code)).
		SetCompletionCodeExpiresAt(expiresAt)
	if strings.TrimSpace(input.BrowserSessionKey) != "" {
		update = update.SetBrowserSessionKey(strings.TrimSpace(input.BrowserSessionKey))
	}
	if _, err := update.Save(ctx); err != nil {
		return nil, err
	}

	return &IssuePendingAuthCompletionCodeResult{
		Code:      code,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *AuthPendingIdentityService) ConsumeCompletionCode(ctx context.Context, rawCode, browserSessionKey string) (*dbent.PendingAuthSession, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("pending auth ent client is not configured")
	}

	codeHash := hashPendingAuthCode(strings.TrimSpace(rawCode))
	session, err := s.entClient.PendingAuthSession.Query().
		Where(pendingauthsession.CompletionCodeHashEQ(codeHash)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrPendingAuthCodeInvalid
		}
		return nil, err
	}

	return s.consumeSession(ctx, session, browserSessionKey, ErrPendingAuthCodeExpired, ErrPendingAuthCodeConsumed)
}

func (s *AuthPendingIdentityService) ConsumeBrowserSession(ctx context.Context, sessionToken, browserSessionKey string) (*dbent.PendingAuthSession, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("pending auth ent client is not configured")
	}

	session, err := s.getBrowserSession(ctx, sessionToken)
	if err != nil {
		return nil, err
	}

	return s.consumeSession(ctx, session, browserSessionKey, ErrPendingAuthSessionExpired, ErrPendingAuthSessionConsumed)
}

func (s *AuthPendingIdentityService) GetBrowserSession(ctx context.Context, sessionToken, browserSessionKey string) (*dbent.PendingAuthSession, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("pending auth ent client is not configured")
	}

	session, err := s.getBrowserSession(ctx, sessionToken)
	if err != nil {
		return nil, err
	}
	if err := validatePendingSessionState(session, browserSessionKey, ErrPendingAuthSessionExpired, ErrPendingAuthSessionConsumed); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *AuthPendingIdentityService) getBrowserSession(ctx context.Context, sessionToken string) (*dbent.PendingAuthSession, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("pending auth ent client is not configured")
	}

	sessionToken = strings.TrimSpace(sessionToken)
	if sessionToken == "" {
		return nil, ErrPendingAuthSessionNotFound
	}

	session, err := s.entClient.PendingAuthSession.Query().
		Where(pendingauthsession.SessionTokenEQ(sessionToken)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrPendingAuthSessionNotFound
		}
		return nil, err
	}
	return session, nil
}

func (s *AuthPendingIdentityService) consumeSession(
	ctx context.Context,
	session *dbent.PendingAuthSession,
	browserSessionKey string,
	expiredErr error,
	consumedErr error,
) (*dbent.PendingAuthSession, error) {
	if err := validatePendingSessionState(session, browserSessionKey, expiredErr, consumedErr); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	updated, err := s.entClient.PendingAuthSession.UpdateOneID(session.ID).
		SetConsumedAt(now).
		SetCompletionCodeHash("").
		ClearCompletionCodeExpiresAt().
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func validatePendingSessionState(session *dbent.PendingAuthSession, browserSessionKey string, expiredErr error, consumedErr error) error {
	if session == nil {
		return ErrPendingAuthSessionNotFound
	}

	now := time.Now().UTC()
	if session.ConsumedAt != nil {
		return consumedErr
	}
	if !session.ExpiresAt.IsZero() && now.After(session.ExpiresAt) {
		return expiredErr
	}
	if session.CompletionCodeExpiresAt != nil && now.After(*session.CompletionCodeExpiresAt) {
		return expiredErr
	}
	if strings.TrimSpace(session.BrowserSessionKey) != "" && strings.TrimSpace(browserSessionKey) != strings.TrimSpace(session.BrowserSessionKey) {
		return ErrPendingAuthBrowserMismatch
	}
	return nil
}

func (s *AuthPendingIdentityService) UpsertAdoptionDecision(ctx context.Context, input PendingIdentityAdoptionDecisionInput) (*dbent.IdentityAdoptionDecision, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("pending auth ent client is not configured")
	}

	if input.IdentityID != nil && *input.IdentityID > 0 {
		if _, err := s.entClient.IdentityAdoptionDecision.Update().
			Where(
				identityadoptiondecision.IdentityIDEQ(*input.IdentityID),
				dbpredicate.IdentityAdoptionDecision(func(s *entsql.Selector) {
					col := s.C(identityadoptiondecision.FieldPendingAuthSessionID)
					s.Where(entsql.Or(
						entsql.IsNull(col),
						entsql.NEQ(col, input.PendingAuthSessionID),
					))
				}),
			).
			ClearIdentityID().
			Save(ctx); err != nil {
			return nil, err
		}
	}

	existing, err := s.entClient.IdentityAdoptionDecision.Query().
		Where(identityadoptiondecision.PendingAuthSessionIDEQ(input.PendingAuthSessionID)).
		Only(ctx)
	if err != nil && !dbent.IsNotFound(err) {
		return nil, err
	}
	if existing == nil {
		create := s.entClient.IdentityAdoptionDecision.Create().
			SetPendingAuthSessionID(input.PendingAuthSessionID).
			SetAdoptDisplayName(input.AdoptDisplayName).
			SetAdoptAvatar(input.AdoptAvatar).
			SetDecidedAt(time.Now().UTC())
		if input.IdentityID != nil {
			create = create.SetIdentityID(*input.IdentityID)
		}
		return create.Save(ctx)
	}

	update := s.entClient.IdentityAdoptionDecision.UpdateOneID(existing.ID).
		SetAdoptDisplayName(input.AdoptDisplayName).
		SetAdoptAvatar(input.AdoptAvatar)
	if input.IdentityID != nil {
		update = update.SetIdentityID(*input.IdentityID)
	}
	return update.Save(ctx)
}

func copyPendingMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func randomOpaqueToken(byteLen int) (string, error) {
	if byteLen <= 0 {
		byteLen = 16
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashPendingAuthCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}
