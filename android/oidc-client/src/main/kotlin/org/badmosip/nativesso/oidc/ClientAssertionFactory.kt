// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import java.time.Clock
import java.time.Duration
import java.util.UUID

/** Which `aud` a client assertion carries. */
public enum class AssertionAudience {
    /**
     * The issuer identifier. FAPI 2.0 Security Profile 5.3.2.1 requires it, and Thunder, the engine
     * inside eSignet Go, accepts nothing else (`clientauth.go`, validateClientAssertion).
     */
    ISSUER,

    /** The URL of the endpoint the assertion is sent to (OpenID Connect Core 9, RFC 7523 3). */
    ENDPOINT,
}

/**
 * Client authentication with `private_key_jwt` (OpenID Connect Core 9, RFC 7523 section 3):
 * a short-lived JWT signed with the client's own key, with `iss` = `sub` = client_id.
 */
public class ClientAssertionFactory(
    private val clientId: String,
    private val signer: JwsSigner,
    private val audience: AssertionAudience = AssertionAudience.ISSUER,
    private val lifetime: Duration = Duration.ofSeconds(60),
    private val clock: Clock = Clock.systemUTC(),
    private val jtiSource: () -> String = { UUID.randomUUID().toString() },
) {
    init {
        require(clientId.isNotBlank()) { "clientId must not be blank" }
        require(!lifetime.isNegative && !lifetime.isZero) { "lifetime must be positive" }
    }

    /**
     * A new assertion for one request. Each call has a fresh `jti`, because the server records it
     * and refuses a second use (RFC 7523 section 3, item 7).
     */
    public fun create(issuer: String, endpoint: String): String {
        val now = clock.instant().epochSecond
        val claims = linkedMapOf<String, Any?>(
            "iss" to clientId,
            "sub" to clientId,
            "aud" to if (audience == AssertionAudience.ISSUER) issuer else endpoint,
            "jti" to jtiSource(),
            "iat" to now,
            "exp" to now + lifetime.seconds,
        )
        return signCompactJws(signer, mapOf("typ" to "JWT"), claims)
    }

    /** Form parameters that authenticate the client (RFC 7521 section 4.2). */
    public fun formParameters(issuer: String, endpoint: String): Map<String, String> = linkedMapOf(
        "client_id" to clientId,
        "client_assertion_type" to ClientAssertion.TYPE_JWT_BEARER,
        "client_assertion" to create(issuer, endpoint),
    )
}
