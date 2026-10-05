// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import com.nimbusds.jose.JOSEException
import com.nimbusds.jose.JWSAlgorithm
import com.nimbusds.jose.JWSVerifier
import com.nimbusds.jose.crypto.ECDSAVerifier
import com.nimbusds.jose.crypto.RSASSAVerifier
import com.nimbusds.jose.jwk.ECKey
import com.nimbusds.jose.jwk.JWK
import com.nimbusds.jose.jwk.JWKSet
import com.nimbusds.jose.jwk.KeyUse
import com.nimbusds.jose.jwk.RSAKey
import com.nimbusds.jwt.JWTClaimsSet
import com.nimbusds.jwt.SignedJWT
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.IOException
import java.security.MessageDigest
import java.text.ParseException
import java.time.Clock
import java.time.Duration
import java.time.Instant

/** The issuer's signing keys (OpenID Connect Core 10.1.1). */
public fun interface JwksSource {
    /** The key set; [forceRefresh] fetches it again, used once when a token names an unknown key. */
    public fun get(forceRefresh: Boolean): JWKSet
}

/** [JwksSource] that downloads `jwks_uri` once and keeps it until a refresh is forced. */
public class HttpJwksSource(private val http: OkHttpClient, private val jwksUri: String) : JwksSource {
    private var cached: JWKSet? = null

    @Synchronized
    override fun get(forceRefresh: Boolean): JWKSet {
        cached?.takeIf { !forceRefresh }?.let { return it }
        val request = Request.Builder().url(jwksUri).header("Accept", "application/json").get().build()
        val body = try {
            http.newCall(request).execute().use { response ->
                if (!response.isSuccessful) throw HttpStatusException(response.code, "JWKS: HTTP ${response.code}")
                response.body?.string().orEmpty()
            }
        } catch (e: IOException) {
            throw NetworkException("JWKS could not be fetched", e)
        }
        return try {
            JWKSet.parse(body).also { cached = it }
        } catch (e: ParseException) {
            throw InvalidResponseException("JWKS is not a JWK set", e)
        }
    }
}

/**
 * What the app expects of an id_token beyond the issuer's signature.
 *
 * @property audience the app's own client_id
 * @property nonce the nonce of the login that produced the token; null for token-exchange results
 * @property accessToken when given and the token has at_hash, at_hash must match it
 * @property deviceSecret when given, ds_hash must be present and match it, and sid must be present
 *   (Native SSO 1.0 section 3.3)
 */
public data class IdTokenExpectations(
    val audience: String,
    val nonce: String? = null,
    val accessToken: String? = null,
    val deviceSecret: String? = null,
) {
    override fun toString(): String =
        "IdTokenExpectations(audience=$audience, nonce=${nonce != null}, accessToken=${accessToken != null}, " +
            "deviceSecret=${deviceSecret != null})"
}

/** A validated id_token. [claims] holds every claim; the properties are the ones the apps read. */
public class IdToken internal constructor(
    public val raw: String,
    public val claims: JWTClaimsSet,
) {
    public val subject: String get() = claims.subject
    public val sessionId: String? get() = claims.getStringClaim("sid")
    public val expiresAt: Instant get() = claims.expirationTime.toInstant()
    public val authTime: Instant? get() = (claims.getClaim("auth_time") as? Number)?.let { Instant.ofEpochSecond(it.toLong()) }
    public val acr: String? get() = claims.getStringClaim("acr")

    override fun toString(): String = "IdToken(sub=$subject, sid=$sessionId, exp=$expiresAt)"
}

/**
 * Validates id_tokens as OpenID Connect Core 3.1.3.7 and Native SSO 1.0 section 3.4.1 ask:
 * algorithm on the allowed list, signature by the issuer's key, `iss`, `aud` (and `azp` when there
 * are several audiences), `exp`, `iat`, `nonce`, `at_hash`, and for a Native SSO login `ds_hash`
 * and `sid`.
 *
 * @param allowedAlgorithms asymmetric algorithms only; "none" and HMAC can never be allowed
 *   because the client holds no shared secret. eSignet signs with PS256 by default.
 * @param dsHash how the server derives ds_hash from the device secret. Native SSO 1.0 section 3.4.1
 *   leaves that to the server; the default is this project's eSignet rule, [TokenHashes.dsHash].
 */
public class IdTokenValidator(
    private val issuer: String,
    private val jwks: JwksSource,
    private val clock: Clock = Clock.systemUTC(),
    private val clockSkew: Duration = Duration.ofSeconds(60),
    allowedAlgorithms: Set<String> = DEFAULT_ALGORITHMS,
    private val dsHash: (deviceSecret: String) -> String = TokenHashes::dsHash,
) {
    private val signatures = IssuerSignatureVerifier(jwks, allowedAlgorithms)

    /** Runs every check; the first failure throws [IdTokenValidationException]. */
    public fun validate(idToken: String, expected: IdTokenExpectations): IdToken {
        val jwt = try {
            SignedJWT.parse(idToken)
        } catch (e: ParseException) {
            fail(IdTokenFailure.MALFORMED, "not a signed JWT", e)
        }
        val alg = jwt.header.algorithm
        verifySignature(jwt)

        val claims = try {
            jwt.jwtClaimsSet
        } catch (e: ParseException) {
            fail(IdTokenFailure.MALFORMED, "claims are not a JSON object", e)
        }
        checkClaims(claims, alg, expected)
        return IdToken(idToken, claims)
    }

    private fun verifySignature(jwt: SignedJWT) {
        when (signatures.check(jwt)) {
            IssuerSignatureVerifier.Result.VALID -> Unit
            IssuerSignatureVerifier.Result.ALGORITHM_NOT_ALLOWED ->
                fail(IdTokenFailure.ALGORITHM, "algorithm ${jwt.header.algorithm.name} is not allowed")
            IssuerSignatureVerifier.Result.UNKNOWN_KEY ->
                fail(IdTokenFailure.UNKNOWN_KEY, "no issuer key for kid=${jwt.header.keyID} and alg=${jwt.header.algorithm}")
            IssuerSignatureVerifier.Result.BAD_SIGNATURE ->
                fail(IdTokenFailure.SIGNATURE, "signature does not verify with the issuer's key")
        }
    }

    private fun checkClaims(claims: JWTClaimsSet, alg: JWSAlgorithm, expected: IdTokenExpectations) {
        val now = clock.instant()

        if (claims.issuer != issuer) fail(IdTokenFailure.ISSUER, "iss is \"${claims.issuer}\", expected \"$issuer\"")

        val audience = claims.audience.orEmpty()
        if (expected.audience !in audience) fail(IdTokenFailure.AUDIENCE, "aud does not include ${expected.audience}")
        if (audience.size > 1) {
            val azp = stringClaim(claims, "azp")
            if (azp != expected.audience) fail(IdTokenFailure.AUDIENCE, "several audiences and azp is not ${expected.audience}")
        }

        val exp = claims.expirationTime?.toInstant() ?: fail(IdTokenFailure.MALFORMED, "exp is missing")
        if (!now.minus(clockSkew).isBefore(exp)) fail(IdTokenFailure.EXPIRED, "expired at $exp")
        val iat = claims.issueTime?.toInstant() ?: fail(IdTokenFailure.MALFORMED, "iat is missing")
        if (iat.isAfter(now.plus(clockSkew))) fail(IdTokenFailure.ISSUED_IN_FUTURE, "iat $iat is in the future")
        if (claims.subject.isNullOrEmpty()) fail(IdTokenFailure.MALFORMED, "sub is missing")

        if (expected.nonce != null && !constantTimeEquals(stringClaim(claims, "nonce"), expected.nonce)) {
            fail(IdTokenFailure.NONCE, "nonce does not match the login")
        }

        val atHash = stringClaim(claims, "at_hash")
        if (expected.accessToken != null && atHash != null &&
            !constantTimeEquals(atHash, TokenHashes.halfHash(expected.accessToken, alg.name))
        ) {
            fail(IdTokenFailure.AT_HASH, "at_hash does not match the access token")
        }

        if (expected.deviceSecret != null) {
            val dsHashClaim = stringClaim(claims, "ds_hash") ?: fail(IdTokenFailure.DS_HASH, "ds_hash is missing")
            if (!constantTimeEquals(dsHashClaim, dsHash(expected.deviceSecret))) {
                fail(IdTokenFailure.DS_HASH, "ds_hash does not match the device secret")
            }
            if (stringClaim(claims, "sid").isNullOrEmpty()) fail(IdTokenFailure.SID_MISSING, "sid is missing")
        }
    }

    private fun stringClaim(claims: JWTClaimsSet, name: String): String? = try {
        claims.getStringClaim(name)
    } catch (e: ParseException) {
        fail(IdTokenFailure.MALFORMED, "$name is not a string", e)
    }

    private fun fail(reason: IdTokenFailure, message: String, cause: Throwable? = null): Nothing =
        throw IdTokenValidationException(reason, message, cause)

    public companion object {
        /** RSA-PSS, RSA PKCS#1 v1.5 and ECDSA with SHA-2. */
        public val DEFAULT_ALGORITHMS: Set<String> =
            setOf("PS256", "PS384", "PS512", "RS256", "RS384", "RS512", "ES256", "ES384", "ES512")
    }
}

/**
 * Checks that a JWS was signed by the issuer: algorithm on the allowed list, a key from the issuer's
 * JWKS with the same kid, signing use and a fitting key type, and a valid signature. When no key
 * matches, the JWKS is fetched once more, so a key rotated in after the cache was filled still works.
 * Used for id_tokens (through [IdTokenValidator]) and for signed userinfo answers ([claimsOf]).
 */
public class IssuerSignatureVerifier(
    private val jwks: JwksSource,
    allowedAlgorithms: Set<String> = IdTokenValidator.DEFAULT_ALGORITHMS,
) {
    private val allowed: Set<JWSAlgorithm> = allowedAlgorithms.map(JWSAlgorithm::parse).toSet()

    init {
        // "none" and HMAC algorithms can never be allowed: the client holds no shared secret.
        val unsupported = allowed.filterNot { it in JWSAlgorithm.Family.RSA || it in JWSAlgorithm.Family.EC }
        require(unsupported.isEmpty()) { "unsupported algorithms: $unsupported" }
    }

    /** Outcome of [check]. */
    public enum class Result { VALID, ALGORITHM_NOT_ALLOWED, UNKNOWN_KEY, BAD_SIGNATURE }

    /** Checks [jwt]'s algorithm and signature. */
    public fun check(jwt: SignedJWT): Result {
        if (jwt.header.algorithm !in allowed) return Result.ALGORITHM_NOT_ALLOWED
        val candidates = candidateKeys(jwks.get(false), jwt).ifEmpty { candidateKeys(jwks.get(true), jwt) }
        if (candidates.isEmpty()) return Result.UNKNOWN_KEY
        val verified = candidates.any { key ->
            try {
                jwt.verify(verifierFor(key))
            } catch (e: JOSEException) {
                false
            }
        }
        return if (verified) Result.VALID else Result.BAD_SIGNATURE
    }

    /**
     * Claims of a signed JWT whose signature checks out, for a signed userinfo answer
     * (OpenID Connect Core 5.3.2); any failure is an [InvalidResponseException].
     */
    public fun claimsOf(jws: String): Map<String, Any?> {
        val jwt = try {
            SignedJWT.parse(jws)
        } catch (e: ParseException) {
            throw InvalidResponseException("signed JWT is malformed", e)
        }
        val result = check(jwt)
        if (result != Result.VALID) throw InvalidResponseException(
                "signed JWT rejected: $result (kid=${jwt.header.keyID}, alg=${jwt.header.algorithm.name}, " +
                    "issuer kids=${jwks.get(false).keys.map { "${it.keyID}/${it.algorithm?.name}/${it.keyUse}" }})",
            )
        return try {
            jwt.jwtClaimsSet.toJSONObject()
        } catch (e: ParseException) {
            throw InvalidResponseException("signed JWT claims are not a JSON object", e)
        }
    }

    private fun candidateKeys(set: JWKSet, jwt: SignedJWT): List<JWK> {
        val alg = jwt.header.algorithm
        val kid = jwt.header.keyID
        return set.keys.filter { key ->
            (kid == null || key.keyID == kid) &&
                (key.keyUse == null || key.keyUse == KeyUse.SIGNATURE) &&
                (key.algorithm == null || key.algorithm.name == alg.name) &&
                when (alg) {
                    in JWSAlgorithm.Family.RSA -> key is RSAKey
                    in JWSAlgorithm.Family.EC -> key is ECKey
                    else -> false
                }
        }
    }

    private fun verifierFor(key: JWK): JWSVerifier = when (key) {
        is RSAKey -> RSASSAVerifier(key)
        is ECKey -> ECDSAVerifier(key)
        else -> throw JOSEException("unsupported key type ${key.keyType}")
    }
}

/** Hash claims that bind a value to an id_token. */
public object TokenHashes {
    /**
     * Left half of the hash of the ASCII [value], base64url: the at_hash construction (OpenID
     * Connect Core 3.1.3.6). The hash follows the id_token's [jwsAlgorithm]: SHA-256 for *256
     * (PS256, eSignet's default), SHA-384 for *384, SHA-512 for *512.
     */
    public fun halfHash(value: String, jwsAlgorithm: String): String {
        val digest = when {
            jwsAlgorithm.endsWith("256") -> "SHA-256"
            jwsAlgorithm.endsWith("384") -> "SHA-384"
            jwsAlgorithm.endsWith("512") -> "SHA-512"
            else -> throw IllegalArgumentException("no hash rule for $jwsAlgorithm")
        }
        val hash = MessageDigest.getInstance(digest).digest(value.toByteArray(Charsets.US_ASCII))
        return B64.encode(hash.copyOfRange(0, hash.size / 2))
    }

    /**
     * ds_hash as this project's eSignet computes it (docs/design.md, "Device secret, ds_hash and
     * sid"): the at_hash construction with SHA-256, whatever the id_token's algorithm, so the value
     * does not change when the signing key does. Native SSO 1.0 section 3.4.1 leaves the rule to the
     * server and asks only that it reveals nothing useful about the device secret.
     */
    public fun dsHash(deviceSecret: String): String = halfHash(deviceSecret, "SHA256")
}

internal fun constantTimeEquals(a: String?, b: String): Boolean =
    a != null && MessageDigest.isEqual(a.toByteArray(Charsets.UTF_8), b.toByteArray(Charsets.UTF_8))
