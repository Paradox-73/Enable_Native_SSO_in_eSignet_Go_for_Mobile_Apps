// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Test
import java.time.Duration

class DpopProofFactoryTest {
    private val signer = EcKeyJwsSigner.generate(keyId = "client-key")
    private val factory = DpopProofFactory(signer, FIXED_CLOCK)

    @Test
    fun `proof header and claims follow RFC 9449 section 4_2`() {
        val proof = factory.proof(DpopRequest("post", "https://idp.example/oauth2/token?x=1#frag"))
        val jws = verifiedJws(proof, signer.publicJwk)

        assertEquals("dpop+jwt", jws.header.type.type)
        assertEquals("ES256", jws.header.algorithm.name)
        assertEquals(signer.publicJwk, jws.header.jwk.toJSONObject())
        assertFalse("public key only", jws.header.jwk.isPrivate)
        assertNull("the jwk identifies the key; no kid", jws.header.keyID)

        val claims = jws.payload.toJSONObject()
        assertEquals("POST", claims["htm"])
        assertEquals("https://idp.example/oauth2/token", claims["htu"])
        assertEquals(NOW.epochSecond, (claims["iat"] as Number).toLong())
        assertFalse("no ath without an access token", "ath" in claims)
        assertFalse("no nonce until the server sends one", "nonce" in claims)
    }

    @Test
    fun `every proof has a new jti`() {
        val a = verifiedJws(factory.proof(DpopRequest("POST", "https://idp.example/t")), signer.publicJwk)
        val b = verifiedJws(factory.proof(DpopRequest("POST", "https://idp.example/t")), signer.publicJwk)
        assertNotEquals(a.payload.toJSONObject()["jti"], b.payload.toJSONObject()["jti"])
    }

    @Test
    fun `ath and nonce are carried when given`() {
        val ath = DpopProofs.accessTokenHash("Kz~8mXK1EalYznwH-LC-1fBAo.4Ljp~zsPE_NeO.gxU")
        // RFC 9449 section 7.1 example value.
        assertEquals("fUHyO2r2Z3DZ53EsNrWBb0xWXoaNy59IiKCAqksmQEo", ath)
        val claims = verifiedJws(
            factory.proof(DpopRequest("GET", "https://idp.example/oauth2/userinfo", ath, "server-nonce")),
            signer.publicJwk,
        ).payload.toJSONObject()
        assertEquals(ath, claims["ath"])
        assertEquals("server-nonce", claims["nonce"])
    }

    @Test
    fun `thumbprint is the RFC 7638 value of the signing key`() {
        assertEquals(JwkThumbprint.compute(signer.publicJwk), factory.thumbprint)
    }

    @Test
    fun `htu normalisation keeps the path bytes and rejects relative URLs`() {
        assertEquals("http://localhost:38088/a%20b", DpopProofs.normalizeHtu("HTTP://localhost:38088/a%20b?q"))
        assertThrows(IllegalArgumentException::class.java) { DpopProofs.normalizeHtu("/oauth2/token") }
    }
}

class ClientAssertionFactoryTest {
    private val signer = EcKeyJwsSigner.generate(keyId = "hub-key-1")
    private val issuer = "http://localhost:38088"
    private val endpoint = "http://localhost:38088/oauth2/token"

    @Test
    fun `assertion claims follow RFC 7523 with the issuer as audience by default`() {
        val factory = ClientAssertionFactory("hub-client", signer, clock = FIXED_CLOCK, lifetime = Duration.ofSeconds(90))
        val jws = verifiedJws(factory.create(issuer, endpoint), signer.publicJwk)
        val claims = jws.payload.toJSONObject()

        assertEquals("hub-key-1", jws.header.keyID)
        assertEquals("hub-client", claims["iss"])
        assertEquals("hub-client", claims["sub"])
        assertEquals(issuer, claims["aud"])
        assertEquals(NOW.epochSecond, (claims["iat"] as Number).toLong())
        assertEquals(NOW.epochSecond + 90, (claims["exp"] as Number).toLong())
    }

    @Test
    fun `endpoint audience mode and unique jti per assertion`() {
        val factory = ClientAssertionFactory("hub-client", signer, AssertionAudience.ENDPOINT, clock = FIXED_CLOCK)
        val a = verifiedJws(factory.create(issuer, endpoint), signer.publicJwk).payload.toJSONObject()
        val b = verifiedJws(factory.create(issuer, endpoint), signer.publicJwk).payload.toJSONObject()
        assertEquals(endpoint, a["aud"])
        assertNotEquals(a["jti"], b["jti"])
    }

    @Test
    fun `form parameters name the jwt-bearer assertion type`() {
        val params = ClientAssertionFactory("hub-client", signer).formParameters(issuer, endpoint)
        assertEquals("hub-client", params["client_id"])
        assertEquals("urn:ietf:params:oauth:client-assertion-type:jwt-bearer", params["client_assertion_type"])
        verifiedJws(params.getValue("client_assertion"), signer.publicJwk)
    }

    @Test
    fun `blank client id or non-positive lifetime is a programming error`() {
        assertThrows(IllegalArgumentException::class.java) { ClientAssertionFactory(" ", signer) }
        assertThrows(IllegalArgumentException::class.java) { ClientAssertionFactory("c", signer, lifetime = Duration.ZERO) }
    }
}
