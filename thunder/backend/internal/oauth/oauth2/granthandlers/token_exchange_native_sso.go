// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package granthandlers

import (
	"context"
	"crypto/subtle"
	"errors"
	"slices"
	"time"

	"github.com/thunder-id/thunderid/internal/attributecache"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/dpop"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/model"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/nativesso"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/revocation"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/tokenservice"
	oauth2utils "github.com/thunder-id/thunderid/internal/oauth/oauth2/utils"
	"github.com/thunder-id/thunderid/internal/system/jose/jws"
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
	errDescNSSODeviceSSOInScope  = "The device_sso scope cannot be requested in a token exchange"
	errDescNSSOScopeWithoutOIDC  = "Native SSO requires the openid scope"
	errDescNSSOScopeExceeded     = "The requested scope exceeds the scope granted to the first app; " +
		"user consent is needed for: "
	errDescNSSOConsentNeeded = "The requested scope exceeds the scope granted to the first app and can be " +
		"approved at consent_uri without a new login; user consent is needed for: "
	errDescNSSOAttributesGone = "The user attributes of the login session are no longer available"
	errDescNSSOInvalidDPoPJkt = "Invalid dpop_jkt parameter"
	errDescNSSOFailed         = "Failed to generate token"
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
	if tokenRequest.DPoPJkt != "" && !jws.IsValidJKT(tokenRequest.DPoPJkt) {
		return invalidRequest(errDescNSSOInvalidDPoPJkt)
	}
	// Native SSO 4.1 and statement task 10b: a requested scope must include openid and must not include
	// device_sso, which only an authorization-code grant may carry (rule R16). No scope is allowed and
	// means the scope granted to the first app.
	if requested := tokenservice.ParseScopes(tokenRequest.Scope); len(requested) > 0 {
		if slices.Contains(requested, constants.ScopeDeviceSSO) {
			return invalidScopeError(errDescNSSODeviceSSOInScope)
		}
		if !slices.Contains(requested, constants.ScopeOpenID) {
			return invalidScopeError(errDescNSSOScopeWithoutOIDC)
		}
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

	// Step 13: scope rules (statement task 10a, bonus B1).
	scopes, errResp := h.resolveNativeSSOScopes(ctx, tokenRequest.Scope, oauthApp, dsClaims)
	if errResp != nil {
		return nil, errResp
	}

	return h.issueNativeSSOTokens(ctx, tokenRequest, oauthApp, dsClaims, scopes)
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
//     the same user (rule R20); its expiry is not required (design section 11.1, rule R21);
//   - ds_hash equals the hash of the presented device secret (statement task 9 iii.1, rule R11);
//   - sid equals the session of the device secret (statement task 9 iii.2, rule R12);
//   - last, an optional cap (oauth.native_sso.id_token_max_age_seconds) on its age since iat; an older
//     id_token is invalid_grant, as for an ended session, because the first app must log in again.
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
	if !constantTimeEqual(idClaims.DeviceSecretHash, nativesso.DeviceSecretHash(tokenRequest.ActorToken)) {
		return invalidRequest(errDescNSSODSHashMismatch)
	}
	if !constantTimeEqual(idClaims.SessionID, dsClaims.SessionID) {
		return invalidRequest(errDescNSSOSIDMismatch)
	}
	// After the binding checks, so a mismatched pair gets the mismatch error, not "too old".
	if maxAge := h.cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds; maxAge > 0 &&
		h.currentTime().Unix()-idClaims.IssuedAt > maxAge {
		// Like an ended session (Native SSO 4.3 step 4): only a new login of the first app helps.
		return &model.ErrorResponse{Error: constants.ErrorInvalidGrant, ErrorDescription: errDescNSSOIDTokenTooOld}
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

// resolveNativeSSOScopes applies the scope rules of design section 12 to the scope requested in the
// exchange, given the scope granted with the device secret:
//   - no scope: the granted scope without device_sso (RFC 8693 2.1 makes scope optional; rule R17);
//   - equal or narrower: allowed, the response carries what was issued (statement task 10a; rule R17);
//   - a value outside the granted scope that the user already approved for this app in this login
//     session on the consent page (consent-only re-consent, bonus B1; rule R28): allowed;
//   - any other value outside the granted scope: the exchange fails with interaction_required, or
//     invalid_scope when oauth.native_sso.scope_exceeded_error says so (statement task 10a, Native SSO
//     4.3 step 6 and 4.4; rule R15). interaction_required is the re-consent signal of bonus B1. The
//     description names the missing scope values only, so the app can tell the user which permission
//     it needs. When the missing values need consent only, the answer also carries consent_uri.
//
// The generic token exchange silently drops scopes the subject token lacks; this branch fails instead.
func (h *tokenExchangeGrantHandler) resolveNativeSSOScopes(ctx context.Context, requestedScope string,
	oauthApp *providers.OAuthClient, dsClaims *providers.DeviceSecretClaims,
) ([]string, *model.ErrorResponse) {
	granted := grantedNativeSSOScopes(dsClaims.Scopes)
	requested := tokenservice.ParseScopes(requestedScope)
	if len(requested) == 0 {
		return granted, nil
	}

	var approved []string
	approvedRead := false
	scopes := make([]string, 0, len(requested))
	var missing []string
	for _, scope := range requested {
		switch {
		case slices.Contains(scopes, scope) || slices.Contains(missing, scope):
			continue
		case slices.Contains(granted, scope):
			scopes = append(scopes, scope)
			continue
		}
		if !approvedRead {
			var errResp *model.ErrorResponse
			approved, errResp = h.approvedExtraScopes(ctx, oauthApp, dsClaims)
			if errResp != nil {
				return nil, errResp
			}
			approvedRead = true
		}
		if slices.Contains(approved, scope) {
			scopes = append(scopes, scope)
		} else {
			missing = append(missing, scope)
		}
	}
	if len(missing) > 0 {
		return nil, h.scopeExceeded(ctx, oauthApp, dsClaims, missing)
	}
	return scopes, nil
}

// approvedExtraScopes returns the scopes beyond the first app's grant that the user approved for the
// requesting app in this login session, limited to those still allowed for it and still needing no
// new user data. Without a consent store, or with consent-only re-consent off, there are none.
func (h *tokenExchangeGrantHandler) approvedExtraScopes(ctx context.Context, oauthApp *providers.OAuthClient,
	dsClaims *providers.DeviceSecretClaims) ([]string, *model.ErrorResponse) {
	if h.consentStore == nil || !h.cfg.OAuth.NativeSSO.ConsentOnlyReconsent {
		return nil, nil
	}
	stored, err := h.consentStore.GrantedScopes(ctx, dsClaims.SessionID, oauthApp.ClientID, dsClaims.Subject)
	if err != nil {
		log.GetLogger().Error(ctx, "Failed to read the Native SSO consent approvals", log.Error(err))
		return nil, nativeSSOServerError()
	}
	approved := make([]string, 0, len(stored))
	for _, scope := range stored {
		// The client's settings are read again at every exchange: removing a scope from the app
		// stops it at once, as for device_sso.
		if nativesso.ConsentOnlyEligible([]string{scope}, oauthApp, dsClaims.Claims) {
			approved = append(approved, scope)
		}
	}
	return approved, nil
}

// scopeExceeded builds the answer for a requested scope beyond what the user approved. When every
// missing value can be approved without a new login (nativesso.ConsentOnlyEligible), a one-time
// consent ticket is stored and its page is named in consent_uri. A failure to store the ticket only
// leaves consent_uri out: the app then falls back to its own login, as without the feature.
func (h *tokenExchangeGrantHandler) scopeExceeded(ctx context.Context, oauthApp *providers.OAuthClient,
	dsClaims *providers.DeviceSecretClaims, missing []string) *model.ErrorResponse {
	errResp := &model.ErrorResponse{
		Error:            h.cfg.OAuth.NativeSSO.ScopeExceededErrorCode(),
		ErrorDescription: errDescNSSOScopeExceeded + tokenservice.JoinScopes(missing),
	}
	nssoCfg := h.cfg.OAuth.NativeSSO
	if h.consentStore == nil || !nssoCfg.ConsentOnlyReconsent ||
		errResp.Error != constants.ErrorInteractionRequired || dsClaims.ExpiresAt == 0 || dsClaims.AuthID == "" ||
		!nativesso.ConsentOnlyEligible(missing, oauthApp, dsClaims.Claims) {
		return errResp
	}
	ttl := nssoCfg.ConsentTicketTTL()
	if remaining := dsClaims.ExpiresAt - h.currentTime().Unix(); remaining < ttl {
		ttl = remaining
	}
	if ttl <= 0 {
		return errResp
	}
	ticket := nativesso.ConsentTicket{
		ClientID:       oauthApp.ClientID,
		FirstClientID:  dsClaims.ClientID,
		Subject:        dsClaims.Subject,
		SessionID:      dsClaims.SessionID,
		AuthID:         dsClaims.AuthID,
		Scopes:         missing,
		GrantExpiresAt: dsClaims.ExpiresAt,
	}
	if len(oauthApp.RedirectURIs) == 1 {
		ticket.RedirectURI = oauthApp.RedirectURIs[0]
	}
	value, err := h.consentStore.IssueTicket(ctx, ticket, ttl)
	if err != nil {
		log.GetLogger().Error(ctx, "Failed to store a Native SSO consent ticket", log.Error(err))
		return errResp
	}
	nativesso.AuditConsent(ctx, nativesso.AuditConsentTicketIssued, &ticket)
	errResp.ErrorDescription = errDescNSSOConsentNeeded + tokenservice.JoinScopes(missing)
	errResp.ConsentURI = h.cfg.BaseURL + constants.OAuth2NativeSSOConsentEndpoint + "?ticket=" + value
	errResp.ConsentExpiresIn = ttl
	return errResp
}

// issueNativeSSOTokens issues the second app's tokens for the login of the device secret (Native SSO
// 4.4, rule R23):
//   - access token: the same user, the resolved scope, the attribute-cache id (aci) of the login so that
//     userinfo answers with the claims the user released, and carrying the sid of the login, so that it
//     stops working when that session ends (sign-out, rule R27). It is bound to the key the second app
//     names in dpop_jkt (RFC 9449 section 10), so the second app signs its own proofs for every later
//     call; without dpop_jkt, to the DPoP key of this request, which step 12 proved is the key of the
//     device secret (statement task 8, decision D3);
//   - id_token: aud is the second app, the same sub, sid, acr and auth_time as the login, and no ds_hash
//     (no device secret is issued to the second app);
//   - user attributes: only the claims both released to the first app (the device secret's claims) and
//     released by the resolved scope under the second app's own scope-to-claims mapping.
func (h *tokenExchangeGrantHandler) issueNativeSSOTokens(ctx context.Context, tokenRequest *model.TokenRequest,
	oauthApp *providers.OAuthClient, dsClaims *providers.DeviceSecretClaims, scopes []string,
) (*model.TokenResponseDTO, *model.ErrorResponse) {
	logger := log.GetLogger().With(log.String(log.LoggerKeyComponentName, "TokenExchangeGrantHandler"))

	attrs, errResp := h.nativeSSOUserAttributes(ctx, oauthApp, dsClaims, scopes)
	if errResp != nil {
		return nil, errResp
	}

	// The request's proof is the first app's key (step 12); the second app may name its own key to bind
	// to. Binding to a key the requester does not hold only makes its own token unusable.
	boundJkt := dpop.GetJkt(ctx)
	if tokenRequest.DPoPJkt != "" {
		boundJkt = tokenRequest.DPoPJkt
	}
	userSubConfig := oauthApp.UserAccessTokenConfig()
	accessToken, err := h.tokenBuilder.BuildAccessToken(ctx, &tokenservice.AccessTokenBuildContext{
		Subject:           dsClaims.Subject,
		Audiences:         []string{oauthApp.ResolveDefaultAudience(tokenRequest.ClientID)},
		ClientID:          tokenRequest.ClientID,
		Scopes:            scopes,
		SubjectAttributes: tokenservice.FilterAttributesByAllowList(attrs, userSubConfig),
		AttributeCacheID:  dsClaims.AttributeCacheID,
		GrantType:         string(providers.GrantTypeTokenExchange),
		OAuthApp:          oauthApp,
		ValidityPeriod:    userSubConfig.ValidityPeriodOrZero(),
		DPoPJkt:           boundJkt,
		SessionID:         dsClaims.SessionID,
	})
	if err != nil {
		logger.Error(ctx, "Failed to generate the Native SSO access token", log.Error(err))
		return nil, nativeSSOServerError()
	}
	response := &model.TokenResponseDTO{AccessToken: *accessToken}

	if slices.Contains(scopes, constants.ScopeOpenID) {
		idToken, err := h.tokenBuilder.BuildIDToken(ctx, &tokenservice.IDTokenBuildContext{
			Subject:        dsClaims.Subject,
			Audience:       tokenRequest.ClientID,
			Scopes:         scopes,
			UserAttributes: attrs,
			AuthTime:       dsClaims.AuthTime,
			OAuthApp:       oauthApp,
			CompletedACR:   dsClaims.ACR,
			SessionID:      dsClaims.SessionID,
		})
		if err != nil {
			logger.Error(ctx, "Failed to generate the Native SSO id_token", log.Error(err))
			return nil, nativeSSOServerError()
		}
		response.IDToken = *idToken
	}
	return response, nil
}

// nativeSSOUserAttributes returns the cached user attributes of the login, limited to the claims the
// second app may receive. A login whose attribute cache has expired answers invalid_grant: its session
// can no longer issue tokens with the user's claims. An opaque attribute token from the identity system
// (providers.RawJWTAttributeKey, as in eSignet) is passed on unchanged; the identity system filters it.
func (h *tokenExchangeGrantHandler) nativeSSOUserAttributes(ctx context.Context, oauthApp *providers.OAuthClient,
	dsClaims *providers.DeviceSecretClaims, scopes []string,
) (map[string]interface{}, *model.ErrorResponse) {
	attrs := make(map[string]interface{})
	if dsClaims.AttributeCacheID == "" {
		return attrs, nil
	}
	if h.attributeCache == nil {
		return nil, nativeSSOServerError()
	}
	cache, svcErr := h.attributeCache.GetAttributeCache(ctx, dsClaims.AttributeCacheID)
	if svcErr != nil {
		if svcErr.Code == attributecache.ErrorAttributeCacheNotFound.Code {
			return nil, &model.ErrorResponse{
				Error:            constants.ErrorInvalidGrant,
				ErrorDescription: errDescNSSOAttributesGone,
			}
		}
		return nil, nativeSSOServerError()
	}
	if cache == nil {
		return attrs, nil
	}

	allowed := make(map[string]bool)
	for _, scope := range scopes {
		for _, claim := range oauth2utils.ResolveScopeClaims(scope, oauthApp.ScopeClaims) {
			if slices.Contains(dsClaims.Claims, claim) {
				allowed[claim] = true
			}
		}
	}
	for name, value := range cache.Attributes {
		if allowed[name] || name == providers.RawJWTAttributeKey {
			attrs[name] = value
		}
	}
	return attrs, nil
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

func invalidScopeError(description string) *model.ErrorResponse {
	return &model.ErrorResponse{Error: constants.ErrorInvalidScope, ErrorDescription: description}
}

func nativeSSOServerError() *model.ErrorResponse {
	return &model.ErrorResponse{Error: constants.ErrorServerError, ErrorDescription: errDescNSSOFailed}
}
