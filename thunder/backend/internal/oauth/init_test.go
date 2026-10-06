// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	oauthconfig "github.com/thunder-id/thunderid/internal/oauth/config"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	sysconfig "github.com/thunder-id/thunderid/internal/system/config"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
	"github.com/thunder-id/thunderid/tests/mocks/crypto/cryptomock"
	"github.com/thunder-id/thunderid/tests/mocks/devicesecretprovidermock"
	"github.com/thunder-id/thunderid/tests/mocks/jose/jwemock"
	"github.com/thunder-id/thunderid/tests/mocks/runtimestoreprovidermock"
	"github.com/thunder-id/thunderid/tests/testhelpers"
)

// The start-up code of the OAuth services: which routes Initialize registers for which settings, and
// that the Native SSO switch follows the device secret provider (rules R01, R03, R27, R28).

type initOptions struct {
	provider     providers.DeviceSecretProvider
	runtimeStore providers.RuntimeStoreProvider
	cfg          oauthconfig.Config
}

func initialize(t *testing.T, opts initOptions) *http.ServeMux {
	t.Helper()
	// The SSRF-safe HTTP client of the JWKS resolver reads the server runtime.
	sysconfig.ResetServerRuntime()
	t.Cleanup(sysconfig.ResetServerRuntime)
	require.NoError(t, sysconfig.InitializeServerRuntime("", &sysconfig.Config{}))
	crypto := cryptomock.NewRuntimeCryptoProviderMock(t)
	crypto.EXPECT().GetSupportedSigningAlgorithms().Return([]string{"ES256"}).Maybe()
	crypto.EXPECT().GetPublicKeys(mock.Anything, mock.Anything).
		Return([]providers.PublicKeyInfo{{KeyID: "k1", Algorithm: "ES256"}}, nil).Maybe()
	jweService := jwemock.NewJWEServiceInterfaceMock(t)
	jweService.EXPECT().SupportedKeyEncryptionAlgorithms().Return([]string{"RSA-OAEP-256"}).Maybe()
	jweService.EXPECT().SupportedContentEncryptionAlgorithms().Return([]string{"A256GCM"}).Maybe()

	mux := http.NewServeMux()
	validator, err := Initialize(mux, nil, nil, nil, jweService, nil, nil, crypto, nil, nil, nil, nil, nil, nil,
		nil, opts.runtimeStore, nil, nil, nil, opts.provider, opts.cfg)
	require.NoError(t, err)
	require.NotNil(t, validator)
	return mux
}

// registered reports whether mux has a route for method and path (not the 404 fallback).
func registered(mux *http.ServeMux, method, path string) bool {
	_, pattern := mux.Handler(httptest.NewRequest(method, path, nil))
	return pattern != ""
}

func boolPtr(b bool) *bool { return &b }

func fetchDiscovery(t *testing.T, mux *http.ServeMux) map[string]interface{} {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var metadata map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &metadata))
	return metadata
}

func TestInitialize_DefaultRoutesWithoutNativeSSO(t *testing.T) {
	mux := initialize(t, initOptions{cfg: testhelpers.OAuthConfig()})

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, constants.OAuth2TokenEndpoint},
		{http.MethodGet, constants.OAuth2AuthorizationEndpoint},
		{http.MethodPost, constants.OAuth2PAREndpoint},
		{http.MethodPost, constants.OAuth2IntrospectionEndpoint},
		{http.MethodGet, constants.OAuth2UserInfoEndpoint},
		{http.MethodGet, constants.OAuth2JWKSEndpoint},
		{http.MethodPost, constants.OAuth2BackchannelAuthEndpoint},
		{http.MethodGet, "/.well-known/openid-configuration"},
	} {
		assert.True(t, registered(mux, route.method, route.path), route)
	}
	// No revocation without the deny list or a provider, no logout unless enabled, and no consent page
	// without Native SSO.
	assert.False(t, registered(mux, http.MethodPost, constants.OAuth2RevokeEndpoint))
	assert.False(t, registered(mux, http.MethodGet, constants.OAuth2LogoutEndpoint))
	assert.False(t, registered(mux, http.MethodGet, constants.OAuth2NativeSSOConsentEndpoint))

	metadata := fetchDiscovery(t, mux)
	assert.NotEqual(t, true, metadata["native_sso_supported"], "no provider: Native SSO is off")
}

func TestInitialize_NativeSSOProviderTurnsFeaturesOn(t *testing.T) {
	cfg := testhelpers.OAuthConfig()
	cfg.OAuth.NativeSSO.ConsentOnlyReconsent = true
	mux := initialize(t, initOptions{
		provider:     devicesecretprovidermock.NewDeviceSecretProviderMock(t),
		runtimeStore: runtimestoreprovidermock.NewRuntimeStoreProviderMock(t),
		cfg:          cfg,
	})

	// The device secret is revoked at RFC 7009 even without the deny list (rule R27).
	assert.True(t, registered(mux, http.MethodPost, constants.OAuth2RevokeEndpoint))
	// Consent-only re-consent page (rule R28).
	assert.True(t, registered(mux, http.MethodGet, constants.OAuth2NativeSSOConsentEndpoint))
	assert.True(t, registered(mux, http.MethodPost, constants.OAuth2NativeSSOConsentEndpoint))
	assert.False(t, registered(mux, http.MethodPut, constants.OAuth2NativeSSOConsentEndpoint))

	// cfg.NativeSSOEnabled is set from the provider before discovery reads the settings (rule R01).
	metadata := fetchDiscovery(t, mux)
	assert.Equal(t, true, metadata["native_sso_supported"])
	assert.Contains(t, metadata["scopes_supported"], constants.ScopeDeviceSSO)
	assert.NotEmpty(t, metadata["revocation_endpoint"])
}

func TestInitialize_ConsentPageNeedsSettingAndStore(t *testing.T) {
	provider := devicesecretprovidermock.NewDeviceSecretProviderMock(t)

	off := initialize(t, initOptions{provider: provider,
		runtimeStore: runtimestoreprovidermock.NewRuntimeStoreProviderMock(t), cfg: testhelpers.OAuthConfig()})
	assert.False(t, registered(off, http.MethodGet, constants.OAuth2NativeSSOConsentEndpoint), "setting off")

	cfg := testhelpers.OAuthConfig()
	cfg.OAuth.NativeSSO.ConsentOnlyReconsent = true
	noStore := initialize(t, initOptions{provider: provider, cfg: cfg})
	assert.False(t, registered(noStore, http.MethodGet, constants.OAuth2NativeSSOConsentEndpoint), "no store")

	noProvider := initialize(t, initOptions{
		runtimeStore: runtimestoreprovidermock.NewRuntimeStoreProviderMock(t), cfg: cfg})
	assert.False(t, registered(noProvider, http.MethodGet, constants.OAuth2NativeSSOConsentEndpoint),
		"no provider")
}

func TestInitialize_OptionalEndpointsFollowSettings(t *testing.T) {
	cfg := testhelpers.OAuthConfig()
	cfg.OAuth.AllowedGrantTypes = []string{string(providers.GrantTypeAuthorizationCode)}
	cfg.OAuth.Logout.Enabled = boolPtr(true)

	mux := initialize(t, initOptions{cfg: cfg})

	assert.False(t, registered(mux, http.MethodPost, constants.OAuth2BackchannelAuthEndpoint),
		"CIBA is not an allowed grant type")
	assert.True(t, registered(mux, http.MethodGet, constants.OAuth2LogoutEndpoint))
	assert.True(t, registered(mux, http.MethodPost, constants.OAuth2TokenEndpoint))
}

func TestInitialize_TokenRevocationEnabledWithoutProvider(t *testing.T) {
	cfg := testhelpers.OAuthConfig()
	cfg.OAuth.TokenRevocation.Enabled = boolPtr(true)

	// Revocation on but no revocation service supplied: the route needs the service, so it stays off.
	mux := initialize(t, initOptions{cfg: cfg})
	assert.False(t, registered(mux, http.MethodPost, constants.OAuth2RevokeEndpoint))
}

func TestNewNativeSSOConsentStore(t *testing.T) {
	provider := devicesecretprovidermock.NewDeviceSecretProviderMock(t)
	store := runtimestoreprovidermock.NewRuntimeStoreProviderMock(t)
	on := testhelpers.OAuthConfig()
	on.OAuth.NativeSSO.ConsentOnlyReconsent = true

	assert.NotNil(t, newNativeSSOConsentStore(provider, store, on))
	assert.Nil(t, newNativeSSOConsentStore(nil, store, on))
	assert.Nil(t, newNativeSSOConsentStore(provider, nil, on))
	assert.Nil(t, newNativeSSOConsentStore(provider, store, testhelpers.OAuthConfig()))
}
