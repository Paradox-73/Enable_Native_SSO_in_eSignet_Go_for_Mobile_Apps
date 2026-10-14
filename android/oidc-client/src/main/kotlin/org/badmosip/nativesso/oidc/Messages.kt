// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import java.security.SecureRandom

/**
 * One interactive login: the values that must survive the trip through the browser and come back
 * unchanged. Apps keep it in saved state so a process death during the browser step does not lose it.
 *
 * @property state CSRF protection for the redirect (RFC 6749 section 10.12)
 * @property nonce binds the id_token to this login (OpenID Connect Core 3.1.2.1)
 * @property pkce code_verifier and its S256 challenge
 */
public data class LoginSession(val state: String, val nonce: String, val pkce: Pkce) {
    override fun toString(): String = "LoginSession(state=$state)"

    public companion object {
        /** Fresh random values: 32 bytes each for state and nonce, a new PKCE pair. */
        public fun create(random: SecureRandom = SecureRandom()): LoginSession = LoginSession(
            state = B64.encode(ByteArray(32).also(random::nextBytes)),
            nonce = B64.encode(ByteArray(32).also(random::nextBytes)),
            pkce = Pkce.generate(random),
        )
    }
}

/**
 * Parameters of a pushed authorization request (RFC 9126). The client adds `client_id`,
 * `response_type=code`, PKCE, `state`, `nonce` and client authentication itself.
 *
 * @property scope include [Scopes.DEVICE_SSO] to ask for a device secret (Native SSO 1.0 section 3.1)
 * @property acrValues for eSignet's OTP login, "mosip:idp:acr:generated-code"
 * @property claims OpenID Connect "claims" request parameter as a JSON string, if any
 * @property prompt for example "consent" to make the server ask again (re-consent)
 */
public data class AuthorizationRequest(
    val redirectUri: String,
    val scope: Set<String>,
    val session: LoginSession,
    val acrValues: String? = null,
    val claims: String? = null,
    val prompt: String? = null,
) {
    init {
        require(Scopes.OPENID in scope) { "an OpenID Connect request needs the openid scope" }
    }
}

/** Answer of the PAR endpoint (RFC 9126 section 2.2). */
public data class ParResponse(val requestUri: String, val expiresIn: Long)

/**
 * A token endpoint answer (RFC 6749 section 5.1, RFC 8693 section 2.2.1, Native SSO 1.0 section 3.4).
 * [toString] hides every token.
 *
 * @property deviceSecret present when the grant included `device_sso`
 * @property issuedTokenType present on token-exchange responses
 */
public data class TokenResponse(
    val accessToken: String,
    val tokenType: String,
    val expiresIn: Long?,
    val scope: String?,
    val idToken: String?,
    val refreshToken: String?,
    val deviceSecret: String?,
    val issuedTokenType: String?,
) {
    override fun toString(): String =
        "TokenResponse(tokenType=$tokenType, expiresIn=$expiresIn, scope=$scope, idToken=${idToken != null}, " +
            "refreshToken=${refreshToken != null}, deviceSecret=${deviceSecret != null}, issuedTokenType=$issuedTokenType)"

    internal companion object {
        private const val WHAT = "token response"

        fun from(m: Map<String, Any?>): TokenResponse = TokenResponse(
            accessToken = m.requireString("access_token", WHAT),
            tokenType = m.requireString("token_type", WHAT),
            expiresIn = m.optLong("expires_in", WHAT),
            scope = m.optString("scope", WHAT),
            idToken = m.optString("id_token", WHAT),
            refreshToken = m.optString("refresh_token", WHAT),
            deviceSecret = m.optString("device_secret", WHAT),
            issuedTokenType = m.optString("issued_token_type", WHAT),
        )
    }
}

/**
 * The Native SSO token exchange the Child App sends (Native SSO 1.0 section 4.1, RFC 8693 2.1).
 *
 * @property idToken the Super App's id_token, sent as subject_token
 * @property deviceSecret the Super App's device secret, sent as actor_token
 * @property scope what the Child App asks for: must hold openid, must not hold device_sso, and
 *   should be a subset of the Super App's granted scope (the server enforces that)
 * @property dpopJkt optional eSignet profile extension, off in the demo apps: RFC 7638 thumbprint of the
 *   Child App's own DPoP key, sent as `dpop_jkt`, and the issued tokens are bound to it. Null (the
 *   statement's flow) binds them to the key of the request's proof, the Super App's.
 */
public data class DeviceSecretExchange(
    val idToken: String,
    val deviceSecret: String,
    val scope: Set<String>,
    val dpopJkt: String? = null,
) {
    init {
        require(idToken.isNotBlank()) { "idToken must not be blank" }
        require(deviceSecret.isNotBlank()) { "deviceSecret must not be blank" }
        require(dpopJkt == null || JKT.matches(dpopJkt)) { "dpopJkt must be a SHA-256 thumbprint: 43 base64url characters" }
        require(Scopes.OPENID in scope) { "the exchange scope must include openid (Native SSO 1.0 section 4.1)" }
        require(Scopes.DEVICE_SSO !in scope) {
            "a token exchange must not ask for device_sso: only the interactive login gets a device secret"
        }
    }

    override fun toString(): String = "DeviceSecretExchange(scope=$scope)"

    private companion object {
        val JKT = Regex("[A-Za-z0-9_-]{43}")
    }
}
