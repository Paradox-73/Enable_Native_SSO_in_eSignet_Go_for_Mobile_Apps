// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import org.badmosip.nativesso.contract.FamilySsoContract
import org.badmosip.nativesso.contract.SigningCertificates

/** Whether the Super App is installed and signed with this app's certificate. */
internal enum class SuperAppState { MISSING, SAME_FAMILY, OTHER_FAMILY }

/**
 * Home screen of the Child App. This build shows the server, its own certificate and whether the
 * Super App is present with the same certificate, the precondition for sharing its sign-in.
 */
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val own = SigningCertificates.sha256Of(packageManager, packageName)
        val superApp = SigningCertificates.sha256Of(packageManager, FamilySsoContract.SUPER_APP_PACKAGE)
        val state = when {
            superApp.isEmpty() -> SuperAppState.MISSING
            superApp.any { theirs -> own.any { it.contentEquals(theirs) } } -> SuperAppState.SAME_FAMILY
            else -> SuperAppState.OTHER_FAMILY
        }
        val certificate = own.firstOrNull()?.let(SigningCertificates::format)
        setContent {
            MaterialTheme {
                Surface(Modifier.fillMaxSize()) {
                    HomeScreen(BuildConfig.ISSUER, certificate, state)
                }
            }
        }
    }
}

@Composable
private fun HomeScreen(issuer: String, certificate: String?, superApp: SuperAppState) {
    Column(
        modifier = Modifier.safeDrawingPadding().padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = stringResource(R.string.app_name),
            style = MaterialTheme.typography.headlineMedium,
            modifier = Modifier.semantics { heading() },
        )
        Text(stringResource(R.string.role), style = MaterialTheme.typography.bodyLarge)
        LabelledValue(stringResource(R.string.issuer_label), issuer)
        LabelledValue(
            stringResource(R.string.certificate_label),
            certificate ?: stringResource(R.string.certificate_unknown),
        )
        val superAppText = when (superApp) {
            SuperAppState.MISSING -> R.string.super_app_missing
            SuperAppState.SAME_FAMILY -> R.string.super_app_same_family
            SuperAppState.OTHER_FAMILY -> R.string.super_app_other_family
        }
        LabelledValue(stringResource(R.string.super_app_label), stringResource(superAppText))
    }
}

@Composable
private fun LabelledValue(label: String, value: String) {
    Card {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(label, style = MaterialTheme.typography.labelLarge)
            Text(value, style = MaterialTheme.typography.bodyMedium)
        }
    }
}
