// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.sso

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import org.badmosip.nativesso.oidc.JwkThumbprint
import org.badmosip.nativesso.superapp.HubApp
import org.badmosip.nativesso.superapp.HubServices
import org.badmosip.nativesso.superapp.auth.HubAuthenticator
import org.badmosip.nativesso.superapp.auth.TokenBundle
import java.time.Instant
import java.time.temporal.ChronoUnit

/**
 * Debug builds only (see `src/debug/AndroidManifest.xml`): `op=seed` stores a made-up shared session
 * ([SEED_ID_TOKEN], [SEED_DEVICE_SECRET], endpoints under the build's issuer), `op=clear` signs out.
 * Used by `android/scripts/run-family-tests.sh`; the values are placeholders, not tokens.
 */
class DebugSessionSeeder : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val op = intent.getStringExtra("op")
        val pending = goAsync()
        Thread {
            try {
                val services = (context.applicationContext as HubApp).services
                when (op) {
                    "seed" -> services.store.write(HubAuthenticator.SESSION, seedSession(services).toJson())
                    "clear" -> services.authenticator.signOut()
                    else -> Log.w(HubServices.TAG, "debug seeder: unknown op $op")
                }
                Log.i(HubServices.TAG, "debug seeder: $op done")
                pending.resultCode = if (op == "seed" || op == "clear") 1 else 0
            } finally {
                pending.finish()
            }
        }.start()
    }

    private fun seedSession(services: HubServices): TokenBundle {
        val issuer = services.config.issuer.trimEnd('/')
        val now = Instant.now().truncatedTo(ChronoUnit.SECONDS)
        return TokenBundle(
            issuer = services.config.issuer,
            clientId = services.config.clientId,
            subject = "debug-seed-subject",
            sessionId = "debug-seed-sid",
            accessToken = "debug-seed.access-token",
            tokenType = "DPoP",
            accessTokenExpiresAt = now.plusSeconds(3600),
            idToken = SEED_ID_TOKEN,
            idTokenExpiresAt = now.plusSeconds(120),
            refreshToken = null,
            deviceSecret = SEED_DEVICE_SECRET,
            scope = "openid profile device_sso",
            dpopKeyThumbprint = JwkThumbprint.compute(services.dpopSigner.publicJwk),
            displayName = "Debug Seed",
            signedInAt = now,
            tokenEndpoint = "$issuer/oauth2/token",
            userinfoEndpoint = "$issuer/oauth2/userinfo",
        )
    }

    companion object {
        const val SEED_ID_TOKEN: String = "debug-seed.id-token"
        const val SEED_DEVICE_SECRET: String = "debug-seed.device-secret"
    }
}
