// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import java.security.SecureRandom

/**
 * PKCE pair (RFC 7636) with the S256 method, the only one FAPI 2.0 allows.
 *
 * @property verifier sent with the code at the token endpoint; keep it secret until then
 * @property challenge sent in the authorization request
 */
public class Pkce private constructor(public val verifier: String, public val challenge: String) {
    public val method: String get() = METHOD

    override fun toString(): String = "Pkce(challenge=$challenge)"

    public companion object {
        public const val METHOD: String = "S256"

        /** A new pair from 32 random bytes: a 43-character verifier (RFC 7636 section 4.1). */
        public fun generate(random: SecureRandom = SecureRandom()): Pkce =
            fromVerifier(B64.encode(ByteArray(32).also(random::nextBytes)))

        /** The pair for a known verifier, which must be 43 to 128 unreserved characters. */
        public fun fromVerifier(verifier: String): Pkce {
            require(verifier.length in 43..128) { "code_verifier must be 43 to 128 characters" }
            require(verifier.all { it.isLetterOrDigit() && it.code < 128 || it in "-._~" }) {
                "code_verifier may hold only unreserved characters"
            }
            return Pkce(verifier, challengeOf(verifier))
        }

        /** `BASE64URL(SHA256(ASCII(code_verifier)))` (RFC 7636 section 4.2). */
        public fun challengeOf(verifier: String): String = B64.encode(sha256(verifier))
    }
}
