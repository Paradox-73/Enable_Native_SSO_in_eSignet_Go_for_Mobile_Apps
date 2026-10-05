// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.auth

import com.nimbusds.jose.util.JSONObjectUtils
import java.text.ParseException
import java.time.Instant

/**
 * One browser login in flight. Kept in the encrypted store (not only in memory or saved state), so
 * the login still completes when Android kills Hub while the user is in the browser.
 *
 * @property deviceSso whether this login asked for `device_sso` ("Use this sign-in in my other apps")
 */
data class PendingLogin(
    val state: String,
    val nonce: String,
    val codeVerifier: String,
    val redirectUri: String,
    val deviceSso: Boolean,
    val createdAt: Instant,
) {
    override fun toString(): String = "PendingLogin(deviceSso=$deviceSso, createdAt=$createdAt)"

    fun toJson(): ByteArray = JSONObjectUtils.toJSONString(
        linkedMapOf<String, Any?>(
            "v" to 1L,
            "state" to state,
            "nonce" to nonce,
            "code_verifier" to codeVerifier,
            "redirect_uri" to redirectUri,
            "device_sso" to deviceSso,
            "created_at" to createdAt.epochSecond,
        ),
    ).toByteArray(Charsets.UTF_8)

    companion object {
        /** Parses [toJson] output; [ParseException] when it is not that. */
        fun fromJson(bytes: ByteArray): PendingLogin {
            val m = parse(bytes)
            return PendingLogin(
                state = m.str("state"),
                nonce = m.str("nonce"),
                codeVerifier = m.str("code_verifier"),
                redirectUri = m.str("redirect_uri"),
                deviceSso = m["device_sso"] as? Boolean ?: throw ParseException("device_sso missing", 0),
                createdAt = Instant.ofEpochSecond(m.long("created_at")),
            )
        }
    }
}

/**
 * Everything Hub keeps after a login, written to the encrypted store as one blob. [toString] hides
 * every token.
 *
 * @property deviceSecret present when the login asked for and got `device_sso` (Native SSO 1.0 3.4)
 * @property dpopKeyThumbprint `jkt` of the DPoP key the tokens (and the device secret) are bound to
 * @property displayName the user's name from userinfo, when the server sent one
 * @property tokenEndpoint the token endpoint from discovery at login; the family service signs
 *   DPoP proofs only for this URL and [userinfoEndpoint] (null in sessions saved before WP-11)
 * @property userinfoEndpoint the userinfo endpoint from discovery at login, or null
 */
data class TokenBundle(
    val issuer: String,
    val clientId: String,
    val subject: String,
    val sessionId: String?,
    val accessToken: String,
    val tokenType: String,
    val accessTokenExpiresAt: Instant?,
    val idToken: String,
    val idTokenExpiresAt: Instant,
    val refreshToken: String?,
    val deviceSecret: String?,
    val scope: String?,
    val dpopKeyThumbprint: String,
    val displayName: String?,
    val signedInAt: Instant,
    val tokenEndpoint: String? = null,
    val userinfoEndpoint: String? = null,
) {
    override fun toString(): String =
        "TokenBundle(issuer=$issuer, clientId=$clientId, sid=$sessionId, scope=$scope, " +
            "deviceSecret=${deviceSecret != null}, refreshToken=${refreshToken != null}, signedInAt=$signedInAt)"

    fun toJson(): ByteArray = JSONObjectUtils.toJSONString(
        linkedMapOf<String, Any?>(
            "v" to 1L,
            "issuer" to issuer,
            "client_id" to clientId,
            "sub" to subject,
            "sid" to sessionId,
            "access_token" to accessToken,
            "token_type" to tokenType,
            "access_token_expires_at" to accessTokenExpiresAt?.epochSecond,
            "id_token" to idToken,
            "id_token_expires_at" to idTokenExpiresAt.epochSecond,
            "refresh_token" to refreshToken,
            "device_secret" to deviceSecret,
            "scope" to scope,
            "dpop_jkt" to dpopKeyThumbprint,
            "display_name" to displayName,
            "signed_in_at" to signedInAt.epochSecond,
            "token_endpoint" to tokenEndpoint,
            "userinfo_endpoint" to userinfoEndpoint,
        ).filterValues { it != null },
    ).toByteArray(Charsets.UTF_8)

    companion object {
        /** Parses [toJson] output; [ParseException] when it is not that. */
        fun fromJson(bytes: ByteArray): TokenBundle {
            val m = parse(bytes)
            return TokenBundle(
                issuer = m.str("issuer"),
                clientId = m.str("client_id"),
                subject = m.str("sub"),
                sessionId = m.optStr("sid"),
                accessToken = m.str("access_token"),
                tokenType = m.str("token_type"),
                accessTokenExpiresAt = m.optLong("access_token_expires_at")?.let(Instant::ofEpochSecond),
                idToken = m.str("id_token"),
                idTokenExpiresAt = Instant.ofEpochSecond(m.long("id_token_expires_at")),
                refreshToken = m.optStr("refresh_token"),
                deviceSecret = m.optStr("device_secret"),
                scope = m.optStr("scope"),
                dpopKeyThumbprint = m.str("dpop_jkt"),
                displayName = m.optStr("display_name"),
                signedInAt = Instant.ofEpochSecond(m.long("signed_in_at")),
                tokenEndpoint = m.optStr("token_endpoint"),
                userinfoEndpoint = m.optStr("userinfo_endpoint"),
            )
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

/** The user's display name from userinfo claims (OpenID Connect Core 5.1 and 5.2). */
object UserInfoNames {
    /**
     * `name`, else a language-tagged `name#xx` (English first), else `given_name` + `family_name`.
     * Returns null when none is a non-blank string.
     */
    fun displayName(claims: Map<String, Any?>): String? {
        (claims["name"] as? String)?.takeIf { it.isNotBlank() }?.let { return it.trim() }
        val tagged = claims.entries.filter { it.key.startsWith("name#") && (it.value as? String)?.isNotBlank() == true }
            .sortedBy { if (it.key.startsWith("name#en")) 0 else 1 }
        tagged.firstOrNull()?.let { return (it.value as String).trim() }
        val parts = listOfNotNull(claims["given_name"] as? String, claims["family_name"] as? String).filter { it.isNotBlank() }
        return parts.takeIf { it.isNotEmpty() }?.joinToString(" ") { it.trim() }
    }
}
