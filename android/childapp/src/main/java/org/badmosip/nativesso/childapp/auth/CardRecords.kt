// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp.auth

import com.nimbusds.jose.util.JSONObjectUtils
import org.badmosip.nativesso.oidc.Scopes
import java.net.URI
import java.text.ParseException
import java.time.Instant

/**
 * What Health Card asks eSignet for.
 *
 * @property scopes the silent sign-in: `openid profile`, a subset of what Hub's login asks for
 * @property extraScope the permission behind "Add my phone number". Hub's login never asks for it, so
 *   asking for it in the token exchange is answered `interaction_required` (bonus B1) and Health Card
 *   runs its own login with `prompt=consent`. eSignet maps it to `phone_number`.
 * @property shareScope the permission behind "Allow card sharing". It releases no user data, so eSignet
 *   answers its `interaction_required` with a `consent_uri` and the user approves it on eSignet's
 *   consent page without a new login (consent-only re-consent).
 */
data class CardConfig(
    val issuer: String,
    val clientId: String,
    val redirectUri: String,
    val scopes: Set<String> = setOf(Scopes.OPENID, "profile"),
    val extraScope: String = "phone",
    val shareScope: String = SHARE_SCOPE,
    val acrValues: String? = OTP_ACR,
    val claims: String? = DEFAULT_CLAIMS,
) {
    init {
        require(Scopes.OPENID in scopes) { "scopes must include openid" }
        require(Scopes.DEVICE_SSO !in scopes && extraScope != Scopes.DEVICE_SSO && shareScope != Scopes.DEVICE_SSO) {
            "Health Card never asks for device_sso"
        }
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

    /**
     * True when [uri] is eSignet's consent page on [issuer] (same scheme, host and port, the consent
     * path): Health Card opens a `consent_uri` only when it is, never another address an answer names.
     */
    fun isConsentPage(uri: String): Boolean = try {
        val expected = URI(issuer)
        val actual = URI(uri)
        actual.scheme.equals(expected.scheme, ignoreCase = true) &&
            actual.host.equals(expected.host, ignoreCase = true) &&
            actual.port == expected.port &&
            actual.rawPath == (expected.rawPath ?: "").trimEnd('/') + CONSENT_PAGE_PATH &&
            actual.rawFragment == null && actual.rawUserInfo == null
    } catch (e: IllegalArgumentException) {
        false
    } catch (e: java.net.URISyntaxException) {
        false
    }

    companion object {
        /** The permission "Allow card sharing" asks for. */
        const val SHARE_SCOPE: String = "health_card_share"

        /** Path of eSignet's consent-only re-consent page. */
        const val CONSENT_PAGE_PATH: String = "/oauth2/native-sso/consent"

        /** eSignet's OTP authentication context class. */
        const val OTP_ACR: String = "mosip:idp:acr:generated-code"

        /** Health Card's own login asks for the name and the phone number in userinfo. */
        const val DEFAULT_CLAIMS: String =
            """{"userinfo":{"name":{"essential":true},"email":{"essential":false},"phone_number":{"essential":false}}}"""
    }
}

/** Which DPoP key Health Card's access token is bound to. */
enum class Binding {
    /** Token exchange: bound to Hub's key, so userinfo needs a proof signed by Hub. */
    HUB,

    /** Health Card's own browser login (re-consent): bound to Health Card's own key. */
    OWN,
}

/**
 * Health Card's sign-in, written to the encrypted store as one blob. [toString] hides every token.
 *
 * @property hubSessionId `sid` of Hub's id_token at the exchange: when Hub's sign-in changes, so does
 *   this, and Health Card signs in again
 */
data class CardSession(
    val issuer: String,
    val clientId: String,
    val subject: String,
    val sessionId: String?,
    val accessToken: String,
    val accessTokenExpiresAt: Instant?,
    val idToken: String,
    val scope: String?,
    val binding: Binding,
    val hubSessionId: String?,
    val name: String?,
    val email: String?,
    val phone: String?,
    val signedInAt: Instant,
) {
    /** The scope values Health Card holds. */
    val scopes: Set<String> get() = scope.orEmpty().split(' ').filter { it.isNotBlank() }.toSet()

    override fun toString(): String =
        "CardSession(issuer=$issuer, clientId=$clientId, sid=$sessionId, scope=$scope, binding=$binding, signedInAt=$signedInAt)"

    fun toJson(): ByteArray = JSONObjectUtils.toJSONString(
        linkedMapOf<String, Any?>(
            "v" to 1L,
            "issuer" to issuer,
            "client_id" to clientId,
            "sub" to subject,
            "sid" to sessionId,
            "access_token" to accessToken,
            "access_token_expires_at" to accessTokenExpiresAt?.epochSecond,
            "id_token" to idToken,
            "scope" to scope,
            "binding" to binding.name,
            "hub_sid" to hubSessionId,
            "name" to name,
            "email" to email,
            "phone" to phone,
            "signed_in_at" to signedInAt.epochSecond,
        ).filterValues { it != null },
    ).toByteArray(Charsets.UTF_8)

    companion object {
        /** Parses [toJson] output; [ParseException] when it is not that. */
        fun fromJson(bytes: ByteArray): CardSession {
            val m = parse(bytes)
            return CardSession(
                issuer = m.str("issuer"),
                clientId = m.str("client_id"),
                subject = m.str("sub"),
                sessionId = m.optStr("sid"),
                accessToken = m.str("access_token"),
                accessTokenExpiresAt = m.optLong("access_token_expires_at")?.let(Instant::ofEpochSecond),
                idToken = m.str("id_token"),
                scope = m.optStr("scope"),
                binding = Binding.entries.firstOrNull { it.name == m.str("binding") }
                    ?: throw ParseException("binding unknown", 0),
                hubSessionId = m.optStr("hub_sid"),
                name = m.optStr("name"),
                email = m.optStr("email"),
                phone = m.optStr("phone"),
                signedInAt = Instant.ofEpochSecond(m.long("signed_in_at")),
            )
        }
    }
}

/** Health Card's own browser login in flight (re-consent), kept encrypted so it survives process death. */
data class PendingConsent(
    val state: String,
    val nonce: String,
    val codeVerifier: String,
    val redirectUri: String,
    val scope: String,
    val createdAt: Instant,
) {
    override fun toString(): String = "PendingConsent(scope=$scope, createdAt=$createdAt)"

    fun toJson(): ByteArray = JSONObjectUtils.toJSONString(
        linkedMapOf<String, Any?>(
            "v" to 1L,
            "state" to state,
            "nonce" to nonce,
            "code_verifier" to codeVerifier,
            "redirect_uri" to redirectUri,
            "scope" to scope,
            "created_at" to createdAt.epochSecond,
        ),
    ).toByteArray(Charsets.UTF_8)

    companion object {
        fun fromJson(bytes: ByteArray): PendingConsent {
            val m = parse(bytes)
            return PendingConsent(
                state = m.str("state"),
                nonce = m.str("nonce"),
                codeVerifier = m.str("code_verifier"),
                redirectUri = m.str("redirect_uri"),
                scope = m.str("scope"),
                createdAt = Instant.ofEpochSecond(m.long("created_at")),
            )
        }
    }
}

/**
 * A consent-only re-consent in flight: eSignet's consent page is open for [scope]. Kept encrypted so the
 * redirect back works after a process death too. It holds no secret: the approval itself is kept by
 * eSignet, and Health Card only repeats the token exchange.
 */
data class PendingConsentOnly(val scope: String, val createdAt: Instant) {
    fun toJson(): ByteArray = JSONObjectUtils.toJSONString(
        linkedMapOf<String, Any?>("v" to 1L, "scope" to scope, "created_at" to createdAt.epochSecond),
    ).toByteArray(Charsets.UTF_8)

    companion object {
        fun fromJson(bytes: ByteArray): PendingConsentOnly {
            val m = parse(bytes)
            return PendingConsentOnly(scope = m.str("scope"), createdAt = Instant.ofEpochSecond(m.long("created_at")))
        }
    }
}

private fun parse(bytes: ByteArray): Map<String, Any?> {
    val m = JSONObjectUtils.parse(String(bytes, Charsets.UTF_8))
    if ((m["v"] as? Number)?.toLong() != 1L) throw ParseException("unknown record version", 0)
    return m
}

private fun Map<String, Any?>.str(name: String): String = optStr(name) ?: throw ParseException("$name missing", 0)

private fun Map<String, Any?>.optStr(name: String): String? = when (val v = this[name]) {
    null -> null
    is String -> v
    else -> throw ParseException("$name is not a string", 0)
}

private fun Map<String, Any?>.long(name: String): Long = optLong(name) ?: throw ParseException("$name missing", 0)

private fun Map<String, Any?>.optLong(name: String): Long? = when (val v = this[name]) {
    null -> null
    is Number -> v.toLong()
    else -> throw ParseException("$name is not a number", 0)
}

/** The user's display name from userinfo claims (OpenID Connect Core 5.1 and 5.2), as Hub reads it. */
object UserInfoNames {
    /** `name`, else a language-tagged `name#xx` (English first), else `given_name` + `family_name`. */
    fun displayName(claims: Map<String, Any?>): String? {
        (claims["name"] as? String)?.takeIf { it.isNotBlank() }?.let { return it.trim() }
        val tagged = claims.entries.filter { it.key.startsWith("name#") && (it.value as? String)?.isNotBlank() == true }
            .sortedBy { if (it.key.startsWith("name#en")) 0 else 1 }
        tagged.firstOrNull()?.let { return (it.value as String).trim() }
        val parts = listOfNotNull(claims["given_name"] as? String, claims["family_name"] as? String).filter { it.isNotBlank() }
        return parts.takeIf { it.isNotEmpty() }?.joinToString(" ") { it.trim() }
    }
}

/** How a debug install names itself to eSignet (the same rule as Hub's). */
object ClientIdentity {
    /** `prefix` + the first 12 hex characters of SHA-256 over the client key's RFC 7638 thumbprint. */
    fun clientIdFor(prefix: String, clientKeyThumbprint: String): String {
        val digest = java.security.MessageDigest.getInstance("SHA-256").digest(clientKeyThumbprint.toByteArray(Charsets.US_ASCII))
        return prefix + digest.take(6).joinToString("") { "%02x".format(it) }
    }
}
