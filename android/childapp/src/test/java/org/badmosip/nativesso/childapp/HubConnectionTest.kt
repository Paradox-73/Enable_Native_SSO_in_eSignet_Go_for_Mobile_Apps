// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import android.app.Application
import android.content.ComponentName
import androidx.test.core.app.ApplicationProvider
import org.badmosip.nativesso.contract.FamilySsoContract
import org.badmosip.nativesso.contract.IFamilySso
import org.badmosip.nativesso.contract.SsoBundle
import org.badmosip.nativesso.oidc.DpopRequest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import java.util.concurrent.atomic.AtomicReference

/**
 * Health Card's side of the family binder: the explicit bind intent, the timeout, and how each of
 * Hub's answers and refusals reaches the screen. Hub is a fake [IFamilySso.Stub] registered with
 * Robolectric for exactly [HubConnection.bindIntent].
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class HubConnectionTest {
    private val app: Application = ApplicationProvider.getApplicationContext()
    private val hubComponent = ComponentName(FamilySsoContract.SUPER_APP_PACKAGE, FamilySsoContract.SERVICE_CLASS)

    private class FakeHub : IFamilySso.Stub() {
        var contractVersion = FamilySsoContract.VERSION
        var bundle: SsoBundle? = null
        var refusal: RuntimeException? = null
        val proofRequests = mutableListOf<List<String?>>()

        override fun getVersion(): Int {
            refusal?.let { throw it }
            return contractVersion
        }

        override fun getSsoBundle(): SsoBundle? {
            refusal?.let { throw it }
            return bundle
        }

        override fun signDpopProof(htm: String?, htu: String?, ath: String?, nonce: String?): String {
            refusal?.let { throw it }
            proofRequests += listOf(htm, htu, ath, nonce)
            return "proof-for-$htm"
        }
    }

    private val hub = FakeHub()

    /** Runs [block] on a worker thread: the test thread is the main thread, where `use` refuses to run. */
    private fun <T> offMain(block: () -> T): T {
        val result = AtomicReference<Result<T>>()
        Thread { result.set(runCatching(block)) }.apply { start(); join() }
        return result.get().getOrThrow()
    }

    @Before
    fun registerHub() {
        shadowOf(app).setComponentNameAndServiceForBindServiceForIntent(HubConnection.bindIntent(), hubComponent, hub)
        shadowOf(app).setBindServiceCallsOnServiceConnectedDirectly(true)
    }

    private fun sharedBundle() = SsoBundle().apply {
        issuer = "http://localhost:38088"
        idToken = "hub-id-token-value"
        deviceSecret = "device-secret-value"
        scope = "openid profile device_sso"
        expiresAtEpochSec = 0
    }

    @Test
    fun `bind intent is explicit to Hub's service with the family action`() {
        val intent = HubConnection.bindIntent()
        assertEquals(FamilySsoContract.ACTION_BIND, intent.action)
        assertEquals(hubComponent, intent.component)
    }

    @Test
    fun `shared sign-in is available and the connection is released`() {
        hub.bundle = sharedBundle()
        val status = offMain { HubConnection(app).status() }
        assertTrue(status is HubStatus.Available)
        val bundle = (status as HubStatus.Available).bundle
        assertEquals("hub-id-token-value", bundle.idToken)
        assertEquals("device-secret-value", bundle.deviceSecret)
        assertFalse(status.toString().contains("value"))
        assertTrue(shadowOf(app).boundServiceConnections.isEmpty())
    }

    @Test
    fun `no bundle means not signed in`() {
        assertEquals(HubStatus.Unavailable(HubException.Reason.NOT_SIGNED_IN), offMain { HubConnection(app).status() })
    }

    @Test
    fun `Hub's coded refusals map to reasons`() {
        val cases = mapOf(
            FamilySsoContract.ERROR_CALLER_NOT_ALLOWED to HubException.Reason.CALLER_REFUSED,
            FamilySsoContract.ERROR_NOT_SIGNED_IN to HubException.Reason.NOT_SIGNED_IN,
            FamilySsoContract.ERROR_RATE_LIMITED to HubException.Reason.RATE_LIMITED,
        )
        for ((code, reason) in cases) {
            hub.refusal = FamilySsoContract.exceptionFor(code, "refused")
            assertEquals(HubStatus.Unavailable(reason), offMain { HubConnection(app).status() })
        }
    }

    @Test
    fun `Android refusing the bind (no family permission) is BIND_REFUSED`() {
        shadowOf(app).setThrowInBindService(SecurityException("Not allowed to bind to service Intent"))
        assertEquals(HubStatus.Unavailable(HubException.Reason.BIND_REFUSED), offMain { HubConnection(app).status() })
    }

    @Test
    fun `Hub not installed is HUB_MISSING`() {
        shadowOf(app).declareComponentUnbindable(hubComponent)
        assertEquals(HubStatus.Unavailable(HubException.Reason.HUB_MISSING), offMain { HubConnection(app).status() })
    }

    @Test
    fun `no connection within the timeout is TIMEOUT`() {
        // The connection callback is posted to the main looper, which this blocked test thread never runs.
        shadowOf(app).setBindServiceCallsOnServiceConnectedDirectly(false)
        val started = System.nanoTime()
        assertEquals(HubStatus.Unavailable(HubException.Reason.TIMEOUT), offMain { HubConnection(app, timeoutMillis = 200).status() })
        assertTrue((System.nanoTime() - started) / 1_000_000 >= 200)
        assertTrue(shadowOf(app).boundServiceConnections.isEmpty())
    }

    @Test
    fun `another contract version is VERSION_MISMATCH`() {
        hub.contractVersion = FamilySsoContract.VERSION + 1
        hub.bundle = sharedBundle()
        assertEquals(HubStatus.Unavailable(HubException.Reason.VERSION_MISMATCH), offMain { HubConnection(app).status() })
    }

    @Test
    fun `proof source forwards the request and surfaces Hub's htu refusal`() {
        val proof = offMain { HubConnection(app).use { HubDpopProofSource(it).proof(DpopRequest("POST", "http://localhost:38088/token", null, "n-1")) } }
        assertEquals("proof-for-POST", proof)
        assertEquals(listOf(listOf("POST", "http://localhost:38088/token", null, "n-1")), hub.proofRequests)

        hub.refusal = FamilySsoContract.exceptionFor(FamilySsoContract.ERROR_HTU_NOT_ALLOWED, "not allowed")
        val e = assertThrows(HubException::class.java) {
            offMain { HubConnection(app).use { HubDpopProofSource(it).proof(DpopRequest("POST", "https://attacker.example/token")) } }
        }
        assertEquals(HubException.Reason.HTU_NOT_ALLOWED, e.reason)
    }

    @Test
    fun `use on the main thread is refused instead of waiting out the timeout`() {
        assertThrows(IllegalStateException::class.java) { HubConnection(app).use { it.version } }
    }

    @Test
    fun `an exception without a code is not swallowed`() {
        hub.refusal = IllegalArgumentException("ath must be 43 base64url characters")
        assertThrows(IllegalArgumentException::class.java) { offMain { HubConnection(app).use { it.ssoBundle } } }
        assertTrue(FamilySsoContract.errorCodeOf(IllegalStateException("plain")) == null)
        assertTrue(FamilySsoContract.errorCodeOf(SecurityException("FAMILY_SSO_ERROR x: y")) == null)
    }
}
