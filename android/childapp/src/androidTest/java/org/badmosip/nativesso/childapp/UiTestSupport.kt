// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import android.content.Context
import android.content.Intent
import android.util.Log
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.BySelector
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.UiObject2
import androidx.test.uiautomator.Until
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean
import java.util.regex.Pattern

/**
 * What Health Card's UI tests share: the device, Health Card's services, screenshots for the
 * evidence folder, a watcher that notices any browser window, and the eSignet login in Chrome.
 *
 * Precondition for both tests: Hub is signed in with "Share this sign-in with my other apps" on.
 * `android/scripts/run-ui-test.sh all` runs Hub's `SuperAppSignInTest` first, which does that.
 */
internal class CardUi {
    val instrumentation = InstrumentationRegistry.getInstrumentation()
    val device: UiDevice = UiDevice.getInstance(instrumentation)
    val context: Context = instrumentation.targetContext
    private val args = InstrumentationRegistry.getArguments()
    val uin: String = args.getString("uin") ?: "1774231323"
    val otp: String = args.getString("otp") ?: "111111"
    val expectedName: String = args.getString("expectedName") ?: "Siddharth K Mansour"
    val services: CardServices get() = (context.applicationContext as CardApp).services
    private val evidence: File by lazy { File(context.getExternalFilesDir(null), "ui-test").apply { mkdirs() } }

    /** Fails with a clear message unless Hub shares a sign-in. */
    fun requireHubSignedInWithSharing() {
        val status = HubConnection(context).status()
        assertTrue(
            "Hub shares no sign-in ($status). Run Hub's SuperAppSignInTest first: bash android/scripts/run-ui-test.sh all",
            status is HubStatus.Available,
        )
    }

    /** Health Card without tokens and allowed to sign in on its own. */
    fun resetHealthCard() {
        services.authenticator.signOut(byUser = false)
        services.authenticator.allowAutomaticSignIn()
    }

    fun launchHealthCard() {
        context.startActivity(
            Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK),
        )
    }

    fun waitFor(tag: String, millis: Long = 30_000): UiObject2 {
        val found = device.wait(Until.findObject(By.res(tag)), millis)
        if (found == null) {
            device.findObject(By.res("error_text"))?.let { throw AssertionError("Health Card shows an error: ${it.text}") }
            device.findObject(By.res("hub_status"))?.let { throw AssertionError("Health Card shows: ${it.text}") }
        }
        assertNotNull("\"$tag\" did not appear within $millis ms", found)
        return found
    }

    /** Saved to ui-test/screens/; `android/scripts/run-ui-test.sh` copies them to the evidence folder. */
    fun screenshot(name: String) {
        val dir = File(evidence, "screens").apply { mkdirs() }
        // takeScreenshot answers false instead of throwing when a busy emulator cannot capture the screen.
        repeat(5) {
            if (runCatching { device.takeScreenshot(File(dir, "$name.png")) }.getOrDefault(false)) return
            device.waitForIdle(1_000)
        }
        Log.w(TAG, "screenshot $name could not be taken")
    }

    /** The last screen and its view tree, kept for the run script (pass or fail). */
    fun keepEvidence() {
        runCatching { device.takeScreenshot(File(evidence, "last-screen.png")) }
        runCatching { device.dumpWindowHierarchy(File(evidence, "last-window.xml")) }
    }

    fun step(message: String) {
        Log.i(TAG, message)
    }

    /** Polls the foreground package every 200 ms; [sawBrowser] is true once a browser was in front. */
    inner class BrowserWatcher : AutoCloseable {
        val sawBrowser = AtomicBoolean(false)
        private val running = AtomicBoolean(true)
        private val thread = Thread {
            while (running.get()) {
                if (device.currentPackageName in BROWSERS) sawBrowser.set(true)
                Thread.sleep(200)
            }
        }.apply {
            isDaemon = true
            start()
        }

        override fun close() {
            running.set(false)
            thread.join(1_000)
        }
    }

    /**
     * Drives eSignet's login pages in Chrome (the same steps as Hub's `SuperAppSignInTest`): UIN,
     * captcha box, "Get OTP", OTP fields, "Verify", consent "Allow", in whatever order they appear.
     * Ends when [done] is true.
     */
    fun loginInBrowser(done: () -> Boolean) {
        val deadline = System.currentTimeMillis() + 180_000
        var uinTyped = false
        var otpTyped = false
        var captchaTicked = false
        while (System.currentTimeMillis() < deadline) {
            if (done()) return
            device.findObject(By.res("error_text"))?.let { throw AssertionError("Health Card shows an error: ${it.text}") }
            val fields = device.findObjects(By.pkg(CHROME).clazz("android.widget.EditText"))
            when {
                !otpTyped && fields.size >= 4 -> {
                    fields.first().click()
                    typeText(otp)
                    otpTyped = true
                    step("OTP typed into ${fields.size} fields")
                }
                !uinTyped && fields.size in 1..3 -> {
                    fields.first().click()
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

    companion object {
        const val TAG = "PS03CardTest"
        const val CHROME = "com.android.chrome"
        val BROWSERS = setOf(CHROME, "com.android.browser", "org.chromium.webview_shell")
        val CAPTCHA: Pattern = Pattern.compile("(?i).*(i am human|i'm not a robot|hcaptcha checkbox).*")
        val GET_OTP_OR_ALLOW: Pattern = Pattern.compile("(?i)\\s*(get otp|send otp|allow|accept|i agree|proceed)\\s*")
        val VERIFY_OR_ALLOW: Pattern = Pattern.compile("(?i)\\s*(verify|log ?in|allow|accept|i agree|proceed)\\s*")
    }
}
