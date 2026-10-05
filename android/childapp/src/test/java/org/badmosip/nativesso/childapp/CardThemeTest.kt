// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** WCAG 2.1 AA: every text color Health Card draws reaches 4.5:1 on its background, light and dark. */
class CardThemeTest {
    @Test
    fun `contrast formula matches known values`() {
        assertEquals(21.0, CardColors.contrast(0xFF000000, 0xFFFFFFFF), 0.01)
        assertEquals(1.0, CardColors.contrast(0xFF777777, 0xFF777777), 0.001)
    }

    @Test
    fun `every text and background pair is at least 4_5 to 1`() {
        for ((name, palette) in listOf("light" to CardColors.light, "dark" to CardColors.dark)) {
            for ((text, background) in palette.textPairs) {
                val ratio = CardColors.contrast(text, background)
                assertTrue("$name: %08X on %08X is %.2f:1".format(text, background, ratio), ratio >= 4.5)
            }
        }
    }
}
