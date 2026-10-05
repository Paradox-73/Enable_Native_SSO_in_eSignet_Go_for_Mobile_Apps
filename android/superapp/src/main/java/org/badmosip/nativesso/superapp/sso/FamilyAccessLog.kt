// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.sso

import com.nimbusds.jose.util.JSONObjectUtils
import org.badmosip.nativesso.securestore.BlobStore
import org.badmosip.nativesso.securestore.SecureStoreException
import org.badmosip.nativesso.superapp.auth.HubAuthenticator
import java.text.ParseException
import java.time.Instant

/** One row of "Apps using this sign-in": which package last received the shared sign-in, and when. */
data class FamilyAccess(val packageName: String, val lastUsed: Instant)

/**
 * "Apps using this sign-in": package name and last time, one row per package, kept in Hub's
 * encrypted store (blob [HubAuthenticator.FAMILY_ACCESS], cleared at sign-out). It holds no token.
 */
class FamilyAccessLog(private val store: BlobStore) {
    @Synchronized
    fun record(packageName: String, at: Instant) {
        val rows = read().associateBy { it.packageName }.toMutableMap()
        rows[packageName] = FamilyAccess(packageName, at)
        val json = JSONObjectUtils.toJSONString(
            linkedMapOf<String, Any?>(
                "v" to 1L,
                "apps" to rows.values.sortedBy { it.packageName }.map { mapOf("package" to it.packageName, "last_used" to it.lastUsed.epochSecond) },
            ),
        )
        store.write(HubAuthenticator.FAMILY_ACCESS, json.toByteArray(Charsets.UTF_8))
    }

    /** The rows, most recent first; empty when nothing was shared or the blob is unreadable. */
    @Synchronized
    fun list(): List<FamilyAccess> = read().sortedByDescending { it.lastUsed }

    private fun read(): List<FamilyAccess> = try {
        val bytes = store.read(HubAuthenticator.FAMILY_ACCESS) ?: return emptyList()
        val m = JSONObjectUtils.parse(String(bytes, Charsets.UTF_8))
        if ((m["v"] as? Number)?.toLong() != 1L) throw ParseException("unknown version", 0)
        (m["apps"] as? List<*>).orEmpty().mapNotNull { row ->
            val r = row as? Map<*, *> ?: return@mapNotNull null
            val pkg = r["package"] as? String ?: return@mapNotNull null
            val at = (r["last_used"] as? Number)?.toLong() ?: return@mapNotNull null
            FamilyAccess(pkg, Instant.ofEpochSecond(at))
        }
    } catch (e: ParseException) {
        store.delete(HubAuthenticator.FAMILY_ACCESS)
        emptyList()
    } catch (e: SecureStoreException) {
        store.delete(HubAuthenticator.FAMILY_ACCESS)
        emptyList()
    }
}
