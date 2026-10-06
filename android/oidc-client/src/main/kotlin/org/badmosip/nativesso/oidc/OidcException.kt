// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

/**
 * Every failure the client reports. Callers branch on the subtype; the message is for logs and
 * never contains tokens or keys.
 */
public sealed class OidcException(message: String, cause: Throwable? = null) : Exception(message, cause)

/**
 * The server answered with an OAuth 2.0 error response (RFC 6749 section 5.2, RFC 6750 section 3,
 * RFC 9449 sections 5 and 7.1, RFC 8693 section 2.2.2, OpenID Connect Core 3.1.2.6).
 *
 * @property error the error as a known value, or [OAuthError.UNKNOWN]
 * @property code the error code exactly as the server sent it
 * @property description the server's error_description, if any
 * @property httpStatus HTTP status of the response; 0 for errors delivered on the redirect URI
 * @property consentUri the server's `consent_uri`, if any: on an `interaction_required` answer of a
 *   Native SSO token exchange, a one-time page of the server where the user approves the extra
 *   scopes without logging in again (eSignet's consent-only re-consent). Never logged.
 */
public class OAuthErrorException(
    public val error: OAuthError,
    public val code: String,
    public val description: String?,
    public val httpStatus: Int,
    public val consentUri: String? = null,
) : OidcException("$code${description?.let { ": $it" } ?: ""} (HTTP $httpStatus)") {

    /**
     * True when the user has to take part again, through a browser login or consent screen.
     * A Native SSO server answers interaction_required when a token exchange asks for more than the
     * original login was granted (Native SSO 1.0 section 4.3 step 6); the app then runs its own
     * login with prompt=consent.
     */
    public val requiresInteraction: Boolean
        get() = error == OAuthError.INTERACTION_REQUIRED || error == OAuthError.CONSENT_REQUIRED ||
            error == OAuthError.LOGIN_REQUIRED

    public companion object {
        /** Builds the exception from a raw error code. */
        public fun of(code: String, description: String?, httpStatus: Int, consentUri: String? = null): OAuthErrorException =
            OAuthErrorException(OAuthError.fromCode(code), code, description, httpStatus, consentUri)
    }
}

/** Error codes the apps react to. */
public enum class OAuthError(public val code: String?) {
    INVALID_REQUEST("invalid_request"),
    INVALID_CLIENT("invalid_client"),
    INVALID_GRANT("invalid_grant"),
    UNAUTHORIZED_CLIENT("unauthorized_client"),
    UNSUPPORTED_GRANT_TYPE("unsupported_grant_type"),
    INVALID_SCOPE("invalid_scope"),
    INVALID_TARGET("invalid_target"),
    ACCESS_DENIED("access_denied"),
    INTERACTION_REQUIRED("interaction_required"),
    CONSENT_REQUIRED("consent_required"),
    LOGIN_REQUIRED("login_required"),
    INVALID_DPOP_PROOF("invalid_dpop_proof"),
    USE_DPOP_NONCE("use_dpop_nonce"),
    INVALID_TOKEN("invalid_token"),
    INSUFFICIENT_SCOPE("insufficient_scope"),
    SERVER_ERROR("server_error"),
    TEMPORARILY_UNAVAILABLE("temporarily_unavailable"),

    /** Any code not listed above; [OAuthErrorException.code] keeps the original. */
    UNKNOWN(null),
    ;

    public companion object {
        /** Maps a code to its value; unlisted codes give [UNKNOWN]. */
        public fun fromCode(code: String): OAuthError = entries.firstOrNull { it.code == code } ?: UNKNOWN
    }
}

/** The server answered with an HTTP error that is not an OAuth 2.0 error response. */
public class HttpStatusException(public val httpStatus: Int, message: String) : OidcException(message)

/** The request could not be sent or the response could not be read (no network, timeout, reset). */
public class NetworkException(message: String, cause: Throwable) : OidcException(message, cause)

/** A response arrived but breaks the protocol: missing fields, wrong types, wrong token type. */
public class InvalidResponseException(message: String, cause: Throwable? = null) : OidcException(message, cause)

/** An id_token failed one of the checks in [IdTokenValidator]. */
public class IdTokenValidationException(
    public val reason: IdTokenFailure,
    message: String,
    cause: Throwable? = null,
) : OidcException("id_token rejected (${reason.name}): $message", cause)

/** Why an id_token was rejected, one value per check. */
public enum class IdTokenFailure {
    /** Not a signed JWT, or a claim has the wrong type. */
    MALFORMED,

    /** The JWS algorithm is not on the allowed list (for example "none" or an HMAC algorithm). */
    ALGORITHM,

    /** No key in the issuer's JWKS matches the token's key id, even after one refresh. */
    UNKNOWN_KEY,

    /** The signature does not verify with the issuer's key. */
    SIGNATURE,
    ISSUER,
    AUDIENCE,
    EXPIRED,
    ISSUED_IN_FUTURE,
    NONCE,

    /** at_hash does not match the access token (OpenID Connect Core 3.1.3.8). */
    AT_HASH,

    /** ds_hash is missing or does not match the device secret (Native SSO 1.0 section 3.4.1). */
    DS_HASH,

    /** sid is missing although a device secret was issued (Native SSO 1.0 section 3.4.1). */
    SID_MISSING,
}
