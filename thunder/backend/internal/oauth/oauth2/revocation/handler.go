// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package revocation

import (
	"net/http"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/clientauth"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/system/log"
	sysutils "github.com/thunder-id/thunderid/internal/system/utils"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// revocationHandler handles OAuth 2.0 token revocation requests (RFC 7009).
type revocationHandler struct {
	// service revokes the engine's JWTs; nil when token revocation is turned off.
	service RevocationServiceInterface
	// deviceSecrets revokes OpenID Connect Native SSO device secrets; nil when Native SSO is off.
	deviceSecrets *deviceSecretRevoker
	logger        *log.Logger
}

// newRevocationHandler creates a new token revocation handler (internal use). Either argument may be
// nil, but not both: the routes are registered only when at least one kind of token can be revoked.
func newRevocationHandler(
	service RevocationServiceInterface, deviceSecretProvider providers.DeviceSecretProvider,
) *revocationHandler {
	h := &revocationHandler{
		service: service,
		logger:  log.GetLogger().With(log.String(log.LoggerKeyComponentName, "RevocationHandler")),
	}
	if deviceSecretProvider != nil {
		h.deviceSecrets = newDeviceSecretRevoker(deviceSecretProvider)
	}
	return h
}

// HandleRevoke handles token revocation requests. Client authentication is enforced upstream by the
// clientauth middleware (401 invalid_client on failure).
func (h *revocationHandler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		sysutils.WriteJSONError(ctx, w, constants.ErrorInvalidRequest, "Failed to decode request body",
			http.StatusBadRequest, nil)
		return
	}

	token := r.FormValue(constants.RequestParamToken)
	if token == "" {
		sysutils.WriteJSONError(ctx, w, constants.ErrorInvalidRequest, "Token parameter is required",
			http.StatusBadRequest, nil)
		return
	}
	tokenTypeHint := r.FormValue(constants.RequestParamTokenTypeHint)

	clientID := ""
	if client := clientauth.GetOAuthClient(ctx); client != nil {
		clientID = client.ClientID
	}

	var revokeOutcome RevokeOutcome
	var err error
	switch {
	case h.deviceSecrets != nil && isDeviceSecret(token, tokenTypeHint):
		revokeOutcome, err = h.deviceSecrets.revoke(ctx, token, clientID)
	case h.service != nil:
		revokeOutcome, err = h.service.RevokeToken(ctx, token, tokenTypeHint, clientID)
	default:
		// Only device secrets are revocable on this server (RFC 7009 2.2.1).
		sysutils.WriteJSONError(ctx, w, constants.ErrorUnsupportedTokenType,
			"Only device secrets can be revoked on this server", http.StatusBadRequest, nil)
		return
	}
	if err != nil {
		h.logger.Error(ctx, "Failed to revoke token", log.Error(err))
		sysutils.WriteJSONError(ctx, w, constants.ErrorServerError,
			"An unexpected error occurred while processing the request",
			http.StatusInternalServerError, nil)
		return
	}

	switch revokeOutcome {
	case RevokeOutcomeNotOwned:
		sysutils.WriteJSONError(ctx, w, constants.ErrorInvalidGrant,
			"The token was not issued to the authenticated client", http.StatusBadRequest, nil)
	default:
		// RFC 7009 §2.2: success is HTTP 200 with an empty body.
		w.WriteHeader(http.StatusOK)
	}
}
