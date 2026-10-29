// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package constants

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type NativeSSOConstantsTestSuite struct {
	suite.Suite
}

func TestNativeSSOConstantsSuite(t *testing.T) {
	suite.Run(t, new(NativeSSOConstantsTestSuite))
}

func (suite *NativeSSOConstantsTestSuite) TestNativeSSONames() {
	assert.Equal(suite.T(), "device_sso", ScopeDeviceSSO)
	assert.Equal(suite.T(), "sid", ClaimSessionID)
	assert.Equal(suite.T(), "ds_hash", ClaimDeviceSecretHash)
	assert.Equal(suite.T(), "device_secret", ClaimDeviceSecret)
	assert.Equal(suite.T(), "interaction_required", ErrorInteractionRequired)
	assert.Equal(suite.T(), TokenTypeIdentifier("urn:openid:params:token-type:device-secret"),
		TokenTypeIdentifierDeviceSecret)
}

// device_sso releases no claims and is granted only to clients configured for it, so it must not
// be among the standard scopes the standalone server advertises and gives to every new client.
func (suite *NativeSSOConstantsTestSuite) TestDeviceSSOScope_NotAStandardOIDCScope() {
	assert.NotContains(suite.T(), StandardOIDCScopes, ScopeDeviceSSO)
}

// The device-secret token type belongs only to the Native SSO profile of the token exchange, so the
// generic token-type checks keep rejecting it.
func (suite *NativeSSOConstantsTestSuite) TestDeviceSecretTokenType_NotAGenericTokenType() {
	assert.False(suite.T(), TokenTypeIdentifierDeviceSecret.IsValid())
	assert.True(suite.T(), TokenTypeIdentifierIDToken.IsValid())
}
