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
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.ExperimentalComposeUiApi
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.testTagsAsResourceId
import androidx.compose.ui.unit.dp
import org.badmosip.nativesso.contract.FamilySsoContract
import org.badmosip.nativesso.contract.SigningCertificates
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

/** Whether the Super App is installed and signed with this app's certificate. */
internal enum class SuperAppState { MISSING, SAME_FAMILY, OTHER_FAMILY }

/**
 * Sign-in screen of the Child App: the server, this app's certificate, whether Hub is installed with
 * the same certificate, and whether Hub has a sign-in it shares (asked over [HubConnection] on a
 * background thread when the screen opens and on "Check again"). The token exchange that turns that
 * sign-in into Health Card's own tokens comes in a later work package.
 */
class MainActivity : ComponentActivity() {
    /** null while asking Hub. */
    private var hubStatus: HubStatus? by mutableStateOf(null)
    private val worker: ExecutorService = Executors.newSingleThreadExecutor()

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
                    HomeScreen(BuildConfig.ISSUER, certificate, state, hubStatus, ::checkHub)
                }
            }
        }
        checkHub()
    }

    private fun checkHub() {
        hubStatus = null
        val connection = HubConnection(applicationContext)
        worker.execute {
            val status = connection.status()
            runOnUiThread { hubStatus = status }
        }
    }

    override fun onDestroy() {
        worker.shutdown()
        super.onDestroy()
    }
}

@Composable
@OptIn(ExperimentalComposeUiApi::class)
private fun HomeScreen(issuer: String, certificate: String?, superApp: SuperAppState, hub: HubStatus?, onCheck: () -> Unit) {
    Column(
        modifier = Modifier.semantics { testTagsAsResourceId = true }.safeDrawingPadding().padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = stringResource(R.string.app_name),
            style = MaterialTheme.typography.headlineMedium,
            modifier = Modifier.semantics { heading() },
        )
        Text(stringResource(R.string.role), style = MaterialTheme.typography.bodyLarge)
        if (hub == null) {
            CircularProgressIndicator()
            Text(stringResource(R.string.hub_checking))
        } else {
            LabelledValue(stringResource(R.string.hub_sign_in_label), hubText(hub), Modifier.testTag("hub_status"))
            OutlinedButton(onClick = onCheck, modifier = Modifier.testTag("check_hub_button")) {
                Text(stringResource(R.string.hub_check_again))
            }
        }
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
private fun hubText(hub: HubStatus): String = when (hub) {
    is HubStatus.Available -> stringResource(R.string.hub_available, hub.bundle.issuer)
    is HubStatus.Unavailable -> stringResource(
        when (hub.reason) {
            HubException.Reason.NOT_SIGNED_IN -> R.string.hub_not_signed_in
            HubException.Reason.HUB_MISSING -> R.string.super_app_missing
            HubException.Reason.BIND_REFUSED, HubException.Reason.CALLER_REFUSED -> R.string.hub_refused
            HubException.Reason.TIMEOUT -> R.string.hub_timeout
            HubException.Reason.RATE_LIMITED -> R.string.hub_rate_limited
            HubException.Reason.VERSION_MISMATCH -> R.string.hub_version
            HubException.Reason.HTU_NOT_ALLOWED, HubException.Reason.FAILED -> R.string.hub_failed
        },
    )
}

@Composable
private fun LabelledValue(label: String, value: String, modifier: Modifier = Modifier) {
    Card(modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(label, style = MaterialTheme.typography.labelLarge)
            Text(value, style = MaterialTheme.typography.bodyMedium)
        }
    }
}
