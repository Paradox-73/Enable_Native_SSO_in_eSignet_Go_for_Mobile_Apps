// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package tokenservice

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	oauthconfig "github.com/thunder-id/thunderid/internal/oauth/config"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/revocation"
	"github.com/thunder-id/thunderid/internal/system/jose/jwt"
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	engineconfig "github.com/thunder-id/thunderid/pkg/thunderidengine/config"
	"github.com/thunder-id/thunderid/tests/mocks/jose/jwtmock"
	"github.com/thunder-id/thunderid/tests/mocks/oauth/oauth2/revocationmock"
)

const nativeSSOTestIssuer = "https://issuer.example.com"

// TokenValidatorNativeSSOTestSuite covers ValidateNativeSSOIDToken, the id_token check of the OpenID
// Connect Native SSO token exchange (statement task 9 iii; rules R20 and R21).
type TokenValidatorNativeSSOTestSuite struct {
	suite.Suite
	jwtService  *jwtmock.JWTServiceInterfaceMock
	enforcement *revocationmock.EnforcementServiceInterfaceMock
	validator   *tokenValidator
}

func TestTokenValidatorNativeSSOSuite(t *testing.T) {
	suite.Run(t, new(TokenValidatorNativeSSOTestSuite))
}

func (s *TokenValidatorNativeSSOTestSuite) SetupTest() {
	s.jwtService = jwtmock.NewJWTServiceInterfaceMock(s.T())
	s.enforcement = revocationmock.NewEnforcementServiceInterfaceMock(s.T())
	s.validator = &tokenValidator{
		cfg: oauthconfig.Config{
			JWT: engineconfig.JWTConfig{Issuer: nativeSSOTestIssuer, Leeway: 30},
		},
		jwtService:         s.jwtService,
		enforcementService: s.enforcement,
	}
}

func nativeSSOTestToken(typ string, claims map[string]interface{}) string {
	header, _ := json.Marshal(map[string]interface{}{"alg": "RS256", "typ": typ, "kid": "k1"})
	payload, _ := json.Marshal(claims)
	return base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

// expiredIDTokenClaims returns the claims of an id_token that expired ten minutes ago, as a Native SSO
// id_token normally is when the second app uses it.
func expiredIDTokenClaims() map[string]interface{} {
	now := time.Now().Unix()
	return map[string]interface{}{
		"iss":     nativeSSOTestIssuer,
		"sub":     "user-1",
		"aud":     "super-app",
		"iat":     now - 720,
		"exp":     now - 600,
		"jti":     "id-token-jti",
		"sid":     "session-1",
		"ds_hash": "hash-1",
	}
}

func (s *TokenValidatorNativeSSOTestSuite) TestNativeSSOIDToken_ExpiredTokenAccepted() {
	token := nativeSSOTestToken(jwt.TokenTypeJWT, expiredIDTokenClaims())
	s.jwtService.EXPECT().VerifyJWTSignature(mock.Anything, token).Return(nil)
	s.enforcement.EXPECT().EnsureNotRevoked(mock.Anything, mock.Anything).Return(nil)

	claims, err := s.validator.ValidateNativeSSOIDToken(s.T().Context(), token)

	s.Require().NoError(err)
	s.Equal("user-1", claims.Sub)
	s.Equal(nativeSSOTestIssuer, claims.Iss)
	s.Equal([]string{"super-app"}, claims.Aud)
	s.Equal("session-1", claims.SessionID)
	s.Equal("hash-1", claims.DeviceSecretHash)
	s.Less(claims.ExpiresAt, time.Now().Unix())
	s.Positive(claims.IssuedAt)
}

func (s *TokenValidatorNativeSSOTestSuite) TestNativeSSOIDToken_Rejected() {
	withClaim := func(key string, value interface{}) map[string]interface{} {
		c := expiredIDTokenClaims()
		if value == nil {
			delete(c, key)
		} else {
			c[key] = value
		}
		return c
	}
	cases := []struct {
		name  string
		token string
	}{
		{"malformed header", "%%%.e30.sig"},
		{"access token typ", nativeSSOTestToken(jwt.TokenTypeAccessToken, expiredIDTokenClaims())},
		{"malformed payload", strings.Split(nativeSSOTestToken(jwt.TokenTypeJWT, nil), ".")[0] + ".%%%.sig"},
		{"refresh token shape", nativeSSOTestToken(jwt.TokenTypeJWT, withClaim("access_token_sub", "user-1"))},
		{"missing iss", nativeSSOTestToken(jwt.TokenTypeJWT, withClaim("iss", nil))},
		{"other issuer", nativeSSOTestToken(jwt.TokenTypeJWT, withClaim("iss", "https://evil.example.com"))},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			_, err := s.validator.ValidateNativeSSOIDToken(s.T().Context(), tc.token)
			s.Error(err)
		})
	}
}

func (s *TokenValidatorNativeSSOTestSuite) TestNativeSSOIDToken_OtherIssuerIsNotTrusted() {
	c := expiredIDTokenClaims()
	c["iss"] = "https://evil.example.com"

	_, err := s.validator.ValidateNativeSSOIDToken(s.T().Context(), nativeSSOTestToken(jwt.TokenTypeJWT, c))

	s.ErrorIs(err, ErrIssuerNotTrusted)
}

func (s *TokenValidatorNativeSSOTestSuite) TestNativeSSOIDToken_BadSignature() {
	token := nativeSSOTestToken(jwt.TokenTypeJWT, expiredIDTokenClaims())
	s.jwtService.EXPECT().VerifyJWTSignature(mock.Anything, token).Return(&tidcommon.ServiceError{Code: "x"})

	_, err := s.validator.ValidateNativeSSOIDToken(s.T().Context(), token)

	s.ErrorContains(err, "signature")
}

func (s *TokenValidatorNativeSSOTestSuite) TestNativeSSOIDToken_MissingOrFutureClaims() {
	cases := map[string]func(map[string]interface{}){
		"missing sub": func(c map[string]interface{}) { delete(c, "sub") },
		"missing iat": func(c map[string]interface{}) { delete(c, "iat") },
		"not before":  func(c map[string]interface{}) { c["nbf"] = time.Now().Unix() + 600 },
	}
	for name, mutate := range cases {
		s.Run(name, func() {
			c := expiredIDTokenClaims()
			mutate(c)
			token := nativeSSOTestToken(jwt.TokenTypeJWT, c)
			s.jwtService.EXPECT().VerifyJWTSignature(mock.Anything, token).Return(nil).Once()

			_, err := s.validator.ValidateNativeSSOIDToken(s.T().Context(), token)

			s.Error(err)
		})
	}
}

func (s *TokenValidatorNativeSSOTestSuite) TestNativeSSOIDToken_Revoked() {
	token := nativeSSOTestToken(jwt.TokenTypeJWT, expiredIDTokenClaims())
	s.jwtService.EXPECT().VerifyJWTSignature(mock.Anything, token).Return(nil)
	s.enforcement.EXPECT().EnsureNotRevoked(mock.Anything, mock.Anything).Return(revocation.ErrTokenRevoked)

	_, err := s.validator.ValidateNativeSSOIDToken(s.T().Context(), token)

	s.True(errors.Is(err, revocation.ErrTokenRevoked))
}
