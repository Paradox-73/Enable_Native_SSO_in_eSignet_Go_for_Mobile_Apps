// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package granthandlers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/internal/attributecache"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/authz"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/dpop"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/model"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/nativesso"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/tokenservice"
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
	"github.com/thunder-id/thunderid/tests/mocks/attributecachemock"
	"github.com/thunder-id/thunderid/tests/mocks/devicesecretprovidermock"
	"github.com/thunder-id/thunderid/tests/mocks/oauth/oauth2/authzmock"
	"github.com/thunder-id/thunderid/tests/mocks/oauth/oauth2/tokenservicemock"
	"github.com/thunder-id/thunderid/tests/mocks/resourcemock"
	"github.com/thunder-id/thunderid/tests/testhelpers"
)

const (
	nativeSSOAuthID       = "auth-request-id"
	nativeSSODeviceSecret = "device-secret-value"
	// nativeSSODSHash is DeviceSecretHash(nativeSSODeviceSecret).
	nativeSSODSHash      = "Ad3fRNZ5FyNN5SYczCVEuQ"
	nativeSSODSLifetime  = int64(28800)
	nativeSSOCodeScopes  = "openid profile device_sso"
	nativeSSORefreshTTL  = int64(86400)
	nativeSSOAccessTTL   = int64(3600)
	nativeSSOCompleteACR = "mosip:idp:acr:generated-code"
)

// AuthorizationCodeNativeSSOTestSuite covers the device_sso part of the authorization-code grant
// (OpenID Connect Native SSO for Mobile Apps 1.0; statement tasks 2, 5 and 6; rules R01, R02, R05 to R08).
type AuthorizationCodeNativeSSOTestSuite struct {
	suite.Suite
	handler       *authorizationCodeGrantHandler
	tokenBuilder  *tokenservicemock.TokenBuilderInterfaceMock
	authzService  *authzmock.AuthorizeServiceInterfaceMock
	attrCache     *attributecachemock.AttributeCacheServiceInterfaceMock
	dsProvider    *devicesecretprovidermock.DeviceSecretProviderMock
	oauthApp      *providers.OAuthClient
	authCode      authz.AuthorizationCode
	tokenRequest  *model.TokenRequest
	dpopCtx       context.Context
	builtIDTokens []*tokenservice.IDTokenBuildContext
}

func TestAuthorizationCodeNativeSSOSuite(t *testing.T) {
	suite.Run(t, new(AuthorizationCodeNativeSSOTestSuite))
}

func (s *AuthorizationCodeNativeSSOTestSuite) SetupTest() {
	s.tokenBuilder = tokenservicemock.NewTokenBuilderInterfaceMock(s.T())
	s.authzService = authzmock.NewAuthorizeServiceInterfaceMock(s.T())
	s.attrCache = attributecachemock.NewAttributeCacheServiceInterfaceMock(s.T())
	s.dsProvider = devicesecretprovidermock.NewDeviceSecretProviderMock(s.T())
	resourceService := resourcemock.NewResourceServiceInterfaceMock(s.T())
	s.builtIDTokens = nil

	s.handler = newAuthorizationCodeGrantHandler(s.authzService, s.tokenBuilder, s.attrCache, resourceService,
		s.dsProvider, testhelpers.OAuthConfig()).(*authorizationCodeGrantHandler)

	s.oauthApp = &providers.OAuthClient{
		OUID:                  "vendor-ou",
		ClientID:              testClientID,
		RedirectURIs:          []string{testClientCallbackURL},
		GrantTypes:            []providers.GrantType{providers.GrantTypeAuthorizationCode},
		ResponseTypes:         []providers.ResponseType{providers.ResponseTypeCode},
		DPoPBoundAccessTokens: true,
		Scopes:                []string{"openid", "profile", constants.ScopeDeviceSSO},
		ScopeClaims:           map[string][]string{"profile": {"name"}},
	}
	s.authCode = authz.AuthorizationCode{
		CodeID:              "test-code-id",
		Code:                "test-auth-code",
		ClientID:            testClientID,
		RedirectURI:         testClientCallbackURL,
		RedirectURIProvided: true,
		AuthorizedUserID:    testUserID,
		AttributeCacheID:    testCacheID,
		TimeCreated:         time.Now().Add(-time.Minute),
		ExpiryTime:          time.Now().Add(4 * time.Minute),
		Scopes:              nativeSSOCodeScopes,
		State:               authz.AuthCodeStateActive,
		CompletedACR:        nativeSSOCompleteACR,
		DPoPJkt:             testDPoPThumbprint,
		AuthID:              nativeSSOAuthID,
	}
	s.tokenRequest = &model.TokenRequest{
		GrantType:   string(providers.GrantTypeAuthorizationCode),
		ClientID:    testClientID,
		Code:        "test-auth-code",
		RedirectURI: testClientCallbackURL,
	}
	s.dpopCtx = dpop.WithJkt(context.Background(), testDPoPThumbprint)
}

// expectCode returns the authorization code and the user attributes of the login.
func (s *AuthorizationCodeNativeSSOTestSuite) expectCode() {
	s.authzService.EXPECT().GetAuthorizationCodeDetails(mock.Anything, testClientID, "test-auth-code").
		Return(&s.authCode, nil)
}

func (s *AuthorizationCodeNativeSSOTestSuite) expectAttributes() {
	s.attrCache.EXPECT().GetAttributeCache(mock.Anything, testCacheID).Return(&attributecache.AttributeCache{
		ID:         testCacheID,
		Attributes: map[string]interface{}{"name": "Asha", "email": "asha@example.com"},
	}, nil)
}

func (s *AuthorizationCodeNativeSSOTestSuite) expectAccessToken() {
	s.tokenBuilder.EXPECT().BuildAccessToken(mock.Anything, mock.Anything).Return(&model.TokenDTO{
		Token:     "access-token",
		TokenType: constants.TokenTypeDPoP,
		ExpiresIn: nativeSSOAccessTTL,
		Scopes:    []string{"openid", "profile", constants.ScopeDeviceSSO},
		ClientID:  testClientID,
		Subject:   testUserID,
	}, nil)
}

func (s *AuthorizationCodeNativeSSOTestSuite) expectIDToken() {
	s.tokenBuilder.EXPECT().BuildIDToken(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, idCtx *tokenservice.IDTokenBuildContext) (*model.TokenDTO, error) {
			s.builtIDTokens = append(s.builtIDTokens, idCtx)
			return &model.TokenDTO{Token: "id-token"}, nil
		})
}

func (s *AuthorizationCodeNativeSSOTestSuite) expectDeviceSecret() *providers.DeviceSecretRequest {
	var got providers.DeviceSecretRequest
	s.dsProvider.EXPECT().GenerateDeviceSecret(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, req providers.DeviceSecretRequest) (
			*providers.DeviceSecret, *tidcommon.ServiceError) {
			got = req
			return &providers.DeviceSecret{
				Value:     nativeSSODeviceSecret,
				ExpiresAt: time.Now().Unix() + nativeSSODSLifetime,
			}, nil
		})
	return &got
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSSO_AllowedClientGetsDeviceSecret() {
	s.expectCode()
	s.expectAttributes()
	s.expectAccessToken()
	req := s.expectDeviceSecret()
	var extendedTTL int
	s.attrCache.EXPECT().ExtendAttributeCacheTTL(mock.Anything, testCacheID, mock.Anything).
		RunAndReturn(func(_ context.Context, _ string, ttl int) *tidcommon.ServiceError {
			extendedTTL = ttl
			return nil
		})
	s.expectIDToken()

	resp, errResp := s.handler.HandleGrant(s.dpopCtx, s.tokenRequest, s.oauthApp)

	s.Require().Nil(errResp)
	s.Require().NotNil(resp)
	s.Equal("access-token", resp.AccessToken.Token)
	s.Equal("id-token", resp.IDToken.Token)
	s.Equal(nativeSSODeviceSecret, resp.DeviceSecret.Token)
	s.InDelta(nativeSSODSLifetime, resp.DeviceSecret.ExpiresIn, 5)

	// The provider receives everything the token exchange needs later (statement task 5).
	s.Equal(testClientID, req.ClientID)
	s.Equal("vendor-ou", req.OUID)
	s.Equal(testUserID, req.Subject)
	s.Equal(nativeSSOAuthID, req.AuthID)
	s.Equal(testDPoPThumbprint, req.DPoPJkt)
	s.Equal(nativeSSOCompleteACR, req.ACR)
	s.Equal(testCacheID, req.AttributeCacheID)
	s.Equal(s.authCode.TimeCreated.Unix(), req.AuthTime)
	s.Equal([]string{"openid", "profile", constants.ScopeDeviceSSO}, req.Scopes)
	s.Equal([]string{"name"}, req.Claims, "names of released claims only; email was not released")
	s.Len(req.SessionID, 22)

	// The id_token carries the same sid and the ds_hash of the device secret (rule R05).
	s.Require().Len(s.builtIDTokens, 1)
	s.Equal(req.SessionID, s.builtIDTokens[0].SessionID)
	s.Equal(nativeSSODSHash, s.builtIDTokens[0].DeviceSecretHash)

	// The cache outlives both the device secret and the longest token.
	s.GreaterOrEqual(int64(extendedTTL), nativeSSODSLifetime-5+constants.AttributeCacheTTLBufferSeconds)
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSSO_SessionIDIsNewForEveryGrant() {
	s.authzService.EXPECT().GetAuthorizationCodeDetails(mock.Anything, testClientID, "test-auth-code").
		Return(&s.authCode, nil).Twice()
	s.attrCache.EXPECT().GetAttributeCache(mock.Anything, testCacheID).
		Return(&attributecache.AttributeCache{ID: testCacheID, Attributes: map[string]interface{}{}}, nil).Twice()
	s.expectAccessToken()
	s.expectDeviceSecret()
	s.attrCache.EXPECT().ExtendAttributeCacheTTL(mock.Anything, testCacheID, mock.Anything).Return(nil)
	s.expectIDToken()

	_, err1 := s.handler.HandleGrant(s.dpopCtx, s.tokenRequest, s.oauthApp)
	_, err2 := s.handler.HandleGrant(s.dpopCtx, s.tokenRequest, s.oauthApp)

	s.Require().Nil(err1)
	s.Require().Nil(err2)
	s.Require().Len(s.builtIDTokens, 2)
	s.NotEqual(s.builtIDTokens[0].SessionID, s.builtIDTokens[1].SessionID)
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSSO_ClientNotAllowedIsInvalidScope() {
	s.oauthApp.Scopes = []string{"openid", "profile"}
	s.expectCode()

	resp, errResp := s.handler.HandleGrant(s.dpopCtx, s.tokenRequest, s.oauthApp)

	s.Nil(resp)
	s.Require().NotNil(errResp)
	s.Equal(constants.ErrorInvalidScope, errResp.Error)
	s.Contains(errResp.ErrorDescription, "not allowed")
	s.dsProvider.AssertNotCalled(s.T(), "GenerateDeviceSecret", mock.Anything, mock.Anything)
	s.tokenBuilder.AssertNotCalled(s.T(), "BuildAccessToken", mock.Anything, mock.Anything)
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSSO_WithoutProviderIsInvalidScope() {
	s.handler.deviceSecretProvider = nil
	s.expectCode()

	resp, errResp := s.handler.HandleGrant(s.dpopCtx, s.tokenRequest, s.oauthApp)

	s.Nil(resp)
	s.Require().NotNil(errResp)
	s.Equal(constants.ErrorInvalidScope, errResp.Error)
	s.Contains(errResp.ErrorDescription, "not supported")
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSSO_MissingDPoPProofIsInvalidRequest() {
	// A client that is not DPoP-bound reaches the handler without a proof; the code itself is not
	// DPoP-bound either, so only the device_sso rule stops it (rule R07).
	s.oauthApp.DPoPBoundAccessTokens = false
	s.authCode.DPoPJkt = ""
	s.expectCode()

	resp, errResp := s.handler.HandleGrant(context.Background(), s.tokenRequest, s.oauthApp)

	s.Nil(resp)
	s.Require().NotNil(errResp)
	s.Equal(constants.ErrorInvalidRequest, errResp.Error)
	s.Equal(errDescDeviceSSORequiresDPoP, errResp.ErrorDescription)
	s.tokenBuilder.AssertNotCalled(s.T(), "BuildAccessToken", mock.Anything, mock.Anything)
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSSO_ProviderFailureIssuesNoTokens() {
	s.expectCode()
	s.expectAttributes()
	s.expectAccessToken()
	s.dsProvider.EXPECT().GenerateDeviceSecret(mock.Anything, mock.Anything).
		Return(nil, &tidcommon.InternalServerError)

	resp, errResp := s.handler.HandleGrant(s.dpopCtx, s.tokenRequest, s.oauthApp)

	s.Nil(resp, "no access token and no id_token without the device secret (rule R08)")
	s.Require().NotNil(errResp)
	s.Equal(constants.ErrorServerError, errResp.Error)
	s.Equal(errDescDeviceSecretFailed, errResp.ErrorDescription)
	s.tokenBuilder.AssertNotCalled(s.T(), "BuildIDToken", mock.Anything, mock.Anything)
	s.attrCache.AssertNotCalled(s.T(), "ExtendAttributeCacheTTL", mock.Anything, mock.Anything, mock.Anything)
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSSO_UnusableDeviceSecretIsServerError() {
	cases := []struct {
		name   string
		secret *providers.DeviceSecret
	}{
		{name: "nil", secret: nil},
		{name: "empty value", secret: &providers.DeviceSecret{ExpiresAt: time.Now().Unix() + 60}},
		{name: "already expired", secret: &providers.DeviceSecret{Value: "v", ExpiresAt: time.Now().Unix() - 1}},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.expectCode()
			s.expectAttributes()
			s.expectAccessToken()
			s.dsProvider.EXPECT().GenerateDeviceSecret(mock.Anything, mock.Anything).Return(tc.secret, nil)

			resp, errResp := s.handler.HandleGrant(s.dpopCtx, s.tokenRequest, s.oauthApp)

			s.Nil(resp)
			s.Require().NotNil(errResp)
			s.Equal(constants.ErrorServerError, errResp.Error)
			s.tokenBuilder.AssertNotCalled(s.T(), "BuildIDToken", mock.Anything, mock.Anything)
		})
	}
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSSO_IncompleteDeviceSecretRequestIsServerError() {
	// A code issued without its authorization request id cannot produce a device secret that the token
	// exchange could check, so the provider is not called.
	s.authCode.AuthID = ""
	s.expectCode()
	s.expectAttributes()
	s.expectAccessToken()

	resp, errResp := s.handler.HandleGrant(s.dpopCtx, s.tokenRequest, s.oauthApp)

	s.Nil(resp)
	s.Require().NotNil(errResp)
	s.Equal(constants.ErrorServerError, errResp.Error)
	s.dsProvider.AssertNotCalled(s.T(), "GenerateDeviceSecret", mock.Anything, mock.Anything)
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSecret_AttributeCacheFailureIssuesNoTokens() {
	s.expectCode()
	s.expectAttributes()
	s.expectAccessToken()
	s.expectDeviceSecret()
	s.attrCache.EXPECT().ExtendAttributeCacheTTL(mock.Anything, testCacheID, mock.Anything).
		Return(&attributecache.ErrorAttributeCacheNotFound)

	resp, errResp := s.handler.HandleGrant(s.dpopCtx, s.tokenRequest, s.oauthApp)

	s.Nil(resp)
	s.Require().NotNil(errResp)
	s.Equal(constants.ErrorServerError, errResp.Error)
	s.tokenBuilder.AssertNotCalled(s.T(), "BuildIDToken", mock.Anything, mock.Anything)
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSecret_NoAttributeCacheSkipsExtension() {
	s.authCode.AttributeCacheID = ""
	s.expectCode()
	s.expectAccessToken()
	req := s.expectDeviceSecret()
	s.expectIDToken()

	resp, errResp := s.handler.HandleGrant(s.dpopCtx, s.tokenRequest, s.oauthApp)

	s.Require().Nil(errResp)
	s.Equal(nativeSSODeviceSecret, resp.DeviceSecret.Token)
	s.Empty(req.AttributeCacheID)
	s.attrCache.AssertNotCalled(s.T(), "ExtendAttributeCacheTTL", mock.Anything, mock.Anything, mock.Anything)
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestNoDeviceSSO_ResponseIsUnchanged() {
	s.authCode.Scopes = "openid profile"
	s.authCode.AuthID = ""
	s.expectCode()
	s.expectAttributes()
	s.expectAccessToken()
	s.expectIDToken()

	resp, errResp := s.handler.HandleGrant(s.dpopCtx, s.tokenRequest, s.oauthApp)

	s.Require().Nil(errResp)
	s.Empty(resp.DeviceSecret.Token)
	s.Require().Len(s.builtIDTokens, 1)
	s.Empty(s.builtIDTokens[0].SessionID)
	s.Empty(s.builtIDTokens[0].DeviceSecretHash)
	s.dsProvider.AssertNotCalled(s.T(), "GenerateDeviceSecret", mock.Anything, mock.Anything)
	s.attrCache.AssertNotCalled(s.T(), "ExtendAttributeCacheTTL", mock.Anything, mock.Anything, mock.Anything)
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestNoDeviceSSO_ProviderNotNeeded() {
	s.handler.deviceSecretProvider = nil
	s.authCode.Scopes = "openid"
	s.authCode.AttributeCacheID = ""
	s.authCode.DPoPJkt = ""
	s.expectCode()
	s.expectAccessToken()
	s.expectIDToken()

	resp, errResp := s.handler.HandleGrant(context.Background(), s.tokenRequest, s.oauthApp)

	s.Require().Nil(errResp)
	s.Empty(resp.DeviceSecret.Token)
}

func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSecret_AttributeCacheTTL() {
	// Device secret longest: its lifetime plus the buffer.
	s.Equal(nativeSSODSLifetime+constants.AttributeCacheTTLBufferSeconds,
		s.handler.attributeCacheTTL(s.oauthApp, nativeSSODSLifetime, nativeSSOAccessTTL))

	// A refresh token that outlives the device secret keeps the cache alive for its own lifetime.
	s.oauthApp.GrantTypes = append(s.oauthApp.GrantTypes, providers.GrantTypeRefreshToken)
	s.Equal(nativeSSORefreshTTL+constants.AttributeCacheTTLBufferSeconds,
		s.handler.attributeCacheTTL(s.oauthApp, nativeSSODSLifetime, nativeSSOAccessTTL))

	// An access token longer than both.
	s.Equal(int64(100000)+constants.AttributeCacheTTLBufferSeconds,
		s.handler.attributeCacheTTL(s.oauthApp, nativeSSODSLifetime, 100000))
}

// TestDeviceSSO_HashMatchesNativeSSOPackage keeps the fixture constant honest.
func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSSO_HashMatchesNativeSSOPackage() {
	s.Equal(nativesso.DeviceSecretHash(nativeSSODeviceSecret, false), nativeSSODSHash)
}

// With oauth.native_sso.ds_hash_full_sha256 the id_token carries the full SHA-256 form of ds_hash.
func (s *AuthorizationCodeNativeSSOTestSuite) TestDeviceSSO_FullSHA256DSHash() {
	s.handler.cfg.OAuth.NativeSSO.DSHashFullSHA256 = true
	s.expectCode()
	s.expectAttributes()
	s.expectAccessToken()
	s.expectDeviceSecret()
	s.attrCache.EXPECT().ExtendAttributeCacheTTL(mock.Anything, testCacheID, mock.Anything).Return(nil)
	s.expectIDToken()

	_, errResp := s.handler.HandleGrant(s.dpopCtx, s.tokenRequest, s.oauthApp)

	s.Require().Nil(errResp)
	s.Require().Len(s.builtIDTokens, 1)
	s.Equal(nativesso.DeviceSecretHash(nativeSSODeviceSecret, true), s.builtIDTokens[0].DeviceSecretHash)
	s.Len(s.builtIDTokens[0].DeviceSecretHash, 43)
}
