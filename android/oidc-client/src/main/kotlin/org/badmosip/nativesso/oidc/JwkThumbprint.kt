// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

/**
 * JWK SHA-256 thumbprint (RFC 7638), the value a DPoP-bound token carries as `cnf.jkt`
 * (RFC 9449 section 6.1) and that the Native SSO device secret binds to.
 */
public object JwkThumbprint {
    private val requiredMembers = mapOf(
        "EC" to listOf("crv", "kty", "x", "y"),
        "RSA" to listOf("e", "kty", "n"),
        "OKP" to listOf("crv", "kty", "x"),
    )

    /**
     * Base64url SHA-256 over the key's required members, in lexicographic order, without
     * whitespace (RFC 7638 section 3.2). Other members, such as "kid" or "alg", do not count.
     */
    public fun compute(jwk: Map<String, Any?>): String {
        val kty = jwk["kty"] as? String ?: throw IllegalArgumentException("JWK has no kty")
        val members = requiredMembers[kty] ?: throw IllegalArgumentException("unsupported kty \"$kty\"")
        val canonical = members.joinToString(",", "{", "}") { name ->
            val value = jwk[name] as? String ?: throw IllegalArgumentException("JWK is missing \"$name\"")
            "\"$name\":\"${escape(value)}\""
        }
        return B64.encode(sha256(canonical.toByteArray(Charsets.UTF_8)))
    }

    // Required members are base64url values and fixed names, so escaping never triggers for a valid
    // key; it keeps a malformed key from producing a different canonical form than the server's.
    private fun escape(value: String): String = buildString {
        for (c in value) {
            when {
                c == '"' -> append("\\\"")
                c == '\\' -> append("\\\\")
                c < ' ' -> append("\\u%04x".format(c.code))
                else -> append(c)
            }
        }
    }
}
