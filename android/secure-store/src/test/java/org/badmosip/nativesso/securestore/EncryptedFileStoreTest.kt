// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.securestore

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey

/** The file format and its checks, with a software AES key in place of the Android Keystore key. */
class EncryptedFileStoreTest {
    @get:Rule
    val folder = TemporaryFolder()

    private val key: SecretKey = KeyGenerator.getInstance("AES").apply { init(256) }.generateKey()
    private val dir: File by lazy { File(folder.root, "store") }
    private val store by lazy { EncryptedFileStore(dir, AesGcmAead { key }) }
    private val secret = "eyJhbGciOiJQUzI1NiJ9.id-token-payload.signature".toByteArray()

    @Test
    fun `round trip returns the bytes written`() {
        store.write("session", secret)
        assertArrayEquals(secret, store.read("session"))
        assertTrue(store.contains("session"))
    }

    @Test
    fun `missing blob reads as null`() {
        assertNull(store.read("session"))
        assertFalse(store.contains("session"))
    }

    @Test
    fun `file holds ciphertext only`() {
        store.write("session", secret)
        val bytes = store.fileOf("session").readBytes()
        assertFalse("plaintext must not appear in the file", String(bytes, Charsets.ISO_8859_1).contains("eyJhbGci"))
        assertEquals("PS3S", String(bytes.copyOfRange(0, 4), Charsets.US_ASCII))
        assertEquals(1, bytes[4].toInt())
        assertEquals(12, bytes[5].toInt())
        assertEquals(6 + 12 + secret.size + 16, bytes.size)
    }

    @Test
    fun `every write uses a new IV`() {
        store.write("a", secret)
        val first = store.fileOf("a").readBytes()
        store.write("a", secret)
        val second = store.fileOf("a").readBytes()
        assertNotEquals(first.copyOfRange(6, 18).toList(), second.copyOfRange(6, 18).toList())
    }

    @Test
    fun `overwrite replaces and leaves no temporary file`() {
        store.write("session", "old".toByteArray())
        store.write("session", "new".toByteArray())
        assertArrayEquals("new".toByteArray(), store.read("session"))
        assertEquals(listOf("session.bin"), dir.list()!!.toList())
    }

    @Test
    fun `a changed ciphertext byte fails`() {
        store.write("session", secret)
        val file = store.fileOf("session")
        val bytes = file.readBytes()
        bytes[bytes.size - 20] = (bytes[bytes.size - 20].toInt() xor 1).toByte()
        file.writeBytes(bytes)
        assertThrows(SecureStoreException::class.java) { store.read("session") }
    }

    @Test
    fun `a changed IV fails`() {
        store.write("session", secret)
        val file = store.fileOf("session")
        val bytes = file.readBytes()
        bytes[7] = (bytes[7].toInt() xor 1).toByte()
        file.writeBytes(bytes)
        assertThrows(SecureStoreException::class.java) { store.read("session") }
    }

    @Test
    fun `a file copied under another name fails`() {
        store.write("session", secret)
        store.fileOf("session").copyTo(store.fileOf("pending-login"))
        assertThrows(SecureStoreException::class.java) { store.read("pending-login") }
    }

    @Test
    fun `wrong magic, unknown version and truncation fail`() {
        val file = store.fileOf("x")
        store.write("x", secret)
        val good = file.readBytes()

        file.writeBytes(good.copyOf().also { it[0] = 'X'.code.toByte() })
        assertThrows(SecureStoreException::class.java) { store.read("x") }

        file.writeBytes(good.copyOf().also { it[4] = 2 })
        assertThrows(SecureStoreException::class.java) { store.read("x") }

        file.writeBytes(good.copyOfRange(0, 6 + 12 + 10))
        assertThrows(SecureStoreException::class.java) { store.read("x") }

        file.writeBytes(good.copyOfRange(0, 3))
        assertThrows(SecureStoreException::class.java) { store.read("x") }
    }

    @Test
    fun `another key cannot read the blob`() {
        store.write("session", secret)
        val otherKey = KeyGenerator.getInstance("AES").apply { init(256) }.generateKey()
        val other = EncryptedFileStore(dir, AesGcmAead { otherKey })
        assertThrows(SecureStoreException::class.java) { other.read("session") }
    }

    @Test
    fun `delete removes the blob`() {
        store.write("session", secret)
        assertTrue(store.delete("session"))
        assertFalse(store.delete("session"))
        assertNull(store.read("session"))
    }

    @Test
    fun `empty blob round trips`() {
        store.write("empty", ByteArray(0))
        assertArrayEquals(ByteArray(0), store.read("empty"))
    }

    @Test
    fun `names outside the allowed set are refused`() {
        for (bad in listOf("", "../x", "a/b", "a\\b", "x.tmp", ".", "..", "é", "a".repeat(65))) {
            assertThrows("name \"$bad\"", IllegalArgumentException::class.java) { store.write(bad, secret) }
        }
        store.write("Ok_name-1.v2", secret)
        assertArrayEquals(secret, store.read("Ok_name-1.v2"))
    }
}
