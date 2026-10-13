// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.uiautomator.By
import androidx.test.uiautomator.Until
import org.badmosip.nativesso.childapp.auth.Binding
import org.badmosip.nativesso.childapp.auth.CardConfig
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import java.util.regex.Pattern

/**
 * Bonus B1, consent-only, on a real device: Health Card asks for the permission `health_card_share`,
 * which Hub's login never granted and which needs no user data. eSignet answers the token exchange
 * with `interaction_required` and a `consent_uri`; Health Card's consent screen says no ID or OTP is
 * needed; eSignet's consent page opens in the browser with no login form; "Allow" sends the browser
 * back; Health Card repeats the token exchange with Hub's sign-in and holds the new scope. Its tokens
 * stay bound to its own DPoP key, as in every exchange.
 */
@RunWith(AndroidJUnit4::class)
class ChildAppConsentOnlyTest {
    private val ui = CardUi()

    @Before
    fun signedInSilently() {
        ui.requireHubSignedInWithSharing()
        ui.resetHealthCard()
        ui.launchHealthCard()
        ui.waitFor("home_name")
    }

    /**
     * Keeps the evidence, then waits out Hub's per-app rate limit (30 calls in 60 s): this test makes
     * about ten Hub calls in a few seconds, and `ChildAppReconsentTest`, which runs next, was refused
     * with "too many calls" on the emulator run of 2026-10-06 without the pause.
     */
    @After
    fun keepEvidenceAndLetHubsRateWindowPass() {
        ui.keepEvidence()
        Thread.sleep(HUB_RATE_WINDOW_MILLIS)
    }

    @Test
    fun permissionIsApprovedOnEsignetsPageWithoutLogin() {
        refuseOnEsignetsPage()
        assertEquals(ui.context.getString(R.string.sharing_not_allowed), sharingValue())
        ui.waitFor("allow_sharing_button").click()
        ui.step("allow card sharing tapped")

        ui.waitFor("consent_title")
        assertEquals(ui.context.getString(R.string.consent_no_login), ui.waitFor("consent_how").text)
        ui.screenshot("health-card-consent-only-screen")
        ui.waitFor("consent_continue_button").click()
        ui.step("continue to eSignet tapped")

        // eSignet's own consent page, with no UIN or OTP field.
        val title = ui.device.wait(Until.findObject(By.pkg(CardUi.CHROME).text(PAGE_TITLE)), 60_000)
        assertNotNull("eSignet's consent page did not open", title)
        assertTrue(
            "the consent page asks for an ID or OTP",
            ui.device.findObjects(By.pkg(CardUi.CHROME).clazz("android.widget.EditText")).isEmpty(),
        )
        ui.device.wait(Until.findObject(By.pkg(CardUi.CHROME).text(NO_LOGIN)), 10_000)
        ui.screenshot("esignet-consent-only-page")
        val allow = ui.device.findObject(By.pkg(CardUi.CHROME).clazz("android.widget.Button").text(ALLOW))
            ?: ui.device.findObject(By.pkg(CardUi.CHROME).text(ALLOW))
        assertNotNull("no Allow button on eSignet's consent page", allow)
        allow!!.click()
        ui.step("Allow tapped on eSignet's consent page")

        val deadline = System.currentTimeMillis() + 60_000
        while (System.currentTimeMillis() < deadline) {
            val session = ui.services.authenticator.currentSession()
            if (session != null && CardConfig.SHARE_SCOPE in session.scopes && ui.device.hasObject(By.res("home_name"))) break
            ui.device.findObject(By.res("error_text"))?.let { throw AssertionError("Health Card shows an error: ${it.text}") }
            Thread.sleep(500)
        }
        ui.waitFor("home_name")
        val session = ui.services.authenticator.currentSession()
        assertNotNull(session)
        assertTrue("the sign-in lacks ${CardConfig.SHARE_SCOPE}: ${session!!.scope}", CardConfig.SHARE_SCOPE in session.scopes)
        assertEquals("still Hub's sign-in (a token exchange), not a login of Health Card's own", Binding.HUB, session.binding)
        assertEquals(ui.context.getString(R.string.sharing_allowed), sharingValue())
        ui.screenshot("health-card-after-consent-only")
        ui.step("consent-only re-consent verified (scope ${session.scope})")
    }

    /** Refusing on eSignet's page: the browser returns, Health Card says nothing changed and keeps Hub's sign-in. Runs first: after "Allow" the grant is kept. */
    private fun refuseOnEsignetsPage() {
        ui.waitFor("allow_sharing_button").click()
        ui.waitFor("consent_continue_button").click()
        val title = ui.device.wait(Until.findObject(By.pkg(CardUi.CHROME).text(PAGE_TITLE)), 60_000)
        assertNotNull("eSignet's consent page did not open", title)
        val deny = ui.device.findObject(By.pkg(CardUi.CHROME).clazz("android.widget.Button").text(NOT_NOW))
            ?: ui.device.findObject(By.pkg(CardUi.CHROME).text(NOT_NOW))
        assertNotNull("no Not now button on eSignet's consent page", deny)
        deny!!.click()
        ui.step("Not now tapped on eSignet's consent page")

        assertEquals(ui.context.getString(R.string.error_consent_declined), ui.waitFor("notice_text", 60_000).text)
        val session = ui.services.authenticator.currentSession()
        assertNotNull(session)
        assertTrue("the refused scope was granted", CardConfig.SHARE_SCOPE !in session!!.scopes)
        assertEquals(Binding.HUB, session.binding)
        assertEquals(ui.context.getString(R.string.sharing_not_allowed), sharingValue())
        ui.screenshot("health-card-consent-only-declined")
    }

    /** The value of the "Card sharing with clinics" item: the last text of the item and its children. */
    private fun sharingValue(): String? {
        val texts = mutableListOf<String>()
        fun collect(node: androidx.test.uiautomator.UiObject2) {
            node.text?.let { texts += it.split(", ") }
            node.children.forEach(::collect)
        }
        collect(ui.waitFor("card_sharing"))
        return texts.lastOrNull()
    }

    private companion object {
        const val HUB_RATE_WINDOW_MILLIS = 61_000L
        val PAGE_TITLE: Pattern = Pattern.compile("(?i)\\s*allow more access\\?\\s*")
        val NO_LOGIN: Pattern = Pattern.compile("(?i).*no new login.*")
        val NOT_NOW: Pattern = Pattern.compile("(?i)\\s*not now\\s*")
        val ALLOW: Pattern = Pattern.compile("(?i)\\s*allow\\s*")
    }
}
