package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

func normalizeOAuthSignupSource(signupSource string) string {
	signupSource = strings.TrimSpace(strings.ToLower(signupSource))
	switch signupSource {
	case "", "email":
		return "email"
	case "linuxdo", "wechat", "oidc":
		return signupSource
	default:
		return "email"
	}
}

// SendPendingOAuthVerifyCode sends a local verification code for pending OAuth
// account-creation flows without relying on the public registration gate.
func (s *AuthService) SendPendingOAuthVerifyCode(ctx context.Context, email string) (*SendVerifyCodeResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, ErrEmailVerifyRequired
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, ErrEmailVerifyRequired
	}
	if isReservedEmail(email) {
		return nil, ErrEmailReserved
	}
	if s == nil || s.emailService == nil {
		return nil, ErrServiceUnavailable
	}

	siteName := "Sub2API"
	if s.settingService != nil {
		siteName = s.settingService.GetSiteName(ctx)
	}
	if err := s.emailService.SendVerifyCode(ctx, email, siteName); err != nil {
		return nil, err
	}
	return &SendVerifyCodeResult{
		Countdown: int(verifyCodeCooldown / time.Second),
	}, nil
}

func (s *AuthService) validateOAuthRegistrationInvitation(ctx context.Context, invitationCode string) (string, error) {
	if s == nil || s.settingService == nil || !s.settingService.IsInvitationCodeEnabled(ctx) {
		return "", nil
	}
	if s.affiliateService == nil {
		return "", ErrServiceUnavailable
	}

	invitationCode = strings.TrimSpace(invitationCode)
	if invitationCode == "" {
		return "", ErrInvitationCodeRequired
	}

	if err := s.affiliateService.ValidateInvitationCode(ctx, invitationCode); err != nil {
		return "", ErrInvitationCodeInvalid
	}
	return invitationCode, nil
}

// VerifyOAuthEmailCode verifies the locally entered email verification code for
// third-party signup and binding flows. This is intentionally independent from
// the global registration email verification toggle.
func (s *AuthService) VerifyOAuthEmailCode(ctx context.Context, email, verifyCode string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	verifyCode = strings.TrimSpace(verifyCode)

	if email == "" {
		return ErrEmailVerifyRequired
	}
	if verifyCode == "" {
		return ErrEmailVerifyRequired
	}
	if s == nil || s.emailService == nil {
		return ErrServiceUnavailable
	}
	return s.emailService.VerifyCode(ctx, email, verifyCode)
}

// RegisterOAuthEmailAccount creates a local account from a third-party first
// login after the user has verified a local email address.
func (s *AuthService) RegisterOAuthEmailAccount(
	ctx context.Context,
	email string,
	password string,
	verifyCode string,
	invitationCode string,
	signupSource string,
) (*TokenPair, *User, error) {
	if s == nil {
		return nil, nil, ErrServiceUnavailable
	}
	if s.settingService == nil || !s.settingService.IsRegistrationEnabled(ctx) {
		return nil, nil, ErrRegDisabled
	}

	email = strings.TrimSpace(strings.ToLower(email))
	if isReservedEmail(email) {
		return nil, nil, ErrEmailReserved
	}
	if err := s.validateRegistrationEmailPolicy(ctx, email); err != nil {
		return nil, nil, err
	}
	if err := s.VerifyOAuthEmailCode(ctx, email, verifyCode); err != nil {
		return nil, nil, err
	}

	if _, err := s.validateOAuthRegistrationInvitation(ctx, invitationCode); err != nil {
		return nil, nil, err
	}

	existsEmail, err := s.userRepo.ExistsByEmail(ctx, email)
	if err != nil {
		return nil, nil, ErrServiceUnavailable
	}
	if existsEmail {
		return nil, nil, ErrEmailExists
	}

	hashedPassword, err := s.HashPassword(password)
	if err != nil {
		return nil, nil, fmt.Errorf("hash password: %w", err)
	}

	signupSource = normalizeOAuthSignupSource(signupSource)
	grantPlan := s.resolveSignupGrantPlan(ctx, signupSource)

	user := &User{
		Email:        email,
		PasswordHash: hashedPassword,
		Role:         RoleUser,
		Balance:      grantPlan.Balance,
		Concurrency:  grantPlan.Concurrency,
		Status:       StatusActive,
		SignupSource: signupSource,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		if errors.Is(err, ErrEmailExists) {
			return nil, nil, ErrEmailExists
		}
		return nil, nil, ErrServiceUnavailable
	}

	tokenPair, err := s.GenerateTokenPair(ctx, user, "")
	if err != nil {
		_ = s.RollbackOAuthEmailAccountCreation(ctx, user.ID, "")
		return nil, nil, fmt.Errorf("generate token pair: %w", err)
	}
	return tokenPair, user, nil
}

// RegisterVerifiedOAuthEmailAccount creates a local account from an OAuth
// provider that has already returned a verified email address.
func (s *AuthService) RegisterVerifiedOAuthEmailAccount(
	ctx context.Context,
	email string,
	password string,
	invitationCode string,
	signupSource string,
) (*TokenPair, *User, error) {
	if s == nil {
		return nil, nil, ErrServiceUnavailable
	}
	if s.settingService == nil || !s.settingService.IsRegistrationEnabled(ctx) {
		return nil, nil, ErrRegDisabled
	}

	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || len(email) > 255 {
		return nil, nil, ErrEmailVerifyRequired
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, nil, ErrEmailVerifyRequired
	}
	if isReservedEmail(email) {
		return nil, nil, ErrEmailReserved
	}
	if err := s.validateRegistrationEmailPolicy(ctx, email); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(password) == "" {
		return nil, nil, infraerrors.BadRequest("PASSWORD_REQUIRED", "password is required")
	}
	if _, err := s.validateOAuthRegistrationInvitation(ctx, invitationCode); err != nil {
		return nil, nil, err
	}

	existsEmail, err := s.userRepo.ExistsByEmail(ctx, email)
	if err != nil {
		return nil, nil, ErrServiceUnavailable
	}
	if existsEmail {
		return nil, nil, ErrEmailExists
	}

	hashedPassword, err := s.HashPassword(password)
	if err != nil {
		return nil, nil, fmt.Errorf("hash password: %w", err)
	}

	signupSource = normalizeOAuthSignupSource(signupSource)
	grantPlan := s.resolveSignupGrantPlan(ctx, signupSource)
	var defaultRPMLimit int
	if s.settingService != nil {
		defaultRPMLimit = s.settingService.GetDefaultUserRPMLimit(ctx)
	}
	user := &User{
		Email:        email,
		PasswordHash: hashedPassword,
		Role:         RoleUser,
		Balance:      grantPlan.Balance,
		Concurrency:  grantPlan.Concurrency,
		RPMLimit:     defaultRPMLimit,
		Status:       StatusActive,
		SignupSource: signupSource,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		if errors.Is(err, ErrEmailExists) {
			return nil, nil, ErrEmailExists
		}
		return nil, nil, ErrServiceUnavailable
	}

	tokenPair, err := s.GenerateTokenPair(ctx, user, "")
	if err != nil {
		_ = s.RollbackOAuthEmailAccountCreation(ctx, user.ID, "")
		return nil, nil, fmt.Errorf("generate token pair: %w", err)
	}
	return tokenPair, user, nil
}

// FinalizeOAuthEmailAccount applies invitation usage and normal signup bootstrap
// only after the pending OAuth flow has fully reached its last reversible step.
func (s *AuthService) FinalizeOAuthEmailAccount(
	ctx context.Context,
	user *User,
	invitationCode string,
	signupSource string,
	affiliateCode string,
) error {
	if s == nil || user == nil || user.ID <= 0 {
		return ErrServiceUnavailable
	}

	signupSource = normalizeOAuthSignupSource(signupSource)
	effectiveInvitationCode, invitationRequired, err := s.resolveRegistrationInvitationCode(ctx, invitationCode, affiliateCode)
	if err != nil {
		return err
	}
	if effectiveInvitationCode != "" {
		if s.affiliateService == nil {
			if invitationRequired {
				return ErrServiceUnavailable
			}
			logger.LegacyPrintf("service.auth", "[Auth] Skipping optional OAuth affiliate invitation code for user %d because affiliate service is unavailable", user.ID)
		} else {
			if err := s.affiliateService.ConsumeInvitationCode(ctx, user.ID, effectiveInvitationCode); err != nil {
				if invitationRequired {
					return ErrInvitationCodeInvalid
				}
				logger.LegacyPrintf("service.auth", "[Auth] Failed to consume optional OAuth affiliate invitation code for user %d: %v", user.ID, err)
			}
		}
	}

	s.updateOAuthSignupSource(ctx, user.ID, signupSource)
	grantPlan := s.resolveSignupGrantPlan(ctx, signupSource)
	s.assignSubscriptions(ctx, user.ID, grantPlan.Subscriptions, "auto assigned by signup defaults")
	return nil
}

// RollbackOAuthEmailAccountCreation removes a partially-created local account.
func (s *AuthService) RollbackOAuthEmailAccountCreation(ctx context.Context, userID int64, invitationCode string) error {
	if s == nil || s.userRepo == nil || userID <= 0 {
		return ErrServiceUnavailable
	}
	if err := s.userRepo.Delete(ctx, userID); err != nil {
		return fmt.Errorf("delete created oauth user: %w", err)
	}
	return nil
}

func (s *AuthService) oauthEmailFlowClient(ctx context.Context) *dbent.Client {
	if s == nil || s.entClient == nil {
		return nil
	}
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return s.entClient
}

func (s *AuthService) updateOAuthSignupSource(ctx context.Context, userID int64, signupSource string) {
	client := s.oauthEmailFlowClient(ctx)
	if client == nil || userID <= 0 || strings.TrimSpace(signupSource) == "" {
		return
	}
	_ = client.User.UpdateOneID(userID).SetSignupSource(signupSource).Exec(ctx)
}

// ValidatePasswordCredentials checks the local password without completing the
// login flow. This is used by pending third-party account adoption flows before
// the external identity has been bound.
func (s *AuthService) ValidatePasswordCredentials(ctx context.Context, email, password string) (*User, error) {
	if s == nil {
		return nil, ErrServiceUnavailable
	}

	user, err := s.userRepo.GetByEmail(ctx, strings.TrimSpace(strings.ToLower(email)))
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, ErrServiceUnavailable
	}
	if !user.IsActive() {
		return nil, ErrUserNotActive
	}
	if !s.CheckPassword(password, user.PasswordHash) {
		return nil, ErrInvalidCredentials
	}
	return user, nil
}

// RecordSuccessfulLogin updates last-login activity after a non-standard login
// flow finishes with a real session.
func (s *AuthService) RecordSuccessfulLogin(ctx context.Context, userID int64) {
	if s != nil && s.userRepo != nil && userID > 0 {
		user, err := s.userRepo.GetByID(ctx, userID)
		if err == nil && user != nil && !isReservedEmail(user.Email) {
			s.backfillEmailIdentityOnSuccessfulLogin(ctx, user)
		}
	}
	s.touchUserLogin(ctx, userID)
}
