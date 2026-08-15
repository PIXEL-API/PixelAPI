package config

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProxyExpiryWorkerConfigurationExistsAndDefaultsDisabled(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)

	configValue := reflect.ValueOf(cfg).Elem()
	field := configValue.FieldByName("ProxyExpiry")
	require.True(t, field.IsValid(), "Config must expose a dedicated proxy_expiry section")
	fieldType, ok := configValue.Type().FieldByName("ProxyExpiry")
	require.True(t, ok)
	require.Equal(t, "proxy_expiry", fieldType.Tag.Get("mapstructure"))

	enabled := field.FieldByName("Enabled")
	require.True(t, enabled.IsValid(), "proxy_expiry.enabled must be explicit")
	require.Equal(t, reflect.Bool, enabled.Kind())
	require.False(t, enabled.Bool(), "proxy expiry worker must be opt-in on first deployment")
}
