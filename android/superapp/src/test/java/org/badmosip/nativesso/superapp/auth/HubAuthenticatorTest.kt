// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.auth

import okhttp3.OkHttpClient
import org.badmosip.nativesso.oidc.EcKeyJwsSigner
import org.badmosip.nativesso.oidc.IdTokenFailure
import org.badmosip.nativesso.oidc.IdTokenValidationException
import org.badmosip.nativesso.oidc.InvalidResponseException
import org.badmosip.nativesso.oidc.JwkThumbprint
import org.badmosip.nativesso.oidc.OAuthErrorException
import org.badmosip.nativesso.oidc.Pkce
import org.badmosip.nativesso.securestore.AesGcmAead
import org.badmosip.nativesso.securestore.EncryptedFileStore
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.security.KeyPairGenerator
import java.security.interfaces.ECPublicKey
import java.security.spec.ECGenParameterSpec
import java.time.Duration
import javax.crypto.KeyGenerator

/** Hub's whole sign-in against a fake eSignet: what is sent, what is checked, what is stored. */
class HubAuthenticatorTest {
    @get:Rule
    val folder = TemporaryFolder()

    private val redirectUri = "org.badmosip.nativesso.superapp://oauth/callback"
    private val clientId = "ps03-hub-test"
    private lateinit var esignet: FakeEsignet
    private val store = MemoryBlobStore()
    private val clock = MutableClock()
    private val http = OkHttpClient()
    private val dpopSigner = newSigner(null)
    private val clientSigner = newSigner("client-kid")
    private val logs = mutableListOf<String>()

    @Before
    fun setUp() {
        esignet = FakeEsignet(clientId).start()
    }

    @After
    fun tearDown() = esignet.stop()

    private fun newSigner(kid: String?): EcKeyJwsSigner {
        val pair = KeyPairGenerator.getInstance("EC").apply { initialize(ECGenParameterSpec("secp256r1")) }.generateKeyPair()
        return EcKeyJwsSigner(pair.private, pair.public as ECPublicKey, kid)
    }

    private var keysForgotten = 0

    private fun authenticator(store: org.badmosip.nativesso.securestore.BlobStore = this.store) = HubAuthenticator(
        config = HubConfig(issuer = esignet.issuer, clientId = clientId, redirectUri = redirectUri),
        http = http,
        dpopSigner = dpopSigner,
        clientSigner = clientSigner,
        store = store,
        clock = clock,
        log = { logs += it },
        forgetDpopKey = { keysForgotten++ },
    )

    @Test
    fun `sign in without sharing stores the session and the name`() {
        val hub = authenticator()
        val url = hub.startLogin(shareWithFamily = false)

        assertEquals("${esignet.server.url("/authorize")}?client_id=$clientId&request_uri=urn%3Aietf%3Aparams%3Aoauth%3Arequest_uri%3Aabc", url)
        val par = esignet.parForm
        assertEquals("openid profile email", par["scope"])
        assertEquals(redirectUri, par["redirect_uri"])
        assertEquals(HubConfig.OTP_ACR, par["acr_values"])
        // The name for Hub's greeting and the email for Health Card; the phone number needs Health Card's own login.
        assertEquals("""{"userinfo":{"name":{"essential":true},"email":{"essential":false}}}""", par["claims"])
        assertEquals("S256", par["code_challenge_method"])
        assertTrue(hub.hasPendingLogin())

        val session = hub.completeLogin(esignet.redirect(redirectUri))

        assertEquals(esignet.subject, session.subject)
        assertEquals("Siddharth K Mansour", session.displayName)
        assertNull(session.deviceSecret)
        assertEquals(JwkThumbprint.compute(dpopSigner.publicJwk), session.dpopKeyThumbprint)
        assertEquals(esignet.accessToken, session.accessToken)
        assertFalse(hub.hasPendingLogin())
        assertEquals(session, hub.currentSession())

        // PKCE: the verifier sent at the token endpoint belongs to the challenge sent at PAR.
        assertEquals(par["code_challenge"], Pkce.challengeOf(esignet.tokenForm.getValue("code_verifier")))
        assertEquals(esignet.code, esignet.tokenForm["code"])
        assertEquals(redirectUri, esignet.tokenForm["redirect_uri"])
    }

    @Test
    fun `client assertion has the issuer as audience and DPoP proofs use the DPoP key`() {
        val hub = authenticator()
        hub.startLogin(false)
        hub.completeLogin(esignet.redirect(redirectUri))

        for (form in listOf(esignet.parForm, esignet.tokenForm)) {
            assertEquals(clientId, form["client_id"])
            assertEquals("urn:ietf:params:oauth:client-assertion-type:jwt-bearer", form["client_assertion_type"])
            val assertion = verifiedJws(form.getValue("client_assertion"), clientSigner.publicJwk)
            assertEquals("client-kid", assertion.header.keyID)
            assertEquals(esignet.issuer, assertion.payload.toJSONObject()["aud"])
            assertEquals(clientId, assertion.payload.toJSONObject()["iss"])
        }
        for (path in listOf("/oauth2/par", "/oauth2/token", "/oidc/userinfo")) {
            val proof = verifiedJws(esignet.dpopHeaders.getValue(path), dpopSigner.publicJwk)
            assertEquals("dpop+jwt", proof.header.type.type)
            assertEquals(esignet.server.url(path).toString(), proof.payload.toJSONObject()["htu"])
        }
    }

    @Test
    fun `sharing asks for device_sso and keeps the device secret after the ds_hash check`() {
        esignet.deviceSecret = "ds-" + "y".repeat(60)
        val hub = authenticator()
        hub.startLogin(shareWithFamily = true)
        assertEquals(setOf("openid", "profile", "email", "device_sso"), esignet.parForm.getValue("scope").split(' ').toSet())

        val session = hub.completeLogin(esignet.redirect(redirectUri))

        assertEquals(esignet.deviceSecret, session.deviceSecret)
        assertEquals("sid-1", session.sessionId)
        assertEquals(esignet.deviceSecret, hub.currentSession()!!.deviceSecret)
        // The family service signs proofs only for these two, as discovered at this login.
        assertEquals(esignet.server.url("/oauth2/token").toString(), hub.currentSession()!!.tokenEndpoint)
        assertEquals(esignet.server.url("/oidc/userinfo").toString(), hub.currentSession()!!.userinfoEndpoint)
    }

    @Test
    fun `a wrong ds_hash rejects the login and stores nothing`() {
        esignet.deviceSecret = "ds-" + "y".repeat(60)
        esignet.dsHashOverride = "AAAAAAAAAAAAAAAAAAAAAA"
        val hub = authenticator()
        hub.startLogin(true)

        val e = assertThrows(IdTokenValidationException::class.java) { hub.completeLogin(esignet.redirect(redirectUri)) }
        assertEquals(IdTokenFailure.DS_HASH, e.reason)
        assertNull(hub.currentSession())
    }

    @Test
    fun `a device secret nobody asked for is refused`() {
        esignet.deviceSecret = "ds-unasked"
        val hub = authenticator()
        hub.startLogin(false)
        assertThrows(InvalidResponseException::class.java) { hub.completeLogin(esignet.redirect(redirectUri)) }
        assertNull(hub.currentSession())
    }

    @Test
    fun `a wrong nonce rejects the login and stores nothing`() {
        esignet.nonceOverride = "another-nonce"
        val hub = authenticator()
        hub.startLogin(false)
        val e = assertThrows(IdTokenValidationException::class.java) { hub.completeLogin(esignet.redirect(redirectUri)) }
        assertEquals(IdTokenFailure.NONCE, e.reason)
        assertNull(hub.currentSession())
    }

    @Test
    fun `a redirect with another state is refused and the login stays pending`() {
        val hub = authenticator()
        hub.startLogin(false)
        val e = assertThrows(LoginException::class.java) { hub.completeLogin(esignet.redirect(redirectUri, state = "forged")) }
        assertEquals(LoginException.Reason.STATE_MISMATCH, e.reason)
        assertTrue(hub.hasPendingLogin())
        assertNull(esignet.tokenForm["code"])

        // The real redirect still works afterwards.
        assertNotNull(hub.completeLogin(esignet.redirect(redirectUri)))
    }

    @Test
    fun `a redirect without state is refused`() {
        val hub = authenticator()
        hub.startLogin(false)
        val e = assertThrows(LoginException::class.java) { hub.completeLogin("$redirectUri?code=abc") }
        assertEquals(LoginException.Reason.STATE_MISMATCH, e.reason)
    }

    @Test
    fun `a redirect to another URI is refused`() {
        val hub = authenticator()
        hub.startLogin(false)
        val e = assertThrows(LoginException::class.java) {
            hub.completeLogin(esignet.redirect("org.badmosip.nativesso.childapp://oauth/callback"))
        }
        assertEquals(LoginException.Reason.NOT_OUR_REDIRECT, e.reason)
    }

    @Test
    fun `the same redirect cannot be used twice`() {
        val hub = authenticator()
        hub.startLogin(false)
        val redirect = esignet.redirect(redirectUri)
        hub.completeLogin(redirect)
        val e = assertThrows(LoginException::class.java) { hub.completeLogin(redirect) }
        assertEquals(LoginException.Reason.NO_PENDING_LOGIN, e.reason)
    }

    @Test
    fun `a login older than ten minutes is refused`() {
        val hub = authenticator()
        hub.startLogin(false)
        clock.now = clock.now.plus(Duration.ofMinutes(11))
        val e = assertThrows(LoginException::class.java) { hub.completeLogin(esignet.redirect(redirectUri)) }
        assertEquals(LoginException.Reason.PENDING_LOGIN_EXPIRED, e.reason)
        assertFalse(hub.hasPendingLogin())
    }

    @Test
    fun `the login finishes in a new process with the same store`() {
        authenticator().startLogin(false)
        // Android killed Hub while the browser was open: a new authenticator, same encrypted store.
        val session = authenticator().completeLogin(esignet.redirect(redirectUri))
        assertEquals(esignet.subject, session.subject)
    }

    @Test
    fun `an error redirect becomes an OAuth error and stores nothing`() {
        val hub = authenticator()
        hub.startLogin(false)
        val state = esignet.parForm.getValue("state")
        val redirect = "$redirectUri?error=access_denied&error_description=user+cancelled&state=" +
            java.net.URLEncoder.encode(state, "UTF-8")
        val e = assertThrows(OAuthErrorException::class.java) { hub.completeLogin(redirect) }
        assertEquals("access_denied", e.code)
        assertNull(hub.currentSession())
        assertFalse(hub.hasPendingLogin())
    }

    @Test
    fun `failed userinfo keeps the login without a name`() {
        esignet.userinfoStatus = 500
        val hub = authenticator()
        hub.startLogin(false)
        val session = hub.completeLogin(esignet.redirect(redirectUri))
        assertNull(session.displayName)
        assertTrue(logs.any { it.startsWith("userinfo failed") })
    }

    @Test
    fun `userinfo about another subject is ignored`() {
        esignet.userinfoSub = "someone-else"
        val hub = authenticator()
        hub.startLogin(false)
        assertNull(hub.completeLogin(esignet.redirect(redirectUri)).displayName)
    }

    @Test
    fun `sign out forgets tokens and any pending login`() {
        val hub = authenticator()
        hub.startLogin(false)
        hub.completeLogin(esignet.redirect(redirectUri))
        hub.startLogin(false)
        store.write(HubAuthenticator.FAMILY_ACCESS, "{}".toByteArray())
        hub.signOut()
        assertNull(hub.currentSession())
        assertFalse(hub.hasPendingLogin())
        assertFalse(store.contains(HubAuthenticator.FAMILY_ACCESS))
        assertEquals("sign out of all apps deletes the DPoP key", 1, keysForgotten)
    }

    @Test
    fun `sign out ends the device secret session on the server with Hub's client key`() {
        esignet.deviceSecret = "ds-" + "y".repeat(60)
        val hub = authenticator()
        hub.startLogin(shareWithFamily = true)
        hub.completeLogin(esignet.redirect(redirectUri))

        hub.signOut()

        val form = checkNotNull(esignet.revokeForm) { "no revocation request" }
        assertEquals(esignet.deviceSecret, form["token"])
        assertEquals("device_secret", form["token_type_hint"])
        assertEquals(clientId, form["client_id"])
        assertNull("revocation needs no DPoP proof", esignet.dpopHeaders["/oauth2/revoke"])
        assertNull(hub.currentSession())
        assertTrue(logs.contains("device secret revoked on the server"))
    }

    @Test
    fun `a failed revocation still signs out locally`() {
        esignet.deviceSecret = "ds-" + "y".repeat(60)
        esignet.revokeStatus = 503
        val hub = authenticator()
        hub.startLogin(shareWithFamily = true)
        hub.completeLogin(esignet.redirect(redirectUri))

        hub.signOut()

        assertNull(hub.currentSession())
        assertEquals(1, keysForgotten)
        assertTrue(logs.any { it.startsWith("device secret not revoked on the server") })
    }

    @Test
    fun `without a device secret or a revocation endpoint sign out sends nothing`() {
        val hub = authenticator()
        hub.startLogin(shareWithFamily = false)
        hub.completeLogin(esignet.redirect(redirectUri))
        hub.signOut()
        assertNull(esignet.revokeForm)

        esignet.advertiseRevocation = false
        esignet.deviceSecret = "ds-" + "y".repeat(60)
        val other = authenticator()
        other.startLogin(shareWithFamily = true)
        other.completeLogin(esignet.redirect(redirectUri))
        other.signOut()
        assertNull(esignet.revokeForm)
        assertTrue(logs.contains("server has no revocation endpoint"))
    }

    @Test
    fun `on disk the session is ciphertext and reads back through the store`() {
        val key = KeyGenerator.getInstance("AES").apply { init(256) }.generateKey()
        val fileStore = EncryptedFileStore(folder.newFolder("secure-store"), AesGcmAead { key })
        val hub = authenticator(fileStore)
        hub.startLogin(true.also { esignet.deviceSecret = "ds-" + "z".repeat(50) })
        val session = hub.completeLogin(esignet.redirect(redirectUri))

        val raw = String(fileStore.fileOf(HubAuthenticator.SESSION).readBytes(), Charsets.ISO_8859_1)
        for (secret in listOf(session.idToken, session.accessToken, session.deviceSecret!!, "eyJ")) {
            assertFalse("a token is readable in the file", raw.contains(secret))
        }
        assertEquals(session, authenticator(fileStore).currentSession())
    }

    @Test
    fun `an unreadable stored session counts as signed out and is removed`() {
        store.write(HubAuthenticator.SESSION, "not json".toByteArray())
        val hub = authenticator()
        assertNull(hub.currentSession())
        assertFalse(store.contains(HubAuthenticator.SESSION))
    }
}
