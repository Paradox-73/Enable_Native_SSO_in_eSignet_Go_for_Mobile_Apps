// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.sso

import android.app.Service
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Binder
import android.os.IBinder
import org.badmosip.nativesso.contract.FamilySsoContract
import org.badmosip.nativesso.contract.IFamilySso
import org.badmosip.nativesso.contract.SigningCertificates
import org.badmosip.nativesso.contract.SsoBundle

/** The Application of an app that serves [FamilySsoService] (Hub's `HubApp`; a test application in tests). */
interface FamilySsoHost {
    /** Created on first use; may touch Android Keystore, so it is first read on a binder thread. */
    val familySsoGate: FamilySsoGate
}

/**
 * Hub's family single sign-on service ([IFamilySso]). The manifest guards it with
 * [FamilySsoContract.PERMISSION] (signature level); every call is then checked again by
 * [FamilySsoGate] with `Binder.getCallingUid()`. Binder calls arrive on binder threads, so the
 * Keystore and file work in the gate never runs on Hub's main thread.
 */
class FamilySsoService : Service() {
    private val gate: FamilySsoGate get() = (application as FamilySsoHost).familySsoGate

    private val binder = object : IFamilySso.Stub() {
        override fun getVersion(): Int = guarded { gate.version(Binder.getCallingUid()) }

        override fun getSsoBundle(): SsoBundle? = guarded {
            gate.bundle(Binder.getCallingUid())?.let { share ->
                SsoBundle().apply {
                    issuer = share.issuer
                    idToken = share.idToken
                    deviceSecret = share.deviceSecret
                    scope = share.scope
                    expiresAtEpochSec = share.expiresAtEpochSec
                }
            }
        }

        override fun signDpopProof(htm: String?, htu: String?, ath: String?, nonce: String?): String = guarded {
            gate.signProof(Binder.getCallingUid(), htm, htu, ath, nonce)
        }
    }

    override fun onBind(intent: Intent?): IBinder? =
        if (intent?.action == FamilySsoContract.ACTION_BIND) binder else null

    /** Turns a refusal into the coded exception of the contract ([FamilySsoContract.exceptionFor]). */
    private inline fun <T> guarded(call: () -> T): T = try {
        call()
    } catch (e: FamilySsoException) {
        throw FamilySsoContract.exceptionFor(e.code, e.message.orEmpty())
    }
}

/** [CallerLookup] over `PackageManager`: the packages of a uid and their current signing certificates. */
class AndroidCallerLookup(private val packageManager: PackageManager) : CallerLookup {
    override fun packagesFor(uid: Int): List<CallerPackage> =
        packageManager.getPackagesForUid(uid).orEmpty().map { name ->
            CallerPackage(name, SigningCertificates.sha256Of(packageManager, name))
        }
}
