package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestSubsiteServiceResetSecretRotatesSecret(t *testing.T) {
	ctx := context.Background()
	repo := newMemorySubsiteRepo()
	encryptor := plainSubsiteEncryptor{}
	svc := NewSubsiteService(repo, encryptor)

	created, err := svc.Create(ctx, CreateSubsiteInput{
		SubsiteID: "site_1",
		Name:      "Site 1",
		PublicURL: "http://127.0.0.1:18081",
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.Secret)
	oldSecret := created.Secret

	reset, err := svc.ResetSecret(ctx, "site_1")
	require.NoError(t, err)
	require.NotEmpty(t, reset.Secret)
	require.NotEqual(t, oldSecret, reset.Secret)
	require.Equal(t, "site_1", reset.Subsite.SubsiteID)

	stored, err := repo.GetBySubsiteID(ctx, "site_1")
	require.NoError(t, err)
	require.Equal(t, hashSubsiteSecret(reset.Secret), stored.SecretHash)
	require.Equal(t, "enc:"+reset.Secret, stored.SecretCiphertext)

	auth := NewSubsiteAuthService(svc, newMemoryNonceStore())
	require.ErrorIs(t, auth.Verify(ctx, signedSubsiteTestRequest(oldSecret, "site_1", "nonce-old")), ErrSubsiteAuthInvalid)
	require.NoError(t, auth.Verify(ctx, signedSubsiteTestRequest(reset.Secret, "site_1", "nonce-new")))
}

func signedSubsiteTestRequest(secret, subsiteID, nonce string) SubsiteSignedRequest {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	bodyHash := sha256.Sum256(nil)
	bodySHA := hex.EncodeToString(bodyHash[:])
	return SubsiteSignedRequest{
		SubsiteID:  subsiteID,
		Method:     "POST",
		Path:       "/api/internal/subsites/heartbeat",
		Timestamp:  timestamp,
		Nonce:      nonce,
		BodySHA256: bodySHA,
		Signature:  SignSubsiteRequest(secret, "POST", "/api/internal/subsites/heartbeat", timestamp, nonce, bodySHA),
	}
}

type plainSubsiteEncryptor struct{}

func (plainSubsiteEncryptor) Encrypt(plaintext string) (string, error) {
	return "enc:" + plaintext, nil
}

func (plainSubsiteEncryptor) Decrypt(ciphertext string) (string, error) {
	if len(ciphertext) < 4 || ciphertext[:4] != "enc:" {
		return "", ErrSubsiteAuthInvalid
	}
	return ciphertext[4:], nil
}

type memorySubsiteRepo struct {
	subsites map[string]*Subsite
}

func newMemorySubsiteRepo() *memorySubsiteRepo {
	return &memorySubsiteRepo{subsites: map[string]*Subsite{}}
}

func (r *memorySubsiteRepo) Create(_ context.Context, subsite *Subsite) error {
	cp := *subsite
	r.subsites[subsite.SubsiteID] = &cp
	return nil
}

func (r *memorySubsiteRepo) GetBySubsiteID(_ context.Context, subsiteID string) (*Subsite, error) {
	subsite, ok := r.subsites[subsiteID]
	if !ok {
		return nil, ErrSubsiteNotFound
	}
	cp := *subsite
	return &cp, nil
}

func (r *memorySubsiteRepo) List(context.Context, pagination.PaginationParams, ListSubsitesFilter) ([]Subsite, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (r *memorySubsiteRepo) Update(_ context.Context, subsite *Subsite) error {
	if _, ok := r.subsites[subsite.SubsiteID]; !ok {
		return ErrSubsiteNotFound
	}
	cp := *subsite
	r.subsites[subsite.SubsiteID] = &cp
	return nil
}

func (r *memorySubsiteRepo) UpdateStatus(_ context.Context, subsiteID, status string) error {
	subsite, ok := r.subsites[subsiteID]
	if !ok {
		return ErrSubsiteNotFound
	}
	subsite.Status = status
	return nil
}

func (r *memorySubsiteRepo) UpdateSecret(_ context.Context, subsiteID, secretHash, secretCiphertext string) error {
	subsite, ok := r.subsites[subsiteID]
	if !ok {
		return ErrSubsiteNotFound
	}
	subsite.SecretHash = secretHash
	subsite.SecretCiphertext = secretCiphertext
	subsite.UpdatedAt = time.Now()
	return nil
}

func (r *memorySubsiteRepo) RecordHeartbeat(context.Context, *SubsiteHeartbeat) error {
	panic("unexpected RecordHeartbeat call")
}

func (r *memorySubsiteRepo) MarkHeartbeatTimeouts(context.Context, time.Time) (int64, error) {
	panic("unexpected MarkHeartbeatTimeouts call")
}

type memoryNonceStore struct {
	claimed map[string]struct{}
}

func newMemoryNonceStore() *memoryNonceStore {
	return &memoryNonceStore{claimed: map[string]struct{}{}}
}

func (s *memoryNonceStore) Claim(_ context.Context, subsiteID, nonce string, _ time.Duration) (bool, error) {
	key := subsiteID + ":" + nonce
	if _, ok := s.claimed[key]; ok {
		return false, nil
	}
	s.claimed[key] = struct{}{}
	return true, nil
}
