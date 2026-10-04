// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
)

// TestSeparateOIDCAndNonOIDCScopes_DeviceSSO checks that device_sso stays with the OIDC scopes without
// a mapping, so it reaches the authorization code instead of being treated as a resource permission.
func TestSeparateOIDCAndNonOIDCScopes_DeviceSSO(t *testing.T) {
	cases := []struct {
		name        string
		scopes      string
		mapping     map[string][]string
		wantOIDC    []string
		wantNonOIDC []string
	}{
		{
			name:     "with openid and no mapping",
			scopes:   "openid device_sso",
			wantOIDC: []string{"openid", "device_sso"},
		},
		{
			name:        "next to a permission scope",
			scopes:      "openid profile device_sso read",
			mapping:     map[string][]string{"profile": {"name"}},
			wantOIDC:    []string{"openid", "profile", "device_sso"},
			wantNonOIDC: []string{"read"},
		},
		{
			name:     "alone",
			scopes:   "device_sso",
			wantOIDC: []string{"device_sso"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oidcScopes, nonOidcScopes := SeparateOIDCAndNonOIDCScopes(tc.scopes, tc.mapping)
			assert.Equal(t, tc.wantOIDC, oidcScopes)
			assert.Equal(t, tc.wantNonOIDC, nonOidcScopes)
		})
	}
}

// TestResolveScopeClaims_DeviceSSOReleasesNothing checks that device_sso adds no user claims to tokens.
func TestResolveScopeClaims_DeviceSSOReleasesNothing(t *testing.T) {
	assert.Nil(t, ResolveScopeClaims(constants.ScopeDeviceSSO, map[string][]string{"profile": {"name"}}))
}
