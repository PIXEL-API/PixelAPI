package service

import "testing"

func TestGatewayServiceIsModelSupportedByAccount_SoraNoMappingAllowsAll(t *testing.T) {
	svc := &GatewayService{}
	account := &Account{
		Platform:    PlatformSora,
		Credentials: map[string]any{},
	}

	if !svc.isModelSupportedByAccount(account, "sora2-landscape-10s") {
		t.Fatalf("expected sora model to be supported when model_mapping is empty")
	}
}

func TestGatewayServiceIsModelSupportedByAccount_SoraLegacyNonSoraMappingDoesNotBlock(t *testing.T) {
	svc := &GatewayService{}
	account := &Account{
		Platform: PlatformSora,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gpt-4o": "gpt-4o",
			},
		},
	}

	if !svc.isModelSupportedByAccount(account, "sora2-landscape-10s") {
		t.Fatalf("expected sora model to be supported when mapping has no sora selectors")
	}
}

func TestGatewayServiceIsModelSupportedByAccount_SoraFamilyAlias(t *testing.T) {
	svc := &GatewayService{}
	account := &Account{
		Platform: PlatformSora,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"sora2": "sora2",
			},
		},
	}

	if !svc.isModelSupportedByAccount(account, "sora2-landscape-15s") {
		t.Fatalf("expected family selector sora2 to support sora2-landscape-15s")
	}
}

func TestGatewayServiceIsModelSupportedByAccount_SoraUnderlyingModelAlias(t *testing.T) {
	svc := &GatewayService{}
	account := &Account{
		Platform: PlatformSora,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"sy_8": "sy_8",
			},
		},
	}

	if !svc.isModelSupportedByAccount(account, "sora2-landscape-10s") {
		t.Fatalf("expected underlying model selector sy_8 to support sora2-landscape-10s")
	}
}

func TestGatewayServiceIsModelSupportedByAccount_SoraExplicitImageSelectorBlocksVideo(t *testing.T) {
	svc := &GatewayService{}
	account := &Account{
		Platform: PlatformSora,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gpt-image": "gpt-image",
			},
		},
	}

	if svc.isModelSupportedByAccount(account, "sora2-landscape-10s") {
		t.Fatalf("expected video model to be blocked when mapping explicitly only allows gpt-image")
	}
}
