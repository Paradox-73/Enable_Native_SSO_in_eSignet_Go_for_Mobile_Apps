// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp

import android.content.Intent
import android.util.Log
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.BySelector
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.UiObject2
import androidx.test.uiautomator.Until
import com.nimbusds.jwt.SignedJWT
import org.badmosip.nativesso.oidc.TokenHashes
import org.badmosip.nativesso.superapp.auth.HubAuthenticator
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File
import java.util.regex.Pattern

/**
 * Hub's real sign-in on a device: Hub opens eSignet's login pages in a Custom Tab, the test types
 * the seeded user's UIN and OTP into Chrome, ticks the captcha box when there is one, allows the
 * consent, and Hub comes back to its home screen with the user's name. Then it checks that an
 * encrypted token bundle with an id_token is on disk.
 *
 * Needs the local stack (`scripts/with-stack.sh`), `adb reverse` for ports 38088 and 38300, and
 * the client registered (`android/scripts/register-client.sh`); `android/scripts/run-ui-test.sh`
 * does all of it. Instrumentation arguments: `uin`, `otp`, `expectedName`.
 *
 * The sharing switch ("Share this sign-in with my other apps") is turned on, so the login asks for
 * `device_sso`: the stored bundle must hold a device secret, and the id_token must carry `sid` and a
 * `ds_hash` that matches it (Native SSO 1.0 section 3.4.1). Health Card's UI tests
 * (`ChildAppSilentSignInTest`, `ChildAppReconsentTest`) run after this test and use that sign-in.
 */
@RunWith(AndroidJUnit4::class)
class SuperAppSignInTest {
    private val instrumentation = InstrumentationRegistry.getInstrumentation()
    private val device = UiDevice.getInstance(instrumentation)
    private val context = instrumentation.targetContext
    private val args = InstrumentationRegistry.getArguments()
    private val uin = args.getString("uin") ?: "1774231323"
    private val otp = args.getString("otp") ?: "111111"
    private val expectedName = args.getString("expectedName") ?: "Siddharth K Mansour"
    private val app get() = context.applicationContext as HubApp
    private val evidence: File by lazy { File(context.getExternalFilesDir(null), "ui-test").apply { mkdirs() } }

    @Before
    fun signedOut() {
        app.services.authenticator.signOut()
    }

    @After
    fun keepEvidence() {
        // Kept for the run script to pull (pass or fail): the last screen and its view tree.
        runCatching { device.takeScreenshot(File(evidence, "last-screen.png")) }
        runCatching { device.dumpWindowHierarchy(File(evidence, "last-window.xml")) }
    }

    @Test
    fun signInWithSharingShowsTheNameAndStoresAnEncryptedDeviceSecret() {
        context.startActivity(
            Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK),
        )
        val signIn = device.wait(Until.findObject(By.res("sign_in_button")), 30_000)
        assertNotNull("Hub's welcome screen did not appear", signIn)
        val share = device.findObject(By.res("share_switch"))
        assertNotNull("the sharing switch is missing", share)
        if (!share.isChecked) share.click()
        assertTrue("the sharing switch did not turn on", device.wait(Until.hasObject(By.res("share_switch").checked(true)), 5_000))
        screenshot("hub-welcome-sharing-on")
        signIn.click()
        step("sign-in tapped")

        loginInBrowser()

        val name = device.wait(Until.findObject(By.res("home_name")), 30_000)
        assertNotNull("Hub's home screen did not appear", name)
        assertEquals(expectedName, name.text)
        step("home screen shows the name")
        screenshot("hub-home")

        // An encrypted token bundle with an id_token is on disk, in noBackupFilesDir.
        val session = app.services.authenticator.currentSession()
        assertNotNull("no stored session", session)
        assertEquals(3, session!!.idToken.split('.').size)
        val deviceSecret = session.deviceSecret
        assertNotNull("the switch was on, so a device secret must be stored", deviceSecret)
        val claims = SignedJWT.parse(session.idToken).jwtClaimsSet
        assertEquals("ds_hash does not match the device secret", TokenHashes.dsHash(deviceSecret!!), claims.getStringClaim("ds_hash"))
        assertNotNull("sid missing", claims.getStringClaim("sid"))
        assertEquals(claims.getStringClaim("sid"), session.sessionId)
        val file = app.services.store.fileOf(HubAuthenticator.SESSION)
        assertTrue("$file missing", file.isFile)
        assertTrue("store outside noBackupFilesDir", file.canonicalPath.startsWith(context.noBackupFilesDir.canonicalPath))
        val raw = String(file.readBytes(), Charsets.ISO_8859_1)
        assertFalse("id_token readable on disk", raw.contains(session.idToken))
        assertFalse("access token readable on disk", raw.contains(session.accessToken))
        assertFalse("device secret readable on disk", raw.contains(deviceSecret))
        assertFalse("JSON readable on disk", raw.contains("id_token"))
        step("encrypted token bundle verified (${file.length()} bytes)")
    }

    /**
     * Drives eSignet's login pages in Chrome. The pages are a web app, so the test looks at what is on
     * screen and acts on it, in any order: UIN field, captcha box, "Get OTP", OTP fields, "Verify",
     * consent "Allow". It ends when Hub's home screen is back.
     */
    private fun loginInBrowser() {
        val deadline = System.currentTimeMillis() + 180_000
        var uinTyped = false
        var otpTyped = false
        var captchaTicked = false
        while (System.currentTimeMillis() < deadline) {
            if (device.hasObject(By.res("home_name"))) return
            device.findObject(By.res("error_text"))?.let { throw AssertionError("Hub shows an error: ${it.text}") }

            val fields = device.findObjects(By.pkg(CHROME).clazz("android.widget.EditText"))
            when {
                !otpTyped && fields.size >= 4 -> {
                    fields.first().click()
                    typeText(otp)
                    otpTyped = true
                    step("OTP typed into ${fields.size} fields")
                }
                !uinTyped && fields.size in 1..3 -> {
                    val field = fields.first()
                    field.click()
                    typeText(uin)
                    uinTyped = true
                    step("UIN typed")
                    device.pressBack() // close the keyboard so the buttons below are visible
                }
                else -> {
                    val captcha = if (captchaTicked) null else findWeb(CAPTCHA)
                    val button = findWeb(if (otpTyped) VERIFY_OR_ALLOW else GET_OTP_OR_ALLOW)
                    when {
                        captcha != null && uinTyped -> {
                            captcha.click()
                            captchaTicked = true
                            step("captcha box tapped")
                            Thread.sleep(3_000)
                        }
                        button != null && button.isEnabled -> {
                            val label = button.text ?: button.contentDescription
                            button.click()
                            step("tapped \"$label\"")
                            Thread.sleep(2_000)
                        }
                        else -> Thread.sleep(1_000)
                    }
                }
            }
        }
        throw AssertionError("the browser login did not finish within 180 s; see ui-test/last-window.xml")
    }

    private fun typeText(text: String) {
        device.executeShellCommand("input text $text")
        Thread.sleep(500)
    }

    /** A Chrome node whose text or content description matches [pattern], preferring buttons. */
    private fun findWeb(pattern: Pattern): UiObject2? {
        val selectors: List<BySelector> = listOf(
            By.pkg(CHROME).clazz("android.widget.Button").text(pattern),
            By.pkg(CHROME).clazz("android.widget.Button").desc(pattern),
            By.pkg(CHROME).clazz("android.widget.CheckBox").text(pattern),
            By.pkg(CHROME).clazz("android.widget.CheckBox").desc(pattern),
            By.pkg(CHROME).text(pattern),
            By.pkg(CHROME).desc(pattern),
        )
        return selectors.firstNotNullOfOrNull { device.findObject(it) }
    }

    private fun step(message: String) {
        Log.i(TAG, message)
    }

    /** Saved to ui-test/screens/; `android/scripts/run-ui-test.sh` copies them to the evidence folder. */
    private fun screenshot(name: String) {
        val dir = File(evidence, "screens").apply { mkdirs() }
        // takeScreenshot answers false instead of throwing when a busy emulator cannot capture the screen.
        repeat(5) {
            if (runCatching { device.takeScreenshot(File(dir, "$name.png")) }.getOrDefault(false)) return
            device.waitForIdle(1_000)
        }
        Log.w(TAG, "screenshot $name could not be taken")
    }

    private companion object {
        const val TAG = "PS03HubTest"
        const val CHROME = "com.android.chrome"
        val CAPTCHA: Pattern = Pattern.compile("(?i).*(i am human|i'm not a robot|hcaptcha checkbox).*")
        val GET_OTP_OR_ALLOW: Pattern = Pattern.compile("(?i)\\s*(get otp|send otp|allow|accept|i agree|proceed)\\s*")
        val VERIFY_OR_ALLOW: Pattern = Pattern.compile("(?i)\\s*(verify|log ?in|allow|accept|i agree|proceed)\\s*")
    }
}
