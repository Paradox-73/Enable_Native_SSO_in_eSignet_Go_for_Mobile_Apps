// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.sso

import org.badmosip.nativesso.contract.FamilySsoContract
import org.badmosip.nativesso.oidc.DpopProofFactory
import org.badmosip.nativesso.oidc.DpopProofs
import org.badmosip.nativesso.oidc.JwkThumbprint
import org.badmosip.nativesso.oidc.SoftwareEcSigner
import org.badmosip.nativesso.superapp.auth.HubAuthenticator
import org.badmosip.nativesso.superapp.auth.MemoryBlobStore
import org.badmosip.nativesso.superapp.auth.MutableClock
import org.badmosip.nativesso.superapp.auth.TokenBundle
import org.badmosip.nativesso.superapp.auth.verifiedJws
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

/**
 * Rules of the family single sign-on service (design rules A04 and A07): who may call, what is
 * handed out, which proofs Hub signs, the rate limit and the access list.
 */
class FamilySsoGateTest {
    private val issuer = "http://localhost:38088"
    private val tokenEndpoint = "$issuer/v1/esignet/oauth/v2/token"
    private val userinfoEndpoint = "$issuer/v1/esignet/oidc/userinfo"
    private val familyCert = byteArrayOf(1, 2, 3, 4)
    private val otherCert = byteArrayOf(9, 9, 9, 9)

    private val childUid = 10_100
    private val intruderUid = 10_200
    private val strangerUid = 10_300
    private val unknownUid = 10_400

    private val packages = mutableMapOf(
        childUid to listOf(CallerPackage(FamilySsoContract.CHILD_APP_PACKAGE, listOf(familyCert))),
        // Same package name as Health Card, other signer: a repackaged app or a permission takeover.
        intruderUid to listOf(CallerPackage(FamilySsoContract.CHILD_APP_PACKAGE, listOf(otherCert))),
        // Signed with the family certificate, but not on the allow-list.
        strangerUid to listOf(CallerPackage("org.badmosip.nativesso.other", listOf(familyCert))),
    )

    private val clock = MutableClock(Instant.ofEpochSecond(1_800_000_000))
    private val store = MemoryBlobStore()
    private val dpopSigner = SoftwareEcSigner.generate()
    private val logLines = mutableListOf<String>()
    private var current: TokenBundle? = session()
    private var hubCerts = listOf(familyCert)

    private fun session(deviceSecret: String? = "device-secret-value") = TokenBundle(
        issuer = issuer,
        clientId = "ps03-hub-0123456789ab",
        subject = "psut-1",
        sessionId = "sid-1",
        accessToken = "hub-access-token-value",
        tokenType = "DPoP",
        accessTokenExpiresAt = null,
        idToken = "hub-id-token-value",
        idTokenExpiresAt = clock.now.plusSeconds(120),
        refreshToken = null,
        deviceSecret = deviceSecret,
        scope = "openid profile device_sso",
        dpopKeyThumbprint = JwkThumbprint.compute(dpopSigner.publicJwk),
        displayName = null,
        signedInAt = clock.now,
        tokenEndpoint = tokenEndpoint,
        userinfoEndpoint = userinfoEndpoint,
    )

    private fun gate(rateLimit: Int = 30) = FamilySsoGate(
        callers = { uid -> packages[uid].orEmpty() },
        familyCertificates = { hubCerts },
        session = { current },
        proofs = DpopProofFactory(dpopSigner, clock),
        accessLog = FamilyAccessLog(store),
        clock = clock,
        rateLimit = rateLimit,
        rateWindowSeconds = 60,
        log = { logLines += it },
    )

    private fun refusedWith(code: Int, call: () -> Unit) {
        val e = assertThrows(FamilySsoException::class.java) { call() }
        assertEquals(e.message, code, e.code)
    }

    @Test
    fun `family app gets version, id_token, device secret, issuer and scope`() {
        val gate = gate()
        assertEquals(FamilySsoContract.VERSION, gate.version(childUid))
        val share = gate.bundle(childUid)!!
        assertEquals(issuer, share.issuer)
        assertEquals("hub-id-token-value", share.idToken)
        assertEquals("device-secret-value", share.deviceSecret)
        assertEquals("openid profile device_sso", share.scope)
        assertEquals(0L, share.expiresAtEpochSec)
        assertFalse(share.toString().contains("token-value") || share.toString().contains("secret-value"))
    }

    @Test
    fun `caller signed with another certificate is refused on every call`() {
        val gate = gate()
        refusedWith(FamilySsoContract.ERROR_CALLER_NOT_ALLOWED) { gate.version(intruderUid) }
        refusedWith(FamilySsoContract.ERROR_CALLER_NOT_ALLOWED) { gate.bundle(intruderUid) }
        refusedWith(FamilySsoContract.ERROR_CALLER_NOT_ALLOWED) { gate.signProof(intruderUid, "POST", tokenEndpoint, null, null) }
        assertTrue(FamilyAccessLog(store).list().isEmpty())
    }

    @Test
    fun `family-signed package outside the allow-list and unknown uid are refused`() {
        val gate = gate()
        refusedWith(FamilySsoContract.ERROR_CALLER_NOT_ALLOWED) { gate.bundle(strangerUid) }
        refusedWith(FamilySsoContract.ERROR_CALLER_NOT_ALLOWED) { gate.bundle(unknownUid) }
    }

    @Test
    fun `a shared uid passes when one of its packages is an allowed family app`() {
        packages[childUid] = listOf(CallerPackage("org.example.unrelated", listOf(otherCert))) + packages.getValue(childUid)
        assertNotNull(gate().bundle(childUid))
    }

    @Test
    fun `nobody is admitted when Hub cannot read its own certificate`() {
        hubCerts = emptyList()
        refusedWith(FamilySsoContract.ERROR_CALLER_NOT_ALLOWED) { gate().bundle(childUid) }
    }

    @Test
    fun `without a shared sign-in there is no bundle and no proof`() {
        current = null
        val gate = gate()
        assertNull(gate.bundle(childUid))
        refusedWith(FamilySsoContract.ERROR_NOT_SIGNED_IN) { gate.signProof(childUid, "POST", tokenEndpoint, null, null) }
        // Signed in, but the user did not turn on "Share this sign-in with my other apps": nothing is shared.
        current = session(deviceSecret = null)
        assertNull(gate.bundle(childUid))
        refusedWith(FamilySsoContract.ERROR_NOT_SIGNED_IN) { gate.signProof(childUid, "POST", tokenEndpoint, null, null) }
        assertTrue(FamilyAccessLog(store).list().isEmpty())
    }

    @Test
    fun `proof for the token endpoint is signed with Hub's DPoP key and fresh claims`() {
        val gate = gate()
        val ath = DpopProofs.accessTokenHash("some-access-token")
        val first = verifiedJws(gate.signProof(childUid, "POST", tokenEndpoint, null, "server-nonce-1"), dpopSigner.publicJwk)
        clock.now = clock.now.plusSeconds(5)
        val second = verifiedJws(gate.signProof(childUid, "GET", userinfoEndpoint, ath, null), dpopSigner.publicJwk)

        assertEquals("dpop+jwt", first.header.type.toString())
        assertEquals(dpopSigner.publicJwk, first.header.jwk.toJSONObject())
        val c1 = first.payload.toJSONObject()
        val c2 = second.payload.toJSONObject()
        assertEquals("POST", c1["htm"])
        assertEquals(tokenEndpoint, c1["htu"])
        assertEquals("server-nonce-1", c1["nonce"])
        assertNull(c1["ath"])
        assertEquals(1_800_000_000L, (c1["iat"] as Number).toLong())
        assertEquals("GET", c2["htm"])
        assertEquals(userinfoEndpoint, c2["htu"])
        assertEquals(ath, c2["ath"])
        assertEquals(1_800_000_005L, (c2["iat"] as Number).toLong())
        assertNotEquals(c1["jti"], c2["jti"])
        // A POST to userinfo is allowed too (OpenID Connect Core 5.3.1).
        assertNotNull(gate.signProof(childUid, "POST", userinfoEndpoint, ath, null))
    }

    @Test
    fun `foreign htu, wrong method and URL tricks are refused`() {
        val gate = gate()
        val refused = listOf(
            "POST" to "https://attacker.example/oauth2/token",
            "POST" to "$issuer/oauth2/authorize",
            "POST" to "$issuer/v1/esignet/oauth/v2/token/extra",
            "POST" to "$tokenEndpoint?x=1",
            "POST" to "$tokenEndpoint#frag",
            "POST" to "http://user@localhost:38088/v1/esignet/oauth/v2/token",
            "POST" to "http://localhost:38089/v1/esignet/oauth/v2/token",
            "POST" to "https://localhost:38088/v1/esignet/oauth/v2/token",
            "POST" to "not a url",
            "GET" to tokenEndpoint,
            "PUT" to tokenEndpoint,
            "post" to tokenEndpoint,
            "DELETE" to userinfoEndpoint,
        )
        for ((htm, htu) in refused) {
            refusedWith(FamilySsoContract.ERROR_HTU_NOT_ALLOWED) { gate.signProof(childUid, htm, htu, null, null) }
        }
        // Host case does not matter; the path does.
        assertNotNull(gate.signProof(childUid, "POST", "http://LOCALHOST:38088/v1/esignet/oauth/v2/token", null, null))
    }

    @Test
    fun `no proof when the session has no discovered endpoints`() {
        current = session().copy(tokenEndpoint = null, userinfoEndpoint = null)
        refusedWith(FamilySsoContract.ERROR_HTU_NOT_ALLOWED) { gate().signProof(childUid, "POST", tokenEndpoint, null, null) }
    }

    @Test
    fun `malformed ath, nonce or missing arguments are rejected`() {
        val gate = gate()
        assertThrows(IllegalArgumentException::class.java) { gate.signProof(childUid, "POST", tokenEndpoint, "short", null) }
        assertThrows(IllegalArgumentException::class.java) { gate.signProof(childUid, "POST", tokenEndpoint, null, "has space") }
        assertThrows(IllegalArgumentException::class.java) { gate.signProof(childUid, "POST", tokenEndpoint, null, "x".repeat(513)) }
        assertThrows(IllegalArgumentException::class.java) { gate.signProof(childUid, null, tokenEndpoint, null, null) }
        assertThrows(IllegalArgumentException::class.java) { gate.signProof(childUid, "POST", null, null, null) }
    }

    @Test
    fun `rate limit per caller, released when the window moves on`() {
        val gate = gate(rateLimit = 3)
        repeat(3) { gate.version(childUid) }
        refusedWith(FamilySsoContract.ERROR_RATE_LIMITED) { gate.bundle(childUid) }
        refusedWith(FamilySsoContract.ERROR_RATE_LIMITED) { gate.signProof(childUid, "POST", tokenEndpoint, null, null) }
        clock.now = clock.now.plusSeconds(61)
        assertNotNull(gate.bundle(childUid))
    }

    @Test
    fun `refused callers do not use up the family app's rate`() {
        val gate = gate(rateLimit = 2)
        repeat(5) { runCatching { gate.version(intruderUid) } }
        gate.version(childUid)
        gate.version(childUid)
        refusedWith(FamilySsoContract.ERROR_RATE_LIMITED) { gate.version(childUid) }
    }

    @Test
    fun `access list records package and time, never a token, and sign-out clears it`() {
        val gate = gate()
        gate.bundle(childUid)
        clock.now = clock.now.plusSeconds(30)
        gate.bundle(childUid)
        val apps = FamilyAccessLog(store).list()
        assertEquals(listOf(FamilyAccess(FamilySsoContract.CHILD_APP_PACKAGE, clock.now)), apps)
        val raw = String(store.read(HubAuthenticator.FAMILY_ACCESS)!!)
        for (secret in listOf("hub-id-token-value", "device-secret-value", "hub-access-token-value")) {
            assertFalse(raw.contains(secret))
            assertFalse(logLines.joinToString().contains(secret))
        }
        store.write(HubAuthenticator.FAMILY_ACCESS, "not json".toByteArray())
        assertTrue(FamilyAccessLog(store).list().isEmpty())
        assertFalse(store.contains(HubAuthenticator.FAMILY_ACCESS))
    }

    @Test
    fun `access list keeps one row per package, newest first`() {
        val log = FamilyAccessLog(store)
        log.record("a.pkg", Instant.ofEpochSecond(100))
        log.record("b.pkg", Instant.ofEpochSecond(200))
        log.record("a.pkg", Instant.ofEpochSecond(300))
        assertEquals(listOf(FamilyAccess("a.pkg", Instant.ofEpochSecond(300)), FamilyAccess("b.pkg", Instant.ofEpochSecond(200))), log.list())
    }
}
