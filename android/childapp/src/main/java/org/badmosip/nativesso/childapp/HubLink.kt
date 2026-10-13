// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import org.badmosip.nativesso.contract.FamilySsoContract
import org.badmosip.nativesso.contract.SsoBundle
import org.badmosip.nativesso.oidc.DpopProofSource

/**
 * The Native SSO inputs Hub shares (Native SSO 1.0 section 4.1). Keep in memory only; [toString]
 * hides both tokens.
 */
class HubShare(val issuer: String, val idToken: String, val deviceSecret: String, val scope: String) {
    override fun toString(): String = "HubShare(issuer=$issuer, scope=$scope)"

    companion object {
        /** The share inside [bundle], or null when a field is missing (treated as nothing shared). */
        fun of(bundle: SsoBundle?): HubShare? {
            if (bundle == null) return null
            val issuer = bundle.issuer?.takeIf { it.isNotBlank() } ?: return null
            val idToken = bundle.idToken?.takeIf { it.isNotBlank() } ?: return null
            val secret = bundle.deviceSecret?.takeIf { it.isNotBlank() } ?: return null
            return HubShare(issuer, idToken, secret, bundle.scope.orEmpty())
        }
    }
}

/** What Health Card can ask Hub while it is connected. Refusals are [HubException]s. */
interface HubCalls {
    /** Hub's shared sign-in, or null when nobody is signed in or sharing is off. */
    fun share(): HubShare?

    /** DPoP proofs signed by Hub's key, the key the device secret is bound to; used for the token exchange only. */
    val proofs: DpopProofSource
}

/** Health Card's access to Hub, so the sign-in logic runs in tests with a fake Hub. */
interface HubLink {
    /** Connects, runs [block], disconnects. */
    fun <T> use(block: (HubCalls) -> T): T

    /** Whether Hub shares a sign-in; never throws. */
    fun status(): HubStatus
}

/** [HubLink] over Hub's family binder service. Blocking: call from a background thread. */
class HubConnectionLink(private val connection: HubConnection) : HubLink {
    override fun <T> use(block: (HubCalls) -> T): T = connection.use { service ->
        block(
            object : HubCalls {
                override fun share(): HubShare? {
                    val version = HubConnection.call { service.version }
                    if (version != FamilySsoContract.VERSION) {
                        throw HubException(HubException.Reason.VERSION_MISMATCH, "Hub speaks contract version $version")
                    }
                    return HubShare.of(HubConnection.call { service.ssoBundle })
                }

                override val proofs: DpopProofSource = HubDpopProofSource(service)
            },
        )
    }

    override fun status(): HubStatus = connection.status()
}
