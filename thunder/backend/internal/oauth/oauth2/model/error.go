// Copyright 2025 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package model

// ErrorResponse represents the OAuth2 error response.
type ErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorURI         string `json:"error_uri,omitempty"`
	// ConsentURI is set only on the interaction_required answer of an OpenID Connect Native SSO token
	// exchange whose extra scopes need the user's consent but no new login: a one-time page of this
	// server where the user allows or declines them. A client that does not know the member ignores it.
	ConsentURI string `json:"consent_uri,omitempty"`
	// ConsentExpiresIn is the lifetime of ConsentURI in seconds.
	ConsentExpiresIn int64 `json:"consent_expires_in,omitempty"`
}
