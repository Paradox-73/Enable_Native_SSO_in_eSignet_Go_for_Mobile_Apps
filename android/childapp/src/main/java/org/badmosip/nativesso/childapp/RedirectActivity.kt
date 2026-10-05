// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.util.Log

/**
 * Receives `org.badmosip.nativesso.childapp://oauth/callback?...` from the browser after the consent
 * login and hands it to [MainActivity], then closes. The `state` check and the code exchange happen
 * in [org.badmosip.nativesso.childapp.auth.CardAuthenticator.completeConsentLogin], which reads the
 * pending login from the encrypted store, so this works after a process death too.
 */
class RedirectActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val data = intent?.data
        if (data != null) {
            Log.i(CardServices.TAG, "redirect received")
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
        const val ACTION_REDIRECT: String = "org.badmosip.nativesso.childapp.action.REDIRECT"
    }
}
