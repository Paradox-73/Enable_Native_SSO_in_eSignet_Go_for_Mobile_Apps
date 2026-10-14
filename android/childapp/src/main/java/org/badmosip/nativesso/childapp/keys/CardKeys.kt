// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp.keys

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import org.badmosip.nativesso.oidc.EcKeyJwsSigner
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PrivateKey
import java.security.interfaces.ECPublicKey
import java.security.spec.ECGenParameterSpec

/**
 * Health Card's own EC P-256 keys in Android Keystore (the same scheme as Hub's `AndroidKeystoreKeys`;
 * the private key never enters the app process):
 * - the `private_key_jwt` client key whose public half is registered with eSignet, used for every
 *   request Health Card sends, including the token exchange;
 * - the DPoP key of Health Card's own browser login (re-consent). Tokens from the token exchange are
 *   bound to Hub's key instead, because the device secret is (statement task 9 iii.4).
 */
object CardKeys {
    private const val ANDROID_KEYSTORE = "AndroidKeyStore"

    /** Alias of the DPoP key of Health Card's own login. */
    const val DPOP_ALIAS: String = "card-dpop-es256"

    /** Alias of the client-authentication key. */
    const val CLIENT_AUTH_ALIAS: String = "card-client-auth-es256"

    /** Signer for [alias], creating the key on first use (StrongBox when the device has one). */
    @Synchronized
    fun signer(alias: String, keyId: String? = null): EcKeyJwsSigner {
        val keyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        (keyStore.getEntry(alias, null) as? KeyStore.PrivateKeyEntry)?.let {
            return EcKeyJwsSigner(it.privateKey, it.certificate.publicKey as ECPublicKey, keyId)
        }
        val (private, public) = try {
            generate(alias, strongBox = true)
        } catch (e: StrongBoxUnavailableException) {
            generate(alias, strongBox = false)
        }
        return EcKeyJwsSigner(private, public, keyId)
    }

    private fun generate(alias: String, strongBox: Boolean): Pair<PrivateKey, ECPublicKey> {
        val spec = KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_SIGN)
            .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
            .setDigests(KeyProperties.DIGEST_SHA256)
            .setIsStrongBoxBacked(strongBox)
            .build()
        val pair = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, ANDROID_KEYSTORE)
            .apply { initialize(spec) }
            .generateKeyPair()
        return pair.private to pair.public as ECPublicKey
    }
}
