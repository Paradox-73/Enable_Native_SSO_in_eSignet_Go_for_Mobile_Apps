// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp

import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.browser.customtabs.CustomTabsIntent
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.ExperimentalComposeUiApi
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.colorResource
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.testTagsAsResourceId
import androidx.compose.ui.unit.dp
import org.badmosip.nativesso.superapp.auth.HubConfig
import org.badmosip.nativesso.superapp.auth.TokenBundle
import org.badmosip.nativesso.superapp.sso.FamilyAccess
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

/** The buttons of every Hub screen, so the screens can be shown in tests without a [HubViewModel]. */
internal interface HubActions {
    fun setShare(share: Boolean)
    fun signIn()
    fun cancel()
    fun signOut()
}

/** What "Technical details" shows: for support and for the demo, never needed to use the app. */
internal class HubDetails(val issuer: String, val clientId: String?)

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
        val actions = object : HubActions {
            override fun setShare(share: Boolean) {
                model.shareWithFamily = share
            }
            override fun signIn() = model.signIn()
            override fun cancel() = model.cancel()
            override fun signOut() = model.signOut()
        }
        setContent {
            HubTheme {
                Surface(Modifier.fillMaxSize()) {
                    val url = model.browserUrl
                    LaunchedEffect(url) {
                        if (url != null) {
                            CustomTabsIntent.Builder().setShowTitle(true).build().launchUrl(this@MainActivity, Uri.parse(url))
                            model.browserOpened()
                        }
                    }
                    HubContent(
                        screen = model.screen,
                        shareWithFamily = model.shareWithFamily,
                        details = HubDetails(BuildConfig.ISSUER, model.clientId),
                        actions = actions,
                        appLabel = ::appLabel,
                    )
                }
            }
        }
    }

    override fun onResume() {
        super.onResume()
        model.refreshFamilyApps()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        redirectOf(intent)?.let(model::onRedirect)
    }

    private fun redirectOf(intent: Intent?): String? =
        intent?.takeIf { it.action == RedirectActivity.ACTION_REDIRECT }?.data?.toString()

    /** The name the launcher shows for [packageName]; null when Android does not let Hub see that app. */
    private fun appLabel(packageName: String): String? = try {
        packageManager.getApplicationLabel(packageManager.getApplicationInfo(packageName, 0)).toString()
    } catch (e: PackageManager.NameNotFoundException) {
        null
    }
}

/**
 * Every Hub screen. Order on screen is the TalkBack order: name, what is happening, what the user
 * can do, then the details. The whole column scrolls, so 200 % font size never cuts anything off.
 */
@Composable
@OptIn(ExperimentalComposeUiApi::class)
internal fun HubContent(
    screen: HubScreen,
    shareWithFamily: Boolean,
    details: HubDetails,
    actions: HubActions,
    now: Instant = Instant.now(),
    appLabel: (String) -> String? = { null },
) {
    val startingText = stringResource(R.string.starting)
    Column(
        modifier = Modifier
            .semantics { testTagsAsResourceId = true }
            .safeDrawingPadding()
            .verticalScroll(rememberScrollState())
            .padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Header()
        when (screen) {
            HubScreen.Starting -> CircularProgressIndicator(Modifier.semantics { contentDescription = startingText })
            is HubScreen.Welcome -> Welcome(screen, shareWithFamily, actions)
            is HubScreen.Working -> Working(screen.inBrowser, actions::cancel)
            is HubScreen.Home -> Home(screen.session, screen.familyApps, now, appLabel, actions::signOut)
        }
        TechnicalDetails(details)
    }
}

/** App mark and name, the same layout as in Health Card. The mark is decoration: TalkBack reads the name. */
@Composable
private fun Header() {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Box(Modifier.size(48.dp).clip(RoundedCornerShape(12.dp)).background(colorResource(R.color.brand))) {
            Image(painterResource(R.drawable.ic_launcher_foreground), contentDescription = null, modifier = Modifier.fillMaxSize())
        }
        Text(
            text = stringResource(R.string.app_name),
            style = MaterialTheme.typography.headlineMedium,
            modifier = Modifier.testTag("app_title").semantics { heading() },
        )
    }
}

@Composable
private fun Welcome(screen: HubScreen.Welcome, shareWithFamily: Boolean, actions: HubActions) {
    Text(stringResource(R.string.tagline), style = MaterialTheme.typography.bodyLarge, modifier = Modifier.testTag("tagline"))
    if (screen.signedOut) {
        Text(
            stringResource(R.string.signed_out_notice),
            style = MaterialTheme.typography.bodyLarge,
            modifier = Modifier.testTag("signed_out_text").semantics { liveRegion = LiveRegionMode.Polite },
        )
    }
    // One TalkBack item for the label and the switch; the whole row is the touch target.
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .testTag("share_switch")
            .toggleable(value = shareWithFamily, role = Role.Switch, onValueChange = actions::setShare)
            .padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.share_switch), modifier = Modifier.weight(1f))
        Switch(checked = shareWithFamily, onCheckedChange = null)
    }
    Button(onClick = actions::signIn, modifier = Modifier.fillMaxWidth().testTag("sign_in_button")) {
        Text(stringResource(R.string.sign_in))
    }
    if (screen.error != null) {
        Text(
            screen.error,
            color = MaterialTheme.colorScheme.error,
            modifier = Modifier.testTag("error_text").semantics { liveRegion = LiveRegionMode.Polite },
        )
    }
}

@Composable
private fun Working(inBrowser: Boolean, onCancel: () -> Unit) {
    val text = stringResource(if (inBrowser) R.string.working_browser else R.string.working)
    CircularProgressIndicator(Modifier.semantics { contentDescription = text })
    Text(text, modifier = Modifier.testTag("working_text").semantics { liveRegion = LiveRegionMode.Polite })
    if (inBrowser) {
        OutlinedButton(onClick = onCancel, modifier = Modifier.fillMaxWidth().testTag("cancel_button")) {
            Text(stringResource(R.string.cancel))
        }
    }
}

@Composable
private fun Home(session: TokenBundle, familyApps: List<FamilyAccess>, now: Instant, appLabel: (String) -> String?, onSignOut: () -> Unit) {
    val format = DateTimeFormatter.ofLocalizedDateTime(FormatStyle.SHORT).withZone(ZoneId.systemDefault())
    Text(stringResource(R.string.signed_in_as), style = MaterialTheme.typography.labelLarge)
    Text(
        session.displayName ?: stringResource(R.string.name_unknown),
        style = MaterialTheme.typography.headlineSmall,
        modifier = Modifier.testTag("home_name"),
    )
    val sharedUntil = session.signedInAt.plusSeconds(HubConfig.DEVICE_SECRET_LIFETIME_SECONDS)
    LabelledValue(
        stringResource(R.string.sharing_label),
        when {
            session.deviceSecret == null -> stringResource(R.string.sharing_off)
            now.isBefore(sharedUntil) -> stringResource(R.string.sharing_on, format.format(sharedUntil))
            else -> stringResource(R.string.sharing_ended, format.format(sharedUntil))
        },
        Modifier.testTag("sharing_status"),
    )
    if (session.deviceSecret != null) {
        val unknown = stringResource(R.string.family_app_unknown)
        val row = stringResource(R.string.family_app_row)
        LabelledValue(
            stringResource(R.string.family_apps_label),
            if (familyApps.isEmpty()) {
                stringResource(R.string.family_apps_none)
            } else {
                // App names as the launcher shows them; a package name is never shown to the user.
                familyApps.joinToString("\n") { row.format(appLabel(it.packageName) ?: unknown, format.format(it.lastUsed)) }
            },
            Modifier.testTag("family_apps"),
        )
    }
    OutlinedButton(onClick = onSignOut, modifier = Modifier.fillMaxWidth().testTag("sign_out_button")) {
        Text(stringResource(R.string.sign_out))
    }
}

/** Server and client identifiers, folded away by default: useful for support, noise for the user. */
@Composable
private fun TechnicalDetails(details: HubDetails) {
    var open by rememberSaveable { mutableStateOf(false) }
    TextButton(onClick = { open = !open }, modifier = Modifier.fillMaxWidth().testTag("details_button")) {
        Text(stringResource(if (open) R.string.details_hide else R.string.details_show))
    }
    if (open) {
        LabelledValue(stringResource(R.string.issuer_label), details.issuer, Modifier.testTag("details_issuer"))
        details.clientId?.let { LabelledValue(stringResource(R.string.client_label), it, Modifier.testTag("details_client")) }
    }
}

@Composable
private fun LabelledValue(label: String, value: String, modifier: Modifier = Modifier) {
    // Read by TalkBack as one item: "label, value".
    Card(
        modifier.fillMaxWidth().semantics(mergeDescendants = true) {},
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.surfaceVariant,
            contentColor = MaterialTheme.colorScheme.onSurface,
        ),
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(label, style = MaterialTheme.typography.labelLarge)
            Text(value, style = MaterialTheme.typography.bodyLarge)
        }
    }
}
