// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp

import android.app.Application
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import org.badmosip.nativesso.oidc.IdTokenValidationException
import org.badmosip.nativesso.oidc.NetworkException
import org.badmosip.nativesso.oidc.OAuthErrorException
import org.badmosip.nativesso.oidc.OidcException
import org.badmosip.nativesso.superapp.auth.LoginException
import org.badmosip.nativesso.superapp.auth.TokenBundle
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

/** What the activity shows. */
sealed interface HubScreen {
    data object Starting : HubScreen
    data class Welcome(val error: String? = null) : HubScreen
    data class Working(val inBrowser: Boolean) : HubScreen
    data class Home(val session: TokenBundle) : HubScreen
}

/**
 * Screen state and the background work behind it. Network and Keystore calls run on one worker
 * thread; results come back on the main thread. Lives across rotation; the login itself survives
 * process death through the pending login in the encrypted store.
 */
class HubViewModel(application: Application) : AndroidViewModel(application) {
    var screen: HubScreen by mutableStateOf(HubScreen.Starting)
        private set

    /** URL the activity must open in a Custom Tab; cleared once opened. */
    var browserUrl: String? by mutableStateOf(null)
        private set

    var shareWithFamily: Boolean by mutableStateOf(false)

    private val worker: ExecutorService = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    private val services get() = getApplication<HubApp>().services

    /** First start of the activity; [redirect] is the intent's URI when it came from the browser. */
    fun start(redirect: String?) {
        if (redirect != null) {
            onRedirect(redirect)
            return
        }
        if (screen != HubScreen.Starting) return
        background({
            services.authenticator.currentSession()
        }) { session ->
            screen = if (session != null) HubScreen.Home(session) else HubScreen.Welcome()
        }
    }

    fun signIn() {
        val share = shareWithFamily
        screen = HubScreen.Working(inBrowser = false)
        background({ services.authenticator.startLogin(share) }) { url ->
            browserUrl = url
            screen = HubScreen.Working(inBrowser = true)
        }
    }

    fun browserOpened() {
        browserUrl = null
    }

    /** The browser came back with [uri] (through [RedirectActivity]). */
    fun onRedirect(uri: String) {
        screen = HubScreen.Working(inBrowser = false)
        background({ services.authenticator.completeLogin(uri) }) { session ->
            screen = HubScreen.Home(session)
        }
    }

    fun cancel() {
        background({ services.authenticator.signOut() }) { screen = HubScreen.Welcome() }
    }

    fun signOut() {
        background({ services.authenticator.signOut() }) { screen = HubScreen.Welcome() }
    }

    private fun <T> background(work: () -> T, done: (T) -> Unit) {
        worker.execute {
            val result = try {
                Result.success(work())
            } catch (e: Exception) {
                Result.failure(e)
            }
            main.post {
                result.fold(done) { e ->
                    Log.w(HubServices.TAG, "failed: ${e.javaClass.simpleName}: ${e.message}")
                    screen = HubScreen.Welcome(error = messageFor(e))
                }
            }
        }
    }

    override fun onCleared() {
        worker.shutdown()
    }

    private fun messageFor(e: Throwable): String {
        val app = getApplication<HubApp>()
        return when (e) {
            is NetworkException -> app.getString(R.string.error_network)
            is OAuthErrorException -> app.getString(R.string.error_server, e.code)
            is IdTokenValidationException -> app.getString(R.string.error_token, e.reason.name)
            is LoginException -> when (e.reason) {
                LoginException.Reason.PENDING_LOGIN_EXPIRED -> app.getString(R.string.error_expired)
                else -> app.getString(R.string.error_no_login)
            }
            is OidcException -> app.getString(R.string.error_response)
            else -> app.getString(R.string.error_unexpected)
        }
    }
}
