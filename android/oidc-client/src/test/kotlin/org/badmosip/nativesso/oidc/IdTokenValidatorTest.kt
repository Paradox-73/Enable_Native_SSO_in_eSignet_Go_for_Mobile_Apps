// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import com.nimbusds.jose.JWSAlgorithm
import com.nimbusds.jose.JWSHeader
import com.nimbusds.jose.crypto.MACSigner
import com.nimbusds.jose.jwk.JWKSet
import com.nimbusds.jwt.PlainJWT
import com.nimbusds.jwt.SignedJWT
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import java.time.Instant

class IdTokenValidatorTest {
    private val issuer = TestIssuer()
    private val deviceSecret = "eyJhbGciOiJSU0EtT0FFUC0yNTYiLCJlbmMiOiJBMjU2R0NNIn0.device-secret"
    private var jwksFetches = 0
    private var jwksSet: JWKSet = IssuerKeys.jwks
    private val jwks = JwksSource { _ -> jwksFetches++; jwksSet }
    private val validator = IdTokenValidator(issuer.issuer, jwks, FIXED_CLOCK)
    private val login = IdTokenExpectations(audience = "hub-client", nonce = "n-0S6_WzA2Mj")

    private fun nativeSsoClaims(extra: Map<String, Any?> = emptyMap()) = issuer.claims(
        extra = mapOf("sid" to "sid-1", "ds_hash" to TokenHashes.dsHash(deviceSecret)) + extra,
    ).build()

    private fun assertRejected(reason: IdTokenFailure, token: String, expected: IdTokenExpectations = login) {
        val e = assertThrows(IdTokenValidationException::class.java) { validator.validate(token, expected) }
        assertEquals(reason, e.reason)
    }

    @Test
    fun `valid PS256 token from a Native SSO login passes and exposes sid`() {
        val token = issuer.signPs256(nativeSsoClaims())
        val idToken = validator.validate(token, login.copy(deviceSecret = deviceSecret))
        assertEquals("psut-123", idToken.subject)
        assertEquals("sid-1", idToken.sessionId)
        assertEquals(NOW.plusSeconds(120), idToken.expiresAt)
    }

    @Test
    fun `valid ES256 token passes`() {
        validator.validate(issuer.signEs256(issuer.claims().build()), login)
    }

    @Test
    fun `token signed by another key with the same kid is rejected`() {
        assertRejected(IdTokenFailure.SIGNATURE, issuer.signPs256(issuer.claims().build(), IssuerKeys.other))
    }

    @Test
    fun `unknown kid triggers one JWKS refresh and then fails`() {
        val token = SignedJWT(JWSHeader.Builder(JWSAlgorithm.PS256).keyID("rotated").build(), issuer.claims().build())
            .apply { sign(com.nimbusds.jose.crypto.RSASSASigner(IssuerKeys.rsa)) }.serialize()
        assertRejected(IdTokenFailure.UNKNOWN_KEY, token)
        assertEquals("cached fetch plus one forced refresh", 2, jwksFetches)
    }

    @Test
    fun `key rotated in after caching is found by the refresh`() {
        jwksSet = JWKSet(IssuerKeys.ec.toPublicJWK()) // the cache does not have the RSA key yet
        val token = issuer.signPs256(issuer.claims().build())
        val refreshing = IdTokenValidator(issuer.issuer, { force -> if (force) IssuerKeys.jwks else jwksSet }, FIXED_CLOCK)
        refreshing.validate(token, login)
    }

    @Test
    fun `HMAC and unsigned tokens are refused`() {
        val hs = SignedJWT(JWSHeader(JWSAlgorithm.HS256), issuer.claims().build())
            .apply { sign(MACSigner(ByteArray(32) { 7 })) }.serialize()
        assertRejected(IdTokenFailure.ALGORITHM, hs)
        assertRejected(IdTokenFailure.MALFORMED, PlainJWT(issuer.claims().build()).serialize())
        assertThrows(IllegalArgumentException::class.java) { IdTokenValidator(issuer.issuer, jwks, allowedAlgorithms = setOf("HS256")) }
    }

    @Test
    fun `wrong issuer is rejected`() {
        val claims = issuer.claims().issuer("http://localhost:38089").build()
        assertRejected(IdTokenFailure.ISSUER, issuer.signPs256(claims))
    }

    @Test
    fun `token for another client is rejected, also with several audiences and a foreign azp`() {
        assertRejected(IdTokenFailure.AUDIENCE, issuer.signPs256(issuer.claims(audience = "health-card-client").build()))
        val multi = issuer.claims().audience(listOf("hub-client", "other")).claim("azp", "other").build()
        assertRejected(IdTokenFailure.AUDIENCE, issuer.signPs256(multi))
    }

    @Test
    fun `expired token is rejected beyond the clock skew and accepted within it`() {
        assertRejected(IdTokenFailure.EXPIRED, issuer.signPs256(issuer.claims(expiresAt = NOW.minusSeconds(61)).build()))
        validator.validate(issuer.signPs256(issuer.claims(expiresAt = NOW.minusSeconds(30)).build()), login)
    }

    @Test
    fun `token issued in the future is rejected`() {
        val claims = issuer.claims(issuedAt = NOW.plusSeconds(300), expiresAt = NOW.plusSeconds(600)).build()
        assertRejected(IdTokenFailure.ISSUED_IN_FUTURE, issuer.signPs256(claims))
    }

    @Test
    fun `nonce from another login is rejected`() {
        assertRejected(IdTokenFailure.NONCE, issuer.signPs256(issuer.claims(nonce = "other").build()))
        assertRejected(IdTokenFailure.NONCE, issuer.signPs256(issuer.claims(nonce = null).build()))
    }

    @Test
    fun `at_hash must match the access token when both are present`() {
        val good = issuer.claims(extra = mapOf("at_hash" to TokenHashes.halfHash("at-123", "PS256"))).build()
        validator.validate(issuer.signPs256(good), login.copy(accessToken = "at-123"))
        assertRejected(IdTokenFailure.AT_HASH, issuer.signPs256(good), login.copy(accessToken = "at-999"))
    }

    @Test
    fun `ds_hash missing or for another device secret is rejected`() {
        val expected = login.copy(deviceSecret = deviceSecret)
        assertRejected(IdTokenFailure.DS_HASH, issuer.signPs256(issuer.claims(extra = mapOf("sid" to "sid-1")).build()), expected)
        val otherSecret = nativeSsoClaims(mapOf("ds_hash" to TokenHashes.dsHash("another-device-secret")))
        assertRejected(IdTokenFailure.DS_HASH, issuer.signPs256(otherSecret), expected)
    }

    @Test
    fun `sid is required with a device secret`() {
        val claims = issuer.claims(extra = mapOf("ds_hash" to TokenHashes.dsHash(deviceSecret))).build()
        assertRejected(IdTokenFailure.SID_MISSING, issuer.signPs256(claims), login.copy(deviceSecret = deviceSecret))
    }

    @Test
    fun `garbage and wrongly typed claims are malformed`() {
        assertRejected(IdTokenFailure.MALFORMED, "not-a-jwt")
        val badNonce = issuer.claims(nonce = null, extra = mapOf("nonce" to 42)).build()
        assertRejected(IdTokenFailure.MALFORMED, issuer.signPs256(badNonce))
        val noExp = issuer.claims().expirationTime(null).build()
        assertRejected(IdTokenFailure.MALFORMED, issuer.signPs256(noExp))
    }

    @Test
    fun `auth_time and acr are exposed`() {
        val claims = issuer.claims(extra = mapOf("auth_time" to NOW.epochSecond - 5, "acr" to "mosip:idp:acr:generated-code")).build()
        val token = validator.validate(issuer.signPs256(claims), login)
        assertEquals(Instant.ofEpochSecond(NOW.epochSecond - 5), token.authTime)
        assertEquals("mosip:idp:acr:generated-code", token.acr)
    }
}
