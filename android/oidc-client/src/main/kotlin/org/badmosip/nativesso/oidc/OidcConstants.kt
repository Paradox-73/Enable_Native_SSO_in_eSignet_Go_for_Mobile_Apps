// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

/** OAuth 2.0 grant type identifiers used by the apps. */
public object GrantTypes {
    public const val AUTHORIZATION_CODE: String = "authorization_code"

    /** RFC 8693 section 2.1. */
    public const val TOKEN_EXCHANGE: String = "urn:ietf:params:oauth:grant-type:token-exchange"
}

/** Token type identifiers (RFC 8693 section 3, Native SSO 1.0 section 4.1). */
public object TokenTypes {
    /**
     * The id_token type, with an underscore, as registered by RFC 8693 section 3, required by
     * Native SSO 1.0 section 4.1 and compared by Thunder (`constants.go`, TokenTypeIdentifierIDToken).
     * The request example in Native SSO 1.0 section 4.2 writes "id-token" with a hyphen; servers
     * following RFC 8693 reject that form.
     */
    public const val ID_TOKEN: String = "urn:ietf:params:oauth:token-type:id_token"

    /** Native SSO 1.0 section 4.1: the device secret sent as actor_token. */
    public const val DEVICE_SECRET: String = "urn:openid:params:token-type:device-secret"

    /**
     * token_type_hint for revoking a device secret at the revocation endpoint (RFC 7009 section 2.1).
     * Native SSO 1.0 defines no hint value; eSignet accepts this one and also recognises a device
     * secret without it.
     */
    public const val DEVICE_SECRET_HINT: String = "device_secret"

    /** RFC 8693 section 3: the issued_token_type of a token-exchange response. */
    public const val ACCESS_TOKEN: String = "urn:ietf:params:oauth:token-type:access_token"
}

/** Scope values with a protocol meaning. */
public object Scopes {
    public const val OPENID: String = "openid"

    /**
     * Native SSO 1.0 section 3.1: asks the authorization server for a device secret. Only the app
     * that runs the interactive login sends it; a token exchange must never ask for it again.
     */
    public const val DEVICE_SSO: String = "device_sso"
}

/** Values for client authentication with a signed JWT (RFC 7523, OpenID Connect Core 9). */
public object ClientAssertion {
    public const val TYPE_JWT_BEARER: String = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
}

/** HTTP names from RFC 9449 (DPoP). */
public object DpopHttp {
    /** Request header carrying the proof. */
    public const val HEADER: String = "DPoP"

    /** Response header carrying a server-provided nonce (RFC 9449 section 8). */
    public const val NONCE_HEADER: String = "DPoP-Nonce"

    /** Authorization scheme for DPoP-bound access tokens (RFC 9449 section 7.1). */
    public const val SCHEME: String = "DPoP"

    /** JWS "typ" of a proof (RFC 9449 section 4.2). */
    public const val PROOF_TYPE: String = "dpop+jwt"

    /**
     * Request parameter naming the thumbprint of the key the issued tokens are bound to (RFC 9449
     * section 10). In the Native SSO token exchange eSignet binds the Child App's tokens to it, while
     * the DPoP header still proves the Super App's key.
     */
    public const val JKT_PARAM: String = "dpop_jkt"
}

/** Space-separated scope string helpers (RFC 6749 section 3.3). */
public object ScopeStrings {
    /** Splits a scope string into its values; blank input gives an empty set. */
    public fun parse(scope: String?): Set<String> =
        scope?.trim()?.split(' ')?.filter { it.isNotEmpty() }?.toCollection(LinkedHashSet()) ?: emptySet()

    /** Joins scope values in their iteration order. */
    public fun format(scopes: Collection<String>): String = scopes.joinToString(" ")
}
