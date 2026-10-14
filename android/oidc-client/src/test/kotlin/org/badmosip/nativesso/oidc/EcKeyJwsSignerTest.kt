// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import com.nimbusds.jose.JWSAlgorithm
import com.nimbusds.jose.JWSHeader
import com.nimbusds.jose.crypto.ECDSAVerifier
import com.nimbusds.jose.jwk.ECKey
import com.nimbusds.jose.util.Base64URL
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.security.KeyPairGenerator
import java.security.Signature
import java.security.interfaces.ECPublicKey
import java.security.spec.ECGenParameterSpec

/**
 * The signer the Keystore keys sit behind, run here with software keys: `Signature` returns DER on
 * both, so the DER to `R || S` path is the same one the phone takes.
 */
class EcKeyJwsSignerTest {
    private val pair = KeyPairGenerator.getInstance("EC").apply { initialize(ECGenParameterSpec("secp256r1")) }.generateKeyPair()
    private val signer = EcKeyJwsSigner(pair.private, pair.public as ECPublicKey, "kid-1")

    @Test
    fun `signatures are 64-byte R and S that verify as ES256`() {
        val verifier = ECDSAVerifier(ECKey.parse(signer.publicJwk))
        val header = JWSHeader(JWSAlgorithm.ES256)
        repeat(50) { i ->
            val input = "header.payload-$i".toByteArray(Charsets.US_ASCII)
            val signature = signer.sign(input)
            assertEquals(64, signature.size)
            assertTrue(verifier.verify(header, input, Base64URL.encode(signature)))
            // The same bytes in DER verify with plain java.security.
            val der = EcSignatures.joseToDer(signature)
            assertTrue(Signature.getInstance("SHA256withECDSA").run { initVerify(pair.public); update(input); verify(der) })
        }
    }

    @Test
    fun `public JWK, thumbprint and key id`() {
        assertEquals(setOf("kty", "crv", "x", "y"), signer.publicJwk.keys)
        assertEquals("P-256", signer.publicJwk["crv"])
        assertEquals("ES256", signer.algorithm)
        assertEquals(JwkThumbprint.compute(signer.publicJwk), signer.thumbprint)
        assertEquals("kid-1", signer.keyId)
        val renamed = signer.withKeyId(null)
        assertNull(renamed.keyId)
        assertEquals(signer.publicJwk, renamed.publicJwk)
    }

    @Test
    fun `a key on another curve is refused`() {
        val p384 = KeyPairGenerator.getInstance("EC").apply { initialize(ECGenParameterSpec("secp384r1")) }.generateKeyPair()
        assertThrows(IllegalArgumentException::class.java) { EcKeyJwsSigner(p384.private, p384.public as ECPublicKey) }
    }
}
