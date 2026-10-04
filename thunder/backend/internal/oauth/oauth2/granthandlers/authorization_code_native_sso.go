// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package granthandlers

import (
	"context"
	"time"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/authz"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/dpop"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/model"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/nativesso"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/tokenservice"
	"github.com/thunder-id/thunderid/internal/system/log"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// Error descriptions of the device_sso part of the authorization-code grant. They name the rule that
// failed and carry no user data.
const (
	errDescDeviceSSORequiresDPoP = "A DPoP proof is required for the device_sso scope"
	errDescDeviceSecretFailed    = "Failed to issue the device secret"
)

// issuedDeviceSecret is a device secret returned by the DeviceSecretProvider, with the login session id
// it was issued for.
type issuedDeviceSecret struct {
	value     string
	expiresIn int64
	sessionID string
}

// checkDeviceSSOGrant checks an authorization code that was granted the device_sso scope (OpenID Connect
// Native SSO for Mobile Apps 1.0), before any token is built:
//   - the client must still allow device_sso and Native SSO must be on: the client's settings can change
//     between the login and the token request (statement task 2), so the check of the authorization
//     request is repeated here; a failure is invalid_scope.
//   - the request must carry a DPoP proof, because the device secret is bound to its key (cnf.jkt). The
//     token service already answers invalid_dpop_proof for a DPoP-bound client without a proof; any other
//     client gets invalid_request.
func (h *authorizationCodeGrantHandler) checkDeviceSSOGrant(
	ctx context.Context, scopes []string, oauthApp *providers.OAuthClient,
) *model.ErrorResponse {
	if errResp := nativesso.ValidateDeviceSSOScope(scopes, oauthApp, h.deviceSecretProvider != nil); errResp != nil {
		return errResp
	}
	if dpop.GetJkt(ctx) == "" {
		return &model.ErrorResponse{
			Error:            constants.ErrorInvalidRequest,
			ErrorDescription: errDescDeviceSSORequiresDPoP,
		}
	}
	return nil
}

// issueDeviceSecret mints the login session id, asks the DeviceSecretProvider for a device secret and
// keeps the cached user attributes of the login for as long as the device secret lives, so that tokens a
// later token exchange issues can carry the same claims. Any failure is server_error, and the caller then
// issues no tokens: the id_token and the device secret are issued both or neither (Native SSO 3.4).
func (h *authorizationCodeGrantHandler) issueDeviceSecret(
	ctx context.Context,
	authCode *authz.AuthorizationCode,
	scopes []string,
	attrs map[string]interface{},
	oauthApp *providers.OAuthClient,
	accessTokenExpiresIn int64,
) (*issuedDeviceSecret, *model.ErrorResponse) {
	logger := log.GetLogger().With(log.String(log.LoggerKeyComponentName, "AuthorizationCodeGrantHandler"))

	request := providers.DeviceSecretRequest{
		ClientID:         authCode.ClientID,
		OUID:             oauthApp.OUID,
		Subject:          authCode.AuthorizedUserID,
		AuthID:           authCode.AuthID,
		SessionID:        nativesso.NewSessionID(),
		Scopes:           scopes,
		ACR:              authCode.CompletedACR,
		Claims:           nativesso.ReleasedClaimNames(scopes, oauthApp.ScopeClaims, authCode.ClaimsRequest, attrs),
		AuthTime:         authCode.TimeCreated.Unix(),
		DPoPJkt:          dpop.GetJkt(ctx),
		AttributeCacheID: authCode.AttributeCacheID,
	}
	if err := request.Validate(); err != nil {
		logger.Error(ctx, "Device secret request is incomplete", log.Error(err))
		return nil, deviceSecretFailed()
	}

	deviceSecret, svcErr := h.deviceSecretProvider.GenerateDeviceSecret(ctx, request)
	if svcErr != nil {
		logger.Error(ctx, "Device secret provider failed", log.String("code", svcErr.Code))
		return nil, deviceSecretFailed()
	}
	if deviceSecret == nil || deviceSecret.Value == "" {
		logger.Error(ctx, "Device secret provider returned no device secret")
		return nil, deviceSecretFailed()
	}
	expiresIn := deviceSecret.ExpiresAt - time.Now().Unix()
	if expiresIn <= 0 {
		logger.Error(ctx, "Device secret provider returned an expired device secret")
		return nil, deviceSecretFailed()
	}

	if authCode.AttributeCacheID != "" {
		ttl := h.attributeCacheTTL(oauthApp, expiresIn, accessTokenExpiresIn)
		if extErr := h.attributeCache.ExtendAttributeCacheTTL(ctx, authCode.AttributeCacheID, int(ttl)); extErr != nil {
			logger.Error(ctx, "Failed to extend attribute cache TTL for the device secret",
				log.String("cache_id", authCode.AttributeCacheID), log.String("code", extErr.Code))
			return nil, deviceSecretFailed()
		}
	}

	return &issuedDeviceSecret{
		value:     deviceSecret.Value,
		expiresIn: expiresIn,
		sessionID: request.SessionID,
	}, nil
}

// attributeCacheTTL returns the lifetime the cached user attributes need once a device secret is issued:
// the longest of the device secret, the access token and (when the client may use it) the refresh token,
// plus the usual buffer. Taking the longest keeps a refresh token that outlives the device secret working.
func (h *authorizationCodeGrantHandler) attributeCacheTTL(
	oauthApp *providers.OAuthClient, deviceSecretExpiresIn, accessTokenExpiresIn int64,
) int64 {
	ttl := max(deviceSecretExpiresIn, accessTokenExpiresIn)
	if oauthApp.IsAllowedGrantType(providers.GrantTypeRefreshToken) {
		refreshTTL := tokenservice.ResolveTokenConfig(h.cfg, oauthApp, tokenservice.TokenTypeRefresh, 0).ValidityPeriod
		ttl = max(ttl, refreshTTL)
	}
	return ttl + constants.AttributeCacheTTLBufferSeconds
}

func deviceSecretFailed() *model.ErrorResponse {
	return &model.ErrorResponse{
		Error:            constants.ErrorServerError,
		ErrorDescription: errDescDeviceSecretFailed,
	}
}
