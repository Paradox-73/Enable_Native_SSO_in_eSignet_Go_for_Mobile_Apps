// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.badmosip.nativesso.contract.FamilySsoContract
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Health Card on a phone without Hub: it shows "Sign in to Hub first" with the reason "Not
 * installed" and keeps no tokens. The test uninstalls Hub, so `run-ui-test.sh all` runs it after
 * every other test and installs Hub again at the end.
 */
@RunWith(AndroidJUnit4::class)
class ChildAppHubMissingTest {
    private val ui = CardUi()

    @After
    fun keepEvidence() = ui.keepEvidence()

    @Test
    fun withoutHubHealthCardAsksToInstallIt() {
        ui.instrumentation.uiAutomation
            .executeShellCommand("pm uninstall ${FamilySsoContract.SUPER_APP_PACKAGE}")
            .close()
        ui.device.waitForIdle(2_000)
        ui.resetHealthCard()
        ui.launchHealthCard()
        ui.waitFor("needs_hub_title")
        assertEquals(ui.context.getString(R.string.super_app_missing), ui.waitFor("hub_status").text)
        ui.screenshot("health-card-hub-missing")
        assertNull("Health Card holds tokens without Hub", ui.services.authenticator.currentSession())
        ui.step("Hub missing shown")
    }
}
