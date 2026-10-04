// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package thunderidengine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	engineconfig "github.com/thunder-id/thunderid/pkg/thunderidengine/config"
	"github.com/thunder-id/thunderid/tests/mocks/devicesecretprovidermock"
)

func (suite *EngineTestSuite) TestWithDeviceSecretProvider_SetsProvider() {
	var ctx engineContext
	provider := devicesecretprovidermock.NewDeviceSecretProviderMock(suite.T())

	WithDeviceSecretProvider(provider)(&ctx)

	assert.Same(suite.T(), provider, ctx.deviceSecretProvider)
}

// Native SSO is off unless the host supplies a provider: no option and an explicit nil both leave
// the engine without one, and the engine context is still valid.
func (suite *EngineTestSuite) TestWithDeviceSecretProvider_NilLeavesNativeSSOOff() {
	suite.T().Run("option omitted", func(t *testing.T) {
		ctx := validEngineContext(t)
		assert.Nil(t, ctx.deviceSecretProvider)
		assert.NoError(t, validateEngineContext(ctx))
	})

	suite.T().Run("explicit nil", func(t *testing.T) {
		ctx := validEngineContext(t)
		WithDeviceSecretProvider(nil)(ctx)
		assert.Nil(t, ctx.deviceSecretProvider)
		assert.NoError(t, validateEngineContext(ctx))
	})
}

func (suite *EngineTestSuite) TestValidateEngineContext_NativeSSOConfig() {
	suite.T().Run("valid settings pass", func(t *testing.T) {
		ctx := validEngineContext(t)
		WithDeviceSecretProvider(devicesecretprovidermock.NewDeviceSecretProviderMock(t))(ctx)
		WithOAuthConfig(engineconfig.OAuthConfig{NativeSSO: engineconfig.NativeSSOConfig{
			IDTokenMaxAgeSeconds: 600, ScopeExceededError: "invalid_scope", ExchangeOnly: true,
		}})(ctx)
		assert.NoError(t, validateEngineContext(ctx))
		assert.Equal(t, int64(600), ctx.oauthConfig.NativeSSO.IDTokenMaxAgeSeconds)
	})

	suite.T().Run("invalid settings fail", func(t *testing.T) {
		ctx := validEngineContext(t)
		WithOAuthConfig(engineconfig.OAuthConfig{NativeSSO: engineconfig.NativeSSOConfig{
			ScopeExceededError: "login_required",
		}})(ctx)
		err := validateEngineContext(ctx)
		assert.ErrorContains(t, err, "thunderidengine: invalid OAuth configuration")
		assert.ErrorContains(t, err, "scope_exceeded_error")
	})
}
