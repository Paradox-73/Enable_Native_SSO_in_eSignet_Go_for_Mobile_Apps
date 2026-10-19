// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp.auth

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import com.nimbusds.jwt.SignedJWT
import org.badmosip.nativesso.childapp.CardFixture
import org.badmosip.nativesso.childapp.HubException
import org.badmosip.nativesso.childapp.json
import org.badmosip.nativesso.childapp.verifiedJws
import org.badmosip.nativesso.contract.FamilySsoContract
import org.badmosip.nativesso.oidc.DpopProofs
import org.badmosip.nativesso.oidc.GrantTypes
import org.badmosip.nativesso.oidc.HttpStatusException
import org.badmosip.nativesso.oidc.IdTokenFailure
import org.badmosip.nativesso.oidc.IdTokenValidationException
import org.badmosip.nativesso.oidc.InvalidResponseException
import org.badmosip.nativesso.oidc.NetworkException
import org.badmosip.nativesso.oidc.OAuthError
import org.badmosip.nativesso.oidc.OAuthErrorException
import org.badmosip.nativesso.oidc.TokenTypes
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

/**
 * Health Card's sign-in against a fake eSignet, with Hub reached through the real binder client
 * ([org.badmosip.nativesso.childapp.HubConnection]) and a fake Hub service: what is sent, which key
 * signs each proof, what is checked, what is stored, and every way it can fail.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class CardAuthenticatorTest {
    private val app: Application = ApplicationProvider.getApplicationContext()
    private lateinit var f: CardFixture

    @Before
    fun setUp() {
        f = CardFixture(app)
    }

    @After
    fun tearDown() = f.stop()

    @Test
    fun `silent sign-in exchanges Hub's tokens and stores Health Card's own`() {
        f.shareHubSignIn()
        val session = f.authenticator().silentSignIn()

        // The token exchange of Native SSO 1.0 section 4.1.
        val form = f.esignet.exchangeForm
        assertEquals(GrantTypes.TOKEN_EXCHANGE, form["grant_type"])
        assertEquals(f.hub.bundle!!.idToken, form["subject_token"])
        assertEquals(TokenTypes.ID_TOKEN, form["subject_token_type"])
        assertEquals(f.esignet.deviceSecret, form["actor_token"])
        assertEquals(TokenTypes.DEVICE_SECRET, form["actor_token_type"])
        assertEquals(f.esignet.issuer, form["audience"])
        assertEquals("openid profile email", form["scope"])
        assertFalse("never device_sso", form.getValue("scope").contains("device_sso"))
        // Health Card's own private_key_jwt, not Hub's.
        val assertion = SignedJWT.parse(form["client_assertion"])
        assertEquals(f.clientId, assertion.jwtClaimsSet.issuer)
        assertTrue(assertion.verify(com.nimbusds.jose.crypto.ECDSAVerifier(com.nimbusds.jose.jwk.ECKey.parse(f.clientKey.publicJwk))))

        // Both proofs come from Hub's key: the device secret and the new access token are bound to it.
        val exchangeProof = verifiedJws(f.esignet.exchangeProofs.single(), f.hub.key.publicJwk)
        assertEquals(f.esignet.tokenEndpoint, exchangeProof.payload.toJSONObject()["htu"])
        val userinfoProof = verifiedJws(f.esignet.userinfoProofs.single(), f.hub.key.publicJwk)
        assertEquals(DpopProofs.accessTokenHash(f.esignet.exchangeAccessToken), userinfoProof.payload.toJSONObject()["ath"])
        assertEquals("GET", userinfoProof.payload.toJSONObject()["htm"])
        assertEquals("DPoP ${f.esignet.exchangeAccessToken}", f.esignet.userinfoAuthorizations.single())
        assertEquals(listOf("POST", "GET"), f.hub.proofRequests.map { it.htm })

        assertEquals(Binding.HUB, session.binding)
        assertEquals(f.esignet.hubSid, session.sessionId)
        assertEquals(f.esignet.hubSid, session.hubSessionId)
        assertEquals("Siddharth K Mansour", session.name)
        assertEquals("siddharth@example.com", session.email)
        assertNull(session.phone)
        assertEquals(f.esignet.exchangeAccessToken, session.accessToken)
        assertEquals(session, f.authenticator().currentSession())
        assertEquals("no browser login took place", 0, f.esignet.calls["/oauth2/par"] ?: 0)
        assertFalse(session.toString().contains(f.esignet.exchangeAccessToken))
    }

    @Test
    fun `a DPoP nonce challenge is answered once with a new proof from Hub`() {
        f.shareHubSignIn()
        f.esignet.nonceChallenges = 1
        f.authenticator().silentSignIn()
        assertEquals(2, f.esignet.exchangeProofs.size)
        assertEquals("server-nonce-1", verifiedJws(f.esignet.exchangeProofs[1], f.hub.key.publicJwk).payload.toJSONObject()["nonce"])
    }

    @Test
    fun `every OAuth error of the exchange reaches the caller and stores nothing`() {
        f.shareHubSignIn()
        val cases = listOf(
            400 to "invalid_request", 400 to "invalid_grant", 400 to "invalid_target", 400 to "unauthorized_client",
            400 to "invalid_scope", 400 to "invalid_dpop_proof", 401 to "invalid_client", 400 to "interaction_required",
            500 to "server_error", 503 to "temporarily_unavailable",
        )
        for ((status, code) in cases) {
            f.esignet.exchangeError = json("""{"error":"$code","error_description":"refused by the test"}""", status)
            val e = assertThrows(OAuthErrorException::class.java) { f.authenticator().silentSignIn() }
            assertEquals(code, e.code)
            assertEquals(status, e.httpStatus)
            assertEquals(code == "interaction_required", e.requiresInteraction)
            assertNull("$code stored a session", f.authenticator().currentSession())
        }
        assertEquals(OAuthError.INVALID_GRANT, OAuthError.fromCode("invalid_grant"))
    }

    @Test
    fun `an HTTP error without an OAuth body and a dead server are told apart`() {
        f.shareHubSignIn()
        f.esignet.exchangeError = okhttp3.mockwebserver.MockResponse().setResponseCode(502).setBody("<html>bad gateway</html>")
        assertEquals(502, assertThrows(HttpStatusException::class.java) { f.authenticator().silentSignIn() }.httpStatus)

        val auth = f.authenticator()
        f.esignet.stop()
        assertThrows(NetworkException::class.java) { auth.silentSignIn() }
        assertNull(auth.currentSession())
    }

    @Test
    fun `an id_token for another client or another login session is refused`() {
        f.shareHubSignIn()
        f.esignet.exchangeAudience = "someone-else"
        val aud = assertThrows(IdTokenValidationException::class.java) { f.authenticator().silentSignIn() }
        assertEquals(IdTokenFailure.AUDIENCE, aud.reason)

        f.esignet.exchangeAudience = null
        f.esignet.exchangeSid = "sid-other"
        val sid = assertThrows(CardException::class.java) { f.authenticator().silentSignIn() }
        assertEquals(CardException.Reason.SESSION_MISMATCH, sid.reason)

        f.esignet.exchangeSid = null
        assertEquals(CardException.Reason.SESSION_MISMATCH, assertThrows(CardException::class.java) { f.authenticator().silentSignIn() }.reason)
        assertNull(f.authenticator().currentSession())
    }

    @Test
    fun `a Bearer token from the exchange is refused`() {
        f.shareHubSignIn()
        f.esignet.exchangeTokenType = "Bearer"
        assertThrows(InvalidResponseException::class.java) { f.authenticator().silentSignIn() }
        assertNull(f.authenticator().currentSession())
    }

    @Test
    fun `Hub not installed, sharing off and Hub's refusals are HubExceptions`() {
        assertEquals(HubException.Reason.NOT_SIGNED_IN, assertThrows(HubException::class.java) { f.authenticator().silentSignIn() }.reason)

        f.shareHubSignIn()
        f.hub.proofRefusal = FamilySsoContract.exceptionFor(FamilySsoContract.ERROR_HTU_NOT_ALLOWED, "no")
        assertEquals(HubException.Reason.HTU_NOT_ALLOWED, assertThrows(HubException::class.java) { f.authenticator().silentSignIn() }.reason)

        f.hub.refusal = FamilySsoContract.exceptionFor(FamilySsoContract.ERROR_CALLER_NOT_ALLOWED, "no")
        assertEquals(HubException.Reason.CALLER_REFUSED, assertThrows(HubException::class.java) { f.authenticator().silentSignIn() }.reason)

        shadowOf(app).declareComponentUnbindable(
            android.content.ComponentName(FamilySsoContract.SUPER_APP_PACKAGE, FamilySsoContract.SERVICE_CLASS),
        )
        assertEquals(HubException.Reason.HUB_MISSING, assertThrows(HubException::class.java) { f.authenticator().silentSignIn() }.reason)
        assertEquals(0, f.esignet.calls["/oauth2/token"] ?: 0)
    }

    @Test
    fun `Hub signed in to another server is refused before anything is sent`() {
        f.hub.bundle = f.esignet.sharedBundle(issuer = "https://elsewhere.example")
        assertEquals(CardException.Reason.OTHER_ISSUER, assertThrows(CardException::class.java) { f.authenticator().silentSignIn() }.reason)
        assertEquals(0, f.esignet.calls["/oauth2/token"] ?: 0)
    }

    @Test
    fun `interaction_required names the missing scope and the consent login asks with prompt=consent`() {
        f.shareHubSignIn()
        val auth = f.authenticator()
        f.esignet.exchangeError = json(
            """{"error":"interaction_required","error_description":"The requested scope exceeds the scope granted to the first app; user consent is needed for: phone"}""",
            400,
        )
        val wanted = setOf("openid", "profile", "phone")
        val e = assertThrows(OAuthErrorException::class.java) { auth.silentSignIn(wanted) }
        assertEquals(setOf("phone"), CardAuthenticator.missingScopes(e, wanted, emptySet()))

        val url = auth.startConsentLogin(wanted)
        assertTrue(url.startsWith(f.esignet.server.url("/authorize").toString()))
        assertEquals("consent", f.esignet.parForm["prompt"])
        assertEquals("openid profile phone", f.esignet.parForm["scope"])
        assertEquals(f.redirectUri, f.esignet.parForm["redirect_uri"])
        assertEquals(CardConfig.OTP_ACR, f.esignet.parForm["acr_values"])
        verifiedJws(f.esignet.parProofs.single(), f.ownDpop.publicJwk) // Health Card's own key now
        assertTrue(auth.hasPendingConsent())

        val session = auth.completeConsentLogin(f.esignet.redirect(f.redirectUri))
        assertFalse(auth.hasPendingConsent())
        assertEquals(Binding.OWN, session.binding)
        assertEquals("+919876543210", session.phone)
        assertEquals(setOf("openid", "profile", "phone"), session.scopes)
        verifiedJws(f.esignet.codeProofs.single(), f.ownDpop.publicJwk)
        verifiedJws(f.esignet.userinfoProofs.single(), f.ownDpop.publicJwk)
        assertEquals(session, auth.currentSession())
    }

    @Test
    fun `missing scopes fall back to what was asked and not held when the description has no list`() {
        val e = OAuthErrorException.of("consent_required", null, 400)
        assertEquals(setOf("phone"), CardAuthenticator.missingScopes(e, setOf("openid", "profile", "phone"), setOf("openid", "profile")))
    }

    @Test
    fun `refusing on eSignet's page changes nothing`() {
        f.shareHubSignIn()
        val auth = f.authenticator()
        val before = auth.silentSignIn()
        auth.startConsentLogin(setOf("openid", "profile", "phone"))
        val e = assertThrows(OAuthErrorException::class.java) {
            auth.completeConsentLogin(f.esignet.redirect(f.redirectUri, extra = "&error=access_denied"))
        }
        assertEquals(OAuthError.ACCESS_DENIED, e.error)
        assertEquals(0, e.httpStatus)
        assertEquals(before, auth.currentSession())
        assertFalse(auth.hasPendingConsent())
        assertTrue("no code was exchanged", f.esignet.codeForm.isEmpty())
    }

    @Test
    fun `a redirect with another state keeps the consent login pending`() {
        val auth = f.authenticator()
        auth.startConsentLogin(setOf("openid", "phone"))
        val e = assertThrows(CardException::class.java) { auth.completeConsentLogin(f.esignet.redirect(f.redirectUri, state = "forged")) }
        assertEquals(CardException.Reason.STATE_MISMATCH, e.reason)
        assertTrue(auth.hasPendingConsent())
        assertEquals(
            CardException.Reason.NOT_OUR_REDIRECT,
            assertThrows(CardException::class.java) { auth.completeConsentLogin("https://evil.example/cb?code=x") }.reason,
        )
        auth.cancelConsent()
        assertEquals(
            CardException.Reason.NO_PENDING_LOGIN,
            assertThrows(CardException::class.java) { auth.completeConsentLogin(f.esignet.redirect(f.redirectUri, state = "x")) }.reason,
        )
    }

    @Test
    fun `sign out forgets the tokens and remembers that the user chose it`() {
        f.shareHubSignIn()
        val auth = f.authenticator()
        auth.silentSignIn()
        auth.signOut(byUser = true)
        assertNull(auth.currentSession())
        assertTrue(auth.signedOutByUser())
        auth.allowAutomaticSignIn()
        assertFalse(auth.signedOutByUser())
        assertNotNull(auth.silentSignIn())
        auth.signOut(byUser = false)
        assertFalse(auth.signedOutByUser())
    }

    @Test
    fun `stored records round-trip and an unreadable blob counts as signed out`() {
        f.shareHubSignIn()
        val session = f.authenticator().silentSignIn()
        assertEquals(session, CardSession.fromJson(session.toJson()))
        f.store.write(CardAuthenticator.SESSION, "not json".toByteArray())
        assertNull(f.authenticator().currentSession())
        assertFalse(f.store.contains(CardAuthenticator.SESSION))
        assertEquals(f.esignet.hubSid, CardAuthenticator.sessionIdOf(f.esignet.hubIdToken()))
        assertNull(CardAuthenticator.sessionIdOf("not a jwt"))
    }

    @Test
    fun `config refuses device_sso and a redirect URI without a host`() {
        assertThrows(IllegalArgumentException::class.java) { CardConfig("http://i", "c", "org.x://oauth/cb", scopes = setOf("openid", "device_sso")) }
        assertThrows(IllegalArgumentException::class.java) { CardConfig("http://i", "c", "org.x:/cb") }
        assertTrue(f.config.isRedirect("${f.redirectUri}?code=1"))
        assertFalse(f.config.isRedirect("org.badmosip.nativesso.superapp://oauth/callback"))
    }
}
