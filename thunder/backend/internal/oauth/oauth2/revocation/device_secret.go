// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package revocation

import (
	"context"
	"fmt"
	"strings"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/system/log"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// deviceSecretRevoker revokes OpenID Connect Native SSO device secrets at the RFC 7009 endpoint. A
// device secret is not a JWT the deny list could hold: revoking it ends its login session through
// the DeviceSecretProvider, after which every token exchange with it fails with invalid_grant
// (Native SSO 4.3, step 4). It needs no database, so it works when the deny list is turned off.
type deviceSecretRevoker struct {
	provider providers.DeviceSecretProvider
	logger   *log.Logger
}

func newDeviceSecretRevoker(provider providers.DeviceSecretProvider) *deviceSecretRevoker {
	return &deviceSecretRevoker{
		provider: provider,
		logger:   log.GetLogger().With(log.String(log.LoggerKeyComponentName, "DeviceSecretRevoker")),
	}
}

// isDeviceSecret reports whether a revocation request is for a device secret: the client says so
// with token_type_hint=device_secret, or sends no hint and a compact JWE (five segments). The engine's
// own access and refresh tokens are compact JWS (three segments), so they never take this path.
func isDeviceSecret(token, tokenTypeHint string) bool {
	switch tokenTypeHint {
	case constants.TokenTypeHintDeviceSecret:
		return true
	case "":
		return strings.Count(token, ".") == 4
	default:
		return false
	}
}

// revoke ends the session of a device secret issued to authenticatedClientID. Per RFC 7009 2.2 a
// value that is not a valid device secret, or whose session has already ended, is a successful
// no-op. A device secret issued to another client is RevokeOutcomeNotOwned and changes nothing. An
// error is returned only for a server-side failure.
func (r *deviceSecretRevoker) revoke(ctx context.Context, token, authenticatedClientID string) (
	RevokeOutcome, error) {
	claims, svcErr := r.provider.ValidateDeviceSecret(ctx, token)
	if svcErr != nil {
		if svcErr.Type == common.ClientErrorType {
			r.logger.Debug(ctx, "Revocation request for an unusable device secret; treating as a no-op success",
				log.String("code", svcErr.Code))
			return RevokeOutcomeRevoked, nil
		}
		return RevokeOutcomeRevoked, fmt.Errorf("failed to validate device secret: %s", svcErr.Code)
	}
	if claims == nil {
		return RevokeOutcomeRevoked, fmt.Errorf("device secret provider returned no claims")
	}

	// A client may only revoke what was issued to it (RFC 7009 2.1): an app that merely holds the
	// device secret, such as another app of the same vendor, cannot end the first app's session.
	if claims.ClientID != authenticatedClientID {
		r.logger.Debug(ctx, "Revocation request for a device secret issued to a different client")
		return RevokeOutcomeNotOwned, nil
	}

	if svcErr := r.provider.EndDeviceSecretSession(ctx, claims); svcErr != nil {
		return RevokeOutcomeRevoked, fmt.Errorf("failed to end device secret session: %s", svcErr.Code)
	}
	r.logger.Debug(ctx, "Revoked device secret and ended its session", log.String("clientId", claims.ClientID))
	return RevokeOutcomeRevoked, nil
}
