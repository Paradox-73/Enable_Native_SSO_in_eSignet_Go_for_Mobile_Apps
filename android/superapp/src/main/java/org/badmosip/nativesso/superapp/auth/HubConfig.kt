// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.auth

import org.badmosip.nativesso.oidc.Scopes
import java.net.URI
import java.security.MessageDigest

/**
 * What Hub asks eSignet for.
 *
 * @property issuer eSignet's issuer identifier; every endpoint comes from its discovery document
 * @property clientId this install's client_id ([ClientIdentity.clientIdFor])
 * @property redirectUri custom scheme with a host, as eSignet's client registration requires
 * @property scopes `openid profile email`; `device_sso` is added per login when the user turns sharing on.
 *   Health Card's token exchange can only reuse these scopes, and eSignet's userinfo for an exchanged token
 *   keeps only the claims of its scopes (`profile` does not include `email`), so the email needs its own scope
 * @property acrValues eSignet's OTP login
 * @property claims OpenID Connect `claims` request parameter (JSON), or null
 */
data class HubConfig(
    val issuer: String,
    val clientId: String,
    val redirectUri: String,
    val scopes: Set<String> = setOf(Scopes.OPENID, "profile", "email"),
    val acrValues: String? = OTP_ACR,
    val claims: String? = DEFAULT_CLAIMS,
) {
    init {
        require(Scopes.OPENID in scopes) { "scopes must include openid" }
        require(Scopes.DEVICE_SSO !in scopes) { "device_sso is chosen per login, not in the base scopes" }
        val uri = URI(redirectUri)
        require(!uri.scheme.isNullOrEmpty() && !uri.host.isNullOrEmpty()) { "redirect URI needs a scheme and a host" }
    }

    /** True when [uri] is this app's redirect URI (same scheme, host and path; query ignored). */
    fun isRedirect(uri: String): Boolean = try {
        val expected = URI(redirectUri)
        val actual = URI(uri)
        actual.scheme.equals(expected.scheme, ignoreCase = true) &&
            actual.host.equals(expected.host, ignoreCase = true) &&
            (actual.rawPath ?: "") == (expected.rawPath ?: "")
    } catch (e: IllegalArgumentException) {
        false
    } catch (e: java.net.URISyntaxException) {
        false
    }

    companion object {
        /** eSignet's OTP authentication context class. */
        const val OTP_ACR: String = "mosip:idp:acr:generated-code"

        /**
         * How long the device secret lets other apps sign in after Hub's login. Hub cannot read the device secret
         * (it is encrypted to eSignet), so the home screen counts from the sign-in time.
         * shortcut: copies eSignet's default `native_sso.device_secret_validity_seconds`; change both together, or
         * have eSignet return the lifetime with the device secret if deployments start to differ.
         */
        const val DEVICE_SECRET_LIFETIME_SECONDS: Long = 28_800

        /**
         * Asks for the name in userinfo so the home screen can greet the user, and for the email as optional so
         * Health Card, which signs in through this login, can show it. The phone number stays out: Health Card asks
         * for it with a new login (re-consent for new user data).
         */
        const val DEFAULT_CLAIMS: String = """{"userinfo":{"name":{"essential":true},"email":{"essential":false}}}"""
    }
}

/** How a debug install names itself to eSignet. */
object ClientIdentity {
    /**
     * `prefix` + the first 12 hex characters of SHA-256 over the client key's RFC 7638 thumbprint.
     * Every install has its own Keystore key, so it gets its own client_id, and eSignet's client
     * registration (which never changes a registered key) is simply done again for it.
     */
    fun clientIdFor(prefix: String, clientKeyThumbprint: String): String {
        val digest = MessageDigest.getInstance("SHA-256").digest(clientKeyThumbprint.toByteArray(Charsets.US_ASCII))
        return prefix + digest.take(6).joinToString("") { "%02x".format(it) }
    }
}
