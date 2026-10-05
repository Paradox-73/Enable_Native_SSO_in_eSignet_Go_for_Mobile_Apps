// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import android.app.Application
import android.content.Context
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import com.nimbusds.jose.util.JSONObjectUtils
import okhttp3.OkHttpClient
import org.badmosip.nativesso.childapp.auth.CardAuthenticator
import org.badmosip.nativesso.childapp.auth.CardConfig
import org.badmosip.nativesso.childapp.auth.ClientIdentity
import org.badmosip.nativesso.childapp.keys.CardKeySigner
import org.badmosip.nativesso.childapp.keys.CardKeys
import org.badmosip.nativesso.securestore.EncryptedFileStore
import org.badmosip.nativesso.securestore.KeystoreSecureStore
import java.io.File
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/** Health Card's Application: holds the one [CardServices] of the process. */
class CardApp : Application() {
    /** Created on first use; touches Android Keystore, so call it off the main thread. */
    val services: CardServices by lazy { CardServices(this) }
}

/**
 * The objects the screen and the tests share: the encrypted store, Health Card's two Keystore keys,
 * the client identity derived from the client key, the link to Hub, and the [CardAuthenticator].
 */
class CardServices(context: Context) {
    val store: EncryptedFileStore = KeystoreSecureStore.create(context)

    /** DPoP key of Health Card's own (re-consent) login. */
    val dpopSigner: CardKeySigner = CardKeys.signer(CardKeys.DPOP_ALIAS)

    /** The client key; its `kid` is its RFC 7638 thumbprint, the same `kid` that is registered. */
    val clientSigner: CardKeySigner = CardKeys.signer(CardKeys.CLIENT_AUTH_ALIAS).let { it.withKeyId(it.thumbprint) }

    val config = CardConfig(
        issuer = BuildConfig.ISSUER,
        clientId = ClientIdentity.clientIdFor(BuildConfig.CLIENT_ID_PREFIX, clientSigner.thumbprint),
        redirectUri = BuildConfig.REDIRECT_URI,
    )

    private val http = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .followRedirects(false)
        .build()

    val hub: HubLink = HubConnectionLink(HubConnection(context.applicationContext))

    val authenticator = CardAuthenticator(
        config = config,
        http = http,
        hub = hub,
        ownDpopSigner = dpopSigner,
        clientSigner = clientSigner,
        store = store,
        log = { debugLog(it) },
    )

    init {
        if (BuildConfig.DEBUG) writeRegistrationInfo(File(context.filesDir, REGISTRATION_FILE))
    }

    /**
     * Debug builds only: the public half of the client key and the client_id, for
     * `android/scripts/register-client.sh childapp`. Public data only; nothing secret is written.
     */
    private fun writeRegistrationInfo(file: File) {
        val jwk = LinkedHashMap<String, Any?>(clientSigner.publicJwk)
        jwk["kid"] = clientSigner.keyId
        jwk["use"] = "sig"
        jwk["alg"] = clientSigner.algorithm
        val json = JSONObjectUtils.toJSONString(
            linkedMapOf<String, Any?>(
                "client_id" to config.clientId,
                "redirect_uri" to config.redirectUri,
                "acr" to config.acrValues,
                "public_jwk" to jwk,
            ),
        )
        if (!file.isFile || file.readText() != json) file.writeText(json)
        debugLog("client registration file ready: client_id=${config.clientId}")
    }

    companion object {
        const val TAG: String = "PS03Card"

        /** Debug builds only, as in Hub: a release build logs nothing (and never a token, in any build). */
        fun debugLog(message: String) {
            if (BuildConfig.DEBUG) Log.i(TAG, message)
        }
        const val REGISTRATION_FILE: String = "client-registration.json"
    }
}

/** Holds the [CardController] across rotation; its work runs on one worker thread. */
class CardViewModel(application: Application) : AndroidViewModel(application) {
    var screen: CardScreen by mutableStateOf(CardScreen.Starting)
        private set

    var browserUrl: String? by mutableStateOf(null)
        private set

    private val worker: ExecutorService = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    val controller = CardController(
        authProvider = { getApplication<CardApp>().services.authenticator },
        hubProvider = { getApplication<CardApp>().services.hub },
        background = { worker.execute(it) },
        main = { main.post(it) },
        log = { CardServices.debugLog(it) },
    ).also { c ->
        c.onChange = {
            screen = c.screen
            browserUrl = c.browserUrl
        }
    }

    override fun onCleared() {
        worker.shutdown()
    }
}
