// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertHasClickAction
import androidx.compose.ui.test.assertHeightIsAtLeast
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertTextContains
import androidx.compose.ui.test.getBoundsInRoot
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.isHeading
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import org.badmosip.nativesso.childapp.auth.Binding
import org.badmosip.nativesso.childapp.auth.CardSession
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.time.Instant

/**
 * Accessibility of every Health Card screen, at 200 % font size on a small phone: headings, one
 * TalkBack item per labelled value, spoken status changes, described progress, 48 dp touch targets,
 * reading order, and that every button reaches its action.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], qualifiers = "w360dp-h640dp")
class CardScreensTest {
    @get:Rule
    val rule = createComposeRule()

    private val about = AboutInfo("http://localhost:38088", "AB:CD:EF", SuperAppState.SAME_FAMILY)
    private val clicks = mutableListOf<String>()
    private val actions = object : CardActions {
        override fun checkAgain() { clicks += "checkAgain" }
        override fun openHub() { clicks += "openHub" }
        override fun signInWithHub() { clicks += "signInWithHub" }
        override fun addPhone() { clicks += "addPhone" }
        override fun signOut() { clicks += "signOut" }
        override fun acceptConsent() { clicks += "acceptConsent" }
        override fun declineConsent() { clicks += "declineConsent" }
        override fun cancelBrowser() { clicks += "cancelBrowser" }
    }

    private val session = CardSession(
        issuer = "http://localhost:38088", clientId = "ps03-card-1", subject = "psut-1", sessionId = "sid-1",
        accessToken = "at", accessTokenExpiresAt = null, idToken = "a.b.c", scope = "openid profile", binding = Binding.HUB,
        hubSessionId = "sid-1", name = "Siddharth K Mansour", email = "siddharth@example.com", phone = null,
        signedInAt = Instant.ofEpochSecond(1_790_000_000),
    )

    private val polite = SemanticsMatcher.expectValue(SemanticsProperties.LiveRegion, LiveRegionMode.Polite)

    private fun show(screen: CardScreen, superApp: SuperAppState = SuperAppState.SAME_FAMILY, dark: Boolean = false) {
        rule.setContent {
            val density = LocalDensity.current
            CompositionLocalProvider(LocalDensity provides Density(density.density, fontScale = 2f)) {
                CardTheme(dark = dark) {
                    CardContent(screen, AboutInfo(about.issuer, about.certificate, superApp), actions)
                }
            }
        }
    }

    /** The button exists, can be reached by scrolling, is at least 48 dp high, and runs [action]. */
    private fun button(tag: String, action: String) {
        rule.onNodeWithTag(tag).performScrollTo().assertIsDisplayed().assertHasClickAction().assertHeightIsAtLeast(48.dp)
        rule.onNodeWithTag(tag).performClick()
        assertEquals(action, clicks.last())
    }

    private fun top(tag: String): Float = rule.onNodeWithTag(tag).getBoundsInRoot().top.value

    @Test
    fun `home reads name, email, phone and method as single items and its buttons work`() {
        show(CardScreen.Home(session))
        rule.onNodeWithTag("home_name").assertTextContains("Siddharth K Mansour")
        rule.onNodeWithTag("card_email").assertTextContains("Email").assertTextContains("siddharth@example.com")
        rule.onNodeWithTag("card_phone").assertTextContains("Phone number").assertTextContains("Not shared with Health Card")
        rule.onNodeWithTag("sign_in_method").assertTextContains("With Hub's sign-in. No password or OTP was needed.")
        rule.onAllNodes(isHeading()).assertCountEquals(2) // app name and "About this app"
        button("add_phone_button", "addPhone")
        button("sign_out_button", "signOut")
    }

    @Test
    fun `home after the consent login has the phone and no add button`() {
        show(CardScreen.Home(session.copy(binding = Binding.OWN, phone = "+919876543210", scope = "openid profile phone")))
        rule.onNodeWithTag("card_phone").assertTextContains("+919876543210")
        rule.onAllNodesWithTag("add_phone_button").assertCountEquals(0)
    }

    @Test
    fun `a notice on home is spoken`() {
        show(CardScreen.Home(session, CardError(Problem.CONSENT_DECLINED)))
        rule.onNodeWithTag("notice_text").assert(polite).assertTextContains("You did not give the extra permission. Nothing changed.")
    }

    @Test
    fun `consent screen names the permission, says the OTP is asked again, and both choices work`() {
        show(CardScreen.Consent(setOf("phone"), setOf("openid", "profile", "phone")))
        rule.onNodeWithTag("consent_title").assert(isHeading())
        rule.onNodeWithTag("consent_text").assertTextContains(
            "Health Card asks to see: your phone number. Your Hub sign-in did not include this, so eSignet must ask you first.",
        )
        assertTrue(top("consent_title") < top("consent_text"))
        assertTrue(top("consent_text") < top("consent_continue_button"))
        assertTrue(top("consent_continue_button") < top("consent_decline_button"))
        button("consent_continue_button", "acceptConsent")
        button("consent_decline_button", "declineConsent")
    }

    @Test
    fun `sign in to Hub first reads title, reason, then actions, then details`() {
        show(CardScreen.NeedsHub(HubException.Reason.NOT_SIGNED_IN))
        rule.onNodeWithTag("needs_hub_title").assert(isHeading())
        rule.onNodeWithTag("hub_status").assert(polite)
            .assertTextContains("Hub has no sign-in it shares. Open Hub, turn on \"Use this sign-in in my other apps\" and sign in.")
        assertTrue(top("needs_hub_title") < top("hub_status"))
        assertTrue(top("hub_status") < top("open_hub_button"))
        assertTrue(top("open_hub_button") < top("check_hub_button"))
        assertTrue(top("check_hub_button") < top("about_title"))
        button("open_hub_button", "openHub")
        button("check_hub_button", "checkAgain")
    }

    @Test
    fun `without Hub installed there is no Open Hub button`() {
        show(CardScreen.NeedsHub(HubException.Reason.HUB_MISSING), superApp = SuperAppState.MISSING)
        rule.onAllNodesWithTag("open_hub_button").assertCountEquals(0)
        rule.onNodeWithTag("hub_status").assertTextContains("Not installed. Install Hub and sign in there first.")
    }

    @Test
    fun `progress is described and the status is spoken`() {
        show(CardScreen.Working(inBrowser = false))
        rule.onNode(hasContentDescription("Signing you in with Hub…")).assertExists()
        rule.onNodeWithTag("working_text").assert(polite)
    }

    @Test
    fun `while the browser is open the user can cancel`() {
        show(CardScreen.Working(inBrowser = true))
        rule.onNodeWithTag("working_text").assertTextContains("Finish on eSignet's page in the browser.")
        button("cancel_button", "cancelBrowser")
    }

    @Test
    fun `an error is spoken and offers try again and Open Hub, in dark mode too`() {
        show(CardScreen.Failed(CardError(Problem.HUB_SESSION_ENDED, "invalid_grant")), dark = true)
        rule.onNodeWithTag("error_text").assert(polite).assertTextContains("Your Hub sign-in has ended. Open Hub and sign in again.")
        button("try_again_button", "signInWithHub")
        button("open_hub_button", "openHub")
    }

    @Test
    fun `signed out offers to continue with Hub`() {
        show(CardScreen.SignedOut())
        rule.onNodeWithTag("signed_out_text").assertTextContains("You signed out of Health Card. Hub is still signed in.")
        button("continue_button", "signInWithHub")
    }
}
