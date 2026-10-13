// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"gopkg.in/yaml.v3"
)

type NativeSSOConfigTestSuite struct {
	suite.Suite
}

func TestNativeSSOConfigSuite(t *testing.T) {
	suite.Run(t, new(NativeSSOConfigTestSuite))
}

func (suite *NativeSSOConfigTestSuite) TestNativeSSOConfig_Validate() {
	cases := []struct {
		name    string
		config  NativeSSOConfig
		wantErr string
	}{
		{name: "zero value passes", config: NativeSSOConfig{}},
		{name: "interaction_required passes",
			config: NativeSSOConfig{ScopeExceededError: "interaction_required", IDTokenMaxAgeSeconds: 3600}},
		{name: "invalid_scope passes",
			config: NativeSSOConfig{ScopeExceededError: "invalid_scope", ExchangeOnly: true}},
		{name: "unknown error code fails",
			config: NativeSSOConfig{ScopeExceededError: "access_denied"}, wantErr: "scope_exceeded_error"},
		{name: "negative id_token age fails",
			config: NativeSSOConfig{IDTokenMaxAgeSeconds: -1}, wantErr: "id_token_max_age_seconds"},
		{name: "consent-only re-consent with the default signal passes",
			config: NativeSSOConfig{ConsentOnlyReconsent: true, ConsentTicketTTLSeconds: 600}},
		{name: "consent-only re-consent with interaction_required passes",
			config: NativeSSOConfig{ConsentOnlyReconsent: true, ScopeExceededError: "interaction_required"}},
		{name: "consent-only re-consent with invalid_scope fails",
			config:  NativeSSOConfig{ConsentOnlyReconsent: true, ScopeExceededError: "invalid_scope"},
			wantErr: "consent_only_reconsent"},
		{name: "negative consent ticket lifetime fails",
			config: NativeSSOConfig{ConsentTicketTTLSeconds: -1}, wantErr: "consent_ticket_ttl_seconds"},
		{name: "consent ticket lifetime over ten minutes fails",
			config: NativeSSOConfig{ConsentTicketTTLSeconds: 601}, wantErr: "consent_ticket_ttl_seconds"},
		{name: "negative consent browser lifetime fails",
			config: NativeSSOConfig{ConsentBrowserTTLSeconds: -1}, wantErr: "consent_browser_ttl_seconds"},
	}
	for _, tc := range cases {
		suite.Run(tc.name, func() {
			err := tc.config.Validate()
			if tc.wantErr == "" {
				assert.NoError(suite.T(), err)
				return
			}
			assert.ErrorContains(suite.T(), err, tc.wantErr)
		})
	}
}

func (suite *NativeSSOConfigTestSuite) TestNativeSSOConfig_ConsentTicketTTL() {
	assert.Equal(suite.T(), DefaultNativeSSOConsentTicketTTLSeconds, NativeSSOConfig{}.ConsentTicketTTL())
	assert.Equal(suite.T(), int64(120), NativeSSOConfig{ConsentTicketTTLSeconds: 120}.ConsentTicketTTL())
}

func (suite *NativeSSOConfigTestSuite) TestNativeSSOConfig_ConsentBrowserTTL() {
	assert.Equal(suite.T(), DefaultNativeSSOConsentBrowserTTLSeconds, NativeSSOConfig{}.ConsentBrowserTTL())
	assert.Equal(suite.T(), int64(3600), NativeSSOConfig{ConsentBrowserTTLSeconds: 3600}.ConsentBrowserTTL())
}

func (suite *NativeSSOConfigTestSuite) TestNativeSSOConfig_ScopeExceededErrorCode() {
	assert.Equal(suite.T(), "interaction_required", NativeSSOConfig{}.ScopeExceededErrorCode())
	assert.Equal(suite.T(), "interaction_required",
		NativeSSOConfig{ScopeExceededError: "interaction_required"}.ScopeExceededErrorCode())
	assert.Equal(suite.T(), "invalid_scope",
		NativeSSOConfig{ScopeExceededError: "invalid_scope"}.ScopeExceededErrorCode())
}

// eSignet reads its oauth section straight into OAuthConfig, so the keys under oauth.native_sso in
// its deployment.yaml must decode, even with unknown fields rejected.
func (suite *NativeSSOConfigTestSuite) TestOAuthConfig_DecodesNativeSSOFromYAML() {
	const doc = `
native_sso:
  id_token_max_age_seconds: 28800
  scope_exceeded_error: invalid_scope
  exchange_only: true
`
	var cfg OAuthConfig
	dec := yaml.NewDecoder(strings.NewReader(doc))
	dec.KnownFields(true)
	suite.Require().NoError(dec.Decode(&cfg))

	assert.Equal(suite.T(), NativeSSOConfig{
		IDTokenMaxAgeSeconds: 28800,
		ScopeExceededError:   "invalid_scope",
		ExchangeOnly:         true,
	}, cfg.NativeSSO)
}

func (suite *NativeSSOConfigTestSuite) TestOAuthConfig_NativeSSODefaultsWhenAbsent() {
	var cfg OAuthConfig
	suite.Require().NoError(yaml.Unmarshal([]byte("allowed_scopes:\n  - openid\n"), &cfg))
	assert.Equal(suite.T(), NativeSSOConfig{}, cfg.NativeSSO)
	assert.NoError(suite.T(), cfg.NativeSSO.Validate())
	assert.False(suite.T(), cfg.NativeSSO.ExchangeOnly)
	assert.Equal(suite.T(), "interaction_required", cfg.NativeSSO.ScopeExceededErrorCode())
}

func (suite *NativeSSOConfigTestSuite) TestOAuthConfig_NativeSSOJSONKeys() {
	raw, err := json.Marshal(OAuthConfig{NativeSSO: NativeSSOConfig{IDTokenMaxAgeSeconds: 60}})
	suite.Require().NoError(err)
	assert.Contains(suite.T(), string(raw), `"native_sso":{"id_token_max_age_seconds":60,`)
}

func (suite *NativeSSOConfigTestSuite) TestOAuthConfig_DecodesConsentOnlyKeys() {
	const doc = `
native_sso:
  consent_only_reconsent: true
  consent_ticket_ttl_seconds: 120
`
	var cfg OAuthConfig
	dec := yaml.NewDecoder(strings.NewReader(doc))
	dec.KnownFields(true)
	suite.Require().NoError(dec.Decode(&cfg))
	assert.True(suite.T(), cfg.NativeSSO.ConsentOnlyReconsent)
	assert.Equal(suite.T(), int64(120), cfg.NativeSSO.ConsentTicketTTLSeconds)
	assert.NoError(suite.T(), cfg.NativeSSO.Validate())
}
