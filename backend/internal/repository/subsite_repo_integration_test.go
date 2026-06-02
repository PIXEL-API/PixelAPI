//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSubsiteRepositoryRecordHeartbeatRecoversUnhealthySubsite(t *testing.T) {
	ctx := context.Background()
	repo := &subsiteRepository{db: integrationDB}
	now := time.Now().UTC()
	subsiteID := "recover-subsite-" + time.Now().UTC().Format("20060102150405.000000000")
	publicURL := "http://127.0.0.1:19081/" + subsiteID
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM subsites WHERE subsite_id = $1", subsiteID)
	})

	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO subsites (
			subsite_id, name, public_url, region, capabilities, status,
			secret_hash, secret_ciphertext, max_qps, max_concurrency,
			version, health_score, metadata, last_heartbeat_at
		)
		VALUES (
			$1, 'Recovered Subsite', $2, 'local', '[]'::jsonb, 'unhealthy',
			'hash', 'ciphertext', 10, 2, 'old', 0, '{}'::jsonb, $3
		)
	`, subsiteID, publicURL, now.Add(-time.Minute))
	require.NoError(t, err)

	err = repo.RecordHeartbeat(ctx, &service.SubsiteHeartbeat{
		SubsiteID:      subsiteID,
		Status:         service.SubsiteStatusActive,
		Version:        "new",
		ActiveRequests: 1,
		QueuedUsage:    0,
		QPS:            0.5,
		ReportedAt:     now,
		RemoteIP:       "127.0.0.1",
		Metadata:       map[string]any{"ready": true},
	})
	require.NoError(t, err)

	got, err := repo.GetBySubsiteID(ctx, subsiteID)
	require.NoError(t, err)
	require.Equal(t, service.SubsiteStatusActive, got.Status)
	require.Equal(t, 100, got.HealthScore)
	require.NotNil(t, got.LastHeartbeatAt)
	require.WithinDuration(t, now, *got.LastHeartbeatAt, time.Second)
}

func TestQuotaReservationRepositoryCreateEnforcesLeaseCapacity(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	suffix := now.Format("20060102150405.000000000")
	subsiteID := "capacity-subsite-" + suffix
	publicURL := "http://127.0.0.1:19082/" + subsiteID
	leaseID := "lease_capacity_" + suffix
	userEmail := "capacity-" + suffix + "@example.com"
	apiKey := "sk-capacity-" + suffix
	var userID, accountID, apiKeyID int64
	var groupID int64

	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM subsites WHERE subsite_id = $1", subsiteID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id = $1", groupID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE email = $1", userEmail)
	})

	err := integrationDB.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, role, status, balance, concurrency)
		VALUES ($1, 'hash', 'user', 'active', 10, 5)
		RETURNING id
	`, userEmail).Scan(&userID)
	require.NoError(t, err)
	err = integrationDB.QueryRowContext(ctx, `
		INSERT INTO accounts (name, platform, type, status, credentials, schedulable)
		VALUES ('Capacity Account', 'openai', 'apikey', 'active', '{}'::jsonb, TRUE)
		RETURNING id
	`).Scan(&accountID)
	require.NoError(t, err)
	err = integrationDB.QueryRowContext(ctx, `
		INSERT INTO groups (name, description, platform, rate_multiplier, is_exclusive, status, subscription_type)
		VALUES ($1, 'Capacity Group', 'openai', 1, FALSE, 'active', 'standard')
		RETURNING id
	`, "capacity-group-"+suffix).Scan(&groupID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO account_groups (account_id, group_id)
		VALUES ($1, $2)
	`, accountID, groupID)
	require.NoError(t, err)
	err = integrationDB.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, key, name, status)
		VALUES ($1, $2, 'Capacity Key', 'active')
		RETURNING id
	`, userID, apiKey).Scan(&apiKeyID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO subsites (
			subsite_id, name, public_url, region, capabilities, status,
			secret_hash, secret_ciphertext, max_qps, max_concurrency,
			version, health_score, metadata
		)
		VALUES (
			$1, 'Capacity Subsite', $2, 'local', '[]'::jsonb, 'active',
			'hash', 'ciphertext', 10, 1, 'test', 100, '{}'::jsonb
		)
	`, subsiteID, publicURL)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO account_leases (
			lease_id, subsite_id, account_id, group_id, platform, status, max_concurrency,
			max_requests, max_tokens, assigned_at, expires_at
		)
		VALUES ($1, $2, $3, $4, 'openai', 'active', 1, 2, 4096, $5, $6)
	`, leaseID, subsiteID, accountID, groupID, now, now.Add(time.Hour))
	require.NoError(t, err)

	repo := NewQuotaReservationRepository(integrationDB)
	first := &service.QuotaReservation{
		ReservationID:      "qres_capacity_1_" + suffix,
		RequestID:          "subreq_capacity_1_" + suffix,
		SubsiteID:          subsiteID,
		LeaseID:            leaseID,
		AccountID:          accountID,
		APIKeyID:           apiKeyID,
		UserID:             userID,
		GroupID:            &groupID,
		Platform:           service.PlatformOpenAI,
		RequestedModel:     "gpt-5.4",
		MappedModel:        "gpt-5.4",
		EstimatedCost:      0.01,
		ReservedRequests:   1,
		ReservedTokens:     1024,
		ActiveRequestUnits: 1,
		BillingType:        service.BillingTypeBalance,
		Status:             service.QuotaReservationStatusReserved,
		RequestFingerprint: "fp_capacity_1_" + suffix,
		ExpiresAt:          now.Add(10 * time.Minute),
	}
	require.NoError(t, repo.Create(ctx, first))

	second := *first
	second.ReservationID = "qres_capacity_2_" + suffix
	second.RequestID = "subreq_capacity_2_" + suffix
	second.RequestFingerprint = "fp_capacity_2_" + suffix
	err = repo.Create(ctx, &second)
	require.ErrorIs(t, err, service.ErrSubsiteLeaseCapacityExceeded)

	require.NoError(t, repo.CancelForSubsite(ctx, subsiteID, first.RequestID))
	require.NoError(t, repo.Create(ctx, &second))
}
