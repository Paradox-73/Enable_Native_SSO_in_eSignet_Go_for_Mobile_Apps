// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

/**
 * The parts of the OpenID Provider metadata (OpenID Connect Discovery 1.0 section 3, RFC 8414,
 * RFC 9126 section 5, RFC 9449 section 5.1, RFC 7009 via RFC 8414 section 2, Native SSO 1.0 section 5)
 * the apps use.
 * Endpoint URLs always come from here, never from app configuration (eSignet RP guide).
 */
public data class ProviderMetadata(
    val issuer: String,
    val authorizationEndpoint: String,
    val tokenEndpoint: String,
    val jwksUri: String,
    val pushedAuthorizationRequestEndpoint: String?,
    val userinfoEndpoint: String?,
    val revocationEndpoint: String? = null,
    val scopesSupported: List<String> = emptyList(),
    val grantTypesSupported: List<String> = emptyList(),
    val idTokenSigningAlgValuesSupported: List<String> = emptyList(),
    val dpopSigningAlgValuesSupported: List<String> = emptyList(),
    val nativeSsoSupported: Boolean = false,
) {
    public companion object {
        private const val WHAT = "provider metadata"

        /** Parses a discovery document; required members missing or of the wrong type are errors. */
        public fun parse(json: String): ProviderMetadata = from(parseJsonObject(json, WHAT))

        internal fun from(m: Map<String, Any?>): ProviderMetadata = ProviderMetadata(
            issuer = m.requireString("issuer", WHAT),
            authorizationEndpoint = m.requireString("authorization_endpoint", WHAT),
            tokenEndpoint = m.requireString("token_endpoint", WHAT),
            jwksUri = m.requireString("jwks_uri", WHAT),
            pushedAuthorizationRequestEndpoint = m.optString("pushed_authorization_request_endpoint", WHAT),
            userinfoEndpoint = m.optString("userinfo_endpoint", WHAT),
            revocationEndpoint = m.optString("revocation_endpoint", WHAT),
            scopesSupported = m.optStringList("scopes_supported", WHAT).orEmpty(),
            grantTypesSupported = m.optStringList("grant_types_supported", WHAT).orEmpty(),
            idTokenSigningAlgValuesSupported = m.optStringList("id_token_signing_alg_values_supported", WHAT).orEmpty(),
            dpopSigningAlgValuesSupported = m.optStringList("dpop_signing_alg_values_supported", WHAT).orEmpty(),
            nativeSsoSupported = when (val v = m["native_sso_supported"]) {
                null -> false
                is Boolean -> v
                else -> throw InvalidResponseException("$WHAT: \"native_sso_supported\" is not a boolean")
            },
        )
    }
}
