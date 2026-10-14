/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	engineconfig "github.com/thunder-id/thunderid/pkg/thunderidengine/config"
)

const (
	// defaultDeviceSecretValiditySeconds is how long a device secret, its login-session record
	// and the cached user attributes of that login live (OpenID Connect Native SSO for Mobile
	// Apps 1.0, 6.3): one working day.
	defaultDeviceSecretValiditySeconds = 28800

	// defaultDeviceSecretEncryptionKeyRefID is the reference id, under OIDCServiceAppID, of the
	// RSA Component Encryption Key the device secret JWE is encrypted to. keymanager creates it
	// on the first GetCertificate call.
	defaultDeviceSecretEncryptionKeyRefID = "DEVICE_SECRET"

	// defaultDeviceSecretSigningKeyRefID is the reference id, under OIDCServiceAppID, of the EC
	// P-256 key the inner device secret JWS is signed with (ES256).
	defaultDeviceSecretSigningKeyRefID = "EC_SECP256R1_SIGN"
)

// NativeSSOConfig holds esignet's own settings for OpenID Connect Native SSO for Mobile Apps 1.0,
// read only by the device secret provider (internal/engine/device_secret_provider.go). The
// protocol settings live in the engine's oauth.native_sso group (engineconfig.NativeSSOConfig),
// which AppConfig.OAuth hands to the engine unchanged.
type NativeSSOConfig struct {
	// Enabled turns OpenID Connect Native SSO on: the device secret provider is handed to the
	// engine and the userinfo filter for token-exchange access tokens is installed. Off (the
	// default when the key is absent) the engine refuses device_sso and the device-secret token
	// exchange, and the service behaves as upstream eSignet.
	Enabled bool `yaml:"enabled"`
	// DeviceSecretValiditySeconds is the lifetime of a device secret and of its session record.
	DeviceSecretValiditySeconds int64 `yaml:"device_secret_validity_seconds"`
	// EncryptionKeyRefID names the Component Encryption Key the device secret is encrypted to.
	EncryptionKeyRefID string `yaml:"encryption_key_ref_id"`
	// SigningKeyRefID names the EC P-256 key that signs the inner device secret JWS. It must not
	// be the key that signs the engine's tokens, so that an id_token can never pass as a device
	// secret signature and the other way round (RFC 8725, 3.12).
	SigningKeyRefID string `yaml:"signing_key_ref_id"`
}

// Validate rejects settings the device secret provider cannot work with. tokenSigningKeyRefID is
// the reference id of the key that signs the engine's tokens (AppConfig.JWT.PreferredKeyID).
func (c NativeSSOConfig) Validate(tokenSigningKeyRefID string) error {
	if c.DeviceSecretValiditySeconds <= 0 {
		return errors.New("native_sso.device_secret_validity_seconds must be positive")
	}
	if strings.TrimSpace(c.EncryptionKeyRefID) == "" {
		return errors.New("native_sso.encryption_key_ref_id is required")
	}
	if strings.TrimSpace(c.SigningKeyRefID) == "" {
		return errors.New("native_sso.signing_key_ref_id is required")
	}
	if c.SigningKeyRefID == tokenSigningKeyRefID {
		return fmt.Errorf("native_sso.signing_key_ref_id %q must differ from the token signing key", c.SigningKeyRefID)
	}
	if c.SigningKeyRefID == c.EncryptionKeyRefID {
		return fmt.Errorf("native_sso.signing_key_ref_id and encryption_key_ref_id must differ, both are %q",
			c.SigningKeyRefID)
	}
	return nil
}

// applyNativeSSODefaults resolves the native_sso and oauth.native_sso settings in the documented
// precedence order: MOSIP_ESIGNET_NATIVE_SSO_* env var, then deployment.yaml, then the default.
func applyNativeSSODefaults(cfg *AppConfig) {
	cfg.NativeSSO.Enabled = envBoolOrConfig("MOSIP_ESIGNET_NATIVE_SSO_ENABLED", cfg.NativeSSO.Enabled)
	cfg.NativeSSO.DeviceSecretValiditySeconds = int64(envIntOrConfigOrDefault(
		"MOSIP_ESIGNET_NATIVE_SSO_DEVICE_SECRET_VALIDITY_SECONDS",
		int(cfg.NativeSSO.DeviceSecretValiditySeconds), defaultDeviceSecretValiditySeconds))
	cfg.NativeSSO.EncryptionKeyRefID = envOrConfigOrDefault("MOSIP_ESIGNET_NATIVE_SSO_ENCRYPTION_KEY_REF_ID",
		cfg.NativeSSO.EncryptionKeyRefID, defaultDeviceSecretEncryptionKeyRefID)
	cfg.NativeSSO.SigningKeyRefID = envOrConfigOrDefault("MOSIP_ESIGNET_NATIVE_SSO_SIGNING_KEY_REF_ID",
		cfg.NativeSSO.SigningKeyRefID, defaultDeviceSecretSigningKeyRefID)

	// No cap by default (Native SSO 6.3 accepts an expired id_token; decision D8): the first app cannot
	// renew its id_token without a new login, so a cap also ends silent sign-in that long after the login.
	// A positive value is a hardening option; the env var set to 0 turns a yaml cap off.
	cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds = int64(envIntOrConfigOrDefaultAllowEnvZero(
		"MOSIP_ESIGNET_NATIVE_SSO_ID_TOKEN_MAX_AGE_SECONDS", int(cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds), 0))
	cfg.OAuth.NativeSSO.ScopeExceededError = envOrConfigOrDefault("MOSIP_ESIGNET_NATIVE_SSO_SCOPE_EXCEEDED_ERROR",
		cfg.OAuth.NativeSSO.ScopeExceededError, engineconfig.NativeSSOScopeExceededInteractionRequired)
	cfg.OAuth.NativeSSO.ExchangeOnly = envBoolOrConfig("MOSIP_ESIGNET_NATIVE_SSO_EXCHANGE_ONLY",
		cfg.OAuth.NativeSSO.ExchangeOnly)
	// Consent-only re-consent: extra scopes that need no new user data are approved on a consent page
	// without a new login. 0 for the ticket lifetime means the engine default (300 s).
	cfg.OAuth.NativeSSO.ConsentOnlyReconsent = envBoolOrConfig("MOSIP_ESIGNET_NATIVE_SSO_CONSENT_ONLY_RECONSENT",
		cfg.OAuth.NativeSSO.ConsentOnlyReconsent)
	cfg.OAuth.NativeSSO.ConsentTicketTTLSeconds = int64(envIntOrConfigOrDefault(
		"MOSIP_ESIGNET_NATIVE_SSO_CONSENT_TICKET_TTL_SECONDS", int(cfg.OAuth.NativeSSO.ConsentTicketTTLSeconds), 0))
	// The browser of a login answers consent pages for as long as the login's device secret is valid.
	cfg.OAuth.NativeSSO.ConsentBrowserTTLSeconds = cfg.NativeSSO.DeviceSecretValiditySeconds
	cfg.OAuth.NativeSSO.ConsentScopeDescriptions = scopeDescriptionsFromEnv(
		os.Getenv("MOSIP_ESIGNET_NATIVE_SSO_CONSENT_SCOPE_DESCRIPTIONS"),
		cfg.OAuth.NativeSSO.ConsentScopeDescriptions)
}

// scopeDescriptionsFromEnv returns the descriptions in raw, a JSON object of scope to text, or
// fromYAML when raw is empty or is not such an object (a bad value never hides the yaml setting).
func scopeDescriptionsFromEnv(raw string, fromYAML map[string]string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return fromYAML
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		fmt.Fprintf(os.Stderr, "MOSIP_ESIGNET_NATIVE_SSO_CONSENT_SCOPE_DESCRIPTIONS ignored: %v\n", err)
		return fromYAML
	}
	return parsed
}
