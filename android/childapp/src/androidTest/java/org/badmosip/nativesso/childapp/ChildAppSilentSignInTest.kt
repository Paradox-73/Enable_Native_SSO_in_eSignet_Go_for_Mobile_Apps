// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.uiautomator.By
import androidx.test.uiautomator.Until
import org.badmosip.nativesso.childapp.auth.Binding
import org.badmosip.nativesso.childapp.auth.CardAuthenticator
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Health Card signs in with Hub's shared sign-in on a real device, against the local stack: it
 * opens on its home screen with the user's name and email (optional in Hub's login), no browser window ever appears, and the stored
 * tokens are Health Card's own (bound to Hub's key, same `sid` as Hub's login), encrypted on disk.
 */
@RunWith(AndroidJUnit4::class)
class ChildAppSilentSignInTest {
    private val ui = CardUi()

    @Before
    fun hubSharesItsSignIn() {
        ui.requireHubSignedInWithSharing()
        ui.resetHealthCard()
    }

    @After
    fun keepEvidence() = ui.keepEvidence()

    @Test
    fun opensSignedInWithoutABrowser() {
        val started = System.currentTimeMillis()
        val sawBrowser = ui.BrowserWatcher().use { watcher ->
            ui.launchHealthCard()
            val name = ui.waitFor("home_name")
            assertEquals(ui.expectedName, name.text)
            ui.waitFor("card_email")
            assertTrue("Health Card does not show the email", ui.device.wait(Until.hasObject(By.textContains(ui.expectedEmail)), 10_000))
            ui.step("home screen after ${System.currentTimeMillis() - started} ms")
            watcher.sawBrowser.get()
        }
        assertFalse("a browser window appeared during the silent sign-in", sawBrowser)
        ui.screenshot("health-card-silent-sign-in")

        val session = ui.services.authenticator.currentSession()
        assertNotNull("no stored session", session)
        assertEquals(Binding.HUB, session!!.binding)
        assertEquals("Health Card's id_token has another sid than Hub's", session.hubSessionId, session.sessionId)
        assertFalse("device_sso must never reach Health Card", session.scopes.contains("device_sso"))
        assertTrue(session.scopes.contains("openid"))
        assertEquals("email from Hub's login", ui.expectedEmail, session.email)

        val file = ui.services.store.fileOf(CardAuthenticator.SESSION)
        assertTrue("$file missing", file.isFile)
        val raw = String(file.readBytes(), Charsets.ISO_8859_1)
        assertFalse("id_token readable on disk", raw.contains(session.idToken))
        assertFalse("access token readable on disk", raw.contains(session.accessToken))
        ui.step("silent sign-in verified")
    }
}
