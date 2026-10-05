// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package revocation

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/discovery"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
	"github.com/thunder-id/thunderid/tests/mocks/devicesecretprovidermock"
	"github.com/thunder-id/thunderid/tests/mocks/oauth/oauth2/discoverymock"
)

// testDeviceSecret has the shape of a compact JWE (five segments); the provider mock decides its fate.
//
//nolint:gosec // Test value, not a credential
const testDeviceSecret = "eyJhbGciOiJSU0EtT0FFUC0yNTYifQ.key.iv.ciphertext.tag"

// DeviceSecretRevocationTestSuite covers rule R27: the app a device secret was issued to revokes it
// at the RFC 7009 endpoint, which ends its login session.
type DeviceSecretRevocationTestSuite struct {
	suite.Suite
	provider    *devicesecretprovidermock.DeviceSecretProviderMock
	serviceMock *RevocationServiceInterfaceMock
	handler     *revocationHandler
}

func TestDeviceSecretRevocationTestSuite(t *testing.T) {
	suite.Run(t, new(DeviceSecretRevocationTestSuite))
}

func (s *DeviceSecretRevocationTestSuite) SetupTest() {
	s.provider = devicesecretprovidermock.NewDeviceSecretProviderMock(s.T())
	s.serviceMock = NewRevocationServiceInterfaceMock(s.T())
	s.handler = newRevocationHandler(s.serviceMock, s.provider)
}

func (s *DeviceSecretRevocationTestSuite) revoke(body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handler.HandleRevoke(rec, newRevokeRequest(body))
	return rec
}

func claimsFor(clientID string) *providers.DeviceSecretClaims {
	return &providers.DeviceSecretClaims{DeviceSecretRequest: providers.DeviceSecretRequest{
		ClientID: clientID, SessionID: "sid-1", AuthID: "auth-1",
	}}
}

func (s *DeviceSecretRevocationTestSuite) TestIsDeviceSecret() {
	s.True(isDeviceSecret("a.b.c", "device_secret"), "the hint decides")
	s.True(isDeviceSecret(testDeviceSecret, ""), "a compact JWE without a hint")
	s.False(isDeviceSecret("header.payload.signature", ""), "a compact JWS is a JWT")
	s.False(isDeviceSecret(testDeviceSecret, "refresh_token"), "another hint keeps the JWT path")
}

func (s *DeviceSecretRevocationTestSuite) TestRevoke_OwnerEndsSession() {
	claims := claimsFor(testClientID)
	s.provider.EXPECT().ValidateDeviceSecret(mock.Anything, testDeviceSecret).Return(claims, nil)
	s.provider.EXPECT().EndDeviceSecretSession(mock.Anything, claims).Return(nil)

	rec := s.revoke("token=" + testDeviceSecret + "&token_type_hint=device_secret")

	s.Equal(http.StatusOK, rec.Code)
	s.Empty(rec.Body.String())
	s.serviceMock.AssertNotCalled(s.T(), "RevokeToken", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func (s *DeviceSecretRevocationTestSuite) TestRevoke_JWEWithoutHintEndsSession() {
	claims := claimsFor(testClientID)
	s.provider.EXPECT().ValidateDeviceSecret(mock.Anything, testDeviceSecret).Return(claims, nil)
	s.provider.EXPECT().EndDeviceSecretSession(mock.Anything, claims).Return(nil)

	s.Equal(http.StatusOK, s.revoke("token="+testDeviceSecret).Code)
}

func (s *DeviceSecretRevocationTestSuite) TestRevoke_OtherClientIsInvalidGrant() {
	s.provider.EXPECT().ValidateDeviceSecret(mock.Anything, testDeviceSecret).
		Return(claimsFor("hub-client"), nil)

	rec := s.revoke("token=" + testDeviceSecret + "&token_type_hint=device_secret")

	s.Equal(http.StatusBadRequest, rec.Code)
	s.Contains(rec.Body.String(), "invalid_grant")
	s.provider.AssertNotCalled(s.T(), "EndDeviceSecretSession", mock.Anything, mock.Anything)
}

func (s *DeviceSecretRevocationTestSuite) TestRevoke_InvalidOrEndedIsNoOp() {
	for _, svcErr := range []common.ServiceError{
		providers.ErrorInvalidDeviceSecret, providers.ErrorDeviceSecretSessionEnded,
	} {
		s.Run(svcErr.Code, func() {
			s.SetupTest()
			e := svcErr
			s.provider.EXPECT().ValidateDeviceSecret(mock.Anything, "forged").Return(nil, &e)

			rec := s.revoke("token=forged&token_type_hint=device_secret")

			s.Equal(http.StatusOK, rec.Code)
			s.provider.AssertNotCalled(s.T(), "EndDeviceSecretSession", mock.Anything, mock.Anything)
		})
	}
}

func (s *DeviceSecretRevocationTestSuite) TestRevoke_ServerErrors() {
	serverErr := &common.ServiceError{Type: common.ServerErrorType, Code: "SSE-5000"}

	s.Run("validation", func() {
		s.SetupTest()
		s.provider.EXPECT().ValidateDeviceSecret(mock.Anything, testDeviceSecret).Return(nil, serverErr)
		s.Equal(http.StatusInternalServerError, s.revoke("token="+testDeviceSecret).Code)
	})
	s.Run("no claims", func() {
		s.SetupTest()
		s.provider.EXPECT().ValidateDeviceSecret(mock.Anything, testDeviceSecret).Return(nil, nil)
		s.Equal(http.StatusInternalServerError, s.revoke("token="+testDeviceSecret).Code)
	})
	s.Run("end session", func() {
		s.SetupTest()
		claims := claimsFor(testClientID)
		s.provider.EXPECT().ValidateDeviceSecret(mock.Anything, testDeviceSecret).Return(claims, nil)
		s.provider.EXPECT().EndDeviceSecretSession(mock.Anything, claims).Return(serverErr)
		rec := s.revoke("token=" + testDeviceSecret)
		s.Equal(http.StatusInternalServerError, rec.Code)
		s.Contains(rec.Body.String(), "server_error")
	})
}

// A JWT still goes to the deny-list service when both are on.
func (s *DeviceSecretRevocationTestSuite) TestRevoke_JWTUsesDenyList() {
	s.serviceMock.On("RevokeToken", mock.Anything, "a.b.c", "", testClientID).Return(RevokeOutcomeRevoked, nil)

	s.Equal(http.StatusOK, s.revoke("token=a.b.c").Code)
	s.provider.AssertNotCalled(s.T(), "ValidateDeviceSecret", mock.Anything, mock.Anything)
}

// With Native SSO on and the deny list off, other tokens are unsupported_token_type (RFC 7009 2.2.1).
func (s *DeviceSecretRevocationTestSuite) TestRevoke_JWTWithoutDenyListIsUnsupported() {
	handler := newRevocationHandler(nil, s.provider)
	rec := httptest.NewRecorder()

	handler.HandleRevoke(rec, newRevokeRequest("token=a.b.c&token_type_hint=refresh_token"))

	s.Equal(http.StatusBadRequest, rec.Code)
	s.Contains(rec.Body.String(), "unsupported_token_type")
}

// The routes exist with only a device secret provider (eSignet runs with the deny list off).
func (s *DeviceSecretRevocationTestSuite) TestRegisterRoutes_DeviceSecretsOnly() {
	disc := discoverymock.NewDiscoveryServiceInterfaceMock(s.T())
	disc.On("GetOAuth2AuthorizationServerMetadata", mock.Anything).
		Return(&discovery.OAuth2AuthorizationServerMetadata{Issuer: "https://issuer.example"})
	mux := http.NewServeMux()

	RegisterRoutes(mux, nil, nil, nil, disc, nil, s.provider, nil, 0)

	_, pattern := mux.Handler(&http.Request{Method: "POST", URL: &url.URL{Path: "/oauth2/revoke"}})
	s.Contains(pattern, "/oauth2/revoke")
}
