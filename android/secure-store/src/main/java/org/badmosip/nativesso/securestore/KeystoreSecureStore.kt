// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.securestore

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import java.io.File
import java.security.KeyStore
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey

/**
 * The Android side: an AES-256 key in Android Keystore and the store folder in `noBackupFilesDir`
 * (never part of Auto Backup or device transfer, on top of the app's `allowBackup=false` and its
 * data-extraction rules).
 */
object KeystoreSecureStore {
    private const val ANDROID_KEYSTORE = "AndroidKeyStore"

    /** A store in `noBackupFilesDir/<folder>`, encrypted with the Keystore key [keyAlias]. */
    fun create(context: Context, folder: String = "secure-store", keyAlias: String = "secure-store-aes"): EncryptedFileStore =
        EncryptedFileStore(File(context.noBackupFilesDir, folder), AesGcmAead { aesKey(keyAlias) })

    /**
     * Returns the AES-256-GCM key [alias], creating it on first use: encrypt and decrypt only,
     * random IVs enforced, no export (Android Keystore never releases key material). StrongBox holds
     * it when the device has one; otherwise the TEE does (on an emulator, software).
     */
    @Synchronized
    fun aesKey(alias: String): SecretKey {
        val keyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        (keyStore.getKey(alias, null) as? SecretKey)?.let { return it }
        return try {
            generate(alias, strongBox = true)
        } catch (e: StrongBoxUnavailableException) {
            generate(alias, strongBox = false)
        }
    }

    private fun generate(alias: String, strongBox: Boolean): SecretKey {
        val spec = KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setKeySize(256)
            .setRandomizedEncryptionRequired(true)
            .setIsStrongBoxBacked(strongBox)
            .build()
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
            .apply { init(spec) }
            .generateKey()
    }
}
