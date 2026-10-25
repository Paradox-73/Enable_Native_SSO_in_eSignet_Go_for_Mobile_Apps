// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package tokenservice

import (
	"context"
	"fmt"
	"time"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/revocation"
	"github.com/thunder-id/thunderid/internal/system/jose/jwt"
)

// NativeSSOIDTokenClaims are the claims of an id_token presented as subject_token in an OpenID Connect
// Native SSO token exchange that the exchange checks against the device secret.
type NativeSSOIDTokenClaims struct {
	Sub string
	Iss string
	Aud []string
	// IssuedAt is the iat claim, used for the optional id_token age cap.
	IssuedAt int64
	// ExpiresAt is the exp claim. It may be in the past: an expired id_token is accepted while its
	// device secret and login session are valid (Native SSO 6.3).
	ExpiresAt int64
	// SessionID is the sid claim: the login session the id_token belongs to.
	SessionID string
	// DeviceSecretHash is the ds_hash claim that binds the id_token to its device secret.
	DeviceSecretHash string
}

// ValidateNativeSSOIDToken validates the id_token of a Native SSO token exchange (Native SSO 4.1 and
// 4.3). The token must:
//   - be a signed id_token: typ JWT (an access token has at+jwt) and no access_token_sub claim (a
//     refresh token has one);
//   - be issued by this server and carry a valid signature of one of its keys;
//   - carry sub and iat, and not be used before its nbf.
//
// Its exp is deliberately not enforced: eSignet id_tokens live two minutes, and Native SSO 6.3 says the
// id_token "may have expired at the time of use". It is accepted only together with a device secret
// that is unexpired, bound to it by ds_hash and sid, and usable only with the first app's DPoP key, so
// those checks decide how long silent sign-in works. The revocation deny list is still applied.
func (tv *tokenValidator) ValidateNativeSSOIDToken(
	ctx context.Context, token string,
) (*NativeSSOIDTokenClaims, error) {
	header, err := jwt.DecodeJWTHeader(token)
	if err != nil {
		return nil, fmt.Errorf("failed to decode id_token header: %w", err)
	}
	if typ, _ := header["typ"].(string); typ != jwt.TokenTypeJWT {
		return nil, fmt.Errorf("subject_token must be an id_token, but has typ %q", typ)
	}

	claims, err := jwt.DecodeJWTPayload(token)
	if err != nil {
		return nil, fmt.Errorf("failed to decode id_token payload: %w", err)
	}
	if _, isRefreshToken := claims["access_token_sub"]; isRefreshToken {
		return nil, fmt.Errorf("subject_token must be an id_token, not a refresh token")
	}

	iss, err := extractStringClaim(claims, "iss")
	if err != nil {
		return nil, fmt.Errorf("id_token is missing 'iss' claim: %w", err)
	}
	if !tv.isSelfIssuer(iss) {
		return nil, fmt.Errorf("%w: id_token was not issued by this server", ErrIssuerNotTrusted)
	}
	if err := tv.verifyTokenSignatureByIssuer(ctx, token, iss); err != nil {
		return nil, fmt.Errorf("invalid id_token signature: %w", err)
	}

	sub, err := extractStringClaim(claims, "sub")
	if err != nil {
		return nil, fmt.Errorf("id_token is missing 'sub' claim: %w", err)
	}
	iat, err := extractInt64Claim(claims, "iat")
	if err != nil {
		return nil, fmt.Errorf("id_token is missing 'iat' claim: %w", err)
	}
	nbf, nbfErr := extractInt64Claim(claims, "nbf")
	if nbfErr == nil && time.Now().Unix() < nbf-tv.cfg.JWT.Leeway {
		return nil, fmt.Errorf("id_token is not yet valid")
	}
	exp, _ := extractInt64Claim(claims, "exp")
	auds, _ := extractAudiences(claims)
	sid, _ := extractStringClaim(claims, constants.ClaimSessionID)
	dsHash, _ := extractStringClaim(claims, constants.ClaimDeviceSecretHash)

	jti, _ := extractStringClaim(claims, "jti")
	tokenFamilyID, _ := extractStringClaim(claims, constants.ClaimTokenFamilyID)
	if err := tv.ensureNotRevoked(ctx, revocationIdentity(claims, jti, tokenFamilyID)); err != nil {
		return nil, err
	}

	return &NativeSSOIDTokenClaims{
		Sub:              sub,
		Iss:              iss,
		Aud:              auds,
		IssuedAt:         iat,
		ExpiresAt:        exp,
		SessionID:        sid,
		DeviceSecretHash: dsHash,
	}, nil
}

// ensureNativeSSOSessionActive refuses an access token issued for a Native SSO login session (it carries
// the session's sid) once that session has ended, for example after the first app revoked its device
// secret ("sign out of all apps"). Userinfo, introspection and every other caller of ValidateAccessToken,
// and a generic token exchange that presents it as subject_token (ValidateSubjectToken), then treat the
// token as revoked, without the deny list. An ended session is revocation.ErrTokenRevoked;
// a session store that cannot answer is revocation.ErrEnforcementUnavailable, so the token is refused
// (fail closed). Tokens without a sid, and every token when Native SSO is off, are not affected.
func (tv *tokenValidator) ensureNativeSSOSessionActive(ctx context.Context, claims map[string]interface{}) error {
	sid, _ := extractStringClaim(claims, constants.ClaimSessionID)
	if sid == "" || tv.deviceSecretProvider == nil {
		return nil
	}
	active, svcErr := tv.deviceSecretProvider.IsDeviceSecretSessionActive(ctx, sid)
	if svcErr != nil {
		return revocation.ErrEnforcementUnavailable
	}
	if !active {
		return revocation.ErrTokenRevoked
	}
	return nil
}
