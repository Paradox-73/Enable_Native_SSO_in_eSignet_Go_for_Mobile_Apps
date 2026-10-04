// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package granthandlers

import (
	"context"
	"crypto/subtle"
	"errors"
	"slices"
	"time"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/dpop"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/model"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/nativesso"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/revocation"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/tokenservice"
	"github.com/thunder-id/thunderid/internal/system/log"
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// legacyDeviceSecretTokenType is the device-secret token type of Native SSO draft 06 and earlier. It is
// recognized only to tell the client which type replaced it (rule R10).
const legacyDeviceSecretTokenType = "urn:x-oath:params:oauth:token-type:device-secret" //nolint:gosec // a type name

// Error descriptions of the Native SSO token exchange. They name the rule that failed and carry no
// token, device secret or user data.
const (
	errDescNSSOLegacyTokenType = "Unsupported actor_token_type urn:x-oath:params:oauth:token-type:device-secret; " +
		"use urn:openid:params:token-type:device-secret"
	errDescNSSONotSupported      = "Unsupported actor_token_type"
	errDescNSSOExchangeOnly      = "Only the OpenID Connect Native SSO token exchange is supported"
	errDescNSSOMissingSubject    = "Missing required parameter: subject_token"
	errDescNSSOSubjectType       = "Native SSO requires subject_token_type urn:ietf:params:oauth:token-type:id_token"
	errDescNSSOMissingActor      = "Missing required parameter: actor_token"
	errDescNSSORequestedType     = "Native SSO issues only access tokens"
	errDescNSSOMissingAudience   = "Missing required parameter: audience"
	errDescNSSOAudience          = "The audience must be this authorization server's issuer"
	errDescNSSOClientNotAllowed  = "The client is not allowed to use the device_sso scope"
	errDescNSSOInvalidDevSecret  = "Invalid actor_token: the device secret is invalid or expired"
	errDescNSSOSessionEnded      = "The login session of the device secret has ended"
	errDescNSSOInvalidIDToken    = "Invalid subject_token: not an id_token issued by this server"
	errDescNSSOIDTokenAudience   = "The subject_token was not issued to the client of the device secret"
	errDescNSSOIDTokenSubject    = "The subject_token and the device secret belong to different users"
	errDescNSSOIDTokenTooOld     = "The subject_token is older than the allowed maximum age"
	errDescNSSODSHashMismatch    = "The subject_token ds_hash does not match the device secret"
	errDescNSSOSIDMismatch       = "The subject_token sid does not match the device secret"
	errDescNSSOOtherVendor       = "The client may not share the login of the client of the device secret"
	errDescNSSONoDPoPKey         = "The device secret is not bound to a DPoP key"
	errDescNSSORevocationUnknown = "Token revocation status could not be verified"
	errDescNSSOFailed            = "Failed to generate token"
)

// validateNativeSSOGrant applies the request-shape rules of OpenID Connect Native SSO for Mobile Apps 1.0
// (4.1) and the exchange-only setting. handled is false when the request is not a Native SSO request
// and the generic token exchange may check it; then errResp is nil.
func (h *tokenExchangeGrantHandler) validateNativeSSOGrant(
	tokenRequest *model.TokenRequest,
) (errResp *model.ErrorResponse, handled bool) {
	switch tokenRequest.ActorTokenType {
	case legacyDeviceSecretTokenType:
		return invalidRequest(errDescNSSOLegacyTokenType), true
	case string(constants.TokenTypeIdentifierDeviceSecret):
		return h.validateNativeSSORequest(tokenRequest), true
	}
	if h.cfg.OAuth.NativeSSO.ExchangeOnly {
		return invalidRequest(errDescNSSOExchangeOnly), true
	}
	return nil, false
}

// validateNativeSSORequest checks a token exchange whose actor_token_type is the device-secret type
// (statement task 9 i). These checks need no token to be decrypted or verified, so they run before
// the DPoP proof and the device secret are looked at (design section 11, steps 3 and 4).
func (h *tokenExchangeGrantHandler) validateNativeSSORequest(tokenRequest *model.TokenRequest) *model.ErrorResponse {
	// Without a DeviceSecretProvider Native SSO is off and the device-secret type is refused like any
	// other unsupported token type (rule R03).
	if h.deviceSecretProvider == nil {
		return invalidRequest(errDescNSSONotSupported)
	}
	if tokenRequest.SubjectToken == "" {
		return invalidRequest(errDescNSSOMissingSubject)
	}
	if constants.TokenTypeIdentifier(tokenRequest.SubjectTokenType) != constants.TokenTypeIdentifierIDToken {
		return invalidRequest(errDescNSSOSubjectType)
	}
	if tokenRequest.ActorToken == "" {
		return invalidRequest(errDescNSSOMissingActor)
	}
	if tokenRequest.RequestedTokenType != "" &&
		constants.TokenTypeIdentifier(tokenRequest.RequestedTokenType) != constants.TokenTypeIdentifierAccessToken {
		return invalidRequest(errDescNSSORequestedType)
	}
	// Native SSO 4.1: audience is REQUIRED and is the issuer of the authorization server (rule R18).
	if len(tokenRequest.Audiences) == 0 {
		return invalidRequest(errDescNSSOMissingAudience)
	}
	if len(tokenRequest.Audiences) != 1 || tokenRequest.Audiences[0] != h.cfg.JWT.Issuer {
		return &model.ErrorResponse{Error: constants.ErrorInvalidTarget, ErrorDescription: errDescNSSOAudience}
	}
	return nil
}

// handleNativeSSOGrant performs the checks of Native SSO 4.3 in the order of design section 11 (steps 6
// to 12) and then issues tokens for the requesting app. Any failure stops processing; no token is
// issued.
func (h *tokenExchangeGrantHandler) handleNativeSSOGrant(ctx context.Context, tokenRequest *model.TokenRequest,
	oauthApp *providers.OAuthClient) (*model.TokenResponseDTO, *model.ErrorResponse) {
	if h.deviceSecretProvider == nil {
		return nil, invalidRequest(errDescNSSONotSupported)
	}

	// Step 6: the requesting app itself must be allowed device_sso (statement task 9 iii.3, rule R13).
	if oauthApp == nil || !slices.Contains(oauthApp.Scopes, constants.ScopeDeviceSSO) {
		return nil, &model.ErrorResponse{
			Error:            constants.ErrorUnauthorizedClient,
			ErrorDescription: errDescNSSOClientNotAllowed,
		}
	}

	// Steps 7 and 8: the provider decrypts and verifies the device secret and checks that its login
	// session is still active (statement task 9 ii, rules R09 and R12).
	dsClaims, errResp := h.validateDeviceSecret(ctx, tokenRequest.ActorToken)
	if errResp != nil {
		return nil, errResp
	}

	// Steps 9 and 10: the id_token and its binding to the device secret (statement task 9 iii.1-2).
	if errResp := h.checkNativeSSOIDToken(ctx, tokenRequest, dsClaims); errResp != nil {
		return nil, errResp
	}

	// Step 11: same vendor (rule R19).
	if errResp := h.checkSameVendor(ctx, oauthApp, dsClaims); errResp != nil {
		return nil, errResp
	}

	// Step 12: the DPoP proof of this request is signed with the key the device secret is bound to
	// (statement task 9 iii.4, rule R14). VerifyProofBinding answers invalid_grant on a mismatch.
	if errResp := dpop.VerifyProofBinding(ctx, dsClaims.DPoPJkt, "device secret"); errResp != nil {
		return nil, errResp
	}

	return h.issueNativeSSOTokens(ctx, tokenRequest, oauthApp, dsClaims)
}

// validateDeviceSecret asks the DeviceSecretProvider to validate the device secret and maps its outcome
// to a token error (design section 8): ErrorDeviceSecretSessionEnded is invalid_grant (Native SSO 4.3
// step 4), any other client error is invalid_request (RFC 8693 2.2.2), a server error is server_error.
func (h *tokenExchangeGrantHandler) validateDeviceSecret(
	ctx context.Context, deviceSecret string,
) (*providers.DeviceSecretClaims, *model.ErrorResponse) {
	logger := log.GetLogger().With(log.String(log.LoggerKeyComponentName, "TokenExchangeGrantHandler"))

	dsClaims, svcErr := h.deviceSecretProvider.ValidateDeviceSecret(ctx, deviceSecret)
	if svcErr != nil {
		logger.Debug(ctx, "Device secret rejected", log.String("code", svcErr.Code))
		switch {
		case svcErr.Code == providers.ErrorDeviceSecretSessionEnded.Code:
			return nil, &model.ErrorResponse{
				Error:            constants.ErrorInvalidGrant,
				ErrorDescription: errDescNSSOSessionEnded,
			}
		case svcErr.Type == tidcommon.ClientErrorType:
			return nil, invalidRequest(errDescNSSOInvalidDevSecret)
		default:
			return nil, nativeSSOServerError()
		}
	}
	if dsClaims == nil {
		logger.Error(ctx, "Device secret provider returned no claims")
		return nil, nativeSSOServerError()
	}

	// The provider checks these already; they are checked again because the rest of the exchange
	// relies on them, and a device secret that lacks them must never be accepted.
	if dsClaims.ClientID == "" || dsClaims.Subject == "" || dsClaims.SessionID == "" ||
		dsClaims.Issuer != h.cfg.JWT.Issuer {
		return nil, invalidRequest(errDescNSSOInvalidDevSecret)
	}
	if dsClaims.ExpiresAt != 0 && dsClaims.ExpiresAt <= h.currentTime().Unix() {
		return nil, invalidRequest(errDescNSSOInvalidDevSecret)
	}
	if dsClaims.DPoPJkt == "" {
		return nil, invalidRequest(errDescNSSONoDPoPKey)
	}
	return dsClaims, nil
}

// checkNativeSSOIDToken validates the id_token presented as subject_token and its binding to the device
// secret (design section 11, steps 9 and 10):
//   - signed by this server, issued by this server, issued to the client of the device secret and for
//     the same user (rule R20); its expiry is not required (design section 11.1, rule R21), but an
//     optional cap (oauth.native_sso.id_token_max_age_seconds) limits its age since iat;
//   - ds_hash equals the hash of the presented device secret (statement task 9 iii.1, rule R11);
//   - sid equals the session of the device secret (statement task 9 iii.2, rule R12).
func (h *tokenExchangeGrantHandler) checkNativeSSOIDToken(ctx context.Context, tokenRequest *model.TokenRequest,
	dsClaims *providers.DeviceSecretClaims) *model.ErrorResponse {
	logger := log.GetLogger().With(log.String(log.LoggerKeyComponentName, "TokenExchangeGrantHandler"))

	idClaims, err := h.tokenValidator.ValidateNativeSSOIDToken(ctx, tokenRequest.SubjectToken)
	if err != nil {
		logger.Debug(ctx, "Native SSO id_token rejected", log.Error(err))
		if errors.Is(err, revocation.ErrEnforcementUnavailable) {
			return &model.ErrorResponse{
				Error:            constants.ErrorServerError,
				ErrorDescription: errDescNSSORevocationUnknown,
			}
		}
		return invalidRequest(errDescNSSOInvalidIDToken)
	}
	if !slices.Contains(idClaims.Aud, dsClaims.ClientID) {
		return invalidRequest(errDescNSSOIDTokenAudience)
	}
	if idClaims.Sub != dsClaims.Subject {
		return invalidRequest(errDescNSSOIDTokenSubject)
	}
	if maxAge := h.cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds; maxAge > 0 &&
		h.currentTime().Unix()-idClaims.IssuedAt > maxAge {
		return invalidRequest(errDescNSSOIDTokenTooOld)
	}
	if !constantTimeEqual(idClaims.DeviceSecretHash, nativesso.DeviceSecretHash(tokenRequest.ActorToken)) {
		return invalidRequest(errDescNSSODSHashMismatch)
	}
	if !constantTimeEqual(idClaims.SessionID, dsClaims.SessionID) {
		return invalidRequest(errDescNSSOSIDMismatch)
	}
	return nil
}

// checkSameVendor applies Native SSO 4.3 step 5 ("SHOULD maintain a list of client_ids that can share")
// as decision D7: the requesting app and the app the device secret was issued to belong to the same
// organization unit (in eSignet, the same relying party), and that app still allows device_sso. Its
// settings are read now, not taken from the device secret, so a change takes effect at once.
func (h *tokenExchangeGrantHandler) checkSameVendor(ctx context.Context, oauthApp *providers.OAuthClient,
	dsClaims *providers.DeviceSecretClaims) *model.ErrorResponse {
	if h.actorProvider == nil {
		return nativeSSOServerError()
	}
	firstApp, svcErr := h.actorProvider.GetOAuthClientByClientID(ctx, dsClaims.ClientID)
	if svcErr != nil && svcErr.Type != tidcommon.ClientErrorType {
		return nativeSSOServerError()
	}
	if svcErr != nil || firstApp == nil || firstApp.OUID == "" || firstApp.OUID != oauthApp.OUID ||
		!slices.Contains(firstApp.Scopes, constants.ScopeDeviceSSO) {
		return invalidRequest(errDescNSSOOtherVendor)
	}
	if dsClaims.OUID != "" && dsClaims.OUID != oauthApp.OUID {
		return invalidRequest(errDescNSSOOtherVendor)
	}
	return nil
}

// issueNativeSSOTokens issues the requesting app's access token for the login of the device secret
// (Native SSO 4.4): the same user, the scope granted with the device secret without device_sso, and
// bound to the DPoP key of this request, which step 12 proved is the key of the device secret
// (statement task 8, decision D3).
func (h *tokenExchangeGrantHandler) issueNativeSSOTokens(ctx context.Context, tokenRequest *model.TokenRequest,
	oauthApp *providers.OAuthClient, dsClaims *providers.DeviceSecretClaims,
) (*model.TokenResponseDTO, *model.ErrorResponse) {
	logger := log.GetLogger().With(log.String(log.LoggerKeyComponentName, "TokenExchangeGrantHandler"))

	scopes := grantedNativeSSOScopes(dsClaims.Scopes)
	userSubConfig := oauthApp.UserAccessTokenConfig()
	accessToken, err := h.tokenBuilder.BuildAccessToken(ctx, &tokenservice.AccessTokenBuildContext{
		Subject:        dsClaims.Subject,
		Audiences:      []string{oauthApp.ResolveDefaultAudience(tokenRequest.ClientID)},
		ClientID:       tokenRequest.ClientID,
		Scopes:         scopes,
		GrantType:      string(providers.GrantTypeTokenExchange),
		OAuthApp:       oauthApp,
		ValidityPeriod: userSubConfig.ValidityPeriodOrZero(),
		DPoPJkt:        dpop.GetJkt(ctx),
	})
	if err != nil {
		logger.Error(ctx, "Failed to generate the Native SSO access token", log.Error(err))
		return nil, nativeSSOServerError()
	}
	return &model.TokenResponseDTO{AccessToken: *accessToken}, nil
}

// grantedNativeSSOScopes returns the scope granted with a device secret without device_sso: the
// device_sso scope is never passed on by a token exchange (statement task 10b).
func grantedNativeSSOScopes(deviceSecretScopes []string) []string {
	scopes := make([]string, 0, len(deviceSecretScopes))
	for _, scope := range deviceSecretScopes {
		if scope != constants.ScopeDeviceSSO && !slices.Contains(scopes, scope) {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

func (h *tokenExchangeGrantHandler) currentTime() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

// constantTimeEqual compares two claim values without leaking, through timing, how much of an
// attacker-supplied value matched. An empty value never matches.
func constantTimeEqual(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func invalidRequest(description string) *model.ErrorResponse {
	return &model.ErrorResponse{Error: constants.ErrorInvalidRequest, ErrorDescription: description}
}

func nativeSSOServerError() *model.ErrorResponse {
	return &model.ErrorResponse{Error: constants.ErrorServerError, ErrorDescription: errDescNSSOFailed}
}
