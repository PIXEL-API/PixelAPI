package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// AccountShareModels describes the current key's bound account and permitted
// model IDs. A non-nil result with no models must remain an empty model list.
type AccountShareModels struct {
	Account *Account
	Models  []string
}

// GetAccountShareModels bypasses the ordinary group model cache: mode groups
// contain accounts from multiple rooms, while permissions belong to each key.
func (s *GatewayService) GetAccountShareModels(ctx context.Context, apiKey *APIKey, platform string) (*AccountShareModels, error) {
	return s.accountShareModeService.modelsForRequest(ctx, apiKey, platform, s.channelService)
}

func (s *OpenAIGatewayService) GetAccountShareModels(ctx context.Context, apiKey *APIKey) (*AccountShareModels, error) {
	return s.accountShareModeService.modelsForRequest(ctx, apiKey, PlatformOpenAI, s.channelService)
}

func (s *AccountShareModeService) modelsForRequest(ctx context.Context, apiKey *APIKey, platform string, channels *ChannelService) (*AccountShareModels, error) {
	if s == nil || apiKey == nil || apiKey.GroupID == nil {
		return nil, nil
	}
	isMode, err := s.IsModeGroupChecked(ctx, *apiKey.GroupID)
	if err != nil {
		return nil, fmt.Errorf("check account share model group: %w", err)
	}
	if !isMode {
		return nil, nil
	}
	if apiKey.UserID <= 0 || apiKey.ID <= 0 {
		return nil, ErrAccountShareModeGroupUnbound
	}
	// This read applies the member's effective terms without activating queued
	// rooms, renewing paid seats, touching idle time, or rebinding accounts.
	membership, listing, err := s.repo.GetActiveMembershipForRequest(ctx, apiKey.UserID, apiKey.ID, *apiKey.GroupID)
	if errors.Is(err, ErrAccountShareListingNotFound) {
		return nil, ErrAccountShareModeGroupUnbound
	}
	if err != nil {
		return nil, fmt.Errorf("read account share model binding: %w", err)
	}
	if membership == nil || listing == nil || membership.AccountID <= 0 {
		return nil, ErrAccountShareModeGroupUnbound
	}
	if s.accountRepo == nil {
		return nil, ErrServiceUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, membership.AccountID)
	if err != nil {
		return nil, fmt.Errorf("read account share model account: %w", err)
	}
	if account == nil || account.ID != membership.AccountID || account.Platform != platform || listing.Platform != platform {
		return nil, ErrAccountShareModeSelection
	}
	if s.pricedModelCatalog == nil {
		return nil, ErrOwnedAccountModelCatalogUnavailable
	}
	candidates, err := s.pricedModelCatalog.ListSelectablePricedModelIDs(ctx, PricedModelQuery{Platform: platform})
	if err != nil {
		return nil, ErrOwnedAccountModelCatalogUnavailable.WithCause(err)
	}
	// Wildcard pricing can authorize concrete room/account models that the
	// selectable catalog cannot enumerate. Never expose the patterns themselves.
	candidates = append(append([]string(nil), candidates...), listing.AllowedModels...)
	for model := range account.GetModelMapping() {
		candidates = append(candidates, model)
	}
	result := &AccountShareModels{Account: account, Models: make([]string, 0, len(candidates))}
	for _, model := range normalizeAllowedModels(candidates) {
		if strings.ContainsAny(model, "*?") {
			continue
		}
		selectionModel, err := accountShareDiscoverySelectionModel(ctx, channels, *apiKey.GroupID, account, model)
		if err != nil {
			return nil, ErrOwnedAccountModelCatalogUnavailable.WithCause(err)
		}
		if !accountShareListingAllowsModel(listing, selectionModel) || !account.IsModelSupported(selectionModel) {
			continue
		}
		priced, err := s.pricedModelCatalog.IsModelPriced(ctx, PricedModelQuery{Platform: platform}, model)
		if err != nil {
			return nil, ErrOwnedAccountModelCatalogUnavailable.WithCause(err)
		}
		if !priced {
			continue
		}
		if selectionModel != model {
			priced, err = s.pricedModelCatalog.IsModelPriced(ctx, PricedModelQuery{Platform: platform}, selectionModel)
			if err != nil {
				return nil, ErrOwnedAccountModelCatalogUnavailable.WithCause(err)
			}
			if !priced {
				continue
			}
		}
		restricted, err := accountShareDiscoveryModelRestricted(ctx, channels, *apiKey.GroupID, account, selectionModel)
		if err != nil {
			return nil, ErrOwnedAccountModelCatalogUnavailable.WithCause(err)
		}
		if !restricted {
			result.Models = append(result.Models, model)
		}
	}
	sort.Strings(result.Models)
	return result, nil
}

// OpenAI-compatible handlers apply channel mapping before selecting the room
// account. Anthropic's native handler checks the original requested model.
func accountShareDiscoverySelectionModel(ctx context.Context, channels *ChannelService, groupID int64, account *Account, model string) (string, error) {
	if channels == nil || !account.IsOpenAICompatible() {
		return model, nil
	}
	mapping, err := channels.ResolveChannelMappingChecked(ctx, groupID, model)
	if err != nil {
		return "", err
	}
	if mapping.Mapped && strings.TrimSpace(mapping.MappedModel) != "" {
		return strings.TrimSpace(mapping.MappedModel), nil
	}
	return model, nil
}

// Use the same billing-model basis as dispatch, including account mappings
// when the channel restricts upstream models. An unbound channel adds no limit.
func accountShareDiscoveryModelRestricted(ctx context.Context, channels *ChannelService, groupID int64, account *Account, model string) (bool, error) {
	if channels == nil {
		return false, nil
	}
	mapping, err := channels.ResolveChannelMappingChecked(ctx, groupID, model)
	if err != nil {
		return false, err
	}
	billingModel := billingModelForRestriction(mapping.BillingModelSource, model, mapping.MappedModel)
	if mapping.BillingModelSource == BillingModelSourceUpstream {
		if account.IsOpenAICompatible() {
			billingModel = resolveOpenAIAccountUpstreamModelForRequest(account, model, false)
		} else {
			billingModel = resolveAccountUpstreamModel(account, model)
		}
	}
	if billingModel == "" {
		return false, nil
	}
	return channels.IsModelRestrictedChecked(ctx, groupID, billingModel)
}
