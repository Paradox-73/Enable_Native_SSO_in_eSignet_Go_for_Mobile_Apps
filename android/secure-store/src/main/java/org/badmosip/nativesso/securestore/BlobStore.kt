// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.securestore

/**
 * Named byte blobs that survive app restarts. The apps keep tokens and in-flight login values in
 * it; [EncryptedFileStore] is the real implementation, tests may use a map.
 *
 * Names are short identifiers: letters, digits, `.`, `_` and `-`, at most 64 characters.
 */
interface BlobStore {
    /** The blob, or null when none is stored under [name]. Throws [SecureStoreException] when it cannot be read back. */
    fun read(name: String): ByteArray?

    /** Stores [data] under [name], replacing what was there. */
    fun write(name: String, data: ByteArray)

    /** Removes the blob; true when one existed. */
    fun delete(name: String): Boolean

    /** True when a blob is stored under [name] (without decrypting it). */
    fun contains(name: String): Boolean
}

/** A blob exists but cannot be decrypted or parsed: tampered with, truncated, or its key is gone. */
class SecureStoreException(message: String, cause: Throwable? = null) : Exception(message, cause)

/** Checks a blob name against the rule in [BlobStore]. */
internal fun requireValidName(name: String) {
    require(name.length in 1..64 && name.all { (it in 'a'..'z') || (it in 'A'..'Z') || (it in '0'..'9') || it in "._-" }) {
        "blob name must be 1 to 64 characters of [A-Za-z0-9._-]"
    }
    require(name != "." && name != ".." && !name.endsWith(EncryptedFileStore.TEMP_SUFFIX)) { "reserved blob name" }
}
