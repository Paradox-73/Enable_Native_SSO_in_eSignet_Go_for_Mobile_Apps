// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import android.os.RemoteException
import org.badmosip.nativesso.contract.FamilySsoContract
import org.badmosip.nativesso.contract.IFamilySso
import org.badmosip.nativesso.contract.SsoBundle
import org.badmosip.nativesso.oidc.DpopProofSource
import org.badmosip.nativesso.oidc.DpopRequest
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

/** Why Hub could not be used. */
class HubException(val reason: Reason, message: String) : Exception(message) {
    enum class Reason {
        /** Hub is not installed, or has no family service Android can bind to. */
        HUB_MISSING,

        /** Android refused the bind: this app does not hold the family permission (another certificate). */
        BIND_REFUSED,

        /** Hub's per-call check refused this app (certificate or package not allowed). */
        CALLER_REFUSED,

        /** Nobody is signed in to Hub, or the sign-in is not shared with other apps. */
        NOT_SIGNED_IN,

        /** Hub refused to sign a proof for that method and URL. */
        HTU_NOT_ALLOWED,

        /** Hub saw too many calls from this app; try again in a minute. */
        RATE_LIMITED,

        /** Hub did not connect within the timeout. */
        TIMEOUT,

        /** Hub speaks another contract version. */
        VERSION_MISMATCH,

        /** Hub's process died during the call, or it answered with something unexpected. */
        FAILED,
    }
}

/** What Health Card can tell the user about Hub's sign-in. */
sealed interface HubStatus {
    /** A shared sign-in exists. [bundle] holds tokens: keep it in memory only, never log it. */
    class Available(val bundle: SsoBundle) : HubStatus {
        override fun toString(): String = "Available(issuer=${bundle.issuer}, scope=${bundle.scope})"
    }

    /** No bundle: the reason says what the user can do. */
    data class Unavailable(val reason: HubException.Reason) : HubStatus
}

/**
 * Health Card's client of Hub's family service ([IFamilySso]): binds with an explicit intent to
 * Hub's service class, waits at most [timeoutMillis] for the connection, makes the calls, unbinds.
 * Hub's coded refusals become [HubException]s.
 *
 * Blocking: call from a background thread. The binding callbacks arrive on the main thread, so a
 * call from the main thread would wait out the whole timeout.
 */
class HubConnection(private val context: Context, private val timeoutMillis: Long = DEFAULT_TIMEOUT_MILLIS) {

    /** Binds, runs [block] with the service, unbinds. */
    fun <T> use(block: (IFamilySso) -> T): T {
        val connected = CountDownLatch(1)
        val binder = AtomicReference<IBinder?>()
        val connection = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName?, service: IBinder?) {
                binder.set(service)
                connected.countDown()
            }

            override fun onServiceDisconnected(name: ComponentName?) {}

            override fun onNullBinding(name: ComponentName?) {
                connected.countDown()
            }

            override fun onBindingDied(name: ComponentName?) {
                connected.countDown()
            }
        }
        val bound = try {
            context.bindService(bindIntent(), connection, Context.BIND_AUTO_CREATE)
        } catch (e: SecurityException) {
            throw HubException(HubException.Reason.BIND_REFUSED, "Android refused the bind: ${e.message}")
        }
        try {
            if (!bound) throw HubException(HubException.Reason.HUB_MISSING, "Hub or its family service is not installed")
            if (!connected.await(timeoutMillis, TimeUnit.MILLISECONDS)) {
                throw HubException(HubException.Reason.TIMEOUT, "Hub did not connect within $timeoutMillis ms")
            }
            val service = binder.get()?.let(IFamilySso.Stub::asInterface)
                ?: throw HubException(HubException.Reason.HUB_MISSING, "Hub returned no service")
            return call { block(service) }
        } finally {
            try {
                context.unbindService(connection)
            } catch (e: IllegalArgumentException) {
                // Not bound (bindService returned false); nothing to undo.
            }
        }
    }

    /** Whether Hub has a shared sign-in for this app; never throws. */
    fun status(): HubStatus = try {
        use { service ->
            if (service.version != FamilySsoContract.VERSION) {
                throw HubException(HubException.Reason.VERSION_MISMATCH, "Hub speaks contract version ${service.version}")
            }
            service.ssoBundle?.let { HubStatus.Available(it) } ?: HubStatus.Unavailable(HubException.Reason.NOT_SIGNED_IN)
        }
    } catch (e: HubException) {
        HubStatus.Unavailable(e.reason)
    }

    companion object {
        const val DEFAULT_TIMEOUT_MILLIS: Long = 5_000

        /** The explicit intent to Hub's family service (implicit service intents are not allowed). */
        fun bindIntent(): Intent = Intent(FamilySsoContract.ACTION_BIND)
            .setComponent(ComponentName(FamilySsoContract.SUPER_APP_PACKAGE, FamilySsoContract.SERVICE_CLASS))

        /** Runs one binder call and turns Hub's refusals and a dead Hub into [HubException]s. */
        internal fun <T> call(block: () -> T): T = try {
            block()
        } catch (e: RemoteException) {
            throw HubException(HubException.Reason.FAILED, "Hub's process went away: ${e.message}")
        } catch (e: RuntimeException) {
            val reason = when (FamilySsoContract.errorCodeOf(e)) {
                FamilySsoContract.ERROR_CALLER_NOT_ALLOWED -> HubException.Reason.CALLER_REFUSED
                FamilySsoContract.ERROR_NOT_SIGNED_IN -> HubException.Reason.NOT_SIGNED_IN
                FamilySsoContract.ERROR_HTU_NOT_ALLOWED -> HubException.Reason.HTU_NOT_ALLOWED
                FamilySsoContract.ERROR_RATE_LIMITED -> HubException.Reason.RATE_LIMITED
                else -> throw e
            }
            throw HubException(reason, e.message.orEmpty())
        }
    }
}

/**
 * DPoP proofs from Hub, for the token exchange and userinfo: the device secret is bound to Hub's
 * key (statement task 9 iii.4), so Health Card cannot sign them itself. Use inside
 * [HubConnection.use]; a refusal is thrown as [HubException].
 */
class HubDpopProofSource(private val service: IFamilySso) : DpopProofSource {
    override fun proof(request: DpopRequest): String =
        HubConnection.call { service.signDpopProof(request.htm, request.htu, request.ath, request.nonce) }
}
