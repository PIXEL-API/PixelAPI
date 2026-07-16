package service

func resolveImageRateMultiplier(apiKey *APIKey, effectiveGroupMultiplier float64) float64 {
	if apiKey != nil && apiKey.Group != nil && apiKey.Group.ImageRateIndependent {
		if apiKey.Group.ImageRateMultiplier < 0 {
			return 0
		}
		return apiKey.Group.ImageRateMultiplier
	}
	return effectiveGroupMultiplier
}

func resolveVideoRateMultiplier(apiKey *APIKey, effectiveGroupMultiplier float64) float64 {
	if apiKey != nil && apiKey.Group != nil && apiKey.Group.VideoRateIndependent {
		if apiKey.Group.VideoRateMultiplier < 0 {
			return 0
		}
		return apiKey.Group.VideoRateMultiplier
	}
	return effectiveGroupMultiplier
}

func videoPriceConfigFromAPIKey(apiKey *APIKey) *VideoPriceConfig {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return &VideoPriceConfig{
		Price480P:  apiKey.Group.VideoPrice480P,
		Price720P:  apiKey.Group.VideoPrice720P,
		Price1080P: apiKey.Group.VideoPrice1080P,
	}
}

func webSearchPricePerCallFromAPIKey(apiKey *APIKey) *float64 {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return apiKey.Group.WebSearchPricePerCall
}
