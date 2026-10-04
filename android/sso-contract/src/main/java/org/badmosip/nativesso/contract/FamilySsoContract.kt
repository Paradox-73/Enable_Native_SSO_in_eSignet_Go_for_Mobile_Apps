// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.contract

/**
 * Names shared by the Super App (server side of [IFamilySso]) and the apps that bind to it.
 *
 * The permission is declared, with the same signature protection level, in the manifest of every
 * family app. If it were declared only by the Super App, an app installed earlier could declare the
 * same name with a weaker level and keep it ("custom permission takeover",
 * https://developer.android.com/privacy-and-security/risks/custom-permissions).
 */
object FamilySsoContract {
    /** Version of this contract; bump it on any incompatible change to [IFamilySso]. */
    const val VERSION = 1

    /** Signature-level permission guarding the Super App's single sign-on service. */
    const val PERMISSION = "org.badmosip.nativesso.permission.FAMILY_SSO"

    /** Intent action of the bound service. */
    const val ACTION_BIND = "org.badmosip.nativesso.action.FAMILY_SSO"

    /** Package of the Super App, the only app that holds the shared sign-in. */
    const val SUPER_APP_PACKAGE = "org.badmosip.nativesso.superapp"

    /** Package of the Child App. */
    const val CHILD_APP_PACKAGE = "org.badmosip.nativesso.childapp"

    /** Fully qualified class name of the bound service inside the Super App. */
    const val SERVICE_CLASS = "org.badmosip.nativesso.superapp.sso.FamilySsoService"

    // Codes carried by android.os.ServiceSpecificException.errorCode.

    /** Nobody is signed in to the Super App, so there is nothing to share or sign. */
    const val ERROR_NOT_SIGNED_IN = 1

    /** The caller is not signed with the family certificate or is not on the package allow-list. */
    const val ERROR_CALLER_NOT_ALLOWED = 2

    /** The requested htm or htu is not one the Super App signs proofs for. */
    const val ERROR_HTU_NOT_ALLOWED = 3

    /** Too many calls in a short time from the same caller. */
    const val ERROR_RATE_LIMITED = 4
}
