// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import org.badmosip.nativesso.childapp.auth.Binding
import org.badmosip.nativesso.childapp.auth.CardAuthenticator
import org.badmosip.nativesso.childapp.auth.CardConfig
import org.badmosip.nativesso.childapp.auth.CardException
import org.badmosip.nativesso.childapp.auth.CardSession
import org.badmosip.nativesso.oidc.HttpStatusException
import org.badmosip.nativesso.oidc.IdTokenValidationException
import org.badmosip.nativesso.oidc.InvalidResponseException
import org.badmosip.nativesso.oidc.NetworkException
import org.badmosip.nativesso.oidc.OAuthError
import org.badmosip.nativesso.oidc.OAuthErrorException

/** What went wrong, in terms of what the user can do; the screen picks the words. */
enum class Problem {
    /** No connection to eSignet. */
    NETWORK,

    /** eSignet answered with an HTTP error and no OAuth error. */
    SERVER_UNAVAILABLE,

    /** `invalid_grant`: Hub's login session (or its device secret) is no longer valid. */
    HUB_SESSION_ENDED,

    /** Another OAuth error; [CardError.detail] is the code. */
    SERVER_REFUSED,

    /** An id_token check failed, or the new tokens do not belong to Hub's login. Nothing was kept. */
    SECURITY_CHECK,

    /** eSignet's answer breaks the protocol. Nothing was kept. */
    BAD_RESPONSE,

    /** Hub could not be used (timeout, busy, refused, other version); [CardError.detail] is the reason. */
    HUB_UNAVAILABLE,

    /** The user said no to the extra permission, here or on eSignet's page. */
    CONSENT_DECLINED,

    /** The browser login took too long. */
    LOGIN_EXPIRED,

    /** A redirect that does not belong to the login in progress. */
    LOGIN_MISMATCH,
    UNEXPECTED,
}

data class CardError(val problem: Problem, val detail: String? = null)

/** What Health Card shows. */
sealed interface CardScreen {
    /** Asking Hub. */
    data object Starting : CardScreen

    /** Signing in; [inBrowser] while eSignet's pages are open for the consent login. */
    data class Working(val inBrowser: Boolean) : CardScreen

    /** "Sign in to Hub first": no shared sign-in (signed out, sharing off, not installed, or Hub refused). */
    data class NeedsHub(val reason: HubException.Reason) : CardScreen

    /** The user signed out of Health Card; it stays signed out until they ask. */
    data class SignedOut(val error: CardError? = null) : CardScreen

    data class Home(val session: CardSession, val notice: CardError? = null) : CardScreen

    /** eSignet needs consent for [missing]; continuing runs Health Card's own login for [scope]. */
    data class Consent(val missing: Set<String>, val scope: Set<String>) : CardScreen

    data class Failed(val error: CardError) : CardScreen
}

/**
 * Health Card's screen logic, without Android types: every decision runs in tests with a fake Hub
 * and a fake eSignet. Work runs through [background]; results come back through [main].
 *
 * On every resume ([refresh]) Hub is asked again: when Hub is signed out (or uninstalled), Health
 * Card clears its own tokens and shows "Sign in to Hub first"; when Hub's login changed, Health Card
 * signs in again silently.
 */
class CardController(
    authProvider: () -> CardAuthenticator,
    hubProvider: () -> HubLink,
    private val background: (Runnable) -> Unit,
    private val main: (Runnable) -> Unit,
    private val log: (String) -> Unit = {},
) {
    // Made on first use, on the background thread: they touch Android Keystore.
    private val auth: CardAuthenticator by lazy(authProvider)
    private val hub: HubLink by lazy(hubProvider)
    private val config: CardConfig get() = auth.config

    var screen: CardScreen = CardScreen.Starting
        private set

    /** URL the activity must open in a Custom Tab; cleared once opened. */
    var browserUrl: String? = null
        private set

    /** A [refresh] is running; set and cleared on the main thread. */
    private var reconciling = false

    /** Called on the main thread after [screen] or [browserUrl] changed. */
    var onChange: () -> Unit = {}

    /** First start; [redirect] is the intent's URI when it came from the browser. */
    fun start(redirect: String?) {
        if (redirect != null) {
            onRedirect(redirect)
            return
        }
        if (screen == CardScreen.Starting) refresh()
    }

    /**
     * On every resume: compares Health Card's state with Hub's. Not while a login or the consent
     * screen is open, and not while the previous check still runs (on first start, [start] and the
     * first resume both ask): every Hub call counts against Hub's per-app rate limit.
     */
    fun refresh() {
        val now = screen
        if ((now is CardScreen.Working) || now is CardScreen.Consent || reconciling) return
        reconciling = true
        run { reconcile() }
    }

    /** "Continue with Hub" or "Try again": allows automatic sign-in again and signs in. */
    fun signInWithHub() {
        show(CardScreen.Working(inBrowser = false))
        run {
            auth.allowAutomaticSignIn()
            CardScreen.Home(auth.silentSignIn(config.scopes))
        }
    }

    /** "Add my phone number": asks for [CardConfig.extraScope] through Hub first. */
    fun addPhone() {
        val held = (screen as? CardScreen.Home)?.session?.scopes.orEmpty()
        var scope: Set<String>? = null
        show(CardScreen.Working(inBrowser = false))
        run(requested = { scope ?: config.scopes }) {
            val wanted = config.scopes + held + config.extraScope
            scope = wanted
            CardScreen.Home(auth.silentSignIn(wanted))
        }
    }

    /** The user agreed on the consent screen: Health Card's own login with `prompt=consent`. */
    fun acceptConsent() {
        val consent = screen as? CardScreen.Consent ?: return
        show(CardScreen.Working(inBrowser = false))
        background.invoke(
            Runnable {
                val result = runCatching { auth.startConsentLogin(consent.scope) }
                main.invoke(
                    Runnable {
                        result.fold(
                            { url ->
                                browserUrl = url
                                show(CardScreen.Working(inBrowser = true))
                            },
                            { e -> run { screenFor(e, consent.scope) } },
                        )
                    },
                )
            },
        )
    }

    /** The user said "Not now": back to the earlier sign-in, if there is one. */
    fun declineConsent() {
        if (screen !is CardScreen.Consent) return
        run { declined() }
    }

    fun browserOpened() {
        browserUrl = null
        onChange()
    }

    /** The browser came back with [uri]. */
    fun onRedirect(uri: String) {
        show(CardScreen.Working(inBrowser = false))
        run { CardScreen.Home(auth.completeConsentLogin(uri)) }
    }

    /** "Cancel" while eSignet's pages are open. */
    fun cancelBrowser() {
        run {
            auth.cancelConsent()
            declined()
        }
    }

    /** "Sign out" in Health Card: its own tokens only; Hub stays signed in. */
    fun signOut() {
        run {
            auth.signOut(byUser = true)
            CardScreen.SignedOut()
        }
    }

    private fun declined(): CardScreen {
        val error = CardError(Problem.CONSENT_DECLINED)
        return auth.currentSession()?.let { CardScreen.Home(it, error) } ?: CardScreen.Failed(error)
    }

    /**
     * Asks Hub for its shared sign-in and, when Health Card must sign in again, does so over the
     * same connection: one bind, one version and one bundle call, then the proofs of the sign-in.
     */
    private fun reconcile(): CardScreen {
        val session = auth.currentSession()
        var shareRead = false
        return try {
            hub.use { calls ->
                val share = calls.share()
                shareRead = true
                when {
                    share == null -> hubUnavailable(session, HubException.Reason.NOT_SIGNED_IN)
                    session == null && auth.signedOutByUser() -> CardScreen.SignedOut()
                    session == null -> CardScreen.Home(auth.silentSignIn(calls, share, config.scopes))
                    session.binding == Binding.HUB &&
                        CardAuthenticator.sessionIdOf(share.idToken) != session.hubSessionId -> {
                        log("Hub's login changed; signing in again")
                        CardScreen.Home(auth.silentSignIn(calls, share, config.scopes))
                    }
                    else -> CardScreen.Home(session)
                }
            }
        } catch (e: HubException) {
            // A refusal while reading the share means Hub could not answer; one during the
            // sign-in is a sign-in failure, shown like any other.
            if (shareRead) throw e
            hubUnavailable(session, e.reason)
        }
    }

    /** Hub has no shared sign-in ([reason]) or could not be asked. */
    private fun hubUnavailable(session: CardSession?, reason: HubException.Reason): CardScreen = when (reason) {
        HubException.Reason.NOT_SIGNED_IN, HubException.Reason.HUB_MISSING -> {
            if (session != null) {
                log("Hub has no shared sign-in ($reason); clearing Health Card's tokens")
                auth.signOut(byUser = false)
            }
            CardScreen.NeedsHub(reason)
        }
        // Hub is there but could not answer now: keep what Health Card has.
        else -> session?.let { CardScreen.Home(it) } ?: CardScreen.NeedsHub(reason)
    }

    /** Runs [work] in the background and shows its screen, or the screen for its failure. */
    private fun run(requested: () -> Set<String> = { config.scopes }, work: () -> CardScreen) {
        background.invoke(
            Runnable {
                val next = try {
                    work()
                } catch (e: Exception) {
                    try {
                        screenFor(e, requested())
                    } catch (inner: Exception) {
                        CardScreen.Failed(CardError(Problem.UNEXPECTED))
                    }
                }
                main.invoke(Runnable { show(next) })
            },
        )
    }

    /** The screen after [e]; runs in the background (it may read the stored session). */
    private fun screenFor(e: Throwable, requested: Set<String>): CardScreen {
        log("failed: ${e.javaClass.simpleName}: ${e.message}")
        if (e is OAuthErrorException && e.requiresInteraction) {
            val held = auth.currentSession()?.scopes.orEmpty()
            return CardScreen.Consent(CardAuthenticator.missingScopes(e, requested, held), requested)
        }
        if (e is HubException && (e.reason == HubException.Reason.NOT_SIGNED_IN || e.reason == HubException.Reason.HUB_MISSING)) {
            return CardScreen.NeedsHub(e.reason)
        }
        val error = when (e) {
            is OAuthErrorException -> when {
                e.error == OAuthError.ACCESS_DENIED && e.httpStatus == 0 -> CardError(Problem.CONSENT_DECLINED)
                e.error == OAuthError.INVALID_GRANT -> CardError(Problem.HUB_SESSION_ENDED, e.code)
                else -> CardError(Problem.SERVER_REFUSED, e.code)
            }
            is HttpStatusException -> CardError(Problem.SERVER_UNAVAILABLE, e.httpStatus.toString())
            is NetworkException -> CardError(Problem.NETWORK)
            is IdTokenValidationException -> CardError(Problem.SECURITY_CHECK, e.reason.name)
            is InvalidResponseException -> CardError(Problem.BAD_RESPONSE)
            is HubException -> CardError(Problem.HUB_UNAVAILABLE, e.reason.name)
            is CardException -> when (e.reason) {
                CardException.Reason.SESSION_MISMATCH, CardException.Reason.OTHER_ISSUER -> CardError(Problem.SECURITY_CHECK, e.reason.name)
                CardException.Reason.PENDING_LOGIN_EXPIRED -> CardError(Problem.LOGIN_EXPIRED)
                else -> CardError(Problem.LOGIN_MISMATCH)
            }
            else -> CardError(Problem.UNEXPECTED)
        }
        return auth.currentSession()?.let { CardScreen.Home(it, error) } ?: CardScreen.Failed(error)
    }

    private fun show(next: CardScreen) {
        screen = next
        reconciling = false
        onChange()
    }
}
