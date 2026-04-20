package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/ent/identityadoptiondecision"
	"github.com/Wei-Shaw/sub2api/ent/pendingauthsession"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestOIDCSyntheticEmailStableAndDistinct(t *testing.T) {
	k1 := oidcIdentityKey("https://issuer.example.com", "subject-a")
	k2 := oidcIdentityKey("https://issuer.example.com", "subject-b")

	e1 := oidcSyntheticEmailFromIdentityKey(k1)
	e1Again := oidcSyntheticEmailFromIdentityKey(k1)
	e2 := oidcSyntheticEmailFromIdentityKey(k2)

	require.Equal(t, e1, e1Again)
	require.NotEqual(t, e1, e2)
	require.Contains(t, e1, "@oidc-connect.invalid")
}

func TestBuildOIDCAuthorizeURLIncludesNonceAndPKCE(t *testing.T) {
	cfg := config.OIDCConnectConfig{
		AuthorizeURL: "https://issuer.example.com/auth",
		ClientID:     "cid",
		Scopes:       "openid email profile",
	}

	u, err := buildOIDCAuthorizeURL(cfg, "state123", "nonce123", "challenge123", "https://app.example.com/callback")
	require.NoError(t, err)
	require.Contains(t, u, "nonce=nonce123")
	require.Contains(t, u, "code_challenge=challenge123")
	require.Contains(t, u, "code_challenge_method=S256")
	require.Contains(t, u, "scope=openid+email+profile")
}

func TestOIDCParseAndValidateIDToken(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	kid := "kid-1"
	jwks := oidcJWKSet{Keys: []oidcJWK{buildRSAJWK(kid, &priv.PublicKey)}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(jwks))
	}))
	defer srv.Close()

	now := time.Now()
	claims := oidcIDTokenClaims{
		Nonce: "nonce-ok",
		Azp:   "client-1",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "https://issuer.example.com",
			Subject:   "subject-1",
			Audience:  jwt.ClaimStrings{"client-1", "another-aud"},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(priv)
	require.NoError(t, err)

	cfg := config.OIDCConnectConfig{
		ClientID:           "client-1",
		IssuerURL:          "https://issuer.example.com",
		JWKSURL:            srv.URL,
		AllowedSigningAlgs: "RS256",
		ClockSkewSeconds:   120,
	}

	parsed, err := oidcParseAndValidateIDToken(context.Background(), cfg, signed, "nonce-ok")
	require.NoError(t, err)
	require.Equal(t, "subject-1", parsed.Subject)
	require.Equal(t, "https://issuer.example.com", parsed.Issuer)

	_, err = oidcParseAndValidateIDToken(context.Background(), cfg, signed, "bad-nonce")
	require.Error(t, err)
}

func TestOIDCParseUserInfoIncludesSuggestedProfile(t *testing.T) {
	cfg := config.OIDCConnectConfig{}

	claims := oidcParseUserInfo(`{
		"sub":"subject-1",
		"preferred_username":"alice",
		"name":"Alice Example",
		"picture":"https://cdn.example/avatar.png",
		"email":"alice@example.com",
		"email_verified":true
	}`, cfg)

	require.Equal(t, "subject-1", claims.Subject)
	require.Equal(t, "alice", claims.Username)
	require.Equal(t, "Alice Example", claims.DisplayName)
	require.Equal(t, "https://cdn.example/avatar.png", claims.AvatarURL)
	require.NotNil(t, claims.EmailVerified)
	require.True(t, *claims.EmailVerified)
}

func buildRSAJWK(kid string, pub *rsa.PublicKey) oidcJWK {
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	return oidcJWK{
		Kty: "RSA",
		Kid: kid,
		Use: "sig",
		Alg: "RS256",
		N:   n,
		E:   e,
	}
}

func TestCompleteOIDCOAuthRegistrationAppliesPendingAdoptionDecision(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandler(t, false)
	ctx := context.Background()

	session, err := client.PendingAuthSession.Create().
		SetSessionToken("oidc-complete-session").
		SetIntent("login").
		SetProviderType("oidc").
		SetProviderKey("https://issuer.example.com").
		SetProviderSubject("oidc-subject-1").
		SetResolvedEmail("93a310f4c1944c5bbd2e246df1f76485@oidc-connect.invalid").
		SetBrowserSessionKey("oidc-browser").
		SetUpstreamIdentityClaims(map[string]any{
			"username":               "oidc_user",
			"issuer":                 "https://issuer.example.com",
			"suggested_display_name": "OIDC Display",
			"suggested_avatar_url":   "https://cdn.example/oidc.png",
		}).
		SetExpiresAt(time.Now().UTC().Add(10 * time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	_, err = service.NewAuthPendingIdentityService(client).UpsertAdoptionDecision(ctx, service.PendingIdentityAdoptionDecisionInput{
		PendingAuthSessionID: session.ID,
		AdoptAvatar:          true,
	})
	require.NoError(t, err)

	body := bytes.NewBufferString(`{"invitation_code":"invite-1","adopt_display_name":true}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/oidc/complete-registration", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
	req.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("oidc-browser")})
	c.Request = req

	handler.CompleteOIDCOAuthRegistration(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	responseData := decodeJSONBody(t, recorder)
	require.NotEmpty(t, responseData["access_token"])

	userEntity, err := client.User.Query().
		Where(dbuser.EmailEQ(session.ResolvedEmail)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "OIDC Display", userEntity.Username)

	identity, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ("oidc"),
			authidentity.ProviderKeyEQ("https://issuer.example.com"),
			authidentity.ProviderSubjectEQ("oidc-subject-1"),
		).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, userEntity.ID, identity.UserID)
	require.Equal(t, "OIDC Display", identity.Metadata["display_name"])
	require.Equal(t, "https://cdn.example/oidc.png", identity.Metadata["avatar_url"])

	decision, err := client.IdentityAdoptionDecision.Query().
		Where(identityadoptiondecision.PendingAuthSessionIDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, decision.IdentityID)
	require.Equal(t, identity.ID, *decision.IdentityID)
	require.True(t, decision.AdoptDisplayName)
	require.True(t, decision.AdoptAvatar)

	consumed, err := client.PendingAuthSession.Query().
		Where(pendingauthsession.IDEQ(session.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, consumed.ConsumedAt)
}
