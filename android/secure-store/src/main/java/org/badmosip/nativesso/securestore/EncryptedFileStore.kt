// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.securestore

import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.security.GeneralSecurityException

/**
 * One encrypted file per blob in [directory].
 *
 * File layout (lengths in bytes):
 *
 * | Bytes | Content |
 * |---|---|
 * | 4 | magic `PS3S` |
 * | 1 | format version, 1 |
 * | 1 | IV length n (12 for AES-GCM) |
 * | n | IV |
 * | rest | AES-GCM ciphertext with its 16-byte tag |
 *
 * The associated data is the magic, the version byte and the blob name, so a file copied over
 * another name, or a changed version byte, fails the tag check. Writes go to a temporary file that
 * is then moved over the old file, so a crash leaves the old blob or the new one, never half of one.
 */
class EncryptedFileStore(private val directory: File, private val aead: Aead) : BlobStore {

    override fun read(name: String): ByteArray? {
        requireValidName(name)
        val file = fileOf(name)
        val bytes = try {
            if (!file.isFile) return null
            file.readBytes()
        } catch (e: IOException) {
            throw SecureStoreException("blob $name could not be read", e)
        }
        if (bytes.size < HEADER_SIZE || !bytes.copyOfRange(0, MAGIC.size).contentEquals(MAGIC)) {
            throw SecureStoreException("blob $name is not in the store format")
        }
        if (bytes[MAGIC.size] != VERSION) throw SecureStoreException("blob $name has unknown format version ${bytes[MAGIC.size]}")
        val ivLength = bytes[MAGIC.size + 1].toInt() and 0xff
        if (ivLength == 0 || bytes.size < HEADER_SIZE + ivLength + TAG_BYTES) {
            throw SecureStoreException("blob $name is truncated")
        }
        val iv = bytes.copyOfRange(HEADER_SIZE, HEADER_SIZE + ivLength)
        val ciphertext = bytes.copyOfRange(HEADER_SIZE + ivLength, bytes.size)
        return try {
            aead.open(Aead.Sealed(iv, ciphertext), associatedData(name))
        } catch (e: GeneralSecurityException) {
            throw SecureStoreException("blob $name failed decryption (changed, or its key is gone)", e)
        }
    }

    override fun write(name: String, data: ByteArray) {
        requireValidName(name)
        val sealed = try {
            aead.seal(data, associatedData(name))
        } catch (e: GeneralSecurityException) {
            throw SecureStoreException("blob $name could not be encrypted", e)
        }
        check(sealed.iv.size in 1..255) { "IV length ${sealed.iv.size} does not fit the format" }
        if (!directory.isDirectory && !directory.mkdirs()) throw SecureStoreException("cannot create $directory")
        val target = fileOf(name)
        val temp = File(directory, name + TEMP_SUFFIX)
        try {
            FileOutputStream(temp).use { out ->
                out.write(MAGIC)
                out.write(VERSION.toInt())
                out.write(sealed.iv.size)
                out.write(sealed.iv)
                out.write(sealed.ciphertext)
                out.fd.sync()
            }
            Files.move(temp.toPath(), target.toPath(), StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE)
        } catch (e: IOException) {
            temp.delete()
            throw SecureStoreException("blob $name could not be written", e)
        }
    }

    override fun delete(name: String): Boolean {
        requireValidName(name)
        return fileOf(name).delete()
    }

    override fun contains(name: String): Boolean {
        requireValidName(name)
        return fileOf(name).isFile
    }

    /** The file that holds [name]; for tests and diagnostics (its bytes are ciphertext). */
    fun fileOf(name: String): File = File(directory, name + FILE_SUFFIX)

    private fun associatedData(name: String): ByteArray = MAGIC + VERSION + name.toByteArray(Charsets.UTF_8)

    companion object {
        private val MAGIC = "PS3S".toByteArray(Charsets.US_ASCII)
        private const val VERSION: Byte = 1
        private const val HEADER_SIZE = 6
        private const val TAG_BYTES = 16
        const val FILE_SUFFIX: String = ".bin"
        const val TEMP_SUFFIX: String = ".tmp"
    }
}
