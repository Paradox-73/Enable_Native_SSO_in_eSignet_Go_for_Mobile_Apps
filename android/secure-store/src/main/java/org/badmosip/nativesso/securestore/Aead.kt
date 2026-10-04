// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.securestore

import java.security.GeneralSecurityException
import javax.crypto.Cipher
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** Authenticated encryption with associated data: a changed byte anywhere makes [open] fail. */
interface Aead {
    /** Encrypts [plaintext]; the cipher picks a fresh random IV each time. */
    fun seal(plaintext: ByteArray, associatedData: ByteArray): Sealed

    /** Decrypts and checks the tag; throws [GeneralSecurityException] when anything was changed. */
    fun open(sealed: Sealed, associatedData: ByteArray): ByteArray

    /** IV and ciphertext with the 16-byte GCM tag at its end. */
    class Sealed(val iv: ByteArray, val ciphertext: ByteArray)
}

/**
 * AES-GCM with a 128-bit tag. [key] is called on every operation, so a Keystore key created after
 * this object is still found.
 *
 * The IV is never chosen here: Android Keystore keys require randomized encryption and refuse a
 * caller-supplied IV, so the cipher generates it (12 random bytes) and [Aead.Sealed.iv] carries it.
 */
class AesGcmAead(private val key: () -> SecretKey) : Aead {
    override fun seal(plaintext: ByteArray, associatedData: ByteArray): Aead.Sealed {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key())
        cipher.updateAAD(associatedData)
        val ciphertext = cipher.doFinal(plaintext)
        return Aead.Sealed(cipher.iv, ciphertext)
    }

    override fun open(sealed: Aead.Sealed, associatedData: ByteArray): ByteArray {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(TAG_BITS, sealed.iv))
        cipher.updateAAD(associatedData)
        return cipher.doFinal(sealed.ciphertext)
    }

    companion object {
        const val TRANSFORMATION: String = "AES/GCM/NoPadding"
        const val TAG_BITS: Int = 128
    }
}
