// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package granthandlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/internal/attributecache"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/dpop"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/model"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/nativesso"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/revocation"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/tokenservice"
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
	"github.com/thunder-id/thunderid/tests/mocks/actorprovidermock"
	"github.com/thunder-id/thunderid/tests/mocks/attributecachemock"
	"github.com/thunder-id/thunderid/tests/mocks/devicesecretprovidermock"
	"github.com/thunder-id/thunderid/tests/mocks/oauth/oauth2/tokenservicemock"
	"github.com/thunder-id/thunderid/tests/mocks/resourcemock"
	"github.com/thunder-id/thunderid/tests/testhelpers"
)

const (
	nssoHubClientID   = "hub-app"
	nssoChildClientID = "health-card"
	nssoVendorOU      = "vendor-ou"
	nssoSessionID     = "session-1"
	nssoHubIDToken    = "hub-id-token" //nolint:gosec // test value, not a credential
)

// TokenExchangeNativeSSOTestSuite covers the OpenID Connect Native SSO branch of the token-exchange grant
// (statement task 9; rules R03, R09 to R14 and R18 to R21, R26).
type TokenExchangeNativeSSOTestSuite struct {
	suite.Suite
	handler        *tokenExchangeGrantHandler
	tokenBuilder   *tokenservicemock.TokenBuilderInterfaceMock
	tokenValidator *tokenservicemock.TokenValidatorInterfaceMock
	actorProvider  *actorprovidermock.ActorProviderMock
	dsProvider     *devicesecretprovidermock.DeviceSecretProviderMock
	attrCache      *attributecachemock.AttributeCacheServiceInterfaceMock
	childApp       *providers.OAuthClient
	hubApp         *providers.OAuthClient
	dsClaims       *providers.DeviceSecretClaims
	idClaims       *tokenservice.NativeSSOIDTokenClaims
	request        *model.TokenRequest
	ctx            context.Context
	now            time.Time
	builtAccess    []*tokenservice.AccessTokenBuildContext
	builtID        []*tokenservice.IDTokenBuildContext
	reused         []providers.ConsentReuse
	reuseErr       *tidcommon.ServiceError
}

func TestTokenExchangeNativeSSOSuite(t *testing.T) {
	suite.Run(t, new(TokenExchangeNativeSSOTestSuite))
}

func (s *TokenExchangeNativeSSOTestSuite) SetupTest() {
	s.tokenBuilder = tokenservicemock.NewTokenBuilderInterfaceMock(s.T())
	s.tokenValidator = tokenservicemock.NewTokenValidatorInterfaceMock(s.T())
	s.actorProvider = actorprovidermock.NewActorProviderMock(s.T())
	s.dsProvider = devicesecretprovidermock.NewDeviceSecretProviderMock(s.T())
	s.attrCache = attributecachemock.NewAttributeCacheServiceInterfaceMock(s.T())
	cfg := testhelpers.OAuthConfig()
	s.handler = newTokenExchangeGrantHandler(s.tokenBuilder, s.tokenValidator,
		resourcemock.NewResourceServiceInterfaceMock(s.T()), s.actorProvider, s.attrCache, s.dsProvider,
		nil, cfg).(*tokenExchangeGrantHandler)
	s.now = time.Now()
	s.handler.now = func() time.Time { return s.now }
	s.builtAccess = nil
	s.builtID = nil
	s.reused = nil
	s.reuseErr = nil
	// Every exchange that issues tokens records the consent reuse; tests read s.reused or set s.reuseErr.
	s.dsProvider.EXPECT().RecordConsentReuse(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, reuse providers.ConsentReuse) *tidcommon.ServiceError {
			s.reused = append(s.reused, reuse)
			return s.reuseErr
		}).Maybe()

	s.childApp = &providers.OAuthClient{
		ClientID:              nssoChildClientID,
		OUID:                  nssoVendorOU,
		GrantTypes:            []providers.GrantType{providers.GrantTypeTokenExchange},
		DPoPBoundAccessTokens: true,
		Scopes:                []string{"openid", "profile", "email", constants.ScopeDeviceSSO},
	}
	s.hubApp = &providers.OAuthClient{
		ClientID: nssoHubClientID,
		OUID:     nssoVendorOU,
		Scopes:   []string{"openid", "profile", "email", constants.ScopeDeviceSSO},
	}
	s.dsClaims = &providers.DeviceSecretClaims{
		DeviceSecretRequest: providers.DeviceSecretRequest{
			ClientID:         nssoHubClientID,
			OUID:             nssoVendorOU,
			Subject:          testUserID,
			AuthID:           nativeSSOAuthID,
			SessionID:        nssoSessionID,
			Scopes:           []string{"openid", "profile", "email", constants.ScopeDeviceSSO},
			ACR:              nativeSSOCompleteACR,
			Claims:           []string{"email", "name"},
			AuthTime:         s.now.Add(-10 * time.Minute).Unix(),
			DPoPJkt:          testDPoPThumbprint,
			AttributeCacheID: testCacheID,
		},
		Issuer:    cfg.JWT.Issuer,
		IssuedAt:  s.now.Add(-10 * time.Minute).Unix(),
		ExpiresAt: s.now.Add(time.Hour).Unix(),
	}
	// An id_token that expired eight minutes ago: the normal case for the second app.
	s.idClaims = &tokenservice.NativeSSOIDTokenClaims{
		Sub:              testUserID,
		Iss:              cfg.JWT.Issuer,
		Aud:              []string{nssoHubClientID},
		IssuedAt:         s.now.Add(-10 * time.Minute).Unix(),
		ExpiresAt:        s.now.Add(-8 * time.Minute).Unix(),
		SessionID:        nssoSessionID,
		DeviceSecretHash: nativeSSODSHash,
	}
	s.request = &model.TokenRequest{
		GrantType:        string(providers.GrantTypeTokenExchange),
		ClientID:         nssoChildClientID,
		SubjectToken:     nssoHubIDToken,
		SubjectTokenType: string(constants.TokenTypeIdentifierIDToken),
		ActorToken:       nativeSSODeviceSecret,
		ActorTokenType:   string(constants.TokenTypeIdentifierDeviceSecret),
		Audiences:        []string{cfg.JWT.Issuer},
	}
	s.ctx = dpop.WithJkt(context.Background(), testDPoPThumbprint)
}

func (s *TokenExchangeNativeSSOTestSuite) expectDeviceSecret() {
	s.dsProvider.EXPECT().ValidateDeviceSecret(mock.Anything, nativeSSODeviceSecret).Return(s.dsClaims, nil)
}

func (s *TokenExchangeNativeSSOTestSuite) expectIDToken() {
	s.tokenValidator.EXPECT().ValidateNativeSSOIDToken(mock.Anything, nssoHubIDToken).Return(s.idClaims, nil)
}

func (s *TokenExchangeNativeSSOTestSuite) expectHub() {
	s.actorProvider.EXPECT().GetOAuthClientByClientID(mock.Anything, nssoHubClientID).Return(s.hubApp, nil)
}

func (s *TokenExchangeNativeSSOTestSuite) expectAccessToken() {
	s.tokenBuilder.EXPECT().BuildAccessToken(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, c *tokenservice.AccessTokenBuildContext) (*model.TokenDTO, error) {
			s.builtAccess = append(s.builtAccess, c)
			return &model.TokenDTO{Token: "child-access-token", TokenType: constants.TokenTypeDPoP,
				Scopes: c.Scopes, ClientID: c.ClientID, Subject: c.Subject}, nil
		})
}

func (s *TokenExchangeNativeSSOTestSuite) expectAttributes() {
	s.attrCache.EXPECT().GetAttributeCache(mock.Anything, testCacheID).Return(&attributecache.AttributeCache{
		ID:         testCacheID,
		Attributes: map[string]interface{}{"email": "asha@example.com", "name": "Asha", "phone_number": "+91"},
	}, nil)
}

func (s *TokenExchangeNativeSSOTestSuite) expectChildIDToken() {
	s.tokenBuilder.EXPECT().BuildIDToken(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, c *tokenservice.IDTokenBuildContext) (*model.TokenDTO, error) {
			s.builtID = append(s.builtID, c)
			return &model.TokenDTO{Token: "child-id-token"}, nil
		})
}

// expectIssue sets up the attribute cache and the token builder for a successful exchange.
func (s *TokenExchangeNativeSSOTestSuite) expectIssue() {
	s.expectAttributes()
	s.expectAccessToken()
	s.expectChildIDToken()
}

// expectChecksPass sets up every collaborator for a request that passes all checks up to issuance.
func (s *TokenExchangeNativeSSOTestSuite) expectChecksPass() {
	s.expectDeviceSecret()
	s.expectIDToken()
	s.expectHub()
}

func (s *TokenExchangeNativeSSOTestSuite) assertError(errResp *model.ErrorResponse, code string) {
	s.Require().NotNil(errResp)
	s.Equal(code, errResp.Error)
	s.NotContains(errResp.ErrorDescription, nativeSSODeviceSecret)
	s.NotContains(errResp.ErrorDescription, testUserID)
}

func (s *TokenExchangeNativeSSOTestSuite) handle() (*model.TokenResponseDTO, *model.ErrorResponse) {
	return s.handler.HandleGrant(s.ctx, s.request, s.childApp)
}

// T9 i: the device-secret actor token type is accepted with an id_token subject (rule R10).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ValidateGrant_AcceptsDeviceSecretActorToken() {
	s.Nil(s.handler.ValidateGrant(s.ctx, s.request, s.childApp))
}

// T9 i: the draft-06 URN is refused with a message naming its replacement (rule R10).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ValidateGrant_LegacyURNRejected() {
	s.request.ActorTokenType = legacyDeviceSecretTokenType
	errResp := s.handler.ValidateGrant(s.ctx, s.request, s.childApp)
	s.assertError(errResp, constants.ErrorInvalidRequest)
	s.Contains(errResp.ErrorDescription, string(constants.TokenTypeIdentifierDeviceSecret))
}

// R03: without a provider the device-secret type is unsupported.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ValidateGrant_DisabledWithoutProvider() {
	s.handler.deviceSecretProvider = nil
	s.assertError(s.handler.ValidateGrant(s.ctx, s.request, s.childApp), constants.ErrorInvalidRequest)
	_, errResp := s.handle()
	s.assertError(errResp, constants.ErrorInvalidRequest)
}

// The generic exchange keeps rejecting the device-secret type when it is not the actor token type.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ValidateGrant_DeviceSecretAsSubjectTypeRejected() {
	s.request.SubjectTokenType = string(constants.TokenTypeIdentifierDeviceSecret)
	s.request.ActorTokenType = string(constants.TokenTypeIdentifierAccessToken)
	s.assertError(s.handler.ValidateGrant(s.ctx, s.request, s.childApp), constants.ErrorInvalidRequest)
}

func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ValidateGrant_RequestShapeRejected() {
	cases := []struct {
		name   string
		mutate func(r *model.TokenRequest)
		code   string
	}{
		{"missing subject_token", func(r *model.TokenRequest) { r.SubjectToken = "" }, constants.ErrorInvalidRequest},
		{"hyphenated id-token type", func(r *model.TokenRequest) {
			r.SubjectTokenType = "urn:ietf:params:oauth:token-type:id-token"
		}, constants.ErrorInvalidRequest},
		{"access token subject", func(r *model.TokenRequest) {
			r.SubjectTokenType = string(constants.TokenTypeIdentifierAccessToken)
		}, constants.ErrorInvalidRequest},
		{"missing actor_token", func(r *model.TokenRequest) { r.ActorToken = "" }, constants.ErrorInvalidRequest},
		{"requested ID-JAG", func(r *model.TokenRequest) {
			r.RequestedTokenType = string(constants.TokenTypeIdentifierIDJAG)
		}, constants.ErrorInvalidRequest},
		{"missing audience", func(r *model.TokenRequest) { r.Audiences = nil }, constants.ErrorInvalidRequest},
		{"other audience", func(r *model.TokenRequest) {
			r.Audiences = []string{"https://other.example.com"}
		}, constants.ErrorInvalidTarget},
		{"two audiences", func(r *model.TokenRequest) {
			r.Audiences = append(r.Audiences, "https://other.example.com")
		}, constants.ErrorInvalidTarget},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			tc.mutate(s.request)
			s.assertError(s.handler.ValidateGrant(s.ctx, s.request, s.childApp), tc.code)
		})
	}
}

func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ValidateGrant_AccessTokenTypeRequestAllowed() {
	s.request.RequestedTokenType = string(constants.TokenTypeIdentifierAccessToken)
	s.Nil(s.handler.ValidateGrant(s.ctx, s.request, s.childApp))
}

// R26: with exchange_only every other token exchange is refused; without it the generic path runs.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_ExchangeOnlyBlocksGenericExchange() {
	generic := &model.TokenRequest{
		GrantType:        string(providers.GrantTypeTokenExchange),
		ClientID:         nssoChildClientID,
		SubjectToken:     "some-access-token",
		SubjectTokenType: string(constants.TokenTypeIdentifierAccessToken),
	}
	s.Nil(s.handler.ValidateGrant(s.ctx, generic, s.childApp))

	s.handler.cfg.OAuth.NativeSSO.ExchangeOnly = true
	s.assertError(s.handler.ValidateGrant(s.ctx, generic, s.childApp), constants.ErrorInvalidRequest)
	s.Nil(s.handler.ValidateGrant(s.ctx, s.request, s.childApp))
}

// T9: an expired id_token with a valid device secret, session, vendor and DPoP key gives a DPoP-bound
// access token for the second app with the device secret's scope minus device_sso (rule R21).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_Success() {
	s.expectChecksPass()
	s.expectIssue()

	resp, errResp := s.handle()

	s.Require().Nil(errResp)
	s.Equal("child-access-token", resp.AccessToken.Token)
	s.Require().Len(s.builtAccess, 1)
	built := s.builtAccess[0]
	s.Equal(testUserID, built.Subject)
	s.Equal(nssoChildClientID, built.ClientID)
	s.Equal([]string{nssoChildClientID}, built.Audiences)
	s.Equal(testDPoPThumbprint, built.DPoPJkt)
	s.Equal(nssoSessionID, built.SessionID, "the access token carries the login's sid (rule R27)")
	s.Equal(string(providers.GrantTypeTokenExchange), built.GrantType)
	s.NotContains(built.Scopes, constants.ScopeDeviceSSO)
	s.Contains(built.Scopes, constants.ScopeOpenID)

	// Statement diagram 2: the silent reuse of the login's consent is recorded for the second app.
	s.Require().Len(s.reused, 1)
	s.Equal(nssoChildClientID, s.reused[0].ClientID)
	s.Same(s.dsClaims, s.reused[0].Device)
	s.Equal(built.Scopes, s.reused[0].Scopes)
}

// Statement diagram 2: no tokens leave without the consent-history record; a failed write is a server error.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_ConsentReuseRecordFails() {
	s.reuseErr = &tidcommon.ServiceError{Code: "ESIGNET-500"}
	s.expectChecksPass()
	s.expectIssue()

	resp, errResp := s.handle()

	s.Nil(resp)
	s.assertError(errResp, constants.ErrorServerError)
	s.Len(s.reused, 1)
}

// Only failed exchanges skip the record: a refused request writes no consent-history row.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_RefusedExchangeRecordsNothing() {
	s.ctx = dpop.WithJkt(context.Background(), "another-key-thumbprint")
	s.expectChecksPass()
	_, errResp := s.handle()
	s.assertError(errResp, constants.ErrorInvalidGrant)
	s.Empty(s.reused)
}

// T9 iii.3: the requesting client must itself allow device_sso (rule R13).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_ClientWithoutDeviceSSO() {
	s.childApp.Scopes = []string{"openid", "email"}
	_, errResp := s.handle()
	s.assertError(errResp, constants.ErrorUnauthorizedClient)
}

// T9 ii: the provider's outcomes map to the error table (rules R09 and R12).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_DeviceSecretRejected() {
	cases := []struct {
		name   string
		svcErr *tidcommon.ServiceError
		code   string
	}{
		{"invalid device secret", &providers.ErrorInvalidDeviceSecret, constants.ErrorInvalidRequest},
		{"session ended", &providers.ErrorDeviceSecretSessionEnded, constants.ErrorInvalidGrant},
		{"other client error", &tidcommon.ServiceError{Type: tidcommon.ClientErrorType, Code: "X-1"},
			constants.ErrorInvalidRequest},
		{"server error", &tidcommon.InternalServerError, constants.ErrorServerError},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.dsProvider.EXPECT().ValidateDeviceSecret(mock.Anything, nativeSSODeviceSecret).Return(nil, tc.svcErr)
			_, errResp := s.handle()
			s.assertError(errResp, tc.code)
		})
	}
}

// Claims a provider returns are checked again before they are trusted (rule R09).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_DeviceSecretClaimsRejected() {
	cases := []struct {
		name   string
		mutate func(c *providers.DeviceSecretClaims)
	}{
		{"other issuer", func(c *providers.DeviceSecretClaims) { c.Issuer = "https://evil.example.com" }},
		{"expired", func(c *providers.DeviceSecretClaims) { c.ExpiresAt = time.Now().Add(-time.Second).Unix() }},
		{"no client", func(c *providers.DeviceSecretClaims) { c.ClientID = "" }},
		{"no subject", func(c *providers.DeviceSecretClaims) { c.Subject = "" }},
		{"no session", func(c *providers.DeviceSecretClaims) { c.SessionID = "" }},
		{"not bound to a DPoP key", func(c *providers.DeviceSecretClaims) { c.DPoPJkt = "" }},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			tc.mutate(s.dsClaims)
			s.expectDeviceSecret()
			_, errResp := s.handle()
			s.assertError(errResp, constants.ErrorInvalidRequest)
		})
	}
}

func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_ProviderReturnsNoClaims() {
	s.dsProvider.EXPECT().ValidateDeviceSecret(mock.Anything, nativeSSODeviceSecret).Return(nil, nil)
	_, errResp := s.handle()
	s.assertError(errResp, constants.ErrorServerError)
}

// T9 iii: an id_token that is forged, from another issuer, or not an id_token is refused (rule R20).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_ForgedIDToken() {
	s.expectDeviceSecret()
	s.tokenValidator.EXPECT().ValidateNativeSSOIDToken(mock.Anything, nssoHubIDToken).
		Return(nil, tokenservice.ErrIssuerNotTrusted)
	_, errResp := s.handle()
	s.assertError(errResp, constants.ErrorInvalidRequest)
}

func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_RevocationUnavailable() {
	s.expectDeviceSecret()
	s.tokenValidator.EXPECT().ValidateNativeSSOIDToken(mock.Anything, nssoHubIDToken).
		Return(nil, errors.Join(errors.New("deny list down"), revocation.ErrEnforcementUnavailable))
	_, errResp := s.handle()
	s.assertError(errResp, constants.ErrorServerError)
}

func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_IDTokenBindingRejected() {
	cases := []struct {
		name   string
		mutate func(c *tokenservice.NativeSSOIDTokenClaims)
	}{
		{"audience is not the device secret's client", func(c *tokenservice.NativeSSOIDTokenClaims) {
			c.Aud = []string{nssoChildClientID}
		}},
		{"other user", func(c *tokenservice.NativeSSOIDTokenClaims) { c.Sub = "someone-else" }},
		{"ds_hash mismatch (T9 iii.1)", func(c *tokenservice.NativeSSOIDTokenClaims) {
			c.DeviceSecretHash = "AAAAAAAAAAAAAAAAAAAAAA"
		}},
		{"no ds_hash", func(c *tokenservice.NativeSSOIDTokenClaims) { c.DeviceSecretHash = "" }},
		{"sid mismatch (T9 iii.2)", func(c *tokenservice.NativeSSOIDTokenClaims) { c.SessionID = "session-2" }},
		{"no sid", func(c *tokenservice.NativeSSOIDTokenClaims) { c.SessionID = "" }},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			// The id_token is also past the age cap: the binding error must win over "too old".
			s.handler.cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds = 300
			tc.mutate(s.idClaims)
			s.expectDeviceSecret()
			s.expectIDToken()
			_, errResp := s.handle()
			s.assertError(errResp, constants.ErrorInvalidRequest)
			s.NotEqual(errDescNSSOIDTokenTooOld, errResp.ErrorDescription)
		})
	}
}

// With oauth.native_sso.ds_hash_full_sha256 the exchange checks the full SHA-256 form and refuses the
// left-half form, so both ends of the binding use one construction.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_FullSHA256DSHash() {
	s.handler.cfg.OAuth.NativeSSO.DSHashFullSHA256 = true
	s.idClaims.DeviceSecretHash = nativesso.DeviceSecretHash(nativeSSODeviceSecret, true)
	s.expectChecksPass()
	s.expectIssue()
	_, errResp := s.handle()
	s.Nil(errResp)

	s.SetupTest()
	s.handler.cfg.OAuth.NativeSSO.DSHashFullSHA256 = true
	s.expectDeviceSecret()
	s.expectIDToken()
	_, errResp = s.handle()
	s.assertError(errResp, constants.ErrorInvalidRequest)
	s.Equal(errDescNSSODSHashMismatch, errResp.ErrorDescription)
}

// R21: an expired id_token inside the optional age cap is accepted.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_ExpiredIDTokenInsideCap() {
	s.handler.cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds = 3600
	s.expectChecksPass()
	s.expectIssue()
	_, errResp := s.handle()
	s.Nil(errResp)
}

// R21: an id_token older than the optional age cap is refused, even with a valid device secret, with
// invalid_grant: like an ended session, only a new login of the first app helps.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_ExpiredIDTokenOutsideCap() {
	s.handler.cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds = 300
	s.expectDeviceSecret()
	s.expectIDToken()
	_, errResp := s.handle()
	s.assertError(errResp, constants.ErrorInvalidGrant)
	s.Equal(errDescNSSOIDTokenTooOld, errResp.ErrorDescription)
}

// Native SSO 4.3 step 5 and D7: only apps of the same vendor that still allows device_sso (rule R19).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_OtherVendorRejected() {
	cases := []struct {
		name   string
		mutate func()
	}{
		{"other organization unit", func() { s.hubApp.OUID = "other-vendor" }},
		{"first app no longer allows device_sso", func() { s.hubApp.Scopes = []string{"openid"} }},
		{"first app without organization unit", func() { s.hubApp.OUID = ""; s.childApp.OUID = "" }},
		{"device secret from another vendor", func() { s.dsClaims.OUID = "other-vendor" }},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			tc.mutate()
			s.expectChecksPass()
			_, errResp := s.handle()
			s.assertError(errResp, constants.ErrorInvalidRequest)
		})
	}
}

func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_FirstAppLookupFails() {
	s.Run("unknown client", func() {
		s.SetupTest()
		s.expectDeviceSecret()
		s.expectIDToken()
		s.actorProvider.EXPECT().GetOAuthClientByClientID(mock.Anything, nssoHubClientID).
			Return(nil, &tidcommon.ServiceError{Type: tidcommon.ClientErrorType, Code: "APP-404"})
		_, errResp := s.handle()
		s.assertError(errResp, constants.ErrorInvalidRequest)
	})
	s.Run("lookup error", func() {
		s.SetupTest()
		s.expectDeviceSecret()
		s.expectIDToken()
		s.actorProvider.EXPECT().GetOAuthClientByClientID(mock.Anything, nssoHubClientID).
			Return(nil, &tidcommon.InternalServerError)
		_, errResp := s.handle()
		s.assertError(errResp, constants.ErrorServerError)
	})
	s.Run("no actor provider", func() {
		s.SetupTest()
		s.handler.actorProvider = nil
		s.expectDeviceSecret()
		s.expectIDToken()
		_, errResp := s.handle()
		s.assertError(errResp, constants.ErrorServerError)
	})
}

// T9 iii.4: the DPoP proof must be signed with the key the device secret is bound to (rule R14).
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_DPoPKeyMismatch() {
	s.ctx = dpop.WithJkt(context.Background(), "another-key-thumbprint")
	s.expectChecksPass()
	_, errResp := s.handle()
	s.assertError(errResp, constants.ErrorInvalidGrant)
}

func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_NoDPoPProof() {
	s.ctx = context.Background()
	s.expectChecksPass()
	_, errResp := s.handle()
	s.assertError(errResp, constants.ErrorInvalidGrant)
}

func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_HandleGrant_TokenBuildFails() {
	s.expectChecksPass()
	s.expectAttributes()
	s.tokenBuilder.EXPECT().BuildAccessToken(mock.Anything, mock.Anything).Return(nil, errors.New("signing failed"))
	_, errResp := s.handle()
	s.assertError(errResp, constants.ErrorServerError)
}

func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_GrantedScopesDropDeviceSSOAndDuplicates() {
	s.Equal([]string{"openid", "email"},
		grantedNativeSSOScopes([]string{"openid", constants.ScopeDeviceSSO, "email", "openid"}))
}
