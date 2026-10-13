/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

var nativeSSOEnvVars = []string{
	"MOSIP_ESIGNET_NATIVE_SSO_ENABLED",
	"MOSIP_ESIGNET_NATIVE_SSO_DEVICE_SECRET_VALIDITY_SECONDS",
	"MOSIP_ESIGNET_NATIVE_SSO_ENCRYPTION_KEY_REF_ID",
	"MOSIP_ESIGNET_NATIVE_SSO_SIGNING_KEY_REF_ID",
	"MOSIP_ESIGNET_NATIVE_SSO_ID_TOKEN_MAX_AGE_SECONDS",
	"MOSIP_ESIGNET_NATIVE_SSO_SCOPE_EXCEEDED_ERROR",
	"MOSIP_ESIGNET_NATIVE_SSO_EXCHANGE_ONLY",
	"MOSIP_ESIGNET_NATIVE_SSO_CONSENT_ONLY_RECONSENT",
	"MOSIP_ESIGNET_NATIVE_SSO_CONSENT_TICKET_TTL_SECONDS",
	"MOSIP_ESIGNET_NATIVE_SSO_CONSENT_SCOPE_DESCRIPTIONS",
}

// NativeSSOConfigTestSuite covers the native_sso and oauth.native_sso settings of OpenID Connect
// Native SSO for Mobile Apps 1.0.
type NativeSSOConfigTestSuite struct {
	suite.Suite
}

func TestNativeSSOConfigTestSuite(t *testing.T) {
	suite.Run(t, new(NativeSSOConfigTestSuite))
}

func (ts *NativeSSOConfigTestSuite) SetupTest() {
	for _, key := range nativeSSOEnvVars {
		ts.T().Setenv(key, "")
	}
}

func (ts *NativeSSOConfigTestSuite) TestApplyNativeSSODefaults_Defaults() {
	cfg := &AppConfig{}
	applyNativeSSODefaults(cfg)

	ts.Equal(NativeSSOConfig{
		DeviceSecretValiditySeconds: 28800,
		EncryptionKeyRefID:          "DEVICE_SECRET",
		SigningKeyRefID:             "EC_SECP256R1_SIGN",
	}, cfg.NativeSSO)
	ts.EqualValues(3600, cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds, "an id_token older than an hour is refused")
	ts.Equal("interaction_required", cfg.OAuth.NativeSSO.ScopeExceededError)
	ts.False(cfg.OAuth.NativeSSO.ExchangeOnly)
	ts.False(cfg.OAuth.NativeSSO.ConsentOnlyReconsent)
	ts.EqualValues(0, cfg.OAuth.NativeSSO.ConsentTicketTTLSeconds)
	ts.EqualValues(28800, cfg.OAuth.NativeSSO.ConsentBrowserTTLSeconds, "the device secret lifetime")
	ts.NoError(cfg.NativeSSO.Validate("RSA_2048"))
	ts.NoError(cfg.OAuth.NativeSSO.Validate())
}

func (ts *NativeSSOConfigTestSuite) TestApplyNativeSSODefaults_YAMLValuesKept() {
	cfg := &AppConfig{}
	cfg.NativeSSO = NativeSSOConfig{Enabled: true, DeviceSecretValiditySeconds: 600, EncryptionKeyRefID: "DS_ENC",
		SigningKeyRefID: "ED25519_SIGN"}
	cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds = 3600
	cfg.OAuth.NativeSSO.ScopeExceededError = "invalid_scope"
	cfg.OAuth.NativeSSO.ExchangeOnly = true
	applyNativeSSODefaults(cfg)

	ts.Equal(NativeSSOConfig{Enabled: true, DeviceSecretValiditySeconds: 600, EncryptionKeyRefID: "DS_ENC",
		SigningKeyRefID: "ED25519_SIGN"}, cfg.NativeSSO)
	ts.EqualValues(3600, cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds)
	ts.Equal("invalid_scope", cfg.OAuth.NativeSSO.ScopeExceededError)
	ts.True(cfg.OAuth.NativeSSO.ExchangeOnly)
}

func (ts *NativeSSOConfigTestSuite) TestApplyNativeSSODefaults_EnvOverridesYAML() {
	t := ts.T()
	t.Setenv("MOSIP_ESIGNET_NATIVE_SSO_DEVICE_SECRET_VALIDITY_SECONDS", "900")
	t.Setenv("MOSIP_ESIGNET_NATIVE_SSO_ENCRYPTION_KEY_REF_ID", "ENV_ENC")
	t.Setenv("MOSIP_ESIGNET_NATIVE_SSO_SIGNING_KEY_REF_ID", "ENV_SIGN")
	t.Setenv("MOSIP_ESIGNET_NATIVE_SSO_ID_TOKEN_MAX_AGE_SECONDS", "120")
	t.Setenv("MOSIP_ESIGNET_NATIVE_SSO_SCOPE_EXCEEDED_ERROR", "invalid_scope")
	t.Setenv("MOSIP_ESIGNET_NATIVE_SSO_EXCHANGE_ONLY", "false")
	t.Setenv("MOSIP_ESIGNET_NATIVE_SSO_ENABLED", "true")

	cfg := &AppConfig{}
	cfg.NativeSSO = NativeSSOConfig{DeviceSecretValiditySeconds: 600, EncryptionKeyRefID: "DS_ENC",
		SigningKeyRefID: "ED25519_SIGN"}
	cfg.OAuth.NativeSSO.ExchangeOnly = true
	applyNativeSSODefaults(cfg)

	ts.Equal(NativeSSOConfig{Enabled: true, DeviceSecretValiditySeconds: 900, EncryptionKeyRefID: "ENV_ENC",
		SigningKeyRefID: "ENV_SIGN"}, cfg.NativeSSO)
	ts.EqualValues(900, cfg.OAuth.NativeSSO.ConsentBrowserTTLSeconds, "follows the device secret lifetime")
	ts.EqualValues(120, cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds)
	ts.Equal("invalid_scope", cfg.OAuth.NativeSSO.ScopeExceededError)
	ts.False(cfg.OAuth.NativeSSO.ExchangeOnly)
}

// The env var set to 0 keeps its meaning from before the 3600 s default: no cap, even over yaml.
func (ts *NativeSSOConfigTestSuite) TestApplyNativeSSODefaults_EnvZeroTurnsIDTokenCapOff() {
	ts.T().Setenv("MOSIP_ESIGNET_NATIVE_SSO_ID_TOKEN_MAX_AGE_SECONDS", "0")
	cfg := &AppConfig{}
	cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds = 3600
	applyNativeSSODefaults(cfg)
	ts.EqualValues(0, cfg.OAuth.NativeSSO.IDTokenMaxAgeSeconds)
}

// Consent-only re-consent: yaml values are kept, and the env vars override them.
func (ts *NativeSSOConfigTestSuite) TestApplyNativeSSODefaults_ConsentOnly() {
	cfg := &AppConfig{}
	cfg.OAuth.NativeSSO.ConsentOnlyReconsent = true
	cfg.OAuth.NativeSSO.ConsentTicketTTLSeconds = 120
	applyNativeSSODefaults(cfg)
	ts.True(cfg.OAuth.NativeSSO.ConsentOnlyReconsent)
	ts.EqualValues(120, cfg.OAuth.NativeSSO.ConsentTicketTTLSeconds)
	ts.NoError(cfg.OAuth.NativeSSO.Validate())

	ts.T().Setenv("MOSIP_ESIGNET_NATIVE_SSO_CONSENT_ONLY_RECONSENT", "false")
	ts.T().Setenv("MOSIP_ESIGNET_NATIVE_SSO_CONSENT_TICKET_TTL_SECONDS", "60")
	applyNativeSSODefaults(cfg)
	ts.False(cfg.OAuth.NativeSSO.ConsentOnlyReconsent)
	ts.EqualValues(60, cfg.OAuth.NativeSSO.ConsentTicketTTLSeconds)
}

// TestApplyNativeSSODefaults_EnvTurnsOff checks that the env var can turn off Native SSO that
// deployment.yaml turned on.
func (ts *NativeSSOConfigTestSuite) TestApplyNativeSSODefaults_EnvTurnsOff() {
	ts.T().Setenv("MOSIP_ESIGNET_NATIVE_SSO_ENABLED", "false")
	cfg := &AppConfig{}
	cfg.NativeSSO.Enabled = true
	applyNativeSSODefaults(cfg)
	ts.False(cfg.NativeSSO.Enabled)
}

func (ts *NativeSSOConfigTestSuite) TestNativeSSOConfig_Validate() {
	valid := NativeSSOConfig{DeviceSecretValiditySeconds: 60, EncryptionKeyRefID: "DEVICE_SECRET",
		SigningKeyRefID: "EC_SECP256R1_SIGN"}
	ts.NoError(valid.Validate("RSA_2048"))

	cases := map[string]struct {
		mutate  func(*NativeSSOConfig)
		wantErr string
	}{
		"zero validity":     {func(c *NativeSSOConfig) { c.DeviceSecretValiditySeconds = 0 }, "validity"},
		"negative validity": {func(c *NativeSSOConfig) { c.DeviceSecretValiditySeconds = -1 }, "validity"},
		"no encryption key": {func(c *NativeSSOConfig) { c.EncryptionKeyRefID = " " }, "encryption_key_ref_id"},
		"no signing key":    {func(c *NativeSSOConfig) { c.SigningKeyRefID = "" }, "signing_key_ref_id"},
		"signing key is the token key": {func(c *NativeSSOConfig) { c.SigningKeyRefID = "RSA_2048" },
			"token signing key"},
		"signing key is the encryption key": {func(c *NativeSSOConfig) { c.SigningKeyRefID = c.EncryptionKeyRefID },
			"must differ"},
	}
	for name, tc := range cases {
		ts.Run(name, func() {
			c := valid
			tc.mutate(&c)
			ts.ErrorContains(c.Validate("RSA_2048"), tc.wantErr)
		})
	}
}

// TestShippedDeploymentYAML loads data/deployment.yaml as the service does, so a typo in the new
// keys fails here (the decoder rejects unknown fields) rather than at start-up.
func (ts *NativeSSOConfigTestSuite) TestShippedDeploymentYAML() {
	t := ts.T()
	data, err := os.ReadFile(filepath.Join("..", "..", "data", "deployment.yaml"))
	ts.Require().NoError(err)
	dir := t.TempDir()
	ts.Require().NoError(os.MkdirAll(filepath.Join(dir, "data"), 0o755))
	ts.Require().NoError(os.WriteFile(filepath.Join(dir, "data", "deployment.yaml"), data, 0o644))
	t.Chdir(dir)
	// The file takes the issuer and the resource server URLs from this variable.
	t.Setenv("MOSIP_ESIGNET_HOST", "http://localhost:38088")

	cfg, err := LoadAppConfig()
	ts.Require().NoError(err)

	ts.Contains(cfg.OAuth.AllowedScopes, "device_sso")
	ts.Contains(cfg.OAuth.AllowedGrantTypes, "urn:ietf:params:oauth:grant-type:token-exchange")
	ts.True(cfg.NativeSSO.Enabled)
	ts.True(cfg.OAuth.NativeSSO.ExchangeOnly)
	ts.NoError(cfg.OAuth.NativeSSO.Validate())
	ts.NoError(cfg.NativeSSO.Validate(cfg.JWT.PreferredKeyID))
	// scope_claims keys become every client's allowed scopes, so device_sso must never be one:
	// a client gets it only through its own allowed_authorization_scopes.
	ts.NotContains(cfg.ScopeClaims, "device_sso")
}

func (ts *NativeSSOConfigTestSuite) TestApplyNativeSSODefaults_ConsentScopeDescriptions() {
	yamlValue := map[string]string{"health_card_share": "from yaml"}

	cfg := &AppConfig{}
	cfg.OAuth.NativeSSO.ConsentScopeDescriptions = yamlValue
	applyNativeSSODefaults(cfg)
	ts.Equal(yamlValue, cfg.OAuth.NativeSSO.ConsentScopeDescriptions, "yaml kept when no env var")

	ts.T().Setenv("MOSIP_ESIGNET_NATIVE_SSO_CONSENT_SCOPE_DESCRIPTIONS", `{"health_card_share":"from env"}`)
	cfg = &AppConfig{}
	cfg.OAuth.NativeSSO.ConsentScopeDescriptions = yamlValue
	applyNativeSSODefaults(cfg)
	ts.Equal(map[string]string{"health_card_share": "from env"}, cfg.OAuth.NativeSSO.ConsentScopeDescriptions)

	ts.T().Setenv("MOSIP_ESIGNET_NATIVE_SSO_CONSENT_SCOPE_DESCRIPTIONS", `not json`)
	cfg = &AppConfig{}
	cfg.OAuth.NativeSSO.ConsentScopeDescriptions = yamlValue
	applyNativeSSODefaults(cfg)
	ts.Equal(yamlValue, cfg.OAuth.NativeSSO.ConsentScopeDescriptions, "a bad env value keeps the yaml setting")
}
