// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.uiautomator.By
import org.badmosip.nativesso.childapp.auth.Binding
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Bonus B1 on a real device: Health Card asks for the phone number, which Hub's login never
 * granted. eSignet answers the token exchange with `interaction_required`; Health Card shows its
 * consent screen; "Continue to eSignet" runs Health Card's own PAR login with `prompt=consent`
 * (UIN, a new OTP, Allow); Health Card comes back signed in with its own tokens and the new scope.
 */
@RunWith(AndroidJUnit4::class)
class ChildAppReconsentTest {
    private val ui = CardUi()

    @Before
    fun signedInSilently() {
        ui.requireHubSignedInWithSharing()
        ui.resetHealthCard()
        ui.launchHealthCard()
        ui.waitFor("home_name")
    }

    @After
    fun keepEvidence() = ui.keepEvidence()

    @Test
    fun extraPermissionShowsConsentAndTheOwnLoginGrantsIt() {
        ui.waitFor("add_phone_button").click()
        ui.step("add phone tapped")

        val title = ui.waitFor("consent_title")
        assertEquals("Health Card needs your permission", title.text)
        val text = ui.waitFor("consent_text").text
        assertTrue("the consent text does not name the phone number: $text", text.contains("phone number"))
        ui.screenshot("health-card-consent-screen")

        ui.waitFor("consent_continue_button").click()
        ui.step("continue to eSignet tapped")
        ui.loginInBrowser {
            ui.device.hasObject(By.res("home_name")) &&
                ui.services.authenticator.currentSession()?.binding == Binding.OWN
        }

        ui.waitFor("home_name")
        ui.screenshot("health-card-after-consent")
        val session = ui.services.authenticator.currentSession()
        assertNotNull(session)
        assertEquals(Binding.OWN, session!!.binding)
        assertTrue("the new sign-in lacks the phone scope: ${session.scope}", session.scopes.contains("phone"))
        ui.step("re-consent verified (phone number shared: ${session.phone != null})")
    }
}
