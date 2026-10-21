// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.uitest

import android.util.Log
import androidx.test.platform.app.InstrumentationRegistry
import com.google.android.apps.common.testing.accessibility.framework.AccessibilityCheckPreset
import com.google.android.apps.common.testing.accessibility.framework.AccessibilityCheckResult.AccessibilityCheckResultType
import com.google.android.apps.common.testing.accessibility.framework.Parameters
import com.google.android.apps.common.testing.accessibility.framework.uielement.AccessibilityHierarchyAndroid
import com.google.android.apps.common.testing.accessibility.framework.utils.contrast.BitmapImage
import org.junit.Assert.assertTrue
import java.io.File
import java.util.Locale

/**
 * Runs Google's Accessibility Test Framework (the checks behind Espresso's `AccessibilityChecks` and
 * Accessibility Scanner, preset LATEST) on the window in front, as TalkBack sees it: touch target
 * size, a label on every control, text and image contrast (from a screenshot), duplicate labels,
 * reading order and more. Fails on any result of type ERROR; warnings are only written down.
 *
 * Shared by the UI tests of both apps (`androidTest` source folder in each app's build file).
 * Windows of other packages (Chrome with eSignet's pages) are skipped: they are not the apps' UI.
 */
object AccessibilityScan {
    private const val TAG = "PS03A11y"
    private const val OUR_PACKAGES = "org.badmosip.nativesso."

    /** Checks the window in front and writes the findings to `<dir>/a11y/<name>.txt`. */
    fun check(name: String, dir: File) {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val automation = instrumentation.uiAutomation
        val root = automation.rootInActiveWindow ?: throw AssertionError("no window in front to check ($name)")
        val pkg = root.packageName?.toString().orEmpty()
        if (!pkg.startsWith(OUR_PACKAGES)) return
        val hierarchy = AccessibilityHierarchyAndroid.newBuilder(root, instrumentation.targetContext).build()
        val parameters = Parameters().apply { automation.takeScreenshot()?.let { putScreenCapture(BitmapImage(it)) } }
        val checks = AccessibilityCheckPreset.getAccessibilityHierarchyChecksForPreset(AccessibilityCheckPreset.LATEST)
        val all = checks.flatMap { it.runCheckOnHierarchy(hierarchy, null, parameters) }
        val results = all.filter { it.type == AccessibilityCheckResultType.ERROR || it.type == AccessibilityCheckResultType.WARNING }
        val lines = results.map { r ->
            val e = r.element
            "${r.type} ${r.sourceCheckClass.simpleName} [${e?.resourceName ?: e?.className}] " +
                "\"${e?.text ?: e?.contentDescription ?: ""}\" ${e?.boundsInScreen}: ${r.getMessage(Locale.ENGLISH)}"
        }
        val summary = "$name ($pkg): ${results.count { it.type == AccessibilityCheckResultType.ERROR }} errors, " +
            "${results.count { it.type == AccessibilityCheckResultType.WARNING }} warnings " +
            "(${hierarchy.activeWindow.allViews.size} elements, ${checks.size} checks, " +
            "${all.count { it.type == AccessibilityCheckResultType.NOT_RUN }} not run)"
        Log.i(TAG, summary)
        lines.forEach { Log.i(TAG, it) }
        all.filter { it.type == AccessibilityCheckResultType.NOT_RUN }.forEach { Log.d(TAG, "not run: ${it.sourceCheckClass.simpleName}: ${it.getMessage(Locale.ENGLISH)}") }
        File(dir, "a11y").apply { mkdirs() }.resolve("$name.txt").writeText((listOf(summary) + lines).joinToString("\n", postfix = "\n"))
        assertTrue("accessibility check failed on $name:\n" + lines.filter { it.startsWith("ERROR") }.joinToString("\n"),
            results.none { it.type == AccessibilityCheckResultType.ERROR })
    }
}
