// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import com.nimbusds.jose.jwk.Curve
import com.nimbusds.jose.jwk.ECKey
import com.nimbusds.jose.jwk.gen.ECKeyGenerator
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.security.KeyPairGenerator
import java.security.Signature
import java.security.spec.ECGenParameterSpec

/** RFC test vectors and the small building blocks under the protocol code. */
class JwkThumbprintTest {
    @Test
    fun `RFC 7638 section 3_1 example thumbprint`() {
        val jwk = mapOf(
            "kty" to "RSA",
            "n" to "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
            "e" to "AQAB",
            "alg" to "RS256",
            "kid" to "2011-04-29",
        )
        assertEquals("NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs", JwkThumbprint.compute(jwk))
    }

    @Test
    fun `EC thumbprint matches an independent implementation and ignores optional members`() {
        val key: ECKey = ECKeyGenerator(Curve.P_256).keyID("ignored").generate()
        val jwk = key.toPublicJWK().toJSONObject()
        assertEquals(key.computeThumbprint().toString(), JwkThumbprint.compute(jwk))
        assertEquals(JwkThumbprint.compute(jwk), JwkThumbprint.compute(jwk - "kid"))
    }

    @Test
    fun `missing required member or unknown key type is rejected`() {
        assertThrows(IllegalArgumentException::class.java) {
            JwkThumbprint.compute(mapOf("kty" to "EC", "crv" to "P-256", "x" to "abc"))
        }
        assertThrows(IllegalArgumentException::class.java) { JwkThumbprint.compute(mapOf("kty" to "oct", "k" to "abc")) }
    }
}

class PkceTest {
    @Test
    fun `RFC 7636 appendix B example challenge`() {
        val pkce = Pkce.fromVerifier("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
        assertEquals("E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", pkce.challenge)
        assertEquals("S256", pkce.method)
    }

    @Test
    fun `generated verifiers are 43 unreserved characters and never repeat`() {
        val a = Pkce.generate()
        val b = Pkce.generate()
        assertEquals(43, a.verifier.length)
        assertTrue(a.verifier.matches(Regex("[A-Za-z0-9_-]+")))
        assertNotEquals(a.verifier, b.verifier)
        assertEquals(Pkce.challengeOf(a.verifier), a.challenge)
    }

    @Test
    fun `verifier that is too short or has reserved characters is rejected`() {
        assertThrows(IllegalArgumentException::class.java) { Pkce.fromVerifier("short") }
        assertThrows(IllegalArgumentException::class.java) { Pkce.fromVerifier("a".repeat(42) + "/") }
    }

    @Test
    fun `toString never shows the verifier`() {
        val pkce = Pkce.generate()
        assertTrue(pkce.verifier !in pkce.toString())
    }
}

class EcSignaturesTest {
    private val keyPair = KeyPairGenerator.getInstance("EC").apply { initialize(ECGenParameterSpec("secp256r1")) }.generateKeyPair()

    @Test
    fun `DER signatures convert to 64-byte R S and back, and still verify`() {
        repeat(20) { i ->
            // 20 signatures make short R or S values (leading zero bytes) very likely to appear.
            val data = "message $i".toByteArray()
            val der = Signature.getInstance("SHA256withECDSA").run { initSign(keyPair.private); update(data); sign() }
            val jose = EcSignatures.derToJose(der)
            assertEquals(64, jose.size)
            val back = EcSignatures.joseToDer(jose)
            val ok = Signature.getInstance("SHA256withECDSA").run { initVerify(keyPair.public); update(data); verify(back) }
            assertTrue(ok)
        }
    }

    @Test
    fun `short values are left-padded`() {
        // SEQUENCE { INTEGER 1, INTEGER 0x80 (needs a sign byte) }
        val der = byteArrayOf(0x30, 0x07, 0x02, 0x01, 0x01, 0x02, 0x02, 0x00, 0x80.toByte())
        val jose = EcSignatures.derToJose(der)
        assertArrayEquals(ByteArray(31) + byteArrayOf(1) + ByteArray(31) + byteArrayOf(0x80.toByte()), jose)
        assertArrayEquals(der, EcSignatures.joseToDer(jose))
    }

    @Test
    fun `garbage is not accepted as a DER signature`() {
        assertThrows(IllegalArgumentException::class.java) { EcSignatures.derToJose(ByteArray(64)) }
        assertThrows(IllegalArgumentException::class.java) {
            EcSignatures.derToJose(byteArrayOf(0x30, 0x06, 0x02, 0x01, 0x01, 0x02, 0x01, 0x01, 0x00))
        }
    }

    @Test
    fun `software signer publishes a public JWK only`() {
        val signer = SoftwareEcSigner.generate()
        assertEquals(setOf("kty", "crv", "x", "y"), signer.publicJwk.keys)
        assertEquals(43, signer.publicJwk.getValue("x").length)
    }
}

class TokenHashesTest {
    @Test
    fun `half hash follows the algorithm's hash size`() {
        assertEquals(22, TokenHashes.halfHash("value", "PS256").length) // 16 bytes
        assertEquals(32, TokenHashes.halfHash("value", "ES384").length) // 24 bytes
        assertEquals(43, TokenHashes.halfHash("value", "RS512").length) // 32 bytes
        assertThrows(IllegalArgumentException::class.java) { TokenHashes.halfHash("value", "EdDSA") }
    }

    @Test
    fun `ds_hash is the left half of SHA-256 over the device secret`() {
        val secret = "eyJhbGciOiJSU0EtT0FFUC0yNTYifQ.example"
        val full = sha256(secret)
        assertEquals(B64.encode(full.copyOfRange(0, 16)), TokenHashes.dsHash(secret))
        assertEquals(TokenHashes.dsHash(secret), TokenHashes.halfHash(secret, "PS256"))
    }
}

class OAuthErrorTest {
    @Test
    fun `known codes map to typed values and unknown codes are kept`() {
        val known = OAuthErrorException.of("interaction_required", "scope phone needs consent", 400)
        assertEquals(OAuthError.INTERACTION_REQUIRED, known.error)
        assertTrue(known.requiresInteraction)

        val unknown = OAuthErrorException.of("vendor_specific", null, 400)
        assertEquals(OAuthError.UNKNOWN, unknown.error)
        assertEquals("vendor_specific", unknown.code)
        assertTrue(!unknown.requiresInteraction)
    }

    @Test
    fun `scope strings split on spaces and keep order`() {
        assertEquals(listOf("openid", "email", "phone"), ScopeStrings.parse(" openid  email phone ").toList())
        assertEquals(emptySet<String>(), ScopeStrings.parse(null))
        assertEquals("openid email", ScopeStrings.format(listOf("openid", "email")))
    }
}
