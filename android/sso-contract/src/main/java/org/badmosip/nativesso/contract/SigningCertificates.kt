// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.contract

import android.content.pm.PackageManager
import java.security.MessageDigest

/** Reads app signing certificates, the identity the family permission and caller checks rely on. */
object SigningCertificates {
    /**
     * SHA-256 digests of the certificates [packageName] is currently signed with (after any key
     * rotation), the same value `apksigner verify --print-certs` prints. Empty if the package is not
     * visible to the caller (Android 11+ needs a <queries> entry). Callers compare whole sets: with
     * several signers, sharing one certificate is not enough.
     */
    fun sha256Of(packageManager: PackageManager, packageName: String): List<ByteArray> {
        val info = try {
            packageManager.getPackageInfo(packageName, PackageManager.GET_SIGNING_CERTIFICATES)
        } catch (e: PackageManager.NameNotFoundException) {
            return emptyList()
        }
        val signingInfo = info.signingInfo ?: return emptyList()
        val certificates = if (signingInfo.hasMultipleSigners()) signingInfo.apkContentsSigners else signingInfo.signingCertificateHistory
        val digest = MessageDigest.getInstance("SHA-256")
        // signingCertificateHistory lists the oldest certificate first; the current one is last.
        val current = if (signingInfo.hasMultipleSigners()) certificates.toList() else listOfNotNull(certificates.lastOrNull())
        return current.map { digest.digest(it.toByteArray()) }
    }

    /** Lower-case hex with colons, as shown to the user ("a7:87:b1:..."). */
    fun format(digest: ByteArray): String = digest.joinToString(":") { "%02x".format(it) }
}
