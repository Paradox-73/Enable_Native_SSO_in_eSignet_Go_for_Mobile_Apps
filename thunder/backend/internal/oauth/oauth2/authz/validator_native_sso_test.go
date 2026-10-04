// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package authz

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	sysconfig "github.com/thunder-id/thunderid/internal/system/config"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// AuthorizeNativeSSOTestSuite covers the device_sso checks of the authorization request
// (OpenID Connect Native SSO for Mobile Apps 1.0, statement task 2).
type AuthorizeNativeSSOTestSuite struct {
	suite.Suite
	oauthApp *providers.OAuthClient
}

func TestAuthorizeNativeSSOSuite(t *testing.T) {
	suite.Run(t, new(AuthorizeNativeSSOTestSuite))
}

func (s *AuthorizeNativeSSOTestSuite) SetupTest() {
	sysconfig.ResetServerRuntime()
	s.Require().NoError(sysconfig.InitializeServerRuntime("/tmp/test", &sysconfig.Config{}))
	s.oauthApp = &providers.OAuthClient{
		ClientID:                "test-client-id",
		RedirectURIs:            []string{"https://client.example.com/callback"},
		GrantTypes:              []providers.GrantType{providers.GrantTypeAuthorizationCode},
		ResponseTypes:           []providers.ResponseType{providers.ResponseTypeCode},
		TokenEndpointAuthMethod: providers.TokenEndpointAuthMethodPrivateKeyJWT,
		Scopes:                  []string{"openid", "profile", constants.ScopeDeviceSSO},
	}
}

func (s *AuthorizeNativeSSOTestSuite) TearDownTest() {
	sysconfig.ResetServerRuntime()
}

func (s *AuthorizeNativeSSOTestSuite) validate(enabled bool, scope string) (bool, string, string) {
	msg := &OAuthMessage{
		RequestQueryParams: url.Values{
			constants.RequestParamClientID:     {"test-client-id"},
			constants.RequestParamRedirectURI:  {"https://client.example.com/callback"},
			constants.RequestParamResponseType: {string(providers.ResponseTypeCode)},
			constants.RequestParamScope:        {scope},
		},
	}
	return newAuthorizationValidator(enabled).validateInitialAuthorizationRequest(context.Background(), msg, s.oauthApp)
}

func (s *AuthorizeNativeSSOTestSuite) TestDeviceSSOAllowedPasses() {
	sendToApp, errCode, _ := s.validate(true, "openid profile device_sso")

	s.False(sendToApp)
	s.Empty(errCode)
}

func (s *AuthorizeNativeSSOTestSuite) TestDeviceSSOClientNotAllowedIsRedirectedAsInvalidScope() {
	s.oauthApp.Scopes = []string{"openid", "profile"}

	sendToApp, errCode, errDesc := s.validate(true, "openid device_sso")

	s.True(sendToApp, "the redirect URI is valid, so the error goes back to the client")
	s.Equal(constants.ErrorInvalidScope, errCode)
	s.Contains(errDesc, "not allowed")
}

func (s *AuthorizeNativeSSOTestSuite) TestDeviceSSOWithoutProviderIsInvalidScope() {
	sendToApp, errCode, errDesc := s.validate(false, "openid device_sso")

	s.True(sendToApp)
	s.Equal(constants.ErrorInvalidScope, errCode)
	s.Contains(errDesc, "not supported")
}

func (s *AuthorizeNativeSSOTestSuite) TestNoDeviceSSOIsUnchanged() {
	s.oauthApp.Scopes = nil

	sendToApp, errCode, _ := s.validate(false, "openid profile")

	s.False(sendToApp)
	s.Empty(errCode)
}
