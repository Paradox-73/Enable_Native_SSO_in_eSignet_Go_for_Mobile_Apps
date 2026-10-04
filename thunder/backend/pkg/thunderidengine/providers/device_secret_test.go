// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package providers

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/pkg/thunderidengine/common"
)

type DeviceSecretTestSuite struct {
	suite.Suite
}

func TestDeviceSecretSuite(t *testing.T) {
	suite.Run(t, new(DeviceSecretTestSuite))
}

func validDeviceSecretRequest() DeviceSecretRequest {
	return DeviceSecretRequest{
		ClientID:         "super-app",
		OUID:             "ou-1",
		Subject:          "user-1",
		AuthID:           "auth-1",
		SessionID:        "sid-1",
		Scopes:           []string{"openid", "profile", ScopeDeviceSSO},
		ACR:              "mosip:idp:acr:generated-code",
		Claims:           []string{"name", "email"},
		AuthTime:         1_790_000_000,
		DPoPJkt:          "0ZcOCORZNYy-DWpqq30jZyJGHTN0d2HglBV3uiguA4I",
		AttributeCacheID: "cache-1",
	}
}

func (suite *DeviceSecretTestSuite) TestDeviceSecretRequest_Validate_ValidRequest() {
	req := validDeviceSecretRequest()
	assert.NoError(suite.T(), req.Validate())
}

func (suite *DeviceSecretTestSuite) TestDeviceSecretRequest_Validate_OptionalFieldsMayBeEmpty() {
	req := validDeviceSecretRequest()
	req.OUID, req.ACR, req.Claims, req.AuthTime, req.AttributeCacheID = "", "", nil, 0, ""
	assert.NoError(suite.T(), req.Validate())
}

func (suite *DeviceSecretTestSuite) TestDeviceSecretRequest_Validate_MissingRequiredField() {
	cases := []struct {
		field  string
		mutate func(*DeviceSecretRequest)
	}{
		{"client_id", func(r *DeviceSecretRequest) { r.ClientID = "" }},
		{"sub", func(r *DeviceSecretRequest) { r.Subject = " " }},
		{"auth_id", func(r *DeviceSecretRequest) { r.AuthID = "" }},
		{"sid", func(r *DeviceSecretRequest) { r.SessionID = "" }},
		{"jkt", func(r *DeviceSecretRequest) { r.DPoPJkt = "" }},
	}
	for _, tc := range cases {
		suite.Run(tc.field, func() {
			req := validDeviceSecretRequest()
			tc.mutate(&req)
			err := req.Validate()
			suite.Require().Error(err)
			assert.True(suite.T(), errors.Is(err, ErrInvalidDeviceSecretRequest))
			assert.Contains(suite.T(), err.Error(), tc.field+" is required")
		})
	}
}

func (suite *DeviceSecretTestSuite) TestDeviceSecretRequest_Validate_ScopeWithoutDeviceSSO() {
	req := validDeviceSecretRequest()
	req.Scopes = []string{"openid", "profile"}
	err := req.Validate()
	suite.Require().Error(err)
	assert.True(suite.T(), errors.Is(err, ErrInvalidDeviceSecretRequest))
	assert.Contains(suite.T(), err.Error(), "scope must include device_sso")

	req.Scopes = nil
	assert.Error(suite.T(), req.Validate())
}

// The claims are what a provider keeps in its session record, so they must survive a JSON round
// trip with the request fields at the top level next to iss, iat and exp.
func (suite *DeviceSecretTestSuite) TestDeviceSecretClaims_JSONRoundTrip() {
	claims := DeviceSecretClaims{
		DeviceSecretRequest: validDeviceSecretRequest(),
		Issuer:              "https://esignet.example",
		IssuedAt:            1_790_000_010,
		ExpiresAt:           1_790_028_810,
	}

	raw, err := json.Marshal(claims)
	suite.Require().NoError(err)

	var flat map[string]interface{}
	suite.Require().NoError(json.Unmarshal(raw, &flat))
	assert.Equal(suite.T(), "super-app", flat["client_id"])
	assert.Equal(suite.T(), "sid-1", flat["sid"])
	assert.Equal(suite.T(), "auth-1", flat["auth_id"])
	assert.Equal(suite.T(), "https://esignet.example", flat["iss"])
	assert.NotContains(suite.T(), flat, "DeviceSecretRequest")

	var decoded DeviceSecretClaims
	suite.Require().NoError(json.Unmarshal(raw, &decoded))
	assert.Equal(suite.T(), claims, decoded)
}

func (suite *DeviceSecretTestSuite) TestDeviceSecretErrors_AreDistinctClientErrors() {
	for _, svcErr := range []common.ServiceError{ErrorInvalidDeviceSecret, ErrorDeviceSecretSessionEnded} {
		assert.Equal(suite.T(), common.ClientErrorType, svcErr.Type)
		assert.NotEmpty(suite.T(), svcErr.Error.Key)
		assert.NotEmpty(suite.T(), svcErr.Error.DefaultValue)
		assert.NotEmpty(suite.T(), svcErr.ErrorDescription.Key)
		assert.NotEmpty(suite.T(), svcErr.ErrorDescription.DefaultValue)
	}
	assert.NotEqual(suite.T(), ErrorInvalidDeviceSecret.Code, ErrorDeviceSecretSessionEnded.Code)
}

func (suite *DeviceSecretTestSuite) TestNativeSSOConstants() {
	assert.Equal(suite.T(), "device_sso", ScopeDeviceSSO)
	assert.Equal(suite.T(), "urn:openid:params:token-type:device-secret", TokenTypeDeviceSecret)
	assert.Equal(suite.T(), RuntimeStoreNamespace("nativesso:session"), NamespaceNativeSSOSession)
}
