// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.browser.customtabs.CustomTabsIntent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.ExperimentalComposeUiApi
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.testTagsAsResourceId
import androidx.compose.ui.unit.dp
import org.badmosip.nativesso.superapp.auth.TokenBundle

/**
 * Hub's only screen host: welcome (sign in, and whether to share the sign-in with the family),
 * a waiting state while the browser is open, and home (the user's name, sign out).
 *
 * `singleTask` in the manifest: when [RedirectActivity] brings Hub back, the Custom Tab above this
 * activity is closed and the redirect arrives in [onNewIntent].
 */
class MainActivity : ComponentActivity() {
    private val model: HubViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        // A redirect in the launch intent is handled once, not again after a rotation.
        model.start(if (savedInstanceState == null) redirectOf(intent) else null)
        setContent {
            MaterialTheme {
                Surface(Modifier.fillMaxSize()) {
                    HubContent(model)
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        redirectOf(intent)?.let(model::onRedirect)
    }

    private fun redirectOf(intent: Intent?): String? =
        intent?.takeIf { it.action == RedirectActivity.ACTION_REDIRECT }?.data?.toString()

    @Composable
    @OptIn(ExperimentalComposeUiApi::class)
    private fun HubContent(model: HubViewModel) {
        val url = model.browserUrl
        LaunchedEffect(url) {
            if (url != null) {
                CustomTabsIntent.Builder().setShowTitle(true).build().launchUrl(this@MainActivity, Uri.parse(url))
                model.browserOpened()
            }
        }
        Column(
            modifier = Modifier
                .semantics { testTagsAsResourceId = true }
                .safeDrawingPadding()
                .padding(24.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Text(
                text = stringResource(R.string.app_name),
                style = MaterialTheme.typography.headlineMedium,
                modifier = Modifier.semantics { heading() },
            )
            when (val screen = model.screen) {
                HubScreen.Starting -> CircularProgressIndicator()
                is HubScreen.Welcome -> Welcome(model, screen.error)
                is HubScreen.Working -> Working(screen.inBrowser, model::cancel)
                is HubScreen.Home -> Home(screen.session, model::signOut)
            }
        }
    }
}

@Composable
private fun Welcome(model: HubViewModel, error: String?) {
    Text(stringResource(R.string.role), style = MaterialTheme.typography.bodyLarge)
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.share_switch), modifier = Modifier.weight(1f))
        Switch(
            checked = model.shareWithFamily,
            onCheckedChange = { model.shareWithFamily = it },
            modifier = Modifier.testTag("share_switch"),
        )
    }
    Button(onClick = model::signIn, modifier = Modifier.fillMaxWidth().testTag("sign_in_button")) {
        Text(stringResource(R.string.sign_in))
    }
    if (error != null) {
        Text(error, color = MaterialTheme.colorScheme.error, modifier = Modifier.testTag("error_text"))
    }
    LabelledValue(stringResource(R.string.issuer_label), BuildConfig.ISSUER)
}

@Composable
private fun Working(inBrowser: Boolean, onCancel: () -> Unit) {
    CircularProgressIndicator()
    Text(
        stringResource(if (inBrowser) R.string.working_browser else R.string.working),
        modifier = Modifier.testTag("working_text"),
    )
    if (inBrowser) {
        OutlinedButton(onClick = onCancel, modifier = Modifier.testTag("cancel_button")) {
            Text(stringResource(R.string.cancel))
        }
    }
}

@Composable
private fun Home(session: TokenBundle, onSignOut: () -> Unit) {
    Text(stringResource(R.string.signed_in_as), style = MaterialTheme.typography.labelLarge)
    Text(
        session.displayName ?: stringResource(R.string.name_unknown),
        style = MaterialTheme.typography.headlineSmall,
        modifier = Modifier.testTag("home_name"),
    )
    LabelledValue(
        stringResource(R.string.sharing_label),
        stringResource(if (session.deviceSecret != null) R.string.sharing_on else R.string.sharing_off),
        Modifier.testTag("sharing_status"),
    )
    LabelledValue(stringResource(R.string.issuer_label), session.issuer)
    OutlinedButton(onClick = onSignOut, modifier = Modifier.testTag("sign_out_button")) {
        Text(stringResource(R.string.sign_out))
    }
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
