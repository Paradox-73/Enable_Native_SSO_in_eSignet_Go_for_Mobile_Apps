// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import android.content.ActivityNotFoundException
import android.content.Intent
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
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
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.testTagsAsResourceId
import androidx.compose.ui.unit.dp
import org.badmosip.nativesso.childapp.auth.Binding
import org.badmosip.nativesso.childapp.auth.CardSession
import org.badmosip.nativesso.contract.FamilySsoContract
import org.badmosip.nativesso.contract.SigningCertificates

/** Whether the Super App is installed and signed with this app's certificate. */
internal enum class SuperAppState { MISSING, SAME_FAMILY, OTHER_FAMILY }

/** What "Technical details" shows: for support and for the demo, never needed to use the app. */
internal class AboutInfo(val issuer: String, val certificate: String?, val superApp: SuperAppState)

/** The buttons of every screen, so the screens can be shown in tests without a [CardController]. */
internal interface CardActions {
    fun checkAgain()
    fun openHub()
    fun signInWithHub()
    fun addPhone()
    fun signOut()
    fun acceptConsent()
    fun declineConsent()
    fun cancelBrowser()
}

/**
 * Health Card's only screen host. Opens silently signed in when Hub shares a sign-in; otherwise
 * "Sign in to Hub first". Asks Hub again on every resume, so a "Sign out of all apps" in Hub signs
 * Health Card out too. `singleTask`: the consent login's redirect arrives in [onNewIntent].
 */
class MainActivity : ComponentActivity() {
    private val model: CardViewModel by viewModels()

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
        val about = AboutInfo(BuildConfig.ISSUER, own.firstOrNull()?.let(SigningCertificates::format), state)
        val controller = model.controller
        val actions = object : CardActions {
            override fun checkAgain() = controller.refresh()
            override fun openHub() = this@MainActivity.openHub()
            override fun signInWithHub() = controller.signInWithHub()
            override fun addPhone() = controller.addPhone()
            override fun signOut() = controller.signOut()
            override fun acceptConsent() = controller.acceptConsent()
            override fun declineConsent() = controller.declineConsent()
            override fun cancelBrowser() = controller.cancelBrowser()
        }
        // A redirect in the launch intent is handled once, not again after a rotation.
        controller.start(if (savedInstanceState == null) redirectOf(intent) else null)
        setContent {
            CardTheme {
                Surface(Modifier.fillMaxSize()) {
                    val url = model.browserUrl
                    LaunchedEffect(url) {
                        if (url != null) {
                            CustomTabsIntent.Builder().setShowTitle(true).build().launchUrl(this@MainActivity, Uri.parse(url))
                            controller.browserOpened()
                        }
                    }
                    CardContent(model.screen, about, actions)
                }
            }
        }
    }

    override fun onResume() {
        super.onResume()
        // Hub may have signed out (or changed its login) while Health Card was in the background.
        model.controller.refresh()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        redirectOf(intent)?.let(model.controller::onRedirect)
    }

    private fun redirectOf(intent: Intent?): String? =
        intent?.takeIf { it.action == RedirectActivity.ACTION_REDIRECT }?.data?.toString()

    private fun openHub() {
        val launch = packageManager.getLaunchIntentForPackage(FamilySsoContract.SUPER_APP_PACKAGE) ?: return
        try {
            startActivity(launch)
        } catch (e: ActivityNotFoundException) {
            // Hub was removed between the check and the tap; the next resume shows it.
        }
    }
}

/**
 * Every screen. Order on screen is the TalkBack order: title, what is happening, what the user can
 * do, then the details. Labelled values are read as one item; changing status lines are polite live
 * regions; the whole column scrolls, so 200 % font size never cuts anything off.
 */
@Composable
@OptIn(ExperimentalComposeUiApi::class)
internal fun CardContent(screen: CardScreen, about: AboutInfo, actions: CardActions) {
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
            CardScreen.Starting -> Busy(stringResource(R.string.hub_checking))
            is CardScreen.Working -> Working(screen.inBrowser, actions)
            is CardScreen.NeedsHub -> NeedsHub(screen.reason, about.superApp, actions)
            is CardScreen.SignedOut -> SignedOut(screen.error, actions)
            is CardScreen.Home -> Home(screen.session, screen.notice, actions)
            is CardScreen.Consent -> Consent(screen.missing, actions)
            is CardScreen.Failed -> Failed(screen.error, actions)
        }
        TechnicalDetails(about)
    }
}

/** App mark and name, the same layout as in Hub. The mark is decoration: TalkBack reads the name. */
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
private fun Busy(text: String) {
    CircularProgressIndicator(Modifier.semantics { contentDescription = text })
    Status(text, "working_text")
}

@Composable
private fun Working(inBrowser: Boolean, actions: CardActions) {
    Busy(stringResource(if (inBrowser) R.string.working_browser else R.string.working))
    if (inBrowser) {
        OutlinedButton(onClick = actions::cancelBrowser, modifier = Modifier.fillMaxWidth().testTag("cancel_button")) {
            Text(stringResource(R.string.cancel))
        }
    }
}

@Composable
private fun NeedsHub(reason: HubException.Reason, superApp: SuperAppState, actions: CardActions) {
    Title(stringResource(R.string.needs_hub_title), "needs_hub_title")
    Text(stringResource(R.string.tagline), style = MaterialTheme.typography.bodyLarge, modifier = Modifier.testTag("tagline"))
    Status(stringResource(hubReasonText(reason)), "hub_status")
    if (superApp != SuperAppState.MISSING) {
        Button(onClick = actions::openHub, modifier = Modifier.fillMaxWidth().testTag("open_hub_button")) {
            Text(stringResource(R.string.open_hub))
        }
    }
    OutlinedButton(onClick = actions::checkAgain, modifier = Modifier.fillMaxWidth().testTag("check_hub_button")) {
        Text(stringResource(R.string.hub_check_again))
    }
}

@Composable
private fun SignedOut(error: CardError?, actions: CardActions) {
    Status(stringResource(R.string.signed_out_text), "signed_out_text")
    error?.let { ErrorText(it) }
    Button(onClick = actions::signInWithHub, modifier = Modifier.fillMaxWidth().testTag("continue_button")) {
        Text(stringResource(R.string.continue_with_hub))
    }
}

@Composable
private fun Home(session: CardSession, notice: CardError?, actions: CardActions) {
    Text(stringResource(R.string.signed_in_as), style = MaterialTheme.typography.labelLarge)
    Text(
        session.name ?: stringResource(R.string.name_unknown),
        style = MaterialTheme.typography.headlineSmall,
        modifier = Modifier.testTag("home_name"),
    )
    notice?.let { ErrorText(it, "notice_text") }
    LabelledValue(stringResource(R.string.email_label), session.email ?: stringResource(R.string.not_shared), "card_email")
    LabelledValue(stringResource(R.string.phone_label), session.phone ?: stringResource(R.string.not_shared), "card_phone")
    LabelledValue(
        stringResource(R.string.how_label),
        stringResource(if (session.binding == Binding.HUB) R.string.how_hub else R.string.how_own),
        "sign_in_method",
    )
    if (session.phone == null) {
        Button(onClick = actions::addPhone, modifier = Modifier.fillMaxWidth().testTag("add_phone_button")) {
            Text(stringResource(R.string.add_phone))
        }
    }
    OutlinedButton(onClick = actions::signOut, modifier = Modifier.fillMaxWidth().testTag("sign_out_button")) {
        Text(stringResource(R.string.sign_out))
    }
}

@Composable
private fun Consent(missing: Set<String>, actions: CardActions) {
    Title(stringResource(R.string.consent_title), "consent_title")
    val names = missing.map { scopeName(it) }.joinToString(", ")
    Text(stringResource(R.string.consent_body, names), modifier = Modifier.testTag("consent_text"))
    Text(stringResource(R.string.consent_otp), style = MaterialTheme.typography.bodyMedium)
    Button(onClick = actions::acceptConsent, modifier = Modifier.fillMaxWidth().testTag("consent_continue_button")) {
        Text(stringResource(R.string.consent_continue))
    }
    OutlinedButton(onClick = actions::declineConsent, modifier = Modifier.fillMaxWidth().testTag("consent_decline_button")) {
        Text(stringResource(R.string.consent_decline))
    }
}

@Composable
private fun Failed(error: CardError, actions: CardActions) {
    ErrorText(error)
    Button(onClick = actions::signInWithHub, modifier = Modifier.fillMaxWidth().testTag("try_again_button")) {
        Text(stringResource(R.string.try_again))
    }
    OutlinedButton(onClick = actions::openHub, modifier = Modifier.fillMaxWidth().testTag("open_hub_button")) {
        Text(stringResource(R.string.open_hub))
    }
}

/** Server, certificate and Hub check, folded away by default: useful for support, noise for the user. */
@Composable
private fun TechnicalDetails(about: AboutInfo) {
    var open by rememberSaveable { mutableStateOf(false) }
    TextButton(onClick = { open = !open }, modifier = Modifier.fillMaxWidth().testTag("details_button")) {
        Text(stringResource(if (open) R.string.details_hide else R.string.details_show))
    }
    if (open) {
        LabelledValue(stringResource(R.string.issuer_label), about.issuer, "details_issuer")
        LabelledValue(
            stringResource(R.string.certificate_label),
            about.certificate ?: stringResource(R.string.certificate_unknown),
            "details_certificate",
        )
        val superAppText = when (about.superApp) {
            SuperAppState.MISSING -> R.string.super_app_missing
            SuperAppState.SAME_FAMILY -> R.string.super_app_same_family
            SuperAppState.OTHER_FAMILY -> R.string.super_app_other_family
        }
        LabelledValue(stringResource(R.string.super_app_label), stringResource(superAppText), "details_hub")
    }
}

@Composable
private fun Title(text: String, tag: String) {
    Text(text, style = MaterialTheme.typography.titleLarge, modifier = Modifier.testTag(tag).semantics { heading() })
}

/** A line that changes while the user waits: TalkBack reads it out when it changes. */
@Composable
private fun Status(text: String, tag: String) {
    Text(
        text,
        style = MaterialTheme.typography.bodyLarge,
        modifier = Modifier.testTag(tag).semantics { liveRegion = LiveRegionMode.Polite },
    )
}

@Composable
private fun ErrorText(error: CardError, tag: String = "error_text") {
    Text(
        errorText(error),
        color = MaterialTheme.colorScheme.error,
        style = MaterialTheme.typography.bodyLarge,
        modifier = Modifier.testTag(tag).semantics { liveRegion = LiveRegionMode.Polite },
    )
}

/** A label and its value, read by TalkBack as one item ("Email, siddharth@example.com"). */
@Composable
private fun LabelledValue(label: String, value: String, tag: String? = null) {
    Card(
        modifier = Modifier.fillMaxWidth().let { if (tag != null) it.testTag(tag) else it }.semantics(mergeDescendants = true) {},
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

@Composable
private fun scopeName(scope: String): String = when (scope) {
    "phone" -> stringResource(R.string.scope_phone)
    "email" -> stringResource(R.string.scope_email)
    "profile" -> stringResource(R.string.scope_profile)
    else -> scope
}

internal fun hubReasonText(reason: HubException.Reason): Int = when (reason) {
    HubException.Reason.NOT_SIGNED_IN -> R.string.hub_not_signed_in
    HubException.Reason.HUB_MISSING -> R.string.super_app_missing
    HubException.Reason.BIND_REFUSED, HubException.Reason.CALLER_REFUSED -> R.string.hub_refused
    HubException.Reason.TIMEOUT -> R.string.hub_timeout
    HubException.Reason.RATE_LIMITED -> R.string.hub_rate_limited
    HubException.Reason.VERSION_MISMATCH -> R.string.hub_version
    HubException.Reason.HTU_NOT_ALLOWED, HubException.Reason.FAILED -> R.string.hub_failed
}

@Composable
private fun errorText(error: CardError): String {
    val detail = error.detail.orEmpty()
    return when (error.problem) {
        Problem.NETWORK -> stringResource(R.string.error_network)
        Problem.SERVER_UNAVAILABLE -> stringResource(R.string.error_server_unavailable, detail)
        Problem.HUB_SESSION_ENDED -> stringResource(R.string.error_hub_session_ended)
        Problem.SERVER_REFUSED -> stringResource(R.string.error_server_refused, detail)
        Problem.SECURITY_CHECK -> stringResource(R.string.error_security)
        Problem.BAD_RESPONSE -> stringResource(R.string.error_response)
        Problem.HUB_UNAVAILABLE -> stringResource(R.string.error_hub)
        Problem.CONSENT_DECLINED -> stringResource(R.string.error_consent_declined)
        Problem.LOGIN_EXPIRED -> stringResource(R.string.error_expired)
        Problem.LOGIN_MISMATCH -> stringResource(R.string.error_mismatch)
        Problem.UNEXPECTED -> stringResource(R.string.error_unexpected)
    }
}
