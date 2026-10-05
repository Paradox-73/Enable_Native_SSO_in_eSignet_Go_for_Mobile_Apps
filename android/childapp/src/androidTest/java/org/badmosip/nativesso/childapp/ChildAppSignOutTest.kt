// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import android.content.Intent
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.uiautomator.By
import androidx.test.uiautomator.Until
import org.badmosip.nativesso.contract.FamilySsoContract
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Sign-out on a real device: "Sign out of Health Card" ends only Health Card's sign-in; "Sign out of
 * all apps" in Hub ends Hub's sign-in (Hub also asks eSignet to end the device secret's session), and
 * Health Card then clears its own tokens and asks for Hub again. Runs last in
 * `run-ui-test.sh all`, because it leaves Hub signed out.
 */
@RunWith(AndroidJUnit4::class)
class ChildAppSignOutTest {
    private val ui = CardUi()

    @Before
    fun hubSharesItsSignIn() {
        ui.requireHubSignedInWithSharing()
        ui.resetHealthCard()
    }

    @After
    fun keepEvidence() = ui.keepEvidence()

    @Test
    fun signOutOfHealthCardThenOfAllApps() {
        ui.launchHealthCard()
        ui.waitFor("home_name")
        ui.waitFor("sign_out_button").click()
        assertEquals(ui.context.getString(R.string.signed_out_text), ui.waitFor("signed_out_text").text)
        ui.screenshot("health-card-signed-out")
        ui.step("Health Card signed out on its own; Hub still signed in")

        val hub = ui.context.packageManager.getLaunchIntentForPackage(FamilySsoContract.SUPER_APP_PACKAGE)
        assertNotNull("Hub is not installed", hub)
        ui.context.startActivity(hub!!.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        val signOut = ui.device.wait(Until.findObject(By.pkg(FamilySsoContract.SUPER_APP_PACKAGE).res("sign_out_button")), 30_000)
        assertNotNull("Hub's \"Sign out of all apps\" did not appear", signOut)
        signOut.click()
        val notice = ui.device.wait(Until.findObject(By.pkg(FamilySsoContract.SUPER_APP_PACKAGE).res("signed_out_text")), 30_000)
        assertNotNull("Hub did not confirm the sign-out", notice)
        ui.screenshot("hub-signed-out")
        ui.step("Hub signed out of all apps")

        ui.launchHealthCard()
        ui.waitFor("needs_hub_title")
        ui.screenshot("health-card-needs-hub")
        assertNull("Health Card kept its tokens after Hub signed out", ui.services.authenticator.currentSession())
        ui.step("Health Card followed Hub's sign-out")
    }
}
