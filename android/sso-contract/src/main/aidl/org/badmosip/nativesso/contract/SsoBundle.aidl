// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.contract;

/**
 * What the Super App shares with a family app so that app can run the Native SSO token exchange
 * (OpenID Connect Native SSO for Mobile Apps 1.0, section 4). It never holds the Super App's
 * access token, and never any private key.
 */
parcelable SsoBundle {
    /** Issuer the tokens come from; the Child App sends it as the token-exchange audience. */
    String issuer;
    /** The Super App's id_token; it carries sid and ds_hash. Sent as subject_token. */
    String idToken;
    /** The device secret from the Super App's token response. Sent as actor_token. */
    String deviceSecret;
    /** Space-separated scope the Super App was granted; the Child App may ask for a subset only. */
    String scope;
    /** When the device secret stops being usable, in seconds since the epoch (0 if unknown). */
    long expiresAtEpochSec;
}
