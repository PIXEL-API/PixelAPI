package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type accountShareBillingIntentRepository struct {
	db *sql.DB
}

var _ service.AccountShareBillingIntentRepository = (*accountShareBillingIntentRepository)(nil)

const accountShareBillingIntentStateColumns = `
	id,
	request_id,
	client_request_id,
	dispatch_id::text,
	attempt_no,
	api_key_id_snapshot,
	membership_id,
	status,
	state_token,
	attempt_count,
	lease_token,
	COALESCE(lease_owner, ''),
	lease_expires_at,
	created_at,
	updated_at`

const accountShareBillingIntentClaimColumns = `
	intent.id,
	intent.request_id,
	intent.client_request_id,
	intent.dispatch_id::text,
	intent.attempt_no,
	intent.api_key_id_snapshot,
	intent.membership_id,
	intent.status,
	intent.state_token,
	intent.attempt_count,
	intent.lease_token,
	COALESCE(intent.lease_owner, ''),
	intent.lease_expires_at,
	intent.created_at,
	intent.updated_at,
	intent.listing_id,
	intent.account_id_snapshot,
	intent.binding_id,
	intent.listing_revision_id,
	intent.terms_revision_number,
	COALESCE(intent.actor_user_id_snapshot, 0),
	intent.actor_role,
	intent.consumer_user_id_snapshot,
	intent.owner_user_id_snapshot,
	intent.command_payload::text,
	intent.command_hash,
	intent.request_fingerprint,
	intent.usage_payload::text,
	intent.usage_payload_hash,
	intent.response_summary::text`

func NewAccountShareBillingIntentRepository(db *sql.DB) service.AccountShareBillingIntentRepository {
	return &accountShareBillingIntentRepository{db: db}
}

func (r *accountShareBillingIntentRepository) validate() error {
	if r == nil || r.db == nil {
		return service.ErrServiceUnavailable
	}
	return nil
}

func (r *accountShareBillingIntentRepository) CreatePrepared(
	ctx context.Context,
	input service.CreateAccountShareBillingIntentInput,
) (*service.AccountShareBillingIntentState, bool, error) {
	if err := r.validate(); err != nil {
		return nil, false, err
	}
	prepared, err := service.PrepareAccountShareBillingIntent(input)
	if err != nil {
		return nil, false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var bindingID int64
	err = tx.QueryRowContext(ctx, `
		WITH locked_listing AS MATERIALIZED (
			SELECT listing.id
			FROM account_share_listings listing
			WHERE listing.id = $1
				AND listing.deleted_at IS NULL
				AND listing.status = 'active'
			FOR UPDATE OF listing
		),
		locked_membership AS MATERIALIZED (
			SELECT membership.id
			FROM account_share_memberships membership
			JOIN locked_listing listing
				ON listing.id = membership.listing_id
			WHERE membership.id = $2
				AND membership.api_key_id = $3
				AND membership.consumer_user_id = $4
				AND membership.account_id = $5
				AND membership.listing_revision_id = $6
				AND membership.status = 'active'
				AND membership.deleted_at IS NULL
			FOR UPDATE OF membership
		)
		SELECT binding.id
		FROM account_share_membership_account_bindings binding
		JOIN locked_membership membership
			ON membership.id = binding.membership_id
		WHERE binding.id = $7
			AND binding.listing_id = $1
			AND binding.account_id_snapshot = $5
			AND binding.listing_revision_id = $6
			AND binding.terms_revision_number = $8
			AND binding.unbound_at IS NULL
		FOR UPDATE OF binding
	`,
		prepared.ListingID,
		prepared.MembershipID,
		prepared.APIKeyID,
		prepared.ConsumerUserID,
		prepared.AccountID,
		prepared.ListingRevisionID,
		prepared.BindingID,
		prepared.TermsRevisionNumber,
	).Scan(&bindingID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, service.ErrAccountShareBillingBindingUnavailable
	}
	if err != nil {
		return nil, false, err
	}
	if bindingID != prepared.BindingID {
		return nil, false, service.ErrAccountShareBillingBindingUnavailable
	}

	actorUserID := accountShareBillingNullablePositiveID(prepared.ActorUserID)
	state, err := scanAccountShareBillingIntentState(tx.QueryRowContext(ctx, `
		INSERT INTO account_share_request_billing_intents (
			request_id,
			client_request_id,
			dispatch_id,
			attempt_no,
			api_key_id,
			api_key_id_snapshot,
			membership_id,
			listing_id,
			account_id,
			account_id_snapshot,
			binding_id,
			listing_revision_id,
			terms_revision_number,
			actor_user_id,
			actor_user_id_snapshot,
			actor_role,
			consumer_user_id,
			consumer_user_id_snapshot,
			owner_user_id,
			owner_user_id_snapshot,
			requested_model,
			routed_model,
			rate_multiplier_snapshot,
			owner_share_ratio_snapshot,
			invite_share_ratio_snapshot,
			platform_share_ratio_snapshot,
			command_schema_version,
			command_payload,
			command_hash,
			request_fingerprint
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$5,
			$6,
			$7,
			$8,
			$8,
			$9,
			$10,
			$11,
			$12,
			$12,
			$13,
			$14,
			$14,
			$15,
			$15,
			$16,
			$17,
			$18::numeric,
			$19::numeric,
			$20::numeric,
			$21::numeric,
			$22,
			$23::jsonb,
			$24,
			$25
		)
		ON CONFLICT (dispatch_id) DO NOTHING
		RETURNING `+accountShareBillingIntentStateColumns,
		prepared.RequestID,
		prepared.ClientRequestID,
		prepared.DispatchID,
		prepared.AttemptNo,
		prepared.APIKeyID,
		prepared.MembershipID,
		prepared.ListingID,
		prepared.AccountID,
		prepared.BindingID,
		prepared.ListingRevisionID,
		prepared.TermsRevisionNumber,
		actorUserID,
		prepared.ActorRole,
		prepared.ConsumerUserID,
		prepared.OwnerUserID,
		prepared.Command.RequestedModel,
		prepared.Command.RoutedModel,
		prepared.Command.RateMultiplier,
		prepared.Command.OwnerShareRatio,
		prepared.Command.InviteShareRatio,
		prepared.Command.PlatformShareRatio,
		prepared.Command.SchemaVersion,
		string(prepared.CommandJSON),
		prepared.CommandHash,
		prepared.RequestFingerprint,
	))
	if err == nil {
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return state, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	var existingFingerprint string
	state, err = scanAccountShareBillingIntentStateWithFingerprint(
		tx.QueryRowContext(ctx, `
			SELECT `+accountShareBillingIntentStateColumns+`, request_fingerprint
			FROM account_share_request_billing_intents
			WHERE dispatch_id = $1::uuid
		`, prepared.DispatchID),
		&existingFingerprint,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, service.ErrAccountShareBillingIntentNotFound
	}
	if err != nil {
		return nil, false, err
	}
	if existingFingerprint != prepared.RequestFingerprint {
		return nil, false, service.ErrAccountShareBillingIntentConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return state, false, nil
}

func (r *accountShareBillingIntentRepository) MarkInFlight(
	ctx context.Context,
	input service.AccountShareBillingIntentTransition,
) (*service.AccountShareBillingIntentState, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := service.ValidateAccountShareBillingIntentTransition(input); err != nil {
		return nil, err
	}
	state, err := scanAccountShareBillingIntentState(r.db.QueryRowContext(ctx, `
		UPDATE account_share_request_billing_intents
		SET status = 'in_flight',
			state_token = state_token + 1,
			forward_started_at = clock_timestamp(),
			updated_at = clock_timestamp()
		WHERE id = $1
			AND state_token = $2
			AND status = 'created'
			AND forward_started_at IS NULL
		RETURNING `+accountShareBillingIntentStateColumns,
		input.ID,
		input.ExpectedStateToken,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, r.classifyMutationMiss(ctx, input.ID, false)
	}
	return state, err
}

func (r *accountShareBillingIntentRepository) MarkReady(
	ctx context.Context,
	input service.MarkAccountShareBillingIntentReadyInput,
) (*service.AccountShareBillingIntentState, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	prepared, err := service.PrepareAccountShareBillingIntentReady(input)
	if err != nil {
		return nil, err
	}
	state, err := scanAccountShareBillingIntentState(r.db.QueryRowContext(ctx, `
		UPDATE account_share_request_billing_intents
		SET status = 'ready',
			state_token = state_token + 1,
			usage_schema_version = $3,
			usage_payload = $4::jsonb,
			usage_payload_hash = $5,
			response_summary = $6::jsonb,
			completed_at = clock_timestamp(),
			next_attempt_at = NULL,
			last_error_code = NULL,
			last_error_message = NULL,
			updated_at = clock_timestamp()
		WHERE id = $1
			AND state_token = $2
			AND status = 'in_flight'
			AND forward_started_at IS NOT NULL
			AND usage_payload IS NULL
		RETURNING `+accountShareBillingIntentStateColumns,
		input.ID,
		input.ExpectedStateToken,
		service.AccountShareBillingUsageSchemaV2,
		string(prepared.UsageJSON),
		prepared.UsageHash,
		string(prepared.ResponseSummaryJSON),
	))
	if errors.Is(err, sql.ErrNoRows) {
		var existingUsageHash string
		existing, loadErr := scanAccountShareBillingIntentStateWithFingerprint(
			r.db.QueryRowContext(ctx, `
				SELECT `+accountShareBillingIntentStateColumns+`, COALESCE(usage_payload_hash, '')
				FROM account_share_request_billing_intents
				WHERE id = $1
			`, input.ID),
			&existingUsageHash,
		)
		if errors.Is(loadErr, sql.ErrNoRows) {
			return nil, service.ErrAccountShareBillingIntentNotFound
		}
		if loadErr != nil {
			return nil, loadErr
		}
		switch existing.Status {
		case service.AccountShareBillingIntentStatusReady,
			service.AccountShareBillingIntentStatusProcessing,
			service.AccountShareBillingIntentStatusSettled:
			if existingUsageHash == prepared.UsageHash {
				return existing, nil
			}
			return nil, service.ErrAccountShareBillingIntentConflict
		default:
			return nil, service.ErrAccountShareBillingIntentStateConflict
		}
	}
	return state, err
}

func (r *accountShareBillingIntentRepository) CancelCreated(
	ctx context.Context,
	input service.AccountShareBillingIntentTransition,
	reasonCode string,
	reasonMessage string,
) (*service.AccountShareBillingIntentState, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	validatedTransition, validatedReasonCode, validatedReasonMessage, err :=
		service.ValidateAccountShareBillingCancellation(input, reasonCode, reasonMessage)
	if err != nil {
		return nil, err
	}
	state, err := scanAccountShareBillingIntentState(r.db.QueryRowContext(ctx, `
		UPDATE account_share_request_billing_intents
		SET status = 'cancelled',
			state_token = state_token + 1,
			completed_at = clock_timestamp(),
			last_error_code = $3,
			last_error_message = NULLIF($4, ''),
			updated_at = clock_timestamp()
		WHERE id = $1
			AND state_token = $2
			AND status = 'created'
			AND forward_started_at IS NULL
		RETURNING `+accountShareBillingIntentStateColumns,
		validatedTransition.ID,
		validatedTransition.ExpectedStateToken,
		validatedReasonCode,
		validatedReasonMessage,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, r.classifyMutationMiss(ctx, input.ID, false)
	}
	return state, err
}

func (r *accountShareBillingIntentRepository) ClaimReady(
	ctx context.Context,
	input service.ClaimAccountShareBillingIntentsInput,
) ([]service.AccountShareBillingIntentWorkItem, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	input, err := service.NormalizeAccountShareBillingClaim(input)
	if err != nil {
		return nil, err
	}
	leaseMilliseconds, err := service.AccountShareBillingLeaseMilliseconds(input.LeaseDuration)
	if err != nil {
		return nil, err
	}
	// Claiming only fences intent rows for a short lease. It deliberately does
	// not lock listing, account, membership, or wallet rows. The idempotent
	// settlement transaction must run idempotently in the global domain-lock
	// order and commit before calling MarkSettled. This SKIP LOCKED query
	// therefore cannot introduce a reverse-lock cycle with ending, rebind, or
	// delete operations.
	rows, err := r.db.QueryContext(ctx, `
		WITH ranked AS MATERIALIZED (
			SELECT
				id,
				membership_id,
				COALESCE(next_attempt_at, lease_expires_at, completed_at, created_at) AS available_at,
				request_id,
				api_key_id_snapshot,
				ROW_NUMBER() OVER (
					PARTITION BY membership_id
					ORDER BY
						COALESCE(next_attempt_at, lease_expires_at, completed_at, created_at) ASC,
						request_id ASC,
						api_key_id_snapshot ASC
				) AS membership_rank
			FROM account_share_request_billing_intents
			WHERE (
					status = 'ready'
					AND (next_attempt_at IS NULL OR next_attempt_at <= clock_timestamp())
				)
				OR (
					status = 'failed'
					AND next_attempt_at IS NOT NULL
					AND next_attempt_at <= clock_timestamp()
				)
				OR (
					status = 'processing'
					AND lease_expires_at <= clock_timestamp()
				)
		),
		candidates AS MATERIALIZED (
			SELECT intent.id
			FROM account_share_request_billing_intents AS intent
			JOIN ranked
				ON ranked.id = intent.id
			WHERE ranked.membership_rank = 1
				AND NOT EXISTS (
					SELECT 1
					FROM account_share_request_billing_intents AS active_intent
					WHERE active_intent.membership_id = ranked.membership_id
						AND active_intent.status = 'processing'
						AND active_intent.lease_expires_at > clock_timestamp()
				)
			ORDER BY
				ranked.membership_rank ASC,
				ranked.available_at ASC,
				ranked.request_id ASC,
				ranked.api_key_id_snapshot ASC
			LIMIT $1
			FOR UPDATE OF intent SKIP LOCKED
		)
		UPDATE account_share_request_billing_intents AS intent
		SET status = 'processing',
			state_token = intent.state_token + 1,
			attempt_count = intent.attempt_count + 1,
			lease_token = intent.lease_token + 1,
			lease_owner = $2,
			lease_expires_at = clock_timestamp() + ($3 * INTERVAL '1 millisecond'),
			next_attempt_at = NULL,
			updated_at = clock_timestamp()
		FROM candidates
		WHERE intent.id = candidates.id
		RETURNING `+accountShareBillingIntentClaimColumns,
		input.Limit,
		input.WorkerID,
		leaseMilliseconds,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	work := make([]service.AccountShareBillingIntentWorkItem, 0, input.Limit)
	for rows.Next() {
		item, scanErr := scanAccountShareBillingIntentWorkItem(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		work = append(work, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return work, nil
}

func (r *accountShareBillingIntentRepository) RenewProcessingLease(
	ctx context.Context,
	input service.AccountShareBillingIntentLeaseTransition,
	leaseDuration time.Duration,
) (*service.AccountShareBillingIntentState, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := service.ValidateAccountShareBillingIntentLeaseTransition(input); err != nil {
		return nil, err
	}
	leaseMilliseconds, err := service.AccountShareBillingLeaseMilliseconds(leaseDuration)
	if err != nil {
		return nil, err
	}
	state, err := scanAccountShareBillingIntentState(r.db.QueryRowContext(ctx, `
		UPDATE account_share_request_billing_intents
		SET lease_expires_at = clock_timestamp() + ($5 * INTERVAL '1 millisecond'),
			updated_at = clock_timestamp()
		WHERE id = $1
			AND state_token = $2
			AND lease_token = $3
			AND lease_owner = $4
			AND status = 'processing'
			AND lease_expires_at > clock_timestamp()
		RETURNING `+accountShareBillingIntentStateColumns,
		input.ID,
		input.ExpectedStateToken,
		input.LeaseToken,
		strings.TrimSpace(input.WorkerID),
		leaseMilliseconds,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, r.classifyMutationMiss(ctx, input.ID, true)
	}
	return state, err
}

func (r *accountShareBillingIntentRepository) MarkSettled(
	ctx context.Context,
	input service.MarkAccountShareBillingIntentSettledInput,
) (*service.AccountShareBillingIntentState, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := service.ValidateAccountShareBillingIntentLeaseTransition(input.AccountShareBillingIntentLeaseTransition); err != nil {
		return nil, err
	}
	if input.UsageLogID != nil && *input.UsageLogID <= 0 {
		return nil, fmt.Errorf("%w: usage_log_id must be positive", service.ErrAccountShareBillingIntentInvalid)
	}
	state, err := scanAccountShareBillingIntentState(r.db.QueryRowContext(ctx, `
		UPDATE account_share_request_billing_intents
		SET status = 'settled',
			state_token = state_token + 1,
			lease_owner = NULL,
			lease_expires_at = NULL,
			next_attempt_at = NULL,
			last_error_code = NULL,
			last_error_message = NULL,
			usage_log_id = $5,
			settled_at = clock_timestamp(),
			updated_at = clock_timestamp()
		WHERE id = $1
			AND state_token = $2
			AND lease_token = $3
			AND lease_owner = $4
			AND status = 'processing'
			AND lease_expires_at > clock_timestamp()
		RETURNING `+accountShareBillingIntentStateColumns,
		input.ID,
		input.ExpectedStateToken,
		input.LeaseToken,
		strings.TrimSpace(input.WorkerID),
		accountShareBillingNullableInt64(input.UsageLogID),
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, r.classifyMutationMiss(ctx, input.ID, true)
	}
	return state, err
}

func (r *accountShareBillingIntentRepository) MarkFailed(
	ctx context.Context,
	input service.MarkAccountShareBillingIntentFailedInput,
) (*service.AccountShareBillingIntentState, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	input, err := service.ValidateAccountShareBillingFailure(input)
	if err != nil {
		return nil, err
	}
	status := service.AccountShareBillingIntentStatusFailed
	if input.NeedsAttention {
		status = service.AccountShareBillingIntentStatusNeedsAttention
	}
	state, err := scanAccountShareBillingIntentState(r.db.QueryRowContext(ctx, `
		UPDATE account_share_request_billing_intents
		SET status = $6,
			state_token = state_token + 1,
			lease_owner = NULL,
			lease_expires_at = NULL,
			next_attempt_at = $7,
			last_error_code = $8,
			last_error_message = NULLIF($9, ''),
			updated_at = clock_timestamp()
		WHERE id = $1
			AND state_token = $2
			AND lease_token = $3
			AND lease_owner = $4
			AND status = $5
			AND lease_expires_at > clock_timestamp()
		RETURNING `+accountShareBillingIntentStateColumns,
		input.ID,
		input.ExpectedStateToken,
		input.LeaseToken,
		strings.TrimSpace(input.WorkerID),
		service.AccountShareBillingIntentStatusProcessing,
		status,
		accountShareBillingNullableTime(input.RetryAt),
		input.ErrorCode,
		input.ErrorMessage,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, r.classifyMutationMiss(ctx, input.ID, true)
	}
	return state, err
}

func (r *accountShareBillingIntentRepository) EscalateStaleToNeedsAttention(
	ctx context.Context,
	input service.EscalateAccountShareBillingIntentInput,
) (*service.AccountShareBillingIntentState, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	input, err := service.ValidateAccountShareBillingEscalation(input)
	if err != nil {
		return nil, err
	}
	state, err := scanAccountShareBillingIntentState(r.db.QueryRowContext(ctx, `
		UPDATE account_share_request_billing_intents
		SET status = 'needs_attention',
			state_token = state_token + 1,
			lease_owner = NULL,
			lease_expires_at = NULL,
			next_attempt_at = NULL,
			last_error_code = $3,
			last_error_message = NULLIF($4, ''),
			updated_at = clock_timestamp()
		WHERE id = $1
			AND state_token = $2
			AND status = 'in_flight'
			AND updated_at <= $5
		RETURNING `+accountShareBillingIntentStateColumns,
		input.ID,
		input.ExpectedStateToken,
		input.ReasonCode,
		input.ReasonMessage,
		input.StaleBefore,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, r.classifyMutationMiss(ctx, input.ID, false)
	}
	return state, err
}

func (r *accountShareBillingIntentRepository) CountPendingByMembership(ctx context.Context, membershipID int64) (int64, error) {
	if err := r.validate(); err != nil {
		return 0, err
	}
	if membershipID <= 0 {
		return 0, fmt.Errorf("%w: membership_id must be positive", service.ErrAccountShareBillingIntentInvalid)
	}
	var count int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)::bigint
		FROM account_share_request_billing_intents
		WHERE membership_id = $1
			AND status NOT IN ('settled', 'cancelled')
	`, membershipID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *accountShareBillingIntentRepository) ListRecoveryCandidates(
	ctx context.Context,
	input service.ListAccountShareBillingRecoveryCandidatesInput,
) ([]service.AccountShareBillingIntentAttentionCandidate, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if input.InFlightStaleBefore.IsZero() || input.CreatedStaleBefore.IsZero() {
		return nil, fmt.Errorf("%w: recovery cutoffs are required", service.ErrAccountShareBillingIntentInvalid)
	}
	if input.Limit <= 0 {
		input.Limit = service.AccountShareBillingIntentDefaultClaimLimit
	}
	if input.Limit > service.AccountShareBillingIntentMaxClaimLimit {
		input.Limit = service.AccountShareBillingIntentMaxClaimLimit
	}
	var afterUpdatedAt any
	var afterID int64
	if input.After != nil {
		if input.After.UpdatedAt.IsZero() || input.After.ID <= 0 {
			return nil, fmt.Errorf("%w: recovery cursor is invalid", service.ErrAccountShareBillingIntentInvalid)
		}
		afterUpdatedAt = input.After.UpdatedAt.UTC()
		afterID = input.After.ID
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+accountShareBillingIntentStateColumns+`,
			CASE
				WHEN status = 'created' THEN 'forward_never_started'
				ELSE 'forward_runtime_lease_expired'
			END,
			COALESCE(last_error_code, ''),
			COALESCE(last_error_message, ''),
			forward_started_at,
			completed_at,
			next_attempt_at
		FROM account_share_request_billing_intents
		WHERE (
				(status = 'created' AND updated_at <= $2)
				OR (status = 'in_flight' AND updated_at <= $1)
			)
			AND ($3::timestamptz IS NULL OR (updated_at, id) > ($3, $4))
		ORDER BY updated_at ASC, id ASC
		LIMIT $5
	`,
		input.InFlightStaleBefore.UTC(),
		input.CreatedStaleBefore.UTC(),
		afterUpdatedAt,
		afterID,
		input.Limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	candidates := make([]service.AccountShareBillingIntentAttentionCandidate, 0, input.Limit)
	for rows.Next() {
		candidate, scanErr := scanAccountShareBillingIntentAttentionCandidate(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		candidates = append(candidates, *candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (r *accountShareBillingIntentRepository) classifyMutationMiss(ctx context.Context, id int64, lease bool) error {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM account_share_request_billing_intents
			WHERE id = $1
		)
	`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return service.ErrAccountShareBillingIntentNotFound
	}
	if lease {
		return service.ErrAccountShareBillingIntentLeaseLost
	}
	return service.ErrAccountShareBillingIntentStateConflict
}

func scanAccountShareBillingIntentState(scanner sqlScanner) (*service.AccountShareBillingIntentState, error) {
	state := &service.AccountShareBillingIntentState{}
	var leaseExpiresAt sql.NullTime
	if err := scanner.Scan(
		&state.ID,
		&state.RequestID,
		&state.ClientRequestID,
		&state.DispatchID,
		&state.AttemptNo,
		&state.APIKeyID,
		&state.MembershipID,
		&state.Status,
		&state.StateToken,
		&state.AttemptCount,
		&state.LeaseToken,
		&state.LeaseOwner,
		&leaseExpiresAt,
		&state.CreatedAt,
		&state.UpdatedAt,
	); err != nil {
		return nil, err
	}
	state.LeaseExpiresAt = accountShareBillingNullTimePointer(leaseExpiresAt)
	return state, nil
}

func scanAccountShareBillingIntentStateWithFingerprint(
	scanner sqlScanner,
	fingerprint *string,
) (*service.AccountShareBillingIntentState, error) {
	state := &service.AccountShareBillingIntentState{}
	var leaseExpiresAt sql.NullTime
	if err := scanner.Scan(
		&state.ID,
		&state.RequestID,
		&state.ClientRequestID,
		&state.DispatchID,
		&state.AttemptNo,
		&state.APIKeyID,
		&state.MembershipID,
		&state.Status,
		&state.StateToken,
		&state.AttemptCount,
		&state.LeaseToken,
		&state.LeaseOwner,
		&leaseExpiresAt,
		&state.CreatedAt,
		&state.UpdatedAt,
		fingerprint,
	); err != nil {
		return nil, err
	}
	state.LeaseExpiresAt = accountShareBillingNullTimePointer(leaseExpiresAt)
	return state, nil
}

func scanAccountShareBillingIntentWorkItem(scanner sqlScanner) (*service.AccountShareBillingIntentWorkItem, error) {
	item := &service.AccountShareBillingIntentWorkItem{}
	var (
		leaseExpiresAt sql.NullTime
		commandJSON    string
		usageJSON      string
		responseJSON   string
	)
	if err := scanner.Scan(
		&item.ID,
		&item.RequestID,
		&item.ClientRequestID,
		&item.DispatchID,
		&item.AttemptNo,
		&item.APIKeyID,
		&item.MembershipID,
		&item.Status,
		&item.StateToken,
		&item.AttemptCount,
		&item.LeaseToken,
		&item.LeaseOwner,
		&leaseExpiresAt,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.ListingID,
		&item.AccountID,
		&item.BindingID,
		&item.ListingRevisionID,
		&item.TermsRevisionNumber,
		&item.ActorUserID,
		&item.ActorRole,
		&item.ConsumerUserID,
		&item.OwnerUserID,
		&commandJSON,
		&item.CommandHash,
		&item.RequestFingerprint,
		&usageJSON,
		&item.UsageHash,
		&responseJSON,
	); err != nil {
		return nil, err
	}
	item.LeaseExpiresAt = accountShareBillingNullTimePointer(leaseExpiresAt)
	command, err := service.DecodeAccountShareBillingCommand([]byte(commandJSON), item.CommandHash)
	if err != nil {
		return nil, err
	}
	usage, err := service.DecodeAccountShareBillingUsage([]byte(usageJSON), item.UsageHash)
	if err != nil {
		return nil, err
	}
	response, err := service.DecodeAccountShareBillingResponseSummary([]byte(responseJSON))
	if err != nil {
		return nil, err
	}
	item.Command = command
	item.Usage = usage
	item.ResponseSummary = response
	return item, nil
}

func scanAccountShareBillingIntentAttentionCandidate(scanner sqlScanner) (*service.AccountShareBillingIntentAttentionCandidate, error) {
	candidate := &service.AccountShareBillingIntentAttentionCandidate{}
	var (
		leaseExpiresAt   sql.NullTime
		forwardStartedAt sql.NullTime
		completedAt      sql.NullTime
		nextAttemptAt    sql.NullTime
	)
	if err := scanner.Scan(
		&candidate.ID,
		&candidate.RequestID,
		&candidate.ClientRequestID,
		&candidate.DispatchID,
		&candidate.AttemptNo,
		&candidate.APIKeyID,
		&candidate.MembershipID,
		&candidate.Status,
		&candidate.StateToken,
		&candidate.AttemptCount,
		&candidate.LeaseToken,
		&candidate.LeaseOwner,
		&leaseExpiresAt,
		&candidate.CreatedAt,
		&candidate.UpdatedAt,
		&candidate.ReasonCode,
		&candidate.LastErrorCode,
		&candidate.LastErrorMessage,
		&forwardStartedAt,
		&completedAt,
		&nextAttemptAt,
	); err != nil {
		return nil, err
	}
	candidate.LeaseExpiresAt = accountShareBillingNullTimePointer(leaseExpiresAt)
	candidate.ForwardStartedAt = accountShareBillingNullTimePointer(forwardStartedAt)
	candidate.CompletedAt = accountShareBillingNullTimePointer(completedAt)
	candidate.NextAttemptAt = accountShareBillingNullTimePointer(nextAttemptAt)
	return candidate, nil
}

func accountShareBillingNullablePositiveID(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func accountShareBillingNullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func accountShareBillingNullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func accountShareBillingNullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}
