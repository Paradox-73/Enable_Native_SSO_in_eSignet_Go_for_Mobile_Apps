// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp

import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertHasClickAction
import androidx.compose.ui.test.assertHeightIsAtLeast
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertTextContains
import androidx.compose.ui.test.getBoundsInRoot
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isHeading
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import org.badmosip.nativesso.superapp.auth.TokenBundle
import org.badmosip.nativesso.superapp.sso.FamilyAccess
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.time.Instant

/**
 * Accessibility and wording of every Hub screen, at 200 % font size on a small phone, the same
 * checks as Health Card's CardScreensTest: one heading, labelled values read as one item, spoken
 * status changes, described progress, 48 dp touch targets, reading order, and no package name or
 * developer detail on screen unless the user opens "Technical details".
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], qualifiers = "w360dp-h640dp")
class HubScreensTest {
    @get:Rule
    val rule = createComposeRule()

    private val clicks = mutableListOf<String>()
    private val actions = object : HubActions {
        override fun setShare(share: Boolean) {
            clicks += "share=$share"
        }
        override fun signIn() {
            clicks += "signIn"
        }
        override fun cancel() {
            clicks += "cancel"
        }
        override fun signOut() {
            clicks += "signOut"
        }
    }
    private val details = HubDetails("http://localhost:38088", "ps03-hub-0123456789ab")

    private val session = TokenBundle(
        issuer = "http://localhost:38088", clientId = "ps03-hub-0123456789ab", subject = "psut-1", sessionId = "sid-1",
        accessToken = "at", tokenType = "DPoP", accessTokenExpiresAt = null, idToken = "a.b.c",
        idTokenExpiresAt = Instant.ofEpochSecond(1_790_000_120), refreshToken = null, deviceSecret = "ds",
        scope = "openid profile device_sso", dpopKeyThumbprint = "jkt", displayName = "Siddharth K Mansour",
        signedInAt = Instant.ofEpochSecond(1_790_000_000), tokenEndpoint = null, userinfoEndpoint = null,
    )

    private val polite = SemanticsMatcher.expectValue(SemanticsProperties.LiveRegion, LiveRegionMode.Polite)

    private fun show(screen: HubScreen, share: Boolean = false, dark: Boolean = false, labels: Map<String, String> = emptyMap()) {
        rule.setContent {
            val density = LocalDensity.current
            CompositionLocalProvider(LocalDensity provides Density(density.density, fontScale = 2f)) {
                HubTheme(dark = dark) {
                    HubContent(screen, share, details, actions) { labels[it] }
                }
            }
        }
        everyControlIsLabelledAndLargeEnough()
    }

    /** Every control on the screen has words for TalkBack and a touch target of at least 48 x 48 dp. */
    private fun everyControlIsLabelledAndLargeEnough() {
        val controls = rule.onAllNodes(hasClickAction()).fetchSemanticsNodes()
        assertTrue("no control on the screen", controls.isNotEmpty())
        val minPx = with(rule.density) { 48.dp.roundToPx() }
        controls.forEach { node ->
            val c = node.config
            val words = c.getOrNull(SemanticsProperties.Text).orEmpty().joinToString(" ") +
                c.getOrNull(SemanticsProperties.ContentDescription).orEmpty().joinToString(" ")
            assertTrue("control without text or description: $c", words.isNotBlank())
            assertTrue("control \"$words\" is ${node.size} px, under 48 x 48 dp ($minPx px)", node.size.width >= minPx && node.size.height >= minPx)
        }
    }

    /** The control exists, can be reached by scrolling, is at least 48 dp high, and runs [action]. */
    private fun button(tag: String, action: String) {
        rule.onNodeWithTag(tag).performScrollTo().assertIsDisplayed().assertHasClickAction().assertHeightIsAtLeast(48.dp)
        rule.onNodeWithTag(tag).performClick()
        assertEquals(action, clicks.last())
    }

    private fun top(tag: String): Float = rule.onNodeWithTag(tag).getBoundsInRoot().top.value

    @Test
    fun `welcome explains Hub, the sharing switch is one item, and sign in works`() {
        show(HubScreen.Welcome())
        rule.onAllNodes(isHeading()).assertCountEquals(1)
        rule.onNodeWithTag("app_title").assert(isHeading()).assertTextContains("Hub")
        rule.onNodeWithTag("tagline").assertTextContains("Sign in once with eSignet.", substring = true)
        rule.onNodeWithTag("share_switch").assert(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Switch))
            .assertTextContains("Share this sign-in with my other apps")
        assertTrue(top("tagline") < top("share_switch"))
        assertTrue(top("share_switch") < top("sign_in_button"))
        assertTrue(top("sign_in_button") < top("details_button"))
        button("share_switch", "share=true")
        button("sign_in_button", "signIn")
    }

    @Test
    fun `an error on the welcome screen is spoken, in dark mode too`() {
        show(HubScreen.Welcome(error = "Cannot reach the eSignet server. Check the connection and try again."), dark = true)
        rule.onNodeWithTag("error_text").assert(polite).assertTextContains("Cannot reach the eSignet server", substring = true)
    }

    @Test
    fun `after sign out Hub says every app was signed out`() {
        show(HubScreen.Welcome(signedOut = true))
        rule.onNodeWithTag("signed_out_text").assert(polite)
            .assertTextContains("You are signed out of Hub and of every app that used this sign-in.")
    }

    @Test
    fun `home lists family apps by their names, never by package`() {
        val apps = listOf(FamilyAccess("org.badmosip.nativesso.childapp", Instant.ofEpochSecond(1_790_000_060)))
        show(HubScreen.Home(session, apps), labels = mapOf("org.badmosip.nativesso.childapp" to "Health Card"))
        rule.onNodeWithTag("home_name").assertTextContains("Siddharth K Mansour")
        rule.onNodeWithTag("sharing_status").assertTextContains("Sign-in shared with my other apps").assertTextContains("Yes")
        rule.onNodeWithTag("family_apps").assertTextContains("Health Card, last used", substring = true)
        rule.onAllNodes(hasText("org.badmosip", substring = true)).assertCountEquals(0)
        button("sign_out_button", "signOut")
    }

    @Test
    fun `an app Hub cannot see is named generically`() {
        val apps = listOf(FamilyAccess("org.example.other", Instant.ofEpochSecond(1_790_000_060)))
        show(HubScreen.Home(session, apps))
        rule.onNodeWithTag("family_apps").assertTextContains("An app from the same publisher", substring = true)
        rule.onAllNodes(hasText("org.example", substring = true)).assertCountEquals(0)
    }

    @Test
    fun `technical details stay folded until asked for`() {
        show(HubScreen.Home(session))
        rule.onAllNodes(hasText("localhost", substring = true)).assertCountEquals(0)
        rule.onAllNodesWithTag("details_issuer").assertCountEquals(0)
        rule.onNodeWithTag("details_button").performScrollTo().assertHeightIsAtLeast(48.dp).performClick()
        rule.onNodeWithTag("details_issuer").performScrollTo().assertTextContains("http://localhost:38088")
        rule.onNodeWithTag("details_client").assertTextContains("ps03-hub-0123456789ab")
    }

    @Test
    fun `progress is described, the status is spoken, and the browser wait can be cancelled`() {
        show(HubScreen.Working(inBrowser = true))
        rule.onNode(hasContentDescription("Finish signing in on eSignet's page in the browser.")).assertExists()
        rule.onNodeWithTag("working_text").assert(polite)
        button("cancel_button", "cancel")
    }
}
