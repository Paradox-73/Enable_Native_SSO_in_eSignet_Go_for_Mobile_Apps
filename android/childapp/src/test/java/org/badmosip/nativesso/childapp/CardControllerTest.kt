// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import android.app.Application
import android.content.ComponentName
import androidx.test.core.app.ApplicationProvider
import org.badmosip.nativesso.childapp.auth.Binding
import org.badmosip.nativesso.childapp.auth.CardAuthenticator
import org.badmosip.nativesso.contract.FamilySsoContract
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

/**
 * What Health Card shows, step by step: silent sign-in, "Sign in to Hub first", every server error,
 * the re-consent screen accepted and declined, and Hub signing out or changing its login.
 * Background work runs inline, so each call returns with the next screen in place.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class CardControllerTest {
    private val app: Application = ApplicationProvider.getApplicationContext()
    private lateinit var f: CardFixture
    private lateinit var auth: CardAuthenticator
    private lateinit var controller: CardController
    private var changes = 0

    @Before
    fun setUp() {
        f = CardFixture(app)
        auth = f.authenticator()
        controller = newController()
    }

    @After
    fun tearDown() = f.stop()

    private fun newController() = CardController(
        authProvider = { auth },
        hubProvider = { f.link },
        background = { it.run() },
        main = { it.run() },
    ).also { it.onChange = { changes++ } }

    private val screen get() = controller.screen

    private fun home(): CardScreen.Home = screen as? CardScreen.Home ?: throw AssertionError("expected Home, got $screen")

    private fun failed(): CardError = when (val s = screen) {
        is CardScreen.Failed -> s.error
        else -> throw AssertionError("expected Failed, got $s")
    }

    private fun interactionRequired() {
        f.esignet.exchangeError = json(
            """{"error":"interaction_required","error_description":"The requested scope exceeds the scope granted to the first app; user consent is needed for: phone"}""",
            400,
        )
    }

    @Test
    fun `opening Health Card with Hub's shared sign-in signs in silently`() {
        f.shareHubSignIn()
        controller.start(null)
        val home = home()
        assertEquals(Binding.HUB, home.session.binding)
        assertEquals("Siddharth K Mansour", home.session.name)
        assertNull(home.notice)
        assertNull("no browser", controller.browserUrl)
        assertTrue(changes > 0)
    }

    @Test
    fun `opening Health Card asks Hub once and signs in over the same connection`() {
        f.shareHubSignIn()
        controller.start(null)
        assertEquals(Binding.HUB, home().session.binding)
        // version, bundle, then one proof for the token exchange and one for userinfo.
        assertEquals(2 + f.hub.proofRequests.size, f.hub.calls.get())
        assertEquals(2, f.hub.proofRequests.size)
    }

    @Test
    fun `a resume while the first check still runs does not ask Hub again`() {
        val queued = ArrayDeque<Runnable>()
        controller = CardController(
            authProvider = { auth },
            hubProvider = { f.link },
            background = { queued.addLast(it) },
            main = { it.run() },
        )
        f.shareHubSignIn()
        controller.start(null)
        controller.refresh() // onResume right after onCreate
        assertEquals(1, queued.size)
        queued.removeFirst().run()
        assertEquals(Binding.HUB, home().session.binding)
        val afterLaunch = f.hub.calls.get()

        controller.refresh() // a later resume asks again: version and bundle, no new sign-in
        queued.removeFirst().run()
        assertEquals(afterLaunch + 2, f.hub.calls.get())
    }

    @Test
    fun `sharing off shows Sign in to Hub first`() {
        controller.start(null)
        assertEquals(CardScreen.NeedsHub(HubException.Reason.NOT_SIGNED_IN), screen)

        f.shareHubSignIn()
        controller.refresh() // "Check again", or the next resume
        assertEquals(Binding.HUB, home().session.binding)
    }

    @Test
    fun `Hub not installed shows Sign in to Hub first with the reason`() {
        shadowOf(app).declareComponentUnbindable(ComponentName(FamilySsoContract.SUPER_APP_PACKAGE, FamilySsoContract.SERVICE_CLASS))
        controller.start(null)
        assertEquals(CardScreen.NeedsHub(HubException.Reason.HUB_MISSING), screen)
    }

    @Test
    fun `each server error becomes a screen the user can act on`() {
        f.shareHubSignIn()
        val cases = mapOf(
            json("""{"error":"invalid_grant","error_description":"The login session of the device secret has ended"}""", 400) to
                CardError(Problem.HUB_SESSION_ENDED, "invalid_grant"),
            json("""{"error":"invalid_request","error_description":"Invalid actor_token"}""", 400) to
                CardError(Problem.SERVER_REFUSED, "invalid_request"),
            json("""{"error":"unauthorized_client"}""", 400) to CardError(Problem.SERVER_REFUSED, "unauthorized_client"),
            json("""{"error":"invalid_dpop_proof"}""", 400) to CardError(Problem.SERVER_REFUSED, "invalid_dpop_proof"),
            json("""{"error":"invalid_scope"}""", 400) to CardError(Problem.SERVER_REFUSED, "invalid_scope"),
            json("""{"error":"server_error","error_description":"Failed to generate token"}""", 500) to
                CardError(Problem.SERVER_REFUSED, "server_error"),
            okhttp3.mockwebserver.MockResponse().setResponseCode(502) to CardError(Problem.SERVER_UNAVAILABLE, "502"),
        )
        for ((answer, expected) in cases) {
            f.esignet.exchangeError = answer
            controller.signInWithHub()
            assertEquals(expected, failed())
        }

        f.esignet.exchangeError = null
        f.esignet.exchangeAudience = "someone-else"
        controller.signInWithHub()
        assertEquals(CardError(Problem.SECURITY_CHECK, "AUDIENCE"), failed())

        f.esignet.exchangeAudience = null
        f.esignet.exchangeSid = "sid-other"
        controller.signInWithHub()
        assertEquals(CardError(Problem.SECURITY_CHECK, "SESSION_MISMATCH"), failed())

        f.esignet.stop()
        controller.signInWithHub()
        assertEquals(CardError(Problem.NETWORK), failed())
        assertNull(auth.currentSession())
    }

    @Test
    fun `Hub refusing or timing out is not mistaken for signed out`() {
        f.shareHubSignIn()
        f.hub.refusal = FamilySsoContract.exceptionFor(FamilySsoContract.ERROR_RATE_LIMITED, "busy")
        controller.start(null)
        assertEquals(CardScreen.NeedsHub(HubException.Reason.RATE_LIMITED), screen)

        f.hub.refusal = null
        controller.refresh()
        val session = home().session
        f.hub.refusal = FamilySsoContract.exceptionFor(FamilySsoContract.ERROR_RATE_LIMITED, "busy")
        controller.refresh()
        assertEquals("a busy Hub keeps Health Card signed in", session, home().session)
    }

    @Test
    fun `re-consent accepted runs Health Card's own login and shows the phone number`() {
        f.shareHubSignIn()
        controller.start(null)
        assertNull(home().session.phone)

        interactionRequired()
        controller.addPhone()
        val consent = screen as CardScreen.Consent
        assertEquals(setOf("phone"), consent.missing)
        assertEquals(setOf("openid", "profile", "phone"), consent.scope)
        assertEquals("openid profile phone", f.esignet.exchangeForm["scope"])

        controller.acceptConsent()
        assertEquals(CardScreen.Working(inBrowser = true), screen)
        val url = controller.browserUrl
        assertNotNull(url)
        assertTrue(url!!.contains("request_uri="))
        assertEquals("consent", f.esignet.parForm["prompt"])
        controller.browserOpened()
        assertNull(controller.browserUrl)

        // The resume after the browser must not disturb the login in progress.
        controller.refresh()
        assertEquals(CardScreen.Working(inBrowser = true), screen)

        controller.onRedirect(f.esignet.redirect(f.redirectUri))
        val home = home()
        assertEquals(Binding.OWN, home.session.binding)
        assertEquals("+919876543210", home.session.phone)
    }

    @Test
    fun `re-consent declined keeps the earlier sign-in and says nothing changed`() {
        f.shareHubSignIn()
        controller.start(null)
        val before = home().session

        interactionRequired()
        controller.addPhone()
        assertTrue(screen is CardScreen.Consent)
        controller.declineConsent()
        assertEquals(CardScreen.Home(before, CardError(Problem.CONSENT_DECLINED)), screen)
        assertEquals(0, f.esignet.calls["/oauth2/par"] ?: 0)
        assertEquals(before, auth.currentSession())
    }

    @Test
    fun `declining on eSignet's page or cancelling the browser keeps the earlier sign-in`() {
        f.shareHubSignIn()
        controller.start(null)
        val before = home().session
        interactionRequired()

        controller.addPhone()
        controller.acceptConsent()
        controller.onRedirect(f.esignet.redirect(f.redirectUri, extra = "&error=access_denied&error_description=user+said+no"))
        assertEquals(CardScreen.Home(before, CardError(Problem.CONSENT_DECLINED)), screen)

        controller.addPhone()
        controller.acceptConsent()
        controller.cancelBrowser()
        assertEquals(CardScreen.Home(before, CardError(Problem.CONSENT_DECLINED)), screen)
        assertEquals(false, auth.hasPendingConsent())
    }

    @Test
    fun `re-consent declined with no earlier sign-in offers to try again`() {
        f.shareHubSignIn()
        interactionRequired()
        controller.start(null)
        assertEquals(setOf("phone"), (screen as CardScreen.Consent).missing)
        controller.declineConsent()
        assertEquals(CardError(Problem.CONSENT_DECLINED), failed())
    }

    @Test
    fun `when Hub signs out of all apps Health Card clears its tokens on the next resume`() {
        f.shareHubSignIn()
        controller.start(null)
        home()

        f.hub.bundle = null // Hub: "Sign out of all apps"
        controller.refresh()
        assertEquals(CardScreen.NeedsHub(HubException.Reason.NOT_SIGNED_IN), screen)
        assertNull(auth.currentSession())
        assertTrue(f.store.blobs.keys.none { it == CardAuthenticator.SESSION })
    }

    @Test
    fun `a session from the consent login is also cleared when Hub signs out`() {
        f.shareHubSignIn()
        controller.start(null)
        interactionRequired()
        controller.addPhone()
        controller.acceptConsent()
        controller.onRedirect(f.esignet.redirect(f.redirectUri))
        assertEquals(Binding.OWN, home().session.binding)

        f.hub.bundle = null
        controller.refresh()
        assertEquals(CardScreen.NeedsHub(HubException.Reason.NOT_SIGNED_IN), screen)
        assertNull(auth.currentSession())
    }

    @Test
    fun `when Hub's login changes Health Card signs in again silently`() {
        f.shareHubSignIn()
        controller.start(null)
        assertEquals(1, f.esignet.exchangeProofs.size)

        f.esignet.exchangeSid = "sid-hub-2"
        f.shareHubSignIn(sid = "sid-hub-2")
        controller.refresh()
        assertEquals("sid-hub-2", home().session.hubSessionId)
        assertEquals(2, f.esignet.exchangeProofs.size)

        controller.refresh() // same login: nothing sent
        assertEquals(2, f.esignet.exchangeProofs.size)
    }

    @Test
    fun `signing out of Health Card stays signed out until the user continues with Hub`() {
        f.shareHubSignIn()
        controller.start(null)
        controller.signOut()
        assertEquals(CardScreen.SignedOut(), screen)
        assertNull(auth.currentSession())

        controller.refresh()
        assertEquals("no automatic sign-in after the user signed out", CardScreen.SignedOut(), screen)
        // A new controller (the app restarted) remembers it too.
        controller = newController()
        controller.start(null)
        assertEquals(CardScreen.SignedOut(), screen)

        controller.signInWithHub()
        assertEquals(Binding.HUB, home().session.binding)
    }
}
