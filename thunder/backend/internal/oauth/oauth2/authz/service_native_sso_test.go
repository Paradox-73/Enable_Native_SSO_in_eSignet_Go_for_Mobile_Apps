// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package authz

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	oauthconfig "github.com/thunder-id/thunderid/internal/oauth/config"
	oauth2model "github.com/thunder-id/thunderid/internal/oauth/oauth2/model"
)

// TestCreateAuthorizationCode_DeviceSSOCarriesAuthID checks that a code granted device_sso keeps the id of
// its authorization request, which the device secret carries as auth_id (statement task 5), and that any
// other code stays as upstream issues it.
func TestCreateAuthorizationCode_DeviceSSOCarriesAuthID(t *testing.T) {
	cases := []struct {
		name       string
		scopes     []string
		wantAuthID string
	}{
		{name: "device_sso granted", scopes: []string{"openid", "device_sso"}, wantAuthID: "auth-request-id"},
		{name: "no device_sso", scopes: []string{"openid", "profile"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authCtx := &authRequestContext{
				OAuthParameters: oauth2model.OAuthParameters{
					ClientID:       "test-client",
					RedirectURI:    "https://client.example.com/callback",
					StandardScopes: tc.scopes,
				},
			}
			claims := &assertionClaims{userID: "user-1", authorizationRequestID: "auth-request-id"}

			code, err := createAuthorizationCode(oauthconfig.Config{}, authCtx, claims, time.Now())

			require.NoError(t, err)
			assert.Equal(t, tc.wantAuthID, code.AuthID)
			assert.Contains(t, code.Scopes, tc.scopes[1])
		})
	}
}
