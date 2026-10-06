// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// Package nativesso holds the rules of OpenID Connect Native SSO for Mobile Apps 1.0 that more than
// one OAuth endpoint applies: which clients may request the device_sso scope, the session id issued
// with a device secret, and the ds_hash claim that binds an id_token to its device secret.
package nativesso

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"slices"
	"sort"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/model"
	oauth2utils "github.com/thunder-id/thunderid/internal/oauth/oauth2/utils"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// sessionIDBytes is the number of random bytes in a session id: 128 bits, the same strength as an
// authorization code, so a session id cannot be guessed.
const sessionIDBytes = 16

// Error descriptions of ValidateDeviceSSOScope. They name the rule that failed and carry no user data.
const (
	errDescDeviceSSONotSupported = "The device_sso scope is not supported"
	errDescDeviceSSONotAllowed   = "The client is not allowed to request the device_sso scope"
	errDescDeviceSSOWithoutOIDC  = "The device_sso scope requires the openid scope"
)

// RequestsDeviceSSO reports whether the scopes include device_sso.
func RequestsDeviceSSO(scopes []string) bool {
	return slices.Contains(scopes, constants.ScopeDeviceSSO)
}

// ValidateDeviceSSOScope checks a request whose scopes include device_sso and returns nil when it
// may proceed, or when device_sso is not requested at all. The pushed authorization request, the
// authorization request and the authorization-code grant all apply it, the last one again because the
// client's settings can change between the login and the token request (statement task 2).
//
//   - enabled is false when the engine has no DeviceSecretProvider: Native SSO is off and device_sso
//     is refused like any scope the server does not support.
//   - The client must list device_sso in its allowed scopes (OAuthClient.Scopes). eSignet fills that
//     list from the client's allowed_authorization_scopes, so this reads exactly that setting.
//   - openid must be requested too: the session id and ds_hash travel in the id_token, and a device
//     secret without an id_token cannot be used (Native SSO 3.4 and 4.1).
//
// Every failure is invalid_scope (RFC 6749 4.1.2.1 and 5.2).
func ValidateDeviceSSOScope(
	scopes []string, oauthApp *providers.OAuthClient, enabled bool,
) *model.ErrorResponse {
	if !RequestsDeviceSSO(scopes) {
		return nil
	}
	if !enabled {
		return invalidScope(errDescDeviceSSONotSupported)
	}
	if oauthApp == nil || !slices.Contains(oauthApp.Scopes, constants.ScopeDeviceSSO) {
		return invalidScope(errDescDeviceSSONotAllowed)
	}
	if !slices.Contains(scopes, constants.ScopeOpenID) {
		return invalidScope(errDescDeviceSSOWithoutOIDC)
	}
	return nil
}

func invalidScope(description string) *model.ErrorResponse {
	return &model.ErrorResponse{Error: constants.ErrorInvalidScope, ErrorDescription: description}
}

// NewSessionID returns a new login-session id for the sid claim: 128 random bits, base64url without
// padding (22 characters).
func NewSessionID() string {
	b := make([]byte, sessionIDBytes)
	// crypto/rand.Read never returns an error: since Go 1.24 it ends the program instead when the
	// operating system cannot supply random bytes.
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DeviceSecretHash returns the ds_hash claim for a device secret: the base64url encoding (no
// padding) of the left-most half of the SHA-256 hash of its ASCII octets. This is the at_hash
// construction of OpenID Connect Core 3.1.3.6. Native SSO 3.4.1 leaves the construction to the server
// but requires that ds_hash leaks no data; a truncated hash of an encrypted value leaks none, and the
// token exchange recomputes it from the presented device secret to check the binding.
func DeviceSecretHash(deviceSecret string) string {
	sum := sha256.Sum256([]byte(deviceSecret))
	return base64.RawURLEncoding.EncodeToString(sum[:len(sum)/2])
}

// ReleasedClaimNames returns the sorted names of the user claims a login released to the client:
// the claims of its granted scopes (the client's scope-to-claims mapping, else the standard OIDC set)
// and the claims named in its claims request parameter. When the cached user attributes are a plain
// map, names without a value are left out; when they are one opaque token from the identity system
// (providers.RawJWTAttributeKey, as in eSignet), the engine cannot see the values and keeps every
// released name. Only names are returned, never values: the device secret carries them as its claims
// entry (statement task 4a vii).
func ReleasedClaimNames(
	scopes []string, scopeClaims map[string][]string, claimsRequest *model.ClaimsRequest,
	userAttributes map[string]interface{},
) []string {
	names := make(map[string]struct{})
	for _, scope := range scopes {
		for _, claim := range oauth2utils.ResolveScopeClaims(scope, scopeClaims) {
			names[claim] = struct{}{}
		}
	}
	if claimsRequest != nil {
		for claim := range claimsRequest.IDToken {
			names[claim] = struct{}{}
		}
		for claim := range claimsRequest.UserInfo {
			names[claim] = struct{}{}
		}
	}

	_, opaque := userAttributes[providers.RawJWTAttributeKey]
	result := make([]string, 0, len(names))
	for name := range names {
		if !opaque {
			if value, ok := userAttributes[name]; !ok || value == nil {
				continue
			}
		}
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// ConsentOnlyEligible reports whether the user may approve the extra scopes of a second app without
// logging in again (consent-only re-consent). Every scope must be allowed for the second app, must
// not be openid or device_sso, and must release no user claim beyond releasedClaims, the claims the
// login already released (the device secret's claims). A scope that needs new user data, such as
// phone after a login without it, needs a new login: eSignet's identity system releases user data
// only after the user authenticates.
func ConsentOnlyEligible(scopes []string, oauthApp *providers.OAuthClient, releasedClaims []string) bool {
	if len(scopes) == 0 || oauthApp == nil {
		return false
	}
	for _, scope := range scopes {
		if scope == constants.ScopeOpenID || scope == constants.ScopeDeviceSSO ||
			!slices.Contains(oauthApp.Scopes, scope) {
			return false
		}
		for _, claim := range oauth2utils.ResolveScopeClaims(scope, oauthApp.ScopeClaims) {
			if !slices.Contains(releasedClaims, claim) {
				return false
			}
		}
	}
	return true
}

// ScopeClaimNames returns the user claims scope releases for oauthApp, for display on the consent
// page; nil for a permission scope that releases none.
func ScopeClaimNames(scope string, oauthApp *providers.OAuthClient) []string {
	if oauthApp == nil {
		return oauth2utils.ResolveScopeClaims(scope, nil)
	}
	return oauth2utils.ResolveScopeClaims(scope, oauthApp.ScopeClaims)
}
