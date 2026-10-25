// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import java.net.URI
import java.time.Clock
import java.util.UUID

/**
 * What a DPoP proof is for (RFC 9449 section 4.2).
 *
 * @property htm HTTP method
 * @property htu target URL; query and fragment are dropped by [DpopProofs.normalizeHtu]
 * @property ath base64url SHA-256 of the access token sent with the request, or null
 * @property nonce the server's latest DPoP-Nonce, or null
 */
public data class DpopRequest(
    val htm: String,
    val htu: String,
    val ath: String? = null,
    val nonce: String? = null,
)

/**
 * Produces DPoP proofs. The Super App answers with its own key ([DpopProofFactory]); the Child App
 * forwards each request to the Super App over the family binder, because the token exchange must
 * be proven with the key the device secret is bound to (Native SSO 4.1).
 */
public fun interface DpopProofSource {
    public fun proof(request: DpopRequest): String
}

/** Helpers shared by proof producers and the HTTP client. */
public object DpopProofs {
    /** `ath`: base64url SHA-256 of the ASCII access token (RFC 9449 section 4.2). */
    public fun accessTokenHash(accessToken: String): String = B64.encode(sha256(accessToken))

    /** The URL without query and fragment, as `htu` must be (RFC 9449 section 4.2). */
    public fun normalizeHtu(url: String): String {
        val uri = URI(url)
        require(uri.isAbsolute && uri.rawAuthority != null) { "htu must be an absolute URL" }
        // Built from the raw parts so percent-escapes in the path stay exactly as given.
        return uri.scheme.lowercase() + "://" + uri.rawAuthority + (uri.rawPath ?: "")
    }
}

/**
 * Builds and signs DPoP proofs with [signer]: header `typ` dpop+jwt, `alg`, public `jwk`; claims
 * `jti` (random, never reused), `htm`, `htu`, `iat`, and `ath` / `nonce` when given.
 */
public class DpopProofFactory(
    private val signer: JwsSigner,
    private val clock: Clock = Clock.systemUTC(),
    private val jtiSource: () -> String = { UUID.randomUUID().toString() },
) : DpopProofSource {

    /** RFC 7638 thumbprint of the signing key: the `cnf.jkt` of tokens bound to it. */
    public val thumbprint: String get() = JwkThumbprint.compute(signer.publicJwk)

    override fun proof(request: DpopRequest): String {
        val header = linkedMapOf<String, Any?>("typ" to DpopHttp.PROOF_TYPE, "jwk" to signer.publicJwk)
        val claims = linkedMapOf<String, Any?>(
            "jti" to jtiSource(),
            "htm" to request.htm.uppercase(),
            "htu" to DpopProofs.normalizeHtu(request.htu),
            "iat" to clock.instant().epochSecond,
            "ath" to request.ath,
            "nonce" to request.nonce,
        )
        return signCompactJws(NoKidSigner(signer), header, claims)
    }

    // A DPoP header identifies its key by the embedded jwk; a kid would be noise (RFC 9449 4.2).
    private class NoKidSigner(private val delegate: JwsSigner) : JwsSigner by delegate {
        override val keyId: String? get() = null
    }
}
