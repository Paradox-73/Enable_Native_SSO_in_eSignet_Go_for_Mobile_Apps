// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp

import android.app.Application
import android.content.Context
import android.util.Log
import com.nimbusds.jose.util.JSONObjectUtils
import okhttp3.OkHttpClient
import org.badmosip.nativesso.contract.SigningCertificates
import org.badmosip.nativesso.oidc.DpopProofFactory
import org.badmosip.nativesso.securestore.EncryptedFileStore
import org.badmosip.nativesso.securestore.KeystoreSecureStore
import org.badmosip.nativesso.superapp.auth.ClientIdentity
import org.badmosip.nativesso.superapp.auth.HubAuthenticator
import org.badmosip.nativesso.superapp.auth.HubConfig
import org.badmosip.nativesso.superapp.keys.AndroidKeystoreKeys
import org.badmosip.nativesso.superapp.keys.EcKeyJwsSigner
import org.badmosip.nativesso.superapp.sso.AndroidCallerLookup
import org.badmosip.nativesso.superapp.sso.FamilyAccessLog
import org.badmosip.nativesso.superapp.sso.FamilySsoGate
import org.badmosip.nativesso.superapp.sso.FamilySsoHost
import java.io.File
import java.util.concurrent.TimeUnit

/** Hub's Application: holds the one [HubServices] of the process. */
class HubApp : Application(), FamilySsoHost {
    /** Created on first use; touches Android Keystore, so call it off the main thread. */
    val services: HubServices by lazy { HubServices(this) }

    override val familySsoGate: FamilySsoGate get() = services.familySso
}

/**
 * The objects the screens and tests share: the encrypted store, the two Keystore keys, the
 * client identity derived from the client key, and the [HubAuthenticator].
 */
class HubServices(context: Context) {
    val store: EncryptedFileStore = KeystoreSecureStore.create(context)

    val dpopSigner: EcKeyJwsSigner = AndroidKeystoreKeys.signer(AndroidKeystoreKeys.DPOP_ALIAS) {
        Log.i(TAG, "DPoP key: $it")
    }

    /** The client key; its `kid` is its RFC 7638 thumbprint, the same `kid` that is registered. */
    val clientSigner: EcKeyJwsSigner = AndroidKeystoreKeys.signer(AndroidKeystoreKeys.CLIENT_AUTH_ALIAS) {
        Log.i(TAG, "client key: $it")
    }.let { it.withKeyId(it.thumbprint) }

    val config = HubConfig(
        issuer = BuildConfig.ISSUER,
        clientId = ClientIdentity.clientIdFor(BuildConfig.CLIENT_ID_PREFIX, clientSigner.thumbprint),
        redirectUri = BuildConfig.REDIRECT_URI,
    )

    private val http = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .followRedirects(false)
        .build()

    val authenticator = HubAuthenticator(
        config = config,
        http = http,
        dpopSigner = dpopSigner,
        clientSigner = clientSigner,
        store = store,
        log = { Log.i(TAG, it) },
    )

    /** "Apps using this sign-in", shown on the home screen. */
    val familyAccess = FamilyAccessLog(store)

    /** The rules of the family single sign-on service (`sso/FamilySsoService`). */
    val familySso = FamilySsoGate(
        callers = AndroidCallerLookup(context.packageManager),
        familyCertificates = { SigningCertificates.sha256Of(context.packageManager, context.packageName) },
        session = authenticator::currentSession,
        proofs = DpopProofFactory(dpopSigner),
        accessLog = familyAccess,
        log = { Log.i(TAG, "family sso: $it") },
    )

    init {
        if (BuildConfig.DEBUG) writeRegistrationInfo(File(context.filesDir, REGISTRATION_FILE))
    }

    /**
     * Debug builds only: the public half of the client key and the client_id, for
     * `android/scripts/register-client.sh`, which reads the file with `adb shell run-as` and
     * registers the client with eSignet. Public data only; nothing secret is written.
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
        Log.i(TAG, "client registration file ready: client_id=${config.clientId}")
    }

    companion object {
        const val TAG: String = "PS03Hub"
        const val REGISTRATION_FILE: String = "client-registration.json"
    }
}
