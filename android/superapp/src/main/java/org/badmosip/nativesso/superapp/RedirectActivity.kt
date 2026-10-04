// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.util.Log

/**
 * Receives `org.badmosip.nativesso.superapp://oauth/callback?...` from the browser and hands it to
 * [MainActivity], then closes. It shows nothing and does no work itself: the `state` check and the
 * code exchange happen in [org.badmosip.nativesso.superapp.auth.HubAuthenticator.completeLogin],
 * which reads the pending login from the encrypted store, so this works even when Android killed
 * Hub's process while the user was in the browser.
 */
class RedirectActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val data = intent?.data
        if (data != null) {
            Log.i(HubServices.TAG, "redirect received")
            startActivity(
                Intent(this, MainActivity::class.java)
                    .setAction(ACTION_REDIRECT)
                    .setData(data)
                    .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP),
            )
        }
        finish()
    }

    companion object {
        /** Action of the intent [MainActivity] gets with the redirect URI as data. */
        const val ACTION_REDIRECT: String = "org.badmosip.nativesso.superapp.action.REDIRECT"
    }
}
