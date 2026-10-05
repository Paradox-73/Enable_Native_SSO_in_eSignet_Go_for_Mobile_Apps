// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import android.util.Base64
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.badmosip.nativesso.oidc.DpopRequest
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Health Card binding to the real Hub on a device. Each method is one step of
 * `android/scripts/run-family-tests.sh`, which installs the right pair of APKs and seeds or clears
 * Hub's session through Hub's debug-only seeder before running it:
 *
 * - [familyBind]: family build, seeded Hub: the bundle and a proof for the token endpoint arrive.
 * - [htuAllowList]: family build, seeded Hub: a proof for a foreign URL is refused.
 * - [signedOut]: family build, Hub signed out: no bundle, proofs refused with ERROR_NOT_SIGNED_IN.
 * - [intruderRefused]: intruder build (debug certificate, no permission): Android refuses the bind.
 * - [takeoverRefused]: takeover build (debug certificate, holds the permission): Hub's per-call check refuses it.
 *
 * Run only through the script; on its own, a method fails when the apps are not in that state.
 */
@RunWith(AndroidJUnit4::class)
class FamilySsoBindTest {
    private val context = InstrumentationRegistry.getInstrumentation().targetContext
    private val connection = HubConnection(context, timeoutMillis = 10_000)
    private val tokenEndpoint = BuildConfig.ISSUER.trimEnd('/') + "/oauth2/token"

    @Test
    fun familyBind() {
        val status = connection.status()
        assertTrue("expected a bundle, got $status", status is HubStatus.Available)
        val bundle = (status as HubStatus.Available).bundle
        assertEquals(SEED_ID_TOKEN, bundle.idToken)
        assertEquals(SEED_DEVICE_SECRET, bundle.deviceSecret)
        assertEquals(BuildConfig.ISSUER, bundle.issuer)

        val (first, second) = connection.use { hub ->
            val source = HubDpopProofSource(hub)
            source.proof(DpopRequest("POST", tokenEndpoint)) to source.proof(DpopRequest("POST", tokenEndpoint))
        }
        val claims = claimsOf(first)
        assertEquals("POST", claims.getString("htm"))
        assertEquals(tokenEndpoint, claims.getString("htu"))
        assertNotEquals(claims.getString("jti"), claimsOf(second).getString("jti"))
    }

    @Test
    fun htuAllowList() {
        val e = assertThrows(HubException::class.java) {
            connection.use { HubDpopProofSource(it).proof(DpopRequest("POST", "https://attacker.example/oauth2/token")) }
        }
        assertEquals(HubException.Reason.HTU_NOT_ALLOWED, e.reason)
    }

    @Test
    fun signedOut() {
        assertEquals(HubStatus.Unavailable(HubException.Reason.NOT_SIGNED_IN), connection.status())
        val e = assertThrows(HubException::class.java) {
            connection.use { HubDpopProofSource(it).proof(DpopRequest("POST", tokenEndpoint)) }
        }
        assertEquals(HubException.Reason.NOT_SIGNED_IN, e.reason)
    }

    @Test
    fun intruderRefused() {
        assertEquals(HubStatus.Unavailable(HubException.Reason.BIND_REFUSED), connection.status())
    }

    @Test
    fun takeoverRefused() {
        assertEquals(HubStatus.Unavailable(HubException.Reason.CALLER_REFUSED), connection.status())
    }

    private fun claimsOf(jws: String): JSONObject =
        JSONObject(String(Base64.decode(jws.split('.')[1], Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)))

    private companion object {
        // Placeholders Hub's debug seeder stores (superapp/src/debug/.../DebugSessionSeeder.kt).
        const val SEED_ID_TOKEN = "debug-seed.id-token"
        const val SEED_DEVICE_SECRET = "debug-seed.device-secret"
    }
}
