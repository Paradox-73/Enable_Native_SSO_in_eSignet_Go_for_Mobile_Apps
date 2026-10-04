// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package nativesso

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/model"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

func TestRequestsDeviceSSO(t *testing.T) {
	assert.True(t, RequestsDeviceSSO([]string{"openid", "device_sso"}))
	assert.False(t, RequestsDeviceSSO([]string{"openid", "profile"}))
	assert.False(t, RequestsDeviceSSO(nil))
	assert.False(t, RequestsDeviceSSO([]string{"device_sso_extra"}), "only the exact scope name counts")
}

func TestValidateDeviceSSOScope(t *testing.T) {
	allowed := &providers.OAuthClient{Scopes: []string{"openid", "profile", constants.ScopeDeviceSSO}}
	notAllowed := &providers.OAuthClient{Scopes: []string{"openid", "profile"}}

	cases := []struct {
		name     string
		scopes   []string
		app      *providers.OAuthClient
		enabled  bool
		wantDesc string
	}{
		{name: "allowed client with openid", scopes: []string{"openid", "device_sso"}, app: allowed, enabled: true},
		{name: "no device_sso while Native SSO is off", scopes: []string{"openid"}, app: notAllowed},
		{name: "no device_sso and no client", scopes: []string{"openid"}, enabled: true},
		{
			name: "Native SSO off", scopes: []string{"openid", "device_sso"}, app: allowed,
			wantDesc: errDescDeviceSSONotSupported,
		},
		{
			name: "client not allowed", scopes: []string{"openid", "device_sso"}, app: notAllowed, enabled: true,
			wantDesc: errDescDeviceSSONotAllowed,
		},
		{
			name: "no client", scopes: []string{"openid", "device_sso"}, enabled: true,
			wantDesc: errDescDeviceSSONotAllowed,
		},
		{
			name: "openid missing", scopes: []string{"profile", "device_sso"}, app: allowed, enabled: true,
			wantDesc: errDescDeviceSSOWithoutOIDC,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errResp := ValidateDeviceSSOScope(tc.scopes, tc.app, tc.enabled)
			if tc.wantDesc == "" {
				assert.Nil(t, errResp)
				return
			}
			require.NotNil(t, errResp)
			assert.Equal(t, constants.ErrorInvalidScope, errResp.Error)
			assert.Equal(t, tc.wantDesc, errResp.ErrorDescription)
		})
	}
}

func TestNewSessionID(t *testing.T) {
	seen := make(map[string]struct{})
	for range 1000 {
		sid := NewSessionID()
		assert.Len(t, sid, 22, "128 bits in base64url without padding")
		raw, err := base64.RawURLEncoding.DecodeString(sid)
		require.NoError(t, err)
		assert.Len(t, raw, sessionIDBytes)
		_, dup := seen[sid]
		assert.False(t, dup, "session ids must not repeat")
		seen[sid] = struct{}{}
	}
}

func TestDeviceSecretHash(t *testing.T) {
	// SHA-256 of the empty string starts with e3b0c442 98fc1c14 9afbf4c8 996fb924 (FIPS 180-2 example).
	assert.Equal(t, "47DEQpj8HBSa-_TImW-5JA", DeviceSecretHash(""))
	assert.Equal(t, "Ad3fRNZ5FyNN5SYczCVEuQ", DeviceSecretHash("device-secret-value"))
	assert.Len(t, DeviceSecretHash("any value"), 22, "left 16 bytes in base64url without padding")
	assert.NotEqual(t, DeviceSecretHash("a"), DeviceSecretHash("b"))
}

func TestReleasedClaimNames_PlainAttributes(t *testing.T) {
	scopeClaims := map[string][]string{"profile": {"name", "picture"}}
	claimsRequest := &model.ClaimsRequest{
		IDToken:  map[string]*model.IndividualClaimRequest{"phone_number": nil},
		UserInfo: map[string]*model.IndividualClaimRequest{"birthdate": nil},
	}
	attrs := map[string]interface{}{
		"name":         "Asha",
		"picture":      nil,
		"phone_number": "+910000000000",
		"birthdate":    "1990-01-01",
		"not_released": "value",
	}

	names := ReleasedClaimNames([]string{"openid", "profile", "device_sso"}, scopeClaims, claimsRequest, attrs)

	assert.Equal(t, []string{"birthdate", "name", "phone_number"}, names,
		"sorted names of released claims that have a value; never the values")
}

func TestReleasedClaimNames_StandardScopeWithoutMapping(t *testing.T) {
	attrs := map[string]interface{}{"email": "a@example.com", "email_verified": true}

	names := ReleasedClaimNames([]string{"openid", "email"}, nil, nil, attrs)

	assert.Equal(t, []string{"email", "email_verified"}, names)
}

func TestReleasedClaimNames_OpaqueAttributesKeepEveryReleasedName(t *testing.T) {
	scopeClaims := map[string][]string{"profile": {"name", "picture"}}
	attrs := map[string]interface{}{providers.RawJWTAttributeKey: "opaque-token"}

	names := ReleasedClaimNames([]string{"openid", "profile"}, scopeClaims, nil, attrs)

	assert.Equal(t, []string{"name", "picture", "sub"}, names,
		"the engine cannot see opaque values, so every name the scopes release is kept, sub of openid included")
}

func TestReleasedClaimNames_NothingReleased(t *testing.T) {
	names := ReleasedClaimNames([]string{"openid", "device_sso"}, nil, nil, nil)

	assert.NotNil(t, names)
	assert.Empty(t, names)
}
