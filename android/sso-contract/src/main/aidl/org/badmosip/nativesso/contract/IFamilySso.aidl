// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.contract;

import org.badmosip.nativesso.contract.SsoBundle;

/**
 * Single sign-on service of the Super App, bound by apps of the same family.
 *
 * The service is protected by FamilySsoContract.PERMISSION (protectionLevel="signature") and checks
 * the caller's signing certificate and package on every call. Refusals arrive as the exceptions
 * FamilySsoContract.exceptionFor makes (SecurityException for a caller that is not allowed,
 * IllegalStateException otherwise); FamilySsoContract.errorCodeOf reads the code back.
 */
interface IFamilySso {
    /** Contract version the Super App implements (FamilySsoContract.VERSION). */
    int getVersion();

    /** The current sign-in, or null when nobody is signed in to the Super App. */
    @nullable SsoBundle getSsoBundle();

    /**
     * A DPoP proof (RFC 9449) signed with the Super App's device-bound key, whose thumbprint is the
     * cnf.jkt inside the device secret. The Super App builds every claim itself (fresh jti and iat)
     * and signs only POST to its issuer's token endpoint, for the token exchange; the caller binds the
     * tokens it gets to its own key and signs its own proofs after that.
     *
     * @param htm   HTTP method, "POST"
     * @param htu   target URL without query and fragment
     * @param ath   base64url SHA-256 of the access token the proof goes with, or null
     * @param nonce server-provided DPoP nonce, or null
     * @return the compact JWS
     */
    String signDpopProof(String htm, String htu, @nullable String ath, @nullable String nonce);
}
