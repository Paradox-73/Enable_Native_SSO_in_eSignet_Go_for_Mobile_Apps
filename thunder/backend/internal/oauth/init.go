// Copyright 2025-2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// Package oauth provides centralized initialization for all OAuth-related services.
package oauth

import (
	"net/http"
	"slices"
	"strings"

	"github.com/thunder-id/thunderid/internal/attributecache"
	"github.com/thunder-id/thunderid/internal/flow/flowexec"
	oauthconfig "github.com/thunder-id/thunderid/internal/oauth/config"
	"github.com/thunder-id/thunderid/internal/oauth/jwks"
	oauth2authz "github.com/thunder-id/thunderid/internal/oauth/oauth2/authz"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/callback"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/ciba"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/discovery"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/dpop"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/granthandlers"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/introspect"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/jti"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/jwksresolver"
	oauth2logout "github.com/thunder-id/thunderid/internal/oauth/oauth2/logout"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/nativesso"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/par"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/revocation"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/token"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/tokenservice"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/userinfo"
	"github.com/thunder-id/thunderid/internal/oauth/scope"
	syshttp "github.com/thunder-id/thunderid/internal/system/http"
	"github.com/thunder-id/thunderid/internal/system/jose/jwe"
	"github.com/thunder-id/thunderid/internal/system/jose/jwt"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// Initialize initializes all OAuth-related services and registers their routes.
func Initialize(
	mux *http.ServeMux,
	actorProvider providers.ActorProvider,
	authnProvider providers.AuthnProviderManager,
	jwtService jwt.JWTServiceInterface,
	jweService jwe.JWEServiceInterface,
	flowExecService flowexec.FlowExecServiceInterface,
	observabilitySvc providers.ObservabilityProvider,
	runtimeCrypto providers.RuntimeCryptoProvider,
	ouService providers.OrganizationUnitProvider,
	attributeCacheSvc attributecache.AttributeCacheServiceInterface,
	authzService providers.AuthorizationProvider,
	resourceService providers.ResourceServerProvider,
	i18nService providers.I18nProvider,
	idpService providers.IDPProvider,
	dpopVerifier dpop.VerifierInterface,
	runtimeStore providers.RuntimeStoreProvider,
	transactioner providers.Transactioner,
	enforcementService revocation.EnforcementServiceInterface,
	revocationSvc revocation.RevocationServiceInterface,
	deviceSecretProvider providers.DeviceSecretProvider,
	cfg oauthconfig.Config,
) (tokenservice.TokenValidatorInterface, error) {
	// OpenID Connect Native SSO is on only when a DeviceSecretProvider is supplied. The flag is set here,
	// before any service reads cfg, so the device_sso checks and the provider cannot disagree.
	cfg.NativeSSOEnabled = deviceSecretProvider != nil

	jwks.Initialize(mux, runtimeCrypto)
	httpClient := syshttp.NewHTTPClientWithCheckRedirect(func(req *http.Request, _ []*http.Request) error {
		return syshttp.IsSSRFSafeURL(req.URL.String())
	})
	resolver := jwksresolver.Initialize(httpClient)
	scopeValidator := scope.Initialize()
	discoveryService := discovery.Initialize(mux, runtimeCrypto, jweService, cfg)
	jtiStore := jti.Initialize(runtimeStore)
	// The revocation services are constructed by the service manager, not here: the session service
	// needs the same criteria revoker, and it is wired before the OAuth engine. This registers the
	// RFC 7009 routes against the already-built service.
	if !cfg.OAuth.TokenRevocation.IsEnabled() {
		enforcementService = nil
		revocationSvc = nil
	}
	// With Native SSO on, the endpoint exists even without the deny list: the app a device secret was
	// issued to revokes it there, which ends its login session in the DeviceSecretProvider.
	if revocationSvc != nil || deviceSecretProvider != nil {
		revocation.RegisterRoutes(mux, jwtService, actorProvider, authnProvider, discoveryService,
			revocationSvc, deviceSecretProvider, jtiStore, cfg.JWT.Leeway)
	}

	tokenBuilder, tokenValidator := tokenservice.Initialize(
		cfg, jwtService, jweService, resolver, idpService, enforcementService, jtiStore)
	parService := par.Initialize(mux, actorProvider, authnProvider, jwtService, discoveryService,
		resourceService, dpopVerifier, cfg, runtimeStore, jtiStore)
	// Consent-only re-consent of Native SSO: the token exchange issues consent tickets and the consent
	// page answers them, in the browser the authorization endpoint marked at the first app's login. All
	// need the provider, the setting and the runtime store.
	consentStore := newNativeSSOConsentStore(deviceSecretProvider, runtimeStore, cfg)
	var consentBrowser *nativesso.ConsentBrowserBinding
	var browserBinder oauth2authz.ConsentBrowserBinder
	if consentStore != nil {
		// The cookie is Secure whenever the consent page is served over TLS.
		consentBrowser = nativesso.NewConsentBrowserBinding(runtimeStore,
			strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://"), cfg.OAuth.NativeSSO.ConsentBrowserTTL())
		browserBinder = consentBrowser
	}
	oauth2AuthzService, err := oauth2authz.Initialize(mux, actorProvider, resourceService,
		jwtService, flowExecService, parService, revocationSvc, cfg, runtimeStore, transactioner, browserBinder)
	if err != nil {
		return nil, err
	}

	var cibaService ciba.CIBAServiceInterface
	if len(cfg.OAuth.AllowedGrantTypes) == 0 ||
		slices.Contains(cfg.OAuth.AllowedGrantTypes, string(providers.GrantTypeCIBA)) {
		cibaService = ciba.Initialize(mux, jwtService, actorProvider, authnProvider, flowExecService,
			discoveryService, resourceService, runtimeStore, jtiStore, cfg)
	}

	if consentStore != nil {
		nativesso.RegisterConsentRoutes(mux, nativesso.NewConsentPageHandler(consentStore, consentBrowser,
			actorProvider, cfg.BaseURL, cfg.OAuth.NativeSSO.ConsentScopeDescriptions))
	}

	grantHandlerProvider := granthandlers.Initialize(
		jwtService, oauth2AuthzService, tokenBuilder, tokenValidator,
		attributeCacheSvc, ouService, authzService, actorProvider, resourceService,
		cibaService, revocationSvc, revocationSvc, deviceSecretProvider, consentStore, cfg)

	token.Initialize(mux, jwtService, actorProvider, authnProvider, grantHandlerProvider,
		scopeValidator, observabilitySvc, discoveryService, dpopVerifier, jtiStore, cfg)
	introspect.Initialize(mux, jwtService, actorProvider, authnProvider, discoveryService, tokenValidator,
		jtiStore, cfg.JWT.Leeway)
	userinfo.Initialize(mux, jwtService, jweService, resolver,
		tokenValidator, actorProvider, attributeCacheSvc,
		discoveryService, dpopVerifier, cfg)
	callback.Initialize(mux, oauth2AuthzService, cibaService, cfg)

	if cfg.OAuth.Logout.IsEnabled() {
		oauth2logout.Initialize(mux, jwtService, actorProvider, flowExecService, runtimeStore, cfg)
	}
	return tokenValidator, nil
}

// newNativeSSOConsentStore returns the consent store of consent-only re-consent, or nil when the
// feature is off: Native SSO off (no provider), oauth.native_sso.consent_only_reconsent false, or no
// runtime store to keep tickets in.
func newNativeSSOConsentStore(deviceSecretProvider providers.DeviceSecretProvider,
	runtimeStore providers.RuntimeStoreProvider, cfg oauthconfig.Config) nativesso.ConsentStoreInterface {
	if deviceSecretProvider == nil || runtimeStore == nil || !cfg.OAuth.NativeSSO.ConsentOnlyReconsent {
		return nil
	}
	return nativesso.NewConsentStore(runtimeStore)
}
