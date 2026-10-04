// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"encoding/json"

	"github.com/stretchr/testify/mock"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/system/cryptolib"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// nativeSSOMetadata returns the OIDC metadata of a discovery service built from the suite's settings
// with Native SSO on or off and the given grant and scope allow lists.
func (suite *DiscoveryTestSuite) nativeSSOMetadata(enabled bool, grants, scopes []string) *OIDCProviderMetadata {
	cfg := suite.oauthCfg
	cfg.NativeSSOEnabled = enabled
	cfg.OAuth.AllowedGrantTypes = grants
	cfg.OAuth.AllowedScopes = scopes
	suite.cryptoMock.EXPECT().GetPublicKeys(mock.Anything, providers.PublicKeyFilter{}).
		Return([]providers.PublicKeyInfo{{KeyID: "k1", Algorithm: string(cryptolib.AlgorithmRS256)}}, nil).Once()
	svc := newDiscoveryService(suite.cryptoMock, newTestJWEService(suite.cryptoMock), cfg)
	metadata, err := svc.GetOIDCMetadata(context.Background())
	suite.Require().NoError(err)
	return metadata
}

var nativeSSOTestGrants = []string{
	string(providers.GrantTypeAuthorizationCode), string(providers.GrantTypeTokenExchange),
}

// Rule R24: with a provider, discovery advertises native_sso_supported, device_sso and the
// token-exchange grant (Native SSO 5).
func (suite *DiscoveryTestSuite) TestDiscovery_NativeSSOAdvertised() {
	metadata := suite.nativeSSOMetadata(true, nativeSSOTestGrants, []string{"openid", "email"})

	suite.True(metadata.NativeSSOSupported)
	suite.Equal([]string{"openid", "email", constants.ScopeDeviceSSO}, metadata.ScopesSupported)
	suite.Contains(metadata.GrantTypesSupported, string(providers.GrantTypeTokenExchange))

	body, err := json.Marshal(metadata)
	suite.Require().NoError(err)
	suite.Contains(string(body), `"native_sso_supported":true`)
}

// A deployment that already lists device_sso gets it once.
func (suite *DiscoveryTestSuite) TestDiscovery_NativeSSOScopeListedOnce() {
	metadata := suite.nativeSSOMetadata(true, nativeSSOTestGrants,
		[]string{"openid", constants.ScopeDeviceSSO})

	suite.Equal([]string{"openid", constants.ScopeDeviceSSO}, metadata.ScopesSupported)
}

// An empty grant allow list allows every grant, so Native SSO is supported.
func (suite *DiscoveryTestSuite) TestDiscovery_NativeSSOWithUnrestrictedGrants() {
	metadata := suite.nativeSSOMetadata(true, nil, []string{"openid"})

	suite.True(metadata.NativeSSOSupported)
	suite.Contains(metadata.ScopesSupported, constants.ScopeDeviceSSO)
}

// Rule R03: without a provider there is no flag and no device_sso, even if a deployment lists it.
func (suite *DiscoveryTestSuite) TestDiscovery_NativeSSONotAdvertisedWithoutProvider() {
	configured := []string{"openid", constants.ScopeDeviceSSO}
	metadata := suite.nativeSSOMetadata(false, nativeSSOTestGrants, configured)

	suite.False(metadata.NativeSSOSupported)
	suite.Equal([]string{"openid"}, metadata.ScopesSupported)
	suite.Equal([]string{"openid", constants.ScopeDeviceSSO}, configured, "configured list must not change")

	body, err := json.Marshal(metadata)
	suite.Require().NoError(err)
	suite.NotContains(string(body), "native_sso_supported")
}

// Without the token-exchange grant no app could use a device secret, so Native SSO is not advertised.
func (suite *DiscoveryTestSuite) TestDiscovery_NativeSSONotAdvertisedWithoutTokenExchange() {
	metadata := suite.nativeSSOMetadata(true, []string{string(providers.GrantTypeAuthorizationCode)},
		[]string{"openid"})

	suite.False(metadata.NativeSSOSupported)
	suite.NotContains(metadata.ScopesSupported, constants.ScopeDeviceSSO)
}
