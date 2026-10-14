// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package granthandlers

import (
	"errors"

	"github.com/stretchr/testify/mock"

	"github.com/thunder-id/thunderid/internal/attributecache"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	engineconfig "github.com/thunder-id/thunderid/pkg/thunderidengine/config"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

var errTestBuild = errors.New("signing failed")

// Scope rules, re-consent signal and the second app's tokens of the Native SSO token exchange
// (statement task 10, bonus B1; rules R15 to R17 and R23).

// T10b: device_sso in the exchange is invalid_scope (rule R16).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Scope_DeviceSSOInExchangeRejected() {
	s.request.Scope = "openid email device_sso"
	errResp := s.handler.ValidateGrant(s.ctx, s.request, s.childApp)
	s.assertError(errResp, constants.ErrorInvalidScope)
	s.Contains(errResp.ErrorDescription, constants.ScopeDeviceSSO)
}

// Native SSO 4.1: a requested scope without openid is invalid_scope (rule R16).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Scope_WithoutOpenIDRejected() {
	s.request.Scope = "email"
	s.assertError(s.handler.ValidateGrant(s.ctx, s.request, s.childApp), constants.ErrorInvalidScope)
}

// T10a and B1: a wider scope fails with interaction_required by default, naming only the missing values
// (rule R15).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Scope_SupersetInteractionRequired() {
	s.request.Scope = "openid email phone address"
	s.Nil(s.handler.ValidateGrant(s.ctx, s.request, s.childApp))
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInteractionRequired)
	s.Contains(errResp.ErrorDescription, "phone address")
	s.NotContains(errResp.ErrorDescription, "email")
}

// T10a: with scope_exceeded_error set to invalid_scope the same request fails with invalid_scope.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Scope_SupersetInvalidScopeSetting() {
	s.handler.cfg.OAuth.NativeSSO.ScopeExceededError = engineconfig.NativeSSOScopeExceededInvalidScope
	s.request.Scope = "openid phone"
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInvalidScope)
	s.Contains(errResp.ErrorDescription, "phone")
}

// The scope check runs after every binding check: a wider scope with a bad device secret is still
// rejected for the device secret, so the re-consent signal is given only to a genuine second app.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Scope_SupersetAfterBindingChecks() {
	s.request.Scope = "openid phone"
	s.dsProvider.EXPECT().ValidateDeviceSecret(mock.Anything, nativeSSODeviceSecret).
		Return(nil, &providers.ErrorInvalidDeviceSecret)

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInvalidRequest)
}

// T10a: a narrower scope is allowed and is what the tokens carry (rule R17).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Scope_NarrowerAllowed() {
	s.request.Scope = "openid email email"
	s.expectChecksPass()
	s.expectIssue()

	resp, errResp := s.handle()

	s.Require().Nil(errResp)
	s.Equal([]string{"openid", "email"}, resp.AccessToken.Scopes)
	s.Equal([]string{"openid", "email"}, s.builtID[0].Scopes)
}

// No scope: the scope granted to the first app without device_sso (rule R17).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Scope_DefaultIsGrantedScope() {
	s.expectChecksPass()
	s.expectIssue()

	resp, errResp := s.handle()

	s.Require().Nil(errResp)
	s.Equal([]string{"openid", "profile", "email"}, resp.AccessToken.Scopes)
}

// Rule R23: the second app's access token carries aci and the Hub key; its id_token has aud = second app,
// the same sub, sid, acr and auth_time, and no ds_hash; claims are those both released to the first app and
// released by the second app's scope.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ChildAppTokens() {
	s.request.Scope = "openid email"
	s.expectChecksPass()
	s.expectIssue()

	resp, errResp := s.handle()

	s.Require().Nil(errResp)
	s.Equal("child-id-token", resp.IDToken.Token)
	access := s.builtAccess[0]
	s.Equal(testCacheID, access.AttributeCacheID)
	s.Equal(testDPoPThumbprint, access.DPoPJkt)

	s.Require().Len(s.builtID, 1)
	id := s.builtID[0]
	s.Equal(nssoChildClientID, id.Audience)
	s.Equal(testUserID, id.Subject)
	s.Equal(nssoSessionID, id.SessionID)
	s.Equal(nativeSSOCompleteACR, id.CompletedACR)
	s.Equal(s.dsClaims.AuthTime, id.AuthTime)
	s.Empty(id.DeviceSecretHash)
	s.Empty(id.Nonce)
	s.Same(s.childApp, id.OAuthApp)
	// email: released to the first app and by the second app's scope. name: released to the first app
	// but not requested. phone_number: cached but never released.
	s.Equal(map[string]interface{}{"email": "asha@example.com"}, id.UserAttributes)
}

// The second app's own scope-to-claims mapping decides which claims a scope releases.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ChildAppClaimsFollowItsScopeMapping() {
	s.childApp.ScopeClaims = map[string][]string{"email": {"email", "name"}}
	s.request.Scope = "openid email"
	s.expectChecksPass()
	s.expectIssue()

	_, errResp := s.handle()

	s.Require().Nil(errResp)
	s.Equal(map[string]interface{}{"email": "asha@example.com", "name": "Asha"}, s.builtID[0].UserAttributes)
	s.Require().Len(s.reused, 1)
	s.ElementsMatch([]string{"email", "name"}, s.reused[0].Claims, "the consent record names the released claims")
}

// An opaque attribute token from the identity system (eSignet) is passed on unchanged.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ChildAppOpaqueAttributesPassedOn() {
	s.expectChecksPass()
	s.attrCache.EXPECT().GetAttributeCache(mock.Anything, testCacheID).Return(&attributecache.AttributeCache{
		ID: testCacheID, Attributes: map[string]interface{}{providers.RawJWTAttributeKey: "opaque"},
	}, nil)
	s.expectAccessToken()
	s.expectChildIDToken()

	_, errResp := s.handle()

	s.Require().Nil(errResp)
	s.Equal(map[string]interface{}{providers.RawJWTAttributeKey: "opaque"}, s.builtID[0].UserAttributes)
}

func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ChildAppWithoutAttributeCache() {
	s.dsClaims.AttributeCacheID = ""
	s.expectChecksPass()
	s.expectAccessToken()
	s.expectChildIDToken()

	_, errResp := s.handle()

	s.Require().Nil(errResp)
	s.Empty(s.builtAccess[0].AttributeCacheID)
	s.Empty(s.builtID[0].UserAttributes)
}

func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ChildAppAttributeCacheErrors() {
	s.Run("cache expired", func() {
		s.SetupTest()
		s.expectChecksPass()
		s.attrCache.EXPECT().GetAttributeCache(mock.Anything, testCacheID).
			Return(nil, &attributecache.ErrorAttributeCacheNotFound)
		_, errResp := s.handle()
		s.assertError(errResp, constants.ErrorInvalidGrant)
	})
	s.Run("cache failure", func() {
		s.SetupTest()
		s.expectChecksPass()
		s.attrCache.EXPECT().GetAttributeCache(mock.Anything, testCacheID).
			Return(nil, &attributecache.ErrorMissingCacheID)
		_, errResp := s.handle()
		s.assertError(errResp, constants.ErrorServerError)
	})
	s.Run("empty cache", func() {
		s.SetupTest()
		s.expectChecksPass()
		s.attrCache.EXPECT().GetAttributeCache(mock.Anything, testCacheID).Return(nil, nil)
		s.expectAccessToken()
		s.expectChildIDToken()
		_, errResp := s.handle()
		s.Nil(errResp)
	})
	s.Run("no cache service", func() {
		s.SetupTest()
		s.handler.attributeCache = nil
		s.expectChecksPass()
		_, errResp := s.handle()
		s.assertError(errResp, constants.ErrorServerError)
	})
}

func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ChildAppIDTokenBuildFails() {
	s.expectChecksPass()
	s.expectAttributes()
	s.expectAccessToken()
	s.tokenBuilder.EXPECT().BuildIDToken(mock.Anything, mock.Anything).Return(nil, errTestBuild)

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorServerError)
}
