//go:build unit

package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateProviderRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		providerKey    string
		providerName   string
		supportedTypes string
		wantErr        bool
		errContains    string
	}{
		{
			name:           "valid easypay with types",
			providerKey:    "easypay",
			providerName:   "MyProvider",
			supportedTypes: "alipay,wxpay",
			wantErr:        false,
		},
		{
			name:           "valid stripe with empty types",
			providerKey:    "stripe",
			providerName:   "Stripe Provider",
			supportedTypes: "",
			wantErr:        false,
		},
		{
			name:           "valid alipay provider",
			providerKey:    "alipay",
			providerName:   "Alipay Direct",
			supportedTypes: "alipay",
			wantErr:        false,
		},
		{
			name:           "valid wxpay provider",
			providerKey:    "wxpay",
			providerName:   "WeChat Pay",
			supportedTypes: "wxpay",
			wantErr:        false,
		},
		{
			name:           "invalid provider key",
			providerKey:    "invalid",
			providerName:   "Name",
			supportedTypes: "alipay",
			wantErr:        true,
			errContains:    "invalid provider key",
		},
		{
			name:           "empty name",
			providerKey:    "easypay",
			providerName:   "",
			supportedTypes: "alipay",
			wantErr:        true,
			errContains:    "provider name is required",
		},
		{
			name:           "whitespace-only name",
			providerKey:    "easypay",
			providerName:   "  ",
			supportedTypes: "alipay",
			wantErr:        true,
			errContains:    "provider name is required",
		},
		{
			name:           "tab-only name",
			providerKey:    "easypay",
			providerName:   "\t",
			supportedTypes: "alipay",
			wantErr:        true,
			errContains:    "provider name is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateProviderRequest(tc.providerKey, tc.providerName, tc.supportedTypes)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIsSensitiveProviderConfigField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		providerKey string
		field       string
		wantSen     bool
	}{
		// Stripe: publishableKey is public, only secretKey/webhookSecret are secrets
		{"stripe", "secretKey", true},
		{"stripe", "webhookSecret", true},
		{"stripe", "SecretKey", true}, // case-insensitive
		{"stripe", "publishableKey", false},
		{"stripe", "appId", false},

		// Alipay
		{"alipay", "privateKey", true},
		{"alipay", "publicKey", true},
		{"alipay", "alipayPublicKey", true},
		{"alipay", "appId", false},
		{"alipay", "notifyUrl", false},

		// Wxpay
		{"wxpay", "privateKey", true},
		{"wxpay", "apiV3Key", true},
		{"wxpay", "publicKey", true},
		{"wxpay", "publicKeyId", false},
		{"wxpay", "certSerial", false},
		{"wxpay", "mchId", false},

		// EasyPay
		{"easypay", "pkey", true},
		{"easypay", "pid", false},
		{"easypay", "apiBase", false},

		// Unknown provider: never sensitive
		{"unknown", "secretKey", false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.providerKey+"/"+tc.field, func(t *testing.T) {
			t.Parallel()

			got := isSensitiveProviderConfigField(tc.providerKey, tc.field)
			assert.Equal(t, tc.wantSen, got, "isSensitiveProviderConfigField(%q, %q)", tc.providerKey, tc.field)
		})
	}
}

func TestJoinTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{
			name:  "multiple types",
			input: []string{"alipay", "wxpay"},
			want:  "alipay,wxpay",
		},
		{
			name:  "single type",
			input: []string{"stripe"},
			want:  "stripe",
		},
		{
			name:  "empty slice",
			input: []string{},
			want:  "",
		},
		{
			name:  "nil slice",
			input: nil,
			want:  "",
		},
		{
			name:  "three types",
			input: []string{"alipay", "wxpay", "stripe"},
			want:  "alipay,wxpay,stripe",
		},
		{
			name:  "types with spaces are not trimmed",
			input: []string{" alipay ", " wxpay "},
			want:  " alipay , wxpay ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := joinTypes(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCreateProviderInstanceRejectsConflictingVisibleMethodEnablement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	}

	_, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    "easypay",
		Name:           "EasyPay Alipay",
		Config:         map[string]string{"pid": "1001"},
		SupportedTypes: []string{"alipay"},
		Enabled:        true,
	})
	require.NoError(t, err)

	_, err = svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    "alipay",
		Name:           "Official Alipay",
		Config:         map[string]string{"appId": "app-1"},
		SupportedTypes: []string{"alipay"},
		Enabled:        true,
	})
	require.Error(t, err)
	require.Equal(t, "PAYMENT_PROVIDER_CONFLICT", infraerrors.Reason(err))
}

func TestUpdateProviderInstanceRejectsEnablingConflictingVisibleMethodProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	}

	existing, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    "easypay",
		Name:           "EasyPay WeChat",
		Config:         map[string]string{"pid": "2001"},
		SupportedTypes: []string{"wxpay"},
		Enabled:        true,
	})
	require.NoError(t, err)
	require.NotNil(t, existing)

	candidate, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    "wxpay",
		Name:           "Official WeChat",
		Config:         map[string]string{"appId": "wx-app"},
		SupportedTypes: []string{"wxpay"},
		Enabled:        false,
	})
	require.NoError(t, err)

	_, err = svc.UpdateProviderInstance(ctx, candidate.ID, UpdateProviderInstanceRequest{
		Enabled: boolPtrValue(true),
	})
	require.Error(t, err)
	require.Equal(t, "PAYMENT_PROVIDER_CONFLICT", infraerrors.Reason(err))
}

func TestUpdateProviderInstancePersistsEnabledAndSupportedTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{
		entClient:     client,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	}

	instance, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    "easypay",
		Name:           "EasyPay",
		Config:         map[string]string{"pid": "3001"},
		SupportedTypes: []string{"alipay"},
		Enabled:        false,
	})
	require.NoError(t, err)

	_, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{
		Enabled:        boolPtrValue(true),
		SupportedTypes: []string{"alipay", "wxpay"},
	})
	require.NoError(t, err)

	saved, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(t, err)
	require.True(t, saved.Enabled)
	require.Equal(t, "alipay,wxpay", saved.SupportedTypes)
}

func boolPtrValue(v bool) *bool {
	return &v
}
