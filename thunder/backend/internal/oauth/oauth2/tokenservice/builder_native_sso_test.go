// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package tokenservice

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// TestBuildIDTokenClaims_NativeSSO checks the sid and ds_hash claims of an id_token issued with a device
// secret (OpenID Connect Native SSO for Mobile Apps 1.0, 3.4.1; rule R05).
func TestBuildIDTokenClaims_NativeSSO(t *testing.T) {
	app := &providers.OAuthClient{
		ClientID:    "test-client",
		ScopeClaims: map[string][]string{"profile": {"name", "sid", "ds_hash"}},
		Token: &providers.OAuthTokenConfig{
			IDToken: &providers.IDTokenConfig{UserAttributes: []string{"name", "sid", "ds_hash"}},
		},
	}
	tb := &tokenBuilder{}

	t.Run("with a device secret", func(t *testing.T) {
		claims := tb.buildIDTokenClaims(&IDTokenBuildContext{
			Subject:          "user-1",
			Audience:         "test-client",
			Scopes:           []string{"openid", "profile", constants.ScopeDeviceSSO},
			UserAttributes:   map[string]interface{}{"name": "Asha"},
			OAuthApp:         app,
			SessionID:        "session-id",
			DeviceSecretHash: "ds-hash",
		})

		assert.Equal(t, "session-id", claims[constants.ClaimSessionID])
		assert.Equal(t, "ds-hash", claims[constants.ClaimDeviceSecretHash])
		assert.Equal(t, "Asha", claims["name"])
	})

	t.Run("user attributes cannot replace them", func(t *testing.T) {
		claims := tb.buildIDTokenClaims(&IDTokenBuildContext{
			Scopes:           []string{"openid", "profile"},
			UserAttributes:   map[string]interface{}{"sid": "forged", "ds_hash": "forged"},
			OAuthApp:         app,
			SessionID:        "session-id",
			DeviceSecretHash: "ds-hash",
		})

		assert.Equal(t, "session-id", claims[constants.ClaimSessionID])
		assert.Equal(t, "ds-hash", claims[constants.ClaimDeviceSecretHash])
	})

	t.Run("without a device secret", func(t *testing.T) {
		claims := tb.buildIDTokenClaims(&IDTokenBuildContext{
			Scopes:         []string{"openid", "profile"},
			UserAttributes: map[string]interface{}{"name": "Asha"},
			OAuthApp:       app,
		})

		assert.NotContains(t, claims, constants.ClaimSessionID)
		assert.NotContains(t, claims, constants.ClaimDeviceSecretHash)
	})
}

// TestBuildAccessTokenClaims_NativeSSO checks the sid claim of an access token issued for a Native SSO
// login session, which token validation uses to refuse the token once the session ends (rule R27).
func TestBuildAccessTokenClaims_NativeSSO(t *testing.T) {
	tb := &tokenBuilder{}

	claims, err := tb.buildAccessTokenClaims(&AccessTokenBuildContext{
		Subject:           "user-1",
		SubjectAttributes: map[string]interface{}{"sid": "forged"},
		SessionID:         "session-id",
	})
	assert.NoError(t, err)
	assert.Equal(t, "session-id", claims[constants.ClaimSessionID], "a user attribute cannot replace it")

	claims, err = tb.buildAccessTokenClaims(&AccessTokenBuildContext{Subject: "user-1"})
	assert.NoError(t, err)
	assert.NotContains(t, claims, constants.ClaimSessionID)
}
