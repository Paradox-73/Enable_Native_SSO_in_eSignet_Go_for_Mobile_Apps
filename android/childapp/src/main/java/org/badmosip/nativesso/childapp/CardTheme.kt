// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import kotlin.math.pow

/**
 * Health Card's colors, light and dark. Every text color reaches a WCAG 2.1 contrast of at least
 * 4.5:1 on every background it is drawn on ([textPairs], checked by `CardThemeTest`).
 */
object CardColors {
    class Palette(
        val primary: Long,
        val onPrimary: Long,
        val surface: Long,
        val onSurface: Long,
        val surfaceVariant: Long,
        val onSurfaceVariant: Long,
        val error: Long,
        val onError: Long,
    ) {
        /** (text, background) pairs as the screens use them. */
        val textPairs: List<Pair<Long, Long>>
            get() = listOf(
                onSurface to surface, onSurface to surfaceVariant, onSurfaceVariant to surfaceVariant,
                onSurfaceVariant to surface, primary to surface, onPrimary to primary, error to surface,
                error to surfaceVariant, onError to error,
            )
    }

    val light = Palette(
        primary = 0xFF0B57A4, onPrimary = 0xFFFFFFFF, surface = 0xFFFFFFFF, onSurface = 0xFF1A1C20,
        surfaceVariant = 0xFFE6ECF4, onSurfaceVariant = 0xFF3D4550, error = 0xFFB3261E, onError = 0xFFFFFFFF,
    )

    val dark = Palette(
        primary = 0xFFA8C8FF, onPrimary = 0xFF00315F, surface = 0xFF111318, onSurface = 0xFFE3E3E8,
        surfaceVariant = 0xFF29303A, onSurfaceVariant = 0xFFC4CAD4, error = 0xFFFFB4AB, onError = 0xFF690005,
    )

    /** WCAG 2.1 contrast ratio of two opaque ARGB colors (1 to 21). */
    fun contrast(a: Long, b: Long): Double {
        val la = luminance(a)
        val lb = luminance(b)
        return (maxOf(la, lb) + 0.05) / (minOf(la, lb) + 0.05)
    }

    private fun luminance(argb: Long): Double {
        fun channel(shift: Int): Double {
            val c = ((argb shr shift) and 0xFF) / 255.0
            return if (c <= 0.03928) c / 12.92 else ((c + 0.055) / 1.055).pow(2.4)
        }
        return 0.2126 * channel(16) + 0.7152 * channel(8) + 0.0722 * channel(0)
    }
}

/** Material 3 with [CardColors]; follows the system's dark setting. Text sizes are in sp, so they scale. */
@Composable
fun CardTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    val p = if (dark) CardColors.dark else CardColors.light
    val scheme = if (dark) {
        darkColorScheme(
            primary = Color(p.primary), onPrimary = Color(p.onPrimary), background = Color(p.surface),
            onBackground = Color(p.onSurface), surface = Color(p.surface), onSurface = Color(p.onSurface),
            surfaceVariant = Color(p.surfaceVariant), onSurfaceVariant = Color(p.onSurfaceVariant),
            error = Color(p.error), onError = Color(p.onError), outline = Color(p.onSurfaceVariant),
        )
    } else {
        lightColorScheme(
            primary = Color(p.primary), onPrimary = Color(p.onPrimary), background = Color(p.surface),
            onBackground = Color(p.onSurface), surface = Color(p.surface), onSurface = Color(p.onSurface),
            surfaceVariant = Color(p.surfaceVariant), onSurfaceVariant = Color(p.onSurfaceVariant),
            error = Color(p.error), onError = Color(p.onError), outline = Color(p.onSurfaceVariant),
        )
    }
    MaterialTheme(colorScheme = scheme, content = content)
}
