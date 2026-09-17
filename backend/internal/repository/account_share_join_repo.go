package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

func ensureAccountShareAccountIdentityInTx(ctx context.Context, tx *sql.Tx, account *service.Account) (*int64, error) {
	if tx == nil || account == nil || account.ID <= 0 {
		return nil, nil
	}
	email := accountShareAccountIdentityEmail(account)
	if email == "" {
		return nil, nil
	}
	platform := strings.ToLower(strings.TrimSpace(account.Platform))
	if platform == "" {
		return nil, nil
	}
	var identityID int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO account_share_account_identities (
			platform, identity_type, identity_value, identity_hint,
			first_account_id, last_account_id, created_at, updated_at
		)
		VALUES ($1, 'email', $2, $3, $4, $4, NOW(), NOW())
		ON CONFLICT (platform, identity_type, identity_value) WHERE deleted_at IS NULL
		DO UPDATE SET
			identity_hint = EXCLUDED.identity_hint,
			last_account_id = EXCLUDED.last_account_id,
			updated_at = NOW()
		RETURNING id
	`, platform, email, accountShareIdentityHint(email), account.ID).Scan(&identityID)
	if err != nil {
		return nil, err
	}
	return &identityID, nil
}

func accountShareAccountIdentityEmail(account *service.Account) string {
	if account == nil {
		return ""
	}
	for _, value := range []string{
		accountShareStringFromMap(account.Credentials, "email"),
		accountShareStringFromMap(account.Credentials, "email_address"),
		accountShareStringFromMap(account.Extra, "email"),
		accountShareStringFromMap(account.Extra, "email_address"),
	} {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			return value
		}
	}
	return ""
}

func accountShareStringFromMap(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func accountShareIdentityHint(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ""
	}
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return ""
	}
	return service.MaskEmailIdentity(email)
}

func normalizeAccountShareListingTermsAliases(terms *service.AccountShareListingTermsSnapshot) {
	if terms == nil {
		return
	}
	// Older immutable membership snapshots predate the explicit Anthropic
	// aliases. Both providers share the same persisted quota threshold
	// columns, so hydrate the aliases without changing the contract value.
	if terms.Anthropic5hLimitPercent <= 0 {
		terms.Anthropic5hLimitPercent = terms.Codex5hLimitPercent
	}
	if terms.Anthropic7dLimitPercent <= 0 {
		terms.Anthropic7dLimitPercent = terms.Codex7dLimitPercent
	}
}

func loadAndValidateAccountShareMembershipTermsSnapshotInTx(
	ctx context.Context,
	tx *sql.Tx,
	membership *service.AccountShareMembership,
) error {
	if tx == nil || membership == nil || membership.ID <= 0 || membership.ListingID <= 0 ||
		(membership.Status != service.AccountShareMembershipStatusActive &&
			membership.Status != service.AccountShareMembershipStatusQueued) {
		return fmt.Errorf(
			"%w: invalid membership terms snapshot input",
			service.ErrAccountShareBillingBindingUnavailable,
		)
	}
	if err := loadAccountShareMembershipTraceSnapshotInTx(ctx, tx, membership); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf(
				"%w: membership %d immutable snapshot is missing",
				service.ErrAccountShareBillingBindingUnavailable,
				membership.ID,
			)
		}
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
			return fmt.Errorf(
				"%w: membership %d immutable terms snapshot is malformed: %v",
				service.ErrAccountShareBillingBindingUnavailable,
				membership.ID,
				err,
			)
		}
		return err
	}

	terms := membership.TermsSnapshot
	if membership.ListingRevisionID == nil || *membership.ListingRevisionID <= 0 ||
		membership.ListingVersionSnapshot == nil || *membership.ListingVersionSnapshot <= 0 ||
		terms == nil ||
		terms.ListingRevisionID != *membership.ListingRevisionID ||
		terms.RowVersion != *membership.ListingVersionSnapshot ||
		terms.SchemaVersion <= 0 {
		return fmt.Errorf(
			"%w: membership %d immutable terms snapshot does not match its listing revision",
			service.ErrAccountShareBillingBindingUnavailable,
			membership.ID,
		)
	}
	return nil
}

func validateAccountShareMembershipTermsRevisionInTx(
	ctx context.Context,
	tx *sql.Tx,
	membership *service.AccountShareMembership,
) error {
	if tx == nil || membership == nil || membership.TermsSnapshot == nil ||
		membership.ListingRevisionID == nil || membership.ListingVersionSnapshot == nil {
		return fmt.Errorf(
			"%w: membership immutable terms revision is unavailable",
			service.ErrAccountShareBillingBindingUnavailable,
		)
	}
	terms := membership.TermsSnapshot
	revision, err := loadAccountShareListingRevisionSnapshotInTx(
		ctx,
		tx,
		membership.ListingID,
		*membership.ListingRevisionID,
	)
	if errors.Is(err, service.ErrAccountShareListingNotFound) {
		return fmt.Errorf(
			"%w: membership %d immutable listing revision is missing",
			service.ErrAccountShareBillingBindingUnavailable,
			membership.ID,
		)
	}
	if err != nil {
		return err
	}
	if !accountShareMembershipTermsMatchRevision(terms, revision) {
		return fmt.Errorf(
			"%w: membership %d immutable terms do not match the listing revision",
			service.ErrAccountShareBillingBindingUnavailable,
			membership.ID,
		)
	}
	return nil
}

func accountShareMembershipTermsMatchRevision(
	terms *service.AccountShareListingTermsSnapshot,
	revision *accountShareListingRevisionSnapshot,
) bool {
	if terms == nil || revision == nil {
		return false
	}
	return terms.ListingRevisionID == revision.ID &&
		terms.RowVersion == revision.RowVersion &&
		terms.SchemaVersion == revision.SchemaVersion &&
		terms.RoomName == revision.RoomName &&
		terms.Status == revision.Status &&
		terms.SeatLimit == revision.SeatLimit &&
		terms.RateMultiplier == revision.RateMultiplier &&
		equalNormalizedAccountShareModels(terms.AllowedModels, revision.AllowedModels) &&
		terms.PerUserConcurrency == revision.PerUserConcurrency &&
		terms.HourlyRate == revision.HourlyRate &&
		terms.HourlyFeeWaiverMinimum == revision.HourlyFeeWaiverMinimum &&
		terms.MinBalanceRequired == revision.MinBalanceRequired &&
		terms.CodexCLIOnly == revision.CodexCLIOnly &&
		terms.Codex5hLimitPercent == revision.Codex5hLimitPercent &&
		terms.Codex7dLimitPercent == revision.Codex7dLimitPercent &&
		terms.Anthropic5hLimitPercent == revision.Codex5hLimitPercent &&
		terms.Anthropic7dLimitPercent == revision.Codex7dLimitPercent
}

func validateAccountShareMembershipOpenRuntimeBindingInTx(
	ctx context.Context,
	tx *sql.Tx,
	membership *service.AccountShareMembership,
) error {
	if tx == nil || membership == nil || membership.ID <= 0 ||
		membership.ListingID <= 0 || membership.AccountID <= 0 ||
		membership.Status != service.AccountShareMembershipStatusActive ||
		membership.TermsSnapshot == nil || membership.ListingRevisionID == nil {
		return fmt.Errorf(
			"%w: invalid active membership runtime binding input",
			service.ErrAccountShareBillingBindingUnavailable,
		)
	}

	terms := membership.TermsSnapshot
	var bindingRevisionID, termsRevisionNumber int64
	err := tx.QueryRowContext(ctx, `
		SELECT
			binding.listing_revision_id,
			binding.terms_revision_number
		FROM account_share_memberships current_membership
		JOIN account_share_membership_account_bindings binding
			ON binding.membership_id = current_membership.id
			AND binding.listing_id = current_membership.listing_id
			AND binding.account_id = current_membership.account_id
			AND binding.account_id_snapshot = current_membership.account_id
			AND binding.listing_revision_id = current_membership.listing_revision_id
			AND binding.unbound_at IS NULL
		JOIN account_share_listing_revisions revision
			ON revision.listing_id = binding.listing_id
			AND revision.id = binding.listing_revision_id
			AND revision.revision_number = binding.terms_revision_number
		WHERE current_membership.id = $1
			AND current_membership.listing_id = $2
			AND current_membership.account_id = $3
			AND current_membership.listing_revision_id = $4
			AND current_membership.status = $5
			AND current_membership.deleted_at IS NULL
	`,
		membership.ID,
		membership.ListingID,
		membership.AccountID,
		*membership.ListingRevisionID,
		service.AccountShareMembershipStatusActive,
	).Scan(&bindingRevisionID, &termsRevisionNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf(
			"%w: membership %d has no matching open immutable binding",
			service.ErrAccountShareBillingBindingUnavailable,
			membership.ID,
		)
	}
	if err != nil {
		return err
	}
	if bindingRevisionID != terms.ListingRevisionID ||
		termsRevisionNumber != terms.RowVersion {
		return fmt.Errorf(
			"%w: membership %d binding revision does not match its immutable terms snapshot",
			service.ErrAccountShareBillingBindingUnavailable,
			membership.ID,
		)
	}
	return nil
}

func loadAndValidateAccountShareMembershipRuntimeSnapshotInTx(
	ctx context.Context,
	tx *sql.Tx,
	membership *service.AccountShareMembership,
) error {
	if err := loadAndValidateAccountShareMembershipTermsSnapshotInTx(ctx, tx, membership); err != nil {
		return err
	}
	if err := validateAccountShareMembershipTermsRevisionInTx(ctx, tx, membership); err != nil {
		return err
	}
	return validateAccountShareMembershipOpenRuntimeBindingInTx(ctx, tx, membership)
}

func applyAccountShareMembershipRuntimeTerms(
	membership *service.AccountShareMembership,
	listing *service.AccountShareListing,
) error {
	if membership == nil || listing == nil || membership.TermsSnapshot == nil ||
		membership.ListingRevisionID == nil ||
		membership.TermsSnapshot.ListingRevisionID != *membership.ListingRevisionID {
		return fmt.Errorf(
			"%w: immutable membership terms cannot be applied to the runtime listing",
			service.ErrAccountShareBillingBindingUnavailable,
		)
	}
	terms := membership.TermsSnapshot
	listing.RateMultiplier = terms.RateMultiplier
	listing.AllowedModels = append([]string(nil), terms.AllowedModels...)
	listing.PerUserConcurrency = terms.PerUserConcurrency
	listing.HourlyRate = terms.HourlyRate
	listing.HourlyFeeWaiverMinimum = terms.HourlyFeeWaiverMinimum
	listing.MinBalanceRequired = terms.MinBalanceRequired
	listing.CodexCLIOnly = terms.CodexCLIOnly
	listing.Codex5hLimitPercent = terms.Codex5hLimitPercent
	listing.Codex7dLimitPercent = terms.Codex7dLimitPercent
	listing.Anthropic5hLimitPercent = terms.Anthropic5hLimitPercent
	listing.Anthropic7dLimitPercent = terms.Anthropic7dLimitPercent
	return nil
}

func ensureAccountShareMembershipBindingAssignmentInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
	accountID int64,
) error {
	snapshot := accountShareRoomAssignmentSnapshot{}
	var projectionCreatedAt time.Time
	err := tx.QueryRowContext(ctx, `
		SELECT
			room_account.listing_id,
			room_account.account_id,
			room_account.owner_user_id,
			account.name,
			account.platform,
			account.account_level,
			account.concurrency,
			room_account.created_at
		FROM account_share_room_accounts room_account
		JOIN accounts account
			ON account.id = room_account.account_id
			AND account.owner_user_id = room_account.owner_user_id
			AND account.deleted_at IS NULL
		WHERE room_account.listing_id = $1
			AND room_account.account_id = $2
			AND room_account.state = 'active'
		FOR UPDATE OF room_account, account
	`, listingID, accountID).Scan(
		&snapshot.ListingID,
		&snapshot.AccountID,
		&snapshot.OwnerUserID,
		&snapshot.AccountName,
		&snapshot.Platform,
		&snapshot.AccountLevel,
		&snapshot.ConfiguredConcurrency,
		&projectionCreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf(
			"account share membership cannot bind account %d: active room projection is missing",
			accountID,
		)
	}
	if err != nil {
		return err
	}
	if projectionCreatedAt.IsZero() {
		return fmt.Errorf(
			"account share room account %d in listing %d has no trustworthy projection timestamp",
			accountID,
			listingID,
		)
	}
	snapshot.Platform = strings.ToLower(strings.TrimSpace(snapshot.Platform))
	snapshot.AccountLevel = service.NormalizeAccountLevel(snapshot.AccountLevel)

	assignments, err := lockAccountShareRoomOpenAssignmentsInTx(ctx, tx, []int64{accountID})
	if err != nil {
		return err
	}
	if assignment, hasAssignment := assignments[accountID]; hasAssignment {
		if assignment.ListingID != listingID {
			return service.ErrAccountShareRoomAccountConflict
		}
		return nil
	}
	_, err = insertBackfilledAccountShareRoomAssignmentInTx(
		ctx,
		tx,
		snapshot,
		projectionCreatedAt,
	)
	return err
}

func (r *accountShareModeRepository) createAccountShareMembershipBindingInTx(
	ctx context.Context,
	tx *sql.Tx,
	membershipID int64,
	listingID int64,
	accountID int64,
	listingRevisionID int64,
	boundByUserID int64,
	boundByRole string,
	bindReason string,
	boundAt time.Time,
) (int64, int64, error) {
	boundByRole = strings.TrimSpace(boundByRole)
	bindReason = strings.TrimSpace(bindReason)
	if r == nil || tx == nil ||
		membershipID <= 0 || listingID <= 0 || accountID <= 0 || listingRevisionID <= 0 ||
		!accountShareBindingActorRoleValid(boundByRole) || bindReason == "" || boundAt.IsZero() {
		return 0, 0, fmt.Errorf("invalid account-share membership binding input")
	}
	if err := ensureAccountShareMembershipBindingAssignmentInTx(
		ctx,
		tx,
		listingID,
		accountID,
	); err != nil {
		return 0, 0, err
	}

	var bindingID, routingGeneration int64
	err := tx.QueryRowContext(ctx, `
		WITH binding_source AS MATERIALIZED (
			SELECT
				assignment.id AS room_account_assignment_id,
				assignment.account_name_snapshot,
				assignment.platform_snapshot,
				assignment.account_level_snapshot,
				assignment.configured_concurrency_snapshot,
				assignment.snapshot_quality,
				revision.revision_number AS terms_revision_number
			FROM account_share_memberships membership
			JOIN account_share_room_accounts room_account
				ON room_account.listing_id = membership.listing_id
				AND room_account.account_id = membership.account_id
				AND room_account.state = 'active'
			JOIN account_share_room_account_assignments assignment
				ON assignment.listing_id = room_account.listing_id
				AND assignment.account_id_snapshot = room_account.account_id
				AND assignment.account_id = room_account.account_id
				AND assignment.detached_at IS NULL
			JOIN accounts bound_account
				ON bound_account.id = room_account.account_id
				AND bound_account.deleted_at IS NULL
			JOIN account_share_listing_revisions revision
				ON revision.listing_id = membership.listing_id
				AND revision.id = membership.listing_revision_id
			WHERE membership.id = $1
				AND membership.listing_id = $2
				AND membership.account_id = $3
				AND membership.listing_revision_id = $4
				AND membership.status IN ('active', 'ending')
				AND membership.deleted_at IS NULL
			FOR UPDATE OF assignment
		),
		next_generation AS MATERIALIZED (
			SELECT COALESCE(MAX(binding.routing_generation), 0) + 1 AS routing_generation
			FROM account_share_membership_account_bindings binding
			WHERE binding.membership_id = $1
		)
		INSERT INTO account_share_membership_account_bindings (
			membership_id, listing_id, account_id, account_id_snapshot,
			room_account_assignment_id, listing_revision_id, terms_revision_number,
			account_name_snapshot, platform_snapshot, account_level_snapshot,
			configured_concurrency_snapshot, routing_generation,
			bound_at, bound_by_user_id, bound_by_role, bind_reason,
			snapshot_quality, created_at
		)
		SELECT
			$1, $2, $3, $3,
			source.room_account_assignment_id, $4, source.terms_revision_number,
			source.account_name_snapshot, source.platform_snapshot, source.account_level_snapshot,
			source.configured_concurrency_snapshot, generation.routing_generation,
			$5, $6, $7, $8,
			source.snapshot_quality, $5
		FROM binding_source source
		CROSS JOIN next_generation generation
		RETURNING id, routing_generation
	`,
		membershipID,
		listingID,
		accountID,
		listingRevisionID,
		boundAt.UTC(),
		nullablePositiveInt64(boundByUserID),
		boundByRole,
		bindReason,
	).Scan(&bindingID, &routingGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, fmt.Errorf(
			"account share membership %d cannot bind account %d: active assignment or revision snapshot is missing",
			membershipID,
			accountID,
		)
	}
	if err != nil {
		return 0, 0, err
	}
	if bindingID <= 0 || routingGeneration <= 0 {
		return 0, 0, fmt.Errorf(
			"account share membership %d produced invalid binding id=%d generation=%d",
			membershipID,
			bindingID,
			routingGeneration,
		)
	}
	return bindingID, routingGeneration, nil
}

func (r *accountShareModeRepository) closeAccountShareMembershipBindingInTx(
	ctx context.Context,
	tx *sql.Tx,
	membershipID int64,
	unboundByUserID int64,
	unboundByRole string,
	unbindReason string,
	unboundAt time.Time,
) (bool, error) {
	unboundByRole = strings.TrimSpace(unboundByRole)
	unbindReason = strings.TrimSpace(unbindReason)
	if r == nil || tx == nil || membershipID <= 0 ||
		!accountShareBindingActorRoleValid(unboundByRole) || unbindReason == "" || unboundAt.IsZero() {
		return false, fmt.Errorf("invalid account-share membership unbinding input")
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE account_share_membership_account_bindings
		SET unbound_at = $1,
			unbound_by_user_id = $2,
			unbound_by_role = $3,
			unbind_reason = $4
		WHERE membership_id = $5
			AND unbound_at IS NULL
	`,
		unboundAt.UTC(),
		nullablePositiveInt64(unboundByUserID),
		unboundByRole,
		unbindReason,
		membershipID,
	)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 1 {
		return false, fmt.Errorf(
			"close account share membership %d binding affected %d rows",
			membershipID,
			affected,
		)
	}
	return affected == 1, nil
}

func accountShareBindingActorRoleValid(role string) bool {
	switch strings.TrimSpace(role) {
	case "owner", "consumer", "admin", "system":
		return true
	default:
		return false
	}
}

func (r *accountShareModeRepository) FindMembershipByJoinIntent(ctx context.Context, consumerUserID, listingID, apiKeyID int64, nonce string) (*service.AccountShareMembership, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	membership, err := findAccountShareMembershipByJoinIntentInTx(ctx, tx, consumerUserID, listingID, apiKeyID, nonce)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return membership, nil
}

func findAccountShareMembershipByJoinIntentInTx(ctx context.Context, tx *sql.Tx, consumerUserID, listingID, apiKeyID int64, nonce string) (*service.AccountShareMembership, error) {
	membership, err := scanAccountShareMembership(tx.QueryRowContext(ctx, `
		SELECT
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		WHERE m.consumer_user_id = $1
			AND m.listing_id = $2
			AND m.api_key_id = $3
			AND m.join_intent_nonce = $4
	`, consumerUserID, listingID, apiKeyID, strings.TrimSpace(nonce)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	switch membership.Status {
	case service.AccountShareMembershipStatusActive:
		if err := loadAccountShareMembershipTraceSnapshotInTx(ctx, tx, membership); err != nil {
			return nil, err
		}
		return membership, nil
	case service.AccountShareMembershipStatusEnding:
		return nil, service.ErrAccountShareMembershipEnding
	default:
		return nil, service.ErrAccountShareJoinIntentConsumed
	}
}

func (r *accountShareModeRepository) JoinListing(ctx context.Context, input service.AccountShareJoinRepositoryInput) (*service.AccountShareMembership, error) {
	consumerUserID := input.ConsumerUserID
	apiKeyID := input.APIKeyID
	listingID := input.ListingID
	idleTimeoutMinutes := input.IdleTimeoutMinutes
	if consumerUserID <= 0 || apiKeyID <= 0 || listingID <= 0 || idleTimeoutMinutes <= 0 {
		return nil, service.ErrAccountShareJoinIntentInvalid
	}
	if input.ExpectedVersion <= 0 ||
		input.ExpectedRevisionID <= 0 ||
		input.AcceptedTerms == nil ||
		input.IntentIssuedAt.IsZero() ||
		strings.TrimSpace(input.IntentNonce) == "" {
		return nil, service.ErrAccountShareJoinIntentInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var accountID, ownerUserID int64
	var status string
	var seatLimit int
	var hourlyRate, hourlyFeeWaiverMinimum, minBalanceRequired float64
	var apiKeyName string
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT a.id, l.owner_user_id, l.status, l.seat_limit, l.hourly_rate, l.hourly_fee_waiver_minimum, l.min_balance_required
		FROM account_share_listings l
		%s
		WHERE l.id = $1
			AND l.deleted_at IS NULL
		FOR UPDATE OF l
	`, accountShareRoomRepresentativeJoinSQL("NOW()")), listingID).Scan(
		&accountID,
		&ownerUserID,
		&status,
		&seatLimit,
		&hourlyRate,
		&hourlyFeeWaiverMinimum,
		&minBalanceRequired,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	apiKeyName, err = lockAccountShareJoinAPIKeyInTx(ctx, tx, apiKeyID, consumerUserID)
	if err != nil {
		return nil, err
	}
	// The listing and API key locks serialize concurrent completions of one intent.
	if membership, replayErr := findAccountShareMembershipByJoinIntentInTx(ctx, tx, consumerUserID, listingID, apiKeyID, input.IntentNonce); membership != nil || replayErr != nil {
		return membership, replayErr
	}
	ownerSelfUse := ownerUserID == consumerUserID
	if status != service.AccountShareListingStatusActive {
		return nil, service.ErrAccountShareListingNotActive
	}
	revisionID, listingVersion, err := ensureAccountShareListingRevisionInTx(ctx, tx, listingID)
	if err != nil {
		return nil, err
	}
	revision, err := loadAccountShareListingRevisionSnapshotInTx(ctx, tx, listingID, revisionID)
	if err != nil {
		return nil, err
	}
	if input.ExpectedVersion > 0 && listingVersion != input.ExpectedVersion {
		return nil, service.ErrAccountShareJoinTermsChanged.WithMetadata(map[string]string{
			"expected_version": strconv.FormatInt(input.ExpectedVersion, 10),
			"actual_version":   strconv.FormatInt(listingVersion, 10),
		})
	}
	if input.ExpectedRevisionID > 0 && revisionID != input.ExpectedRevisionID {
		return nil, service.ErrAccountShareJoinTermsChanged.WithMetadata(map[string]string{
			"expected_revision_id": strconv.FormatInt(input.ExpectedRevisionID, 10),
			"actual_revision_id":   strconv.FormatInt(revisionID, 10),
		})
	}
	if input.AcceptedTerms != nil && !accountShareMembershipTermsMatchRevision(input.AcceptedTerms, revision) {
		return nil, service.ErrAccountShareJoinTermsChanged
	}
	termsSnapshot := revision.termsSnapshot()
	termsSnapshotJSON, err := json.Marshal(termsSnapshot)
	if err != nil {
		return nil, err
	}
	seatLimit = revision.SeatLimit
	hourlyRate = revision.HourlyRate
	hourlyFeeWaiverMinimum = revision.HourlyFeeWaiverMinimum
	minBalanceRequired = revision.MinBalanceRequired
	now := time.Now().UTC()
	if ownerSelfUse {
		hourlyRate = 0
		hourlyFeeWaiverMinimum = 0
	}
	prepayDuration := service.AccountShareModeSeatPrepayDuration
	prepayAmount := accountShareSeatCharge(hourlyRate, prepayDuration)
	paidUntil := now.Add(prepayDuration)
	var userBalance decimal.Decimal
	userRows, err := tx.QueryContext(ctx, `
		SELECT id, balance::text
		FROM users
		WHERE id IN ($1, $2)
			AND deleted_at IS NULL
		ORDER BY id ASC
		FOR UPDATE
	`, consumerUserID, ownerUserID)
	if err != nil {
		return nil, err
	}
	consumerExists, ownerExists := false, false
	for userRows.Next() {
		var userID int64
		var balanceText string
		if err := userRows.Scan(&userID, &balanceText); err != nil {
			_ = userRows.Close()
			return nil, err
		}
		if userID == consumerUserID {
			balance, parseErr := decimal.NewFromString(balanceText)
			if parseErr != nil {
				_ = userRows.Close()
				return nil, fmt.Errorf("parse consumer %d balance: %w", userID, parseErr)
			}
			consumerExists, userBalance = true, balance
		}
		if userID == ownerUserID {
			ownerExists = true
		}
	}
	rowsErr := userRows.Err()
	_ = userRows.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}
	if !consumerExists || !ownerExists {
		return nil, service.ErrUserNotFound
	}
	if !ownerSelfUse && userBalance.LessThan(decimalFromFloat(minBalanceRequired)) {
		return nil, service.ErrAccountShareBalanceBelowMinimum
	}

	existing, err := scanAccountShareMembership(tx.QueryRowContext(ctx, `
		SELECT
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		WHERE m.consumer_user_id = $1
			AND m.listing_id = $2
			AND m.status IN ($3, $4)
			AND m.deleted_at IS NULL
		ORDER BY CASE WHEN m.status = $4 THEN 0 ELSE 1 END, m.id ASC
		LIMIT 1
	`,
		consumerUserID,
		listingID,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusEnding,
	))
	if err == nil {
		if existing.Status == service.AccountShareMembershipStatusEnding {
			return nil, service.ErrAccountShareMembershipEnding.WithMetadata(map[string]string{
				"membership_id": strconv.FormatInt(existing.ID, 10),
				"listing_id":    strconv.FormatInt(listingID, 10),
			})
		}
		if existing.APIKeyID != apiKeyID {
			return nil, service.ErrAccountShareAlreadyUsing.WithMetadata(map[string]string{
				"membership_id": strconv.FormatInt(existing.ID, 10),
				"listing_id":    strconv.FormatInt(listingID, 10),
			})
		}
		return nil, service.ErrAccountShareAPIKeyAlreadyBound
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if !input.IntentIssuedAt.IsZero() {
		var consumed bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM account_share_memberships
				WHERE consumer_user_id = $1
					AND api_key_id = $2
					AND listing_id = $3
					AND created_at >= $4
					AND status IN ($5, $6)
			)
		`,
			consumerUserID,
			apiKeyID,
			listingID,
			input.IntentIssuedAt,
			service.AccountShareMembershipStatusEnding,
			service.AccountShareMembershipStatusEnded,
		).Scan(&consumed); err != nil {
			return nil, err
		}
		if consumed {
			return nil, service.ErrAccountShareJoinIntentConsumed
		}
	}

	var hasLiveMembership bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM account_share_memberships
			WHERE api_key_id = $1 AND status IN ('active', 'ending')
				AND deleted_at IS NULL
		)
	`, apiKeyID).Scan(&hasLiveMembership); err != nil {
		return nil, err
	}
	if hasLiveMembership {
		return nil, service.ErrAccountShareAPIKeyAlreadyBound
	}
	if !ownerSelfUse {
		activeSeats, err := liveAccountShareSeatCountInTx(ctx, tx, listingID)
		if err != nil {
			return nil, err
		}
		if activeSeats >= seatLimit {
			return nil, service.ErrAccountShareRoomFull
		}
	}
	unavailable, err := r.accountShareAccountUnavailableInTx(ctx, tx, accountID, now)
	if err != nil {
		return nil, err
	}
	if unavailable {
		return nil, service.ErrAccountShareAccountUnavailable
	}
	const queueRank = 1
	if !ownerSelfUse && prepayAmount > 0 &&
		userBalance.LessThan(decimalFromFloat(minBalanceRequired).Add(decimalFromFloat(prepayAmount))) {
		return nil, service.ErrAccountShareModePrepayInsufficient
	}

	membership := &service.AccountShareMembership{}
	var endedAt, lastRequestAt sql.NullTime
	var paidUntilScan, billedUntilScan, dispatchFailedAt, dispatchCooldownUntil sql.NullTime
	var waiverWindowStartedAt, waiverWindowLastRequestAt sql.NullTime
	var endedReason sql.NullString
	var membershipAccountID sql.NullInt64
	var paidUntilValue any
	var billedUntilValue any
	var waiverWindowStartedAtValue any
	membershipStatus := service.AccountShareMembershipStatusActive
	if prepayAmount > 0 {
		paidUntilValue = paidUntil
		billedUntilValue = now
		waiverWindowStartedAtValue = now
	} else {
		paidUntilValue = nil
		billedUntilValue = nil
		waiverWindowStartedAtValue = nil
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO account_share_memberships (
			listing_id, account_id, consumer_user_id, api_key_id, status,
			queue_rank, hourly_rate_snapshot, hourly_fee_waiver_minimum_snapshot, idle_timeout_minutes, joined_at, last_request_at,
			ended_reason, paid_until, billed_until, waiver_window_started_at, waiver_window_usage_amount,
			waiver_window_request_count, waiver_window_last_request_at, dispatch_failed_at, dispatch_cooldown_until,
			queue_expires_at,
			listing_revision_id, listing_version_snapshot, room_name_snapshot, owner_user_id_snapshot,
			owner_username_snapshot, platform_snapshot, account_level_snapshot, api_key_name_snapshot,
			terms_snapshot, snapshot_quality, join_intent_nonce, unverified_upstream_ack_at, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5::varchar(20), $6, $7, $8, $9, $10, NULL, NULL, $11, $12, $13, 0, 0, NULL, NULL, NULL,
			NULL,
			$14, $15, $16, $17, $18, $19, $20, $21, $22::jsonb, $23, $24, $25, NOW(), NOW()
		)
		RETURNING id, listing_id, account_id, consumer_user_id, api_key_id, status, queue_rank,
			hourly_rate_snapshot, hourly_fee_waiver_minimum_snapshot, idle_timeout_minutes, joined_at, last_request_at, ended_at,
			ended_reason, paid_until, billed_until, waiver_window_started_at, waiver_window_usage_amount,
			waiver_window_request_count, waiver_window_last_request_at, dispatch_failed_at, dispatch_cooldown_until, created_at, updated_at
	`,
		listingID,
		accountID,
		consumerUserID,
		apiKeyID,
		membershipStatus,
		queueRank,
		hourlyRate,
		hourlyFeeWaiverMinimum,
		idleTimeoutMinutes,
		now,
		paidUntilValue,
		billedUntilValue,
		waiverWindowStartedAtValue,
		revisionID,
		listingVersion,
		revision.RoomName,
		ownerUserID,
		revision.OwnerDisplayName,
		revision.Platform,
		revision.AccountLevel,
		strings.TrimSpace(apiKeyName),
		string(termsSnapshotJSON),
		service.AccountShareSnapshotQualityExact,
		strings.TrimSpace(input.IntentNonce),
		input.AcknowledgedUnverifiedAt,
	).Scan(
		&membership.ID,
		&membership.ListingID,
		&membershipAccountID,
		&membership.ConsumerUserID,
		&membership.APIKeyID,
		&membership.Status,
		&membership.QueueRank,
		&membership.HourlyRateSnapshot,
		&membership.HourlyFeeWaiverMinimumSnapshot,
		&membership.IdleTimeoutMinutes,
		&membership.JoinedAt,
		&lastRequestAt,
		&endedAt,
		&endedReason,
		&paidUntilScan,
		&billedUntilScan,
		&waiverWindowStartedAt,
		&membership.WaiverWindowUsageAmount,
		&membership.WaiverWindowRequestCount,
		&waiverWindowLastRequestAt,
		&dispatchFailedAt,
		&dispatchCooldownUntil,
		&membership.CreatedAt,
		&membership.UpdatedAt,
	)
	if err != nil {
		return nil, translateAccountShareMembershipConflict(err)
	}
	if membershipAccountID.Valid {
		membership.AccountID = membershipAccountID.Int64
	} else {
		return nil, fmt.Errorf("account share membership %d in status %q has no account binding", membership.ID, membership.Status)
	}
	membership.OwnerUserID = ownerUserID
	membership.ListingRevisionID = &revisionID
	membership.ListingVersionSnapshot = &listingVersion
	membership.RoomNameSnapshot = revision.RoomName
	membership.OwnerUserIDSnapshot = &ownerUserID
	membership.OwnerUsernameSnapshot = revision.OwnerDisplayName
	membership.PlatformSnapshot = revision.Platform
	membership.AccountLevelSnapshot = revision.AccountLevel
	membership.APIKeyNameSnapshot = strings.TrimSpace(apiKeyName)
	membership.TermsSnapshot = termsSnapshot
	membership.SnapshotQuality = service.AccountShareSnapshotQualityExact
	if endedAt.Valid {
		membership.EndedAt = &endedAt.Time
	}
	if lastRequestAt.Valid {
		membership.LastRequestAt = &lastRequestAt.Time
	}
	if endedReason.Valid {
		membership.EndedReason = endedReason.String
	}
	if paidUntilScan.Valid {
		membership.PaidUntil = &paidUntilScan.Time
	}
	if billedUntilScan.Valid {
		membership.BilledUntil = &billedUntilScan.Time
	}
	if waiverWindowStartedAt.Valid {
		membership.WaiverWindowStartedAt = &waiverWindowStartedAt.Time
	}
	if waiverWindowLastRequestAt.Valid {
		membership.WaiverWindowLastRequestAt = &waiverWindowLastRequestAt.Time
	}
	if dispatchFailedAt.Valid {
		membership.DispatchFailedAt = &dispatchFailedAt.Time
	}
	if dispatchCooldownUntil.Valid {
		membership.DispatchCooldownUntil = &dispatchCooldownUntil.Time
	}
	membership.OwnerUserID = ownerUserID
	boundByRole := "consumer"
	if ownerSelfUse {
		boundByRole = "owner"
	}
	if _, _, err := r.createAccountShareMembershipBindingInTx(
		ctx,
		tx,
		membership.ID,
		listingID,
		accountID,
		revisionID,
		consumerUserID,
		boundByRole,
		"join_activation",
		now,
	); err != nil {
		return nil, err
	}

	if prepayAmount > 0 {
		newBalance := userBalance.Sub(decimalFromFloat(prepayAmount))
		if _, err := tx.ExecContext(ctx, `
			UPDATE users
			SET balance = $1::numeric,
				updated_at = NOW()
			WHERE id = $2
				AND deleted_at IS NULL
		`, newBalance.StringFixed(10), consumerUserID); err != nil {
			return nil, err
		}
		if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
			UserID:          consumerUserID,
			Direction:       "debit",
			Amount:          decimalFromFloat(prepayAmount),
			Reason:          accountShareSeatPrepayReason,
			RefType:         accountShareSeatPrepayRefType,
			RefID:           accountShareSeatPrepayRefID(membership.ID, paidUntil),
			BalanceAfter:    newBalance,
			RequireInserted: true,
			Metadata: map[string]any{
				"listing_id":    listingID,
				"account_id":    accountID,
				"membership_id": membership.ID,
				"hourly_rate":   hourlyRate,
				"duration_ms":   int(prepayDuration.Milliseconds()),
				"paid_until":    paidUntil.Format(time.RFC3339),
				"prepay_stage":  "join",
				"seat_billing":  true,
				"consumer_user": consumerUserID,
			},
		}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return membership, nil
}

func lockAccountShareJoinAPIKeyInTx(
	ctx context.Context,
	tx *sql.Tx,
	apiKeyID int64,
	consumerUserID int64,
) (string, error) {
	if tx == nil || apiKeyID <= 0 || consumerUserID <= 0 {
		return "", service.ErrAPIKeyNotFound
	}
	var apiKeyName string
	err := tx.QueryRowContext(ctx, `
		SELECT name
		FROM api_keys
		WHERE id = $1
			AND user_id = $2
			AND deleted_at IS NULL
		FOR UPDATE
	`, apiKeyID, consumerUserID).Scan(&apiKeyName)
	if errors.Is(err, sql.ErrNoRows) {
		return "", service.ErrAPIKeyNotFound
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(apiKeyName), nil
}

func liveAccountShareSeatCountInTx(ctx context.Context, tx *sql.Tx, listingID int64) (int, error) {
	var activeSeats int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)::int
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
			AND l.deleted_at IS NULL
		WHERE m.listing_id = $1
			AND m.status IN ($2, $3)
			AND m.deleted_at IS NULL
			AND m.consumer_user_id <> l.owner_user_id
	`,
		listingID,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusEnding,
	).Scan(&activeSeats); err != nil {
		return 0, err
	}
	return activeSeats, nil
}
