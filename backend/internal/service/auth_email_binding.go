package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// BindEmailIdentity verifies and binds a local email/password identity to the current user.
func (s *AuthService) BindEmailIdentity(
	ctx context.Context,
	userID int64,
	email string,
	verifyCode string,
	password string,
) (*User, error) {
	if s == nil {
		return nil, ErrServiceUnavailable
	}

	normalizedEmail, err := normalizeEmailForIdentityBinding(email)
	if err != nil {
		return nil, err
	}
	if isReservedEmail(normalizedEmail) {
		return nil, ErrEmailReserved
	}
	if strings.TrimSpace(password) == "" {
		return nil, ErrPasswordRequired
	}
	if err := s.VerifyOAuthEmailCode(ctx, normalizedEmail, verifyCode); err != nil {
		return nil, err
	}

	currentUser, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	existingUser, err := s.userRepo.GetByEmail(ctx, normalizedEmail)
	switch {
	case err == nil && existingUser != nil && existingUser.ID != userID:
		return nil, ErrEmailExists
	case err != nil && !errors.Is(err, ErrUserNotFound):
		return nil, ErrServiceUnavailable
	}

	hashedPassword, err := s.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	firstRealEmailBind := !hasBindableEmailIdentitySubject(currentUser.Email)
	if firstRealEmailBind && s.entClient != nil {
		if err := s.bindEmailIdentityWithDefaultsTx(ctx, currentUser, normalizedEmail, hashedPassword); err != nil {
			return nil, err
		}
		return currentUser, nil
	}

	currentUser.Email = normalizedEmail
	currentUser.PasswordHash = hashedPassword
	if err := s.userRepo.Update(ctx, currentUser); err != nil {
		if errors.Is(err, ErrEmailExists) {
			return nil, ErrEmailExists
		}
		return nil, ErrServiceUnavailable
	}

	if firstRealEmailBind {
		if err := s.ApplyProviderDefaultSettingsOnFirstBind(ctx, userID, "email"); err != nil {
			return nil, fmt.Errorf("apply email first bind defaults: %w", err)
		}
	}

	return currentUser, nil
}

// SendEmailIdentityBindCode sends a verification code for authenticated email binding flows.
func (s *AuthService) SendEmailIdentityBindCode(ctx context.Context, userID int64, email string) error {
	if s == nil {
		return ErrServiceUnavailable
	}

	normalizedEmail, err := normalizeEmailForIdentityBinding(email)
	if err != nil {
		return err
	}
	if isReservedEmail(normalizedEmail) {
		return ErrEmailReserved
	}
	if s.emailService == nil {
		return ErrServiceUnavailable
	}
	if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return ErrUserNotFound
		}
		return ErrServiceUnavailable
	}

	existingUser, err := s.userRepo.GetByEmail(ctx, normalizedEmail)
	switch {
	case err == nil && existingUser != nil && existingUser.ID != userID:
		return ErrEmailExists
	case err != nil && !errors.Is(err, ErrUserNotFound):
		return ErrServiceUnavailable
	}

	siteName := "Sub2API"
	if s.settingService != nil {
		siteName = s.settingService.GetSiteName(ctx)
	}
	return s.emailService.SendVerifyCode(ctx, normalizedEmail, siteName)
}

func normalizeEmailForIdentityBinding(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" || len(normalized) > 255 {
		return "", infraerrors.BadRequest("INVALID_EMAIL", "invalid email")
	}
	if _, err := mail.ParseAddress(normalized); err != nil {
		return "", infraerrors.BadRequest("INVALID_EMAIL", "invalid email")
	}
	return normalized, nil
}

func hasBindableEmailIdentitySubject(email string) bool {
	normalized := strings.ToLower(strings.TrimSpace(email))
	return normalized != "" && !isReservedEmail(normalized)
}

func (s *AuthService) bindEmailIdentityWithDefaultsTx(
	ctx context.Context,
	currentUser *User,
	email string,
	hashedPassword string,
) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return s.bindEmailIdentityWithDefaults(ctx, tx.Client(), currentUser, email, hashedPassword)
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return ErrServiceUnavailable
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := s.bindEmailIdentityWithDefaults(txCtx, tx.Client(), currentUser, email, hashedPassword); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return ErrServiceUnavailable
	}
	return nil
}

func (s *AuthService) bindEmailIdentityWithDefaults(
	ctx context.Context,
	client *dbent.Client,
	currentUser *User,
	email string,
	hashedPassword string,
) error {
	if client == nil || currentUser == nil || currentUser.ID <= 0 {
		return ErrServiceUnavailable
	}

	oldEmail := currentUser.Email
	if _, err := client.User.UpdateOneID(currentUser.ID).
		SetEmail(email).
		SetPasswordHash(hashedPassword).
		Save(ctx); err != nil {
		if dbent.IsConstraintError(err) {
			return ErrEmailExists
		}
		return ErrServiceUnavailable
	}

	if err := replaceBoundEmailAuthIdentityWithClient(ctx, client, currentUser.ID, oldEmail, email, "auth_service_email_bind"); err != nil {
		if errors.Is(err, ErrEmailExists) {
			return ErrEmailExists
		}
		return ErrServiceUnavailable
	}

	if err := s.ApplyProviderDefaultSettingsOnFirstBind(ctx, currentUser.ID, "email"); err != nil {
		return fmt.Errorf("apply email first bind defaults: %w", err)
	}

	updatedUser, err := client.User.Get(ctx, currentUser.ID)
	if err != nil {
		return ErrServiceUnavailable
	}
	currentUser.Email = updatedUser.Email
	currentUser.PasswordHash = updatedUser.PasswordHash
	currentUser.Balance = updatedUser.Balance
	currentUser.Concurrency = updatedUser.Concurrency
	currentUser.UpdatedAt = updatedUser.UpdatedAt
	return nil
}

func replaceBoundEmailAuthIdentityWithClient(
	ctx context.Context,
	client *dbent.Client,
	userID int64,
	oldEmail string,
	newEmail string,
	source string,
) error {
	newSubject := normalizeBoundEmailAuthIdentitySubject(newEmail)
	if err := ensureBoundEmailAuthIdentityWithClient(ctx, client, userID, newSubject, source); err != nil {
		return err
	}

	oldSubject := normalizeBoundEmailAuthIdentitySubject(oldEmail)
	if oldSubject == "" || oldSubject == newSubject {
		return nil
	}

	_, err := client.AuthIdentity.Delete().
		Where(
			authidentity.UserIDEQ(userID),
			authidentity.ProviderTypeEQ("email"),
			authidentity.ProviderKeyEQ("email"),
			authidentity.ProviderSubjectEQ(oldSubject),
		).
		Exec(ctx)
	return err
}

func ensureBoundEmailAuthIdentityWithClient(
	ctx context.Context,
	client *dbent.Client,
	userID int64,
	subject string,
	source string,
) error {
	if client == nil || userID <= 0 || subject == "" {
		return nil
	}

	if strings.TrimSpace(source) == "" {
		source = "auth_service_email_bind"
	}

	if err := client.AuthIdentity.Create().
		SetUserID(userID).
		SetProviderType("email").
		SetProviderKey("email").
		SetProviderSubject(subject).
		SetVerifiedAt(time.Now().UTC()).
		SetMetadata(map[string]any{"source": strings.TrimSpace(source)}).
		OnConflictColumns(
			authidentity.FieldProviderType,
			authidentity.FieldProviderKey,
			authidentity.FieldProviderSubject,
		).
		DoNothing().
		Exec(ctx); err != nil {
		return err
	}

	identity, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ("email"),
			authidentity.ProviderKeyEQ("email"),
			authidentity.ProviderSubjectEQ(subject),
		).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if identity.UserID != userID {
		return ErrEmailExists
	}
	return nil
}

func normalizeBoundEmailAuthIdentitySubject(email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" || isReservedEmail(normalized) {
		return ""
	}
	return normalized
}
