package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// GetAvailableImageModels discovers concrete image models for this key without
// the legacy /models defaults. Catalog failures and an empty result are distinct.
func (s *GatewayService) GetAvailableImageModels(ctx context.Context, apiKey *APIKey, platform string) ([]string, error) {
	models := make([]string, 0)
	if apiKey == nil || apiKey.GroupID == nil || apiKey.Group == nil ||
		!GroupAllowsImageGeneration(apiKey.Group) ||
		(platform != PlatformOpenAI && platform != PlatformGrok) || apiKey.Group.Platform != platform {
		return models, nil
	}
	if s == nil || s.channelService == nil {
		return nil, ErrServiceUnavailable
	}
	// Strict lookup also preserves a cached catalog load failure; the legacy
	// catalog readers otherwise treat that short error-cache window as empty.
	channelLookup, err := s.channelService.lookupGroupChannelChecked(ctx, *apiKey.GroupID)
	if err != nil {
		return nil, fmt.Errorf("get image model channel: %w", err)
	}

	roomModels, err := s.GetAccountShareModels(ctx, apiKey, platform)
	if err != nil {
		return nil, err
	}
	if roomModels != nil {
		for _, model := range roomModels.Models {
			if !imageDiscoveryModelForPlatform(model, platform) || !imageDiscoveryAccountSupports(roomModels.Account, platform) {
				continue
			}
			selectionModel, err := accountShareDiscoverySelectionModel(ctx, s.channelService, *apiKey.GroupID, roomModels.Account, model)
			if err != nil {
				return nil, err
			}
			if imageDiscoveryModelForPlatform(selectionModel, platform) && imageDiscoveryModelForPlatform(roomModels.Account.GetMappedModel(selectionModel), platform) {
				models = append(models, model)
			}
		}
		return models, nil
	}
	if s.accountRepo == nil {
		return nil, ErrServiceUnavailable
	}
	accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, *apiKey.GroupID)
	if err != nil {
		return nil, fmt.Errorf("list image model accounts: %w", err)
	}
	eligibleAccounts := make([]*Account, 0, len(accounts))
	for i := range accounts {
		if imageDiscoveryAccountSupports(&accounts[i], platform) {
			eligibleAccounts = append(eligibleAccounts, &accounts[i])
		}
	}
	if len(eligibleAccounts) == 0 {
		return models, nil
	}

	// The platform catalog is the existing pricing ceiling, including private
	// groups without a directly bound channel. Account whitelists and the current
	// group's channel restrictions below narrow that catalog to this key.
	query := PricedModelQuery{Platform: platform}
	candidates, err := s.channelService.ListSelectablePricedModelIDs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list priced image models: %w", err)
	}
	for _, account := range eligibleAccounts {
		for model := range account.GetModelMapping() {
			candidates = append(candidates, model)
		}
	}
	if channelLookup != nil {
		for model := range channelLookup.channel.ModelMapping[platform] {
			candidates = append(candidates, model)
		}
	}
	for _, model := range normalizeAllowedModels(candidates) {
		if !imageDiscoveryModelForPlatform(model, platform) {
			continue
		}
		priced, err := s.channelService.IsModelPriced(ctx, query, model)
		if err != nil {
			return nil, fmt.Errorf("check image model pricing: %w", err)
		}
		if !priced {
			continue
		}
		for _, account := range eligibleAccounts {
			selectionModel, err := accountShareDiscoverySelectionModel(ctx, s.channelService, *apiKey.GroupID, account, model)
			if err != nil {
				return nil, err
			}
			if !imageDiscoveryModelForPlatform(selectionModel, platform) || !account.IsModelSupported(selectionModel) ||
				!imageDiscoveryModelForPlatform(account.GetMappedModel(selectionModel), platform) {
				continue
			}
			if selectionModel != model {
				priced, err = s.channelService.IsModelPriced(ctx, query, selectionModel)
				if err != nil {
					return nil, fmt.Errorf("check mapped image model pricing: %w", err)
				}
				if !priced {
					continue
				}
			}
			restricted, err := accountShareDiscoveryModelRestricted(ctx, s.channelService, *apiKey.GroupID, account, selectionModel)
			if err != nil {
				return nil, err
			}
			if !restricted {
				models = append(models, model)
				break
			}
		}
	}
	sort.Strings(models)
	return models, nil
}

func imageDiscoveryModelForPlatform(model, platform string) bool {
	if strings.ContainsAny(model, "*?") {
		return false
	}
	if platform == PlatformGrok {
		return isGrokImageGenerationModel(model)
	}
	return platform == PlatformOpenAI && isOpenAIImageGenerationModel(model)
}

func imageDiscoveryAccountSupports(account *Account, platform string) bool {
	if account == nil || account.Platform != platform {
		return false
	}
	if platform == PlatformOpenAI {
		return account.SupportsOpenAIImageCapability(OpenAIImagesCapabilityBasic)
	}
	return platform == PlatformGrok && (account.Type == AccountTypeAPIKey || account.Type == AccountTypeOAuth) &&
		account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGrokMediaGeneration)
}
