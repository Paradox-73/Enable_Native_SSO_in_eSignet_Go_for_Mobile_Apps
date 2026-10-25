// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package token

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/model"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// Consent-only re-consent (rule R28): the interaction_required answer of a Native SSO exchange carries
// consent_uri and consent_expires_in, and is never cached.
func (suite *TokenHandlerTestSuite) TestHandleTokenRequest_NativeSSOConsentURI() {
	handler := suite.newHandler()
	formData := url.Values{}
	formData.Set("grant_type", string(providers.GrantTypeTokenExchange))
	req := suite.withClientContext(suite.buildRequest(formData), &providers.OAuthClient{ClientID: "test-client-id"})

	suite.mockTokenService.EXPECT().
		ProcessTokenRequest(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, &model.ErrorResponse{
			Error:            constants.ErrorInteractionRequired,
			ErrorDescription: "consent needed",
			ConsentURI:       "https://esignet.example/oauth2/native-sso/consent?ticket=t",
			ConsentExpiresIn: 300,
		})

	rr := httptest.NewRecorder()
	handler.HandleTokenRequest(rr, req)

	assert.Equal(suite.T(), http.StatusBadRequest, rr.Code)
	assert.Equal(suite.T(), "no-store", rr.Header().Get("Cache-Control"))
	assert.Contains(suite.T(), rr.Header().Get("Content-Type"), "application/json")
	var response map[string]interface{}
	assert.NoError(suite.T(), json.Unmarshal(rr.Body.Bytes(), &response))
	assert.Equal(suite.T(), "interaction_required", response["error"])
	assert.Equal(suite.T(), "consent needed", response["error_description"])
	assert.Equal(suite.T(), "https://esignet.example/oauth2/native-sso/consent?ticket=t", response["consent_uri"])
	assert.Equal(suite.T(), float64(300), response["consent_expires_in"])
}

// Without consent_uri the error keeps its two upstream members only.
func (suite *TokenHandlerTestSuite) TestHandleTokenRequest_InteractionRequiredWithoutConsentURI() {
	handler := suite.newHandler()
	formData := url.Values{}
	formData.Set("grant_type", string(providers.GrantTypeTokenExchange))
	req := suite.withClientContext(suite.buildRequest(formData), &providers.OAuthClient{ClientID: "test-client-id"})

	suite.mockTokenService.EXPECT().
		ProcessTokenRequest(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, &model.ErrorResponse{Error: constants.ErrorInteractionRequired, ErrorDescription: "login"})

	rr := httptest.NewRecorder()
	handler.HandleTokenRequest(rr, req)

	var response map[string]interface{}
	assert.NoError(suite.T(), json.Unmarshal(rr.Body.Bytes(), &response))
	assert.Len(suite.T(), response, 2)
	assert.NotContains(suite.T(), response, "consent_uri")
}
