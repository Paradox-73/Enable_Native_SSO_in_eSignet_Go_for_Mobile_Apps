// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TokenModelTestSuite struct {
	suite.Suite
}

func TestTokenModelSuite(t *testing.T) {
	suite.Run(t, new(TokenModelTestSuite))
}

// A response without a device secret must look exactly as before Native SSO existed.
func (suite *TokenModelTestSuite) TestTokenResponse_JSONOmitsEmptyDeviceSecret() {
	raw, err := json.Marshal(TokenResponse{AccessToken: "at", TokenType: "DPoP", ExpiresIn: 300, IDToken: "idt"})
	suite.Require().NoError(err)

	assert.NotContains(suite.T(), string(raw), "device_secret")
	assert.JSONEq(suite.T(),
		`{"access_token":"at","token_type":"DPoP","expires_in":300,"id_token":"idt"}`, string(raw))
}

func (suite *TokenModelTestSuite) TestTokenResponse_JSONCarriesDeviceSecret() {
	raw, err := json.Marshal(TokenResponse{
		AccessToken: "at", TokenType: "DPoP", ExpiresIn: 300, Scope: "openid device_sso",
		IDToken: "idt", DeviceSecret: "eyJhbGciOiJSU0EtT0FFUC0yNTYifQ.ds",
	})
	suite.Require().NoError(err)

	var fields map[string]interface{}
	suite.Require().NoError(json.Unmarshal(raw, &fields))
	assert.Equal(suite.T(), "eyJhbGciOiJSU0EtT0FFUC0yNTYifQ.ds", fields["device_secret"])
	assert.Equal(suite.T(), "idt", fields["id_token"])
	assert.Equal(suite.T(), "at", fields["access_token"])
}
