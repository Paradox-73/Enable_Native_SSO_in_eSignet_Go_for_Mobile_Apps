// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.keys

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
 * EC P-256 signing keys in Android Keystore. The private key never enters the app process and has
 * no export path; StrongBox holds it when the device has one, otherwise the TEE (software on an
 * emulator). Hub has two: the DPoP key its tokens and device secret are bound to, and the
 * `private_key_jwt` client key whose public half is registered with eSignet.
 */
object AndroidKeystoreKeys {
    private const val ANDROID_KEYSTORE = "AndroidKeyStore"

    /** Alias of the DPoP key. */
    const val DPOP_ALIAS: String = "hub-dpop-es256"

    /** Alias of the client-authentication key. */
    const val CLIENT_AUTH_ALIAS: String = "hub-client-auth-es256"

    /** Where a key lives, as far as the app can tell when it creates the key. */
    enum class Backing { STRONGBOX, TEE_OR_SOFTWARE, EXISTING }

    /** Signer for [alias], creating the key on first use; [backing] reports what happened. */
    @Synchronized
    fun signer(alias: String, keyId: String? = null, backing: ((Backing) -> Unit)? = null): EcKeyJwsSigner {
        val keyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        val existing = keyStore.getEntry(alias, null) as? KeyStore.PrivateKeyEntry
        if (existing != null) {
            backing?.invoke(Backing.EXISTING)
            return EcKeyJwsSigner(existing.privateKey, existing.certificate.publicKey as ECPublicKey, keyId)
        }
        val (pair, where) = try {
            generate(alias, strongBox = true) to Backing.STRONGBOX
        } catch (e: StrongBoxUnavailableException) {
            generate(alias, strongBox = false) to Backing.TEE_OR_SOFTWARE
        }
        backing?.invoke(where)
        return EcKeyJwsSigner(pair.first, pair.second, keyId)
    }

    /** Deletes the key [alias] (no error when it does not exist). */
    @Synchronized
    fun delete(alias: String) {
        KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }.deleteEntry(alias)
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
