// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.keys

import org.badmosip.nativesso.oidc.DpopProofFactory
import org.badmosip.nativesso.oidc.DpopRequest
import org.badmosip.nativesso.oidc.JwkThumbprint
import org.badmosip.nativesso.oidc.SoftwareEcSigner
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/** Hub's DPoP key after "Sign out of all apps": deleted, and a new one at the next use. */
class ReplaceableSignerTest {
    private var made = 0
    private var deleted = 0
    private val signer = ReplaceableSigner(load = { made++; SoftwareEcSigner.generate() }, delete = { deleted++ })

    @Test
    fun `the key is made on first use and kept`() {
        assertEquals(0, made)
        val jwk = signer.publicJwk
        DpopProofFactory(signer).proof(DpopRequest("POST", "http://localhost:38088/oauth2/token"))
        assertEquals(jwk, signer.publicJwk)
        assertEquals(1, made)
        assertEquals("ES256", signer.algorithm)
    }

    @Test
    fun `destroy deletes the key and the next proof uses a new one`() {
        val before = JwkThumbprint.compute(signer.publicJwk)
        signer.destroy()
        assertEquals(1, deleted)
        val after = JwkThumbprint.compute(signer.publicJwk)
        assertNotEquals(before, after)
        assertEquals(2, made)
    }
}
