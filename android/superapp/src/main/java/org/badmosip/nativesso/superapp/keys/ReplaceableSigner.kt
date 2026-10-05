// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.keys

import org.badmosip.nativesso.oidc.JwsSigner

/**
 * A [JwsSigner] whose key can be thrown away: Hub's DPoP key. "Sign out of all apps" calls
 * [destroy], which deletes the key ([delete]); the next use makes a new one ([load]). Every token
 * and device secret bound to the old key (Hub's and, through the token exchange, Health Card's) then
 * needs a proof nobody can sign any more.
 */
class ReplaceableSigner(private val load: () -> JwsSigner, private val delete: () -> Unit) : JwsSigner {
    @Volatile private var current: JwsSigner? = null

    private fun key(): JwsSigner = current ?: synchronized(this) { current ?: load().also { current = it } }

    override val algorithm: String get() = key().algorithm
    override val publicJwk: Map<String, String> get() = key().publicJwk
    override val keyId: String? get() = key().keyId

    override fun sign(signingInput: ByteArray): ByteArray = key().sign(signingInput)

    /** Deletes the key; the next call makes a new one. */
    @Synchronized
    fun destroy() {
        current = null
        delete()
    }
}
