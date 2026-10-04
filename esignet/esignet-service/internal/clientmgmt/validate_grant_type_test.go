/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package clientmgmt

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

const tokenExchangeGrantType = "urn:ietf:params:oauth:grant-type:token-exchange"

// GrantTypeValidationTestSuite covers the grant types a client may register, including the
// token-exchange grant of OpenID Connect Native SSO for Mobile Apps 1.0.
type GrantTypeValidationTestSuite struct {
	suite.Suite
}

func TestGrantTypeValidationTestSuite(t *testing.T) {
	suite.Run(t, new(GrantTypeValidationTestSuite))
}

func (ts *GrantTypeValidationTestSuite) TestValidGrantTypes() {
	for name, grants := range map[string][]string{
		"authorization code only": {"authorization_code"},
		"token exchange only":     {tokenExchangeGrantType},
		"both":                    {"authorization_code", tokenExchangeGrantType},
	} {
		ts.Run(name, func() {
			ts.NoError(validateGrantTypes(grants, 1, 3))
		})
	}
}

func (ts *GrantTypeValidationTestSuite) TestInvalidGrantTypes() {
	for name, grants := range map[string][]string{
		"implicit":                      {"implicit"},
		"refresh token":                 {"refresh_token"},
		"client credentials":            {"client_credentials"},
		"older token exchange spelling": {"urn:x-oath:params:oauth:grant-type:token-exchange"},
		"token exchange twice":          {tokenExchangeGrantType, tokenExchangeGrantType},
		"empty string":                  {""},
	} {
		ts.Run(name, func() {
			ts.Equal("invalid_grant_type", errCode(ts.T(), validateGrantTypes(grants, 1, 3)))
		})
	}
}

func (ts *GrantTypeValidationTestSuite) TestGrantTypeOnCreateUpdateAndPatch() {
	ts.Run("create accepts token exchange", func() {
		req := validCreateRequest()
		req.GrantTypes = []string{"authorization_code", tokenExchangeGrantType}
		ts.NoError(ValidateCreate(ProfileOIDC, req, nil))
	})
	ts.Run("create rejects an unknown grant", func() {
		req := validCreateRequest()
		req.GrantTypes = []string{tokenExchangeGrantType, "password"}
		ts.Equal("invalid_grant_type", errCode(ts.T(), ValidateCreate(ProfileOIDC, req, nil)))
	})
	ts.Run("update accepts token exchange", func() {
		req := validUpdateRequest()
		req.GrantTypes = []string{tokenExchangeGrantType}
		ts.NoError(ValidateUpdate(ProfileOIDC, req))
	})
	ts.Run("patch accepts token exchange", func() {
		merged := validUpdateRequest()
		merged.GrantTypes = []string{"authorization_code", tokenExchangeGrantType}
		ts.NoError(ValidatePatch(ProfileOIDC, merged, PatchFields{GrantTypes: true}, NullableJWK{}, nil))
	})
}
