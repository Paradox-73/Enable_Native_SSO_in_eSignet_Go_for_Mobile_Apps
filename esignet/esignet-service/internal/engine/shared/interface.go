/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package shared

import (
	"context"

	"github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// ConsolidatedAuthnProvider extends providers.AuthnProviderInterface with sendOTP capability.
type ConsolidatedAuthnProvider interface {
	providers.AuthnProviderInterface

	// SendOTP sends an OTP to the user based on the provided identifiers and metadata.
	SendOTP(_ context.Context, identifiers map[string]interface{},
		metadata *providers.AuthnMetadata) (*SendOTPResult, *common.ServiceError)

	// GetSigningCertificates retrieves public keys used by the ID system to sign userinfo responses.
	GetSigningCertificates(ctx context.Context) ([]CertificateData, *common.ServiceError)
}

// SigningCertificateRefresher is implemented by an ID system provider that caches its signing
// certificates: it fetches them again, bypassing that cache. It is called when a userinfo token
// names a key the cached list does not hold (the ID system created or rotated a key since).
type SigningCertificateRefresher interface {
	RefreshSigningCertificates(ctx context.Context) ([]CertificateData, *common.ServiceError)
}
