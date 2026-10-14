// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package providers

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/thunder-id/thunderid/pkg/thunderidengine/common"
)

// DeviceSecretRequest carries what the authorization-code grant knows about a login when it asks a
// DeviceSecretProvider for a device secret (OpenID Connect Native SSO for Mobile Apps 1.0, 3.2).
// The same values come back as DeviceSecretClaims when the device secret is validated during a
// token exchange, so they are everything the exchange needs to issue tokens for that login.
type DeviceSecretRequest struct {
	// ClientID is the client the device secret is issued to (the first app on the device).
	ClientID string `json:"client_id"`
	// OUID is the organization unit of that client.
	OUID string `json:"ou_id,omitempty"`
	// Subject is the sub of the authenticated user.
	Subject string `json:"sub"`
	// AuthID identifies the authorization request (the login) the code was issued for.
	AuthID string `json:"auth_id"`
	// SessionID is the login session the device secret belongs to. The id_token carries it as the
	// sid claim; the token exchange rejects the device secret once that session has ended.
	SessionID string `json:"sid"`
	// Scopes are the scopes granted to the client, including device_sso.
	Scopes []string `json:"scopes"`
	// ACR is the authentication context class reference of the login, if any.
	ACR string `json:"acr,omitempty"`
	// Claims are the names of the user claims the user consented to release to the client.
	Claims []string `json:"claims,omitempty"`
	// AuthTime is when the user authenticated, in seconds since the Unix epoch.
	AuthTime int64 `json:"auth_time,omitempty"`
	// DPoPJkt is the RFC 7638 thumbprint of the client's DPoP key (the cnf.jkt binding, RFC 9449
	// 6.1). Only a proof signed with that key can use the device secret.
	DPoPJkt string `json:"jkt"`
	// AttributeCacheID points at the cached user attributes of the login, so that tokens issued by
	// a later token exchange can carry the same claims without a new login.
	AttributeCacheID string `json:"attribute_cache_id,omitempty"`
}

// Validate reports the first required field that is missing. A request without these values
// cannot produce a device secret that a later token exchange could check.
func (r *DeviceSecretRequest) Validate() error {
	required := []struct{ name, value string }{
		{"client_id", r.ClientID},
		{"sub", r.Subject},
		{"auth_id", r.AuthID},
		{"sid", r.SessionID},
		{"jkt", r.DPoPJkt},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidDeviceSecretRequest, field.name)
		}
	}
	if !slices.Contains(r.Scopes, ScopeDeviceSSO) {
		return fmt.Errorf("%w: scope must include %s", ErrInvalidDeviceSecretRequest, ScopeDeviceSSO)
	}
	return nil
}

// DeviceSecret is a device secret returned by DeviceSecretProvider.GenerateDeviceSecret.
type DeviceSecret struct {
	// Value is the device_secret returned to the client. Clients treat it as opaque.
	Value string
	// ExpiresAt is when the device secret stops being accepted, in seconds since the Unix epoch.
	ExpiresAt int64
}

// DeviceSecretClaims is what DeviceSecretProvider.ValidateDeviceSecret returns for a valid device
// secret: the values it was issued for, plus its issuer and lifetime.
type DeviceSecretClaims struct {
	DeviceSecretRequest
	// Issuer is the iss of the device secret.
	Issuer string `json:"iss"`
	// IssuedAt is when the device secret was issued, in seconds since the Unix epoch.
	IssuedAt int64 `json:"iat"`
	// ExpiresAt is when the device secret stops being accepted, in seconds since the Unix epoch.
	ExpiresAt int64 `json:"exp"`
}

// ConsentReuse describes a token exchange that issued tokens to a second app under the consent the
// user gave at the first app's login (or on the consent page of this login), without asking again.
// The statement's Child App diagram records such a silent reuse in the consent history.
type ConsentReuse struct {
	// ClientID is the second app the tokens were issued to.
	ClientID string
	// Device is the validated device secret the exchange presented.
	Device *DeviceSecretClaims
	// Scopes are the scopes issued to the second app.
	Scopes []string
	// Claims are the names of the user claims released to the second app.
	Claims []string
}

// ErrInvalidDeviceSecretRequest is wrapped by DeviceSecretRequest.Validate.
var ErrInvalidDeviceSecretRequest = errors.New("invalid device secret request")

// ErrorInvalidDeviceSecret is returned by DeviceSecretProvider.ValidateDeviceSecret for a device
// secret that is malformed, forged, expired or from another issuer. The token exchange answers
// invalid_request (RFC 8693 2.2.2).
var ErrorInvalidDeviceSecret = common.ServiceError{
	Type: common.ClientErrorType,
	Code: "NSSO-1001",
	Error: common.I18nMessage{
		Key:          "error.invalid_device_secret",
		DefaultValue: "Invalid device secret",
	},
	ErrorDescription: common.I18nMessage{
		Key:          "error.invalid_device_secret_description",
		DefaultValue: "The device secret is malformed, expired or was not issued by this server",
	},
}

// ErrorDeviceSecretSessionEnded is returned by DeviceSecretProvider.ValidateDeviceSecret for a
// genuine device secret whose login session is no longer active. The token exchange answers
// invalid_grant (Native SSO 4.3, step 4).
var ErrorDeviceSecretSessionEnded = common.ServiceError{
	Type: common.ClientErrorType,
	Code: "NSSO-1002",
	Error: common.I18nMessage{
		Key:          "error.device_secret_session_ended",
		DefaultValue: "Session ended",
	},
	ErrorDescription: common.I18nMessage{
		Key:          "error.device_secret_session_ended_description",
		DefaultValue: "The login session of the device secret is no longer active",
	},
}
