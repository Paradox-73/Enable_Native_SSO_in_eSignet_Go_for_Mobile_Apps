// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.auth

import okhttp3.OkHttpClient
import org.badmosip.nativesso.oidc.AuthorizationRequest
import org.badmosip.nativesso.oidc.ClientAssertionFactory
import org.badmosip.nativesso.oidc.DpopProofFactory
import org.badmosip.nativesso.oidc.HttpJwksSource
import org.badmosip.nativesso.oidc.IdTokenExpectations
import org.badmosip.nativesso.oidc.IdTokenValidator
import org.badmosip.nativesso.oidc.InvalidResponseException
import org.badmosip.nativesso.oidc.IssuerSignatureVerifier
import org.badmosip.nativesso.oidc.JwksSource
import org.badmosip.nativesso.oidc.JwsSigner
import org.badmosip.nativesso.oidc.LoginSession
import org.badmosip.nativesso.oidc.OidcClient
import org.badmosip.nativesso.oidc.OidcException
import org.badmosip.nativesso.oidc.Pkce
import org.badmosip.nativesso.oidc.ProviderMetadata
import org.badmosip.nativesso.oidc.Scopes
import org.badmosip.nativesso.oidc.JwkThumbprint
import org.badmosip.nativesso.securestore.BlobStore
import org.badmosip.nativesso.securestore.SecureStoreException
import java.net.URI
import java.net.URISyntaxException
import java.net.URLDecoder
import java.security.MessageDigest
import java.text.ParseException
import java.time.Clock
import java.time.Duration
import java.time.temporal.ChronoUnit

/**
 * Hub's sign-in with eSignet, without any Android code so the whole flow runs in JVM tests:
 *
 * 1. [startLogin]: discovery, PAR (PKCE, DPoP proof, `private_key_jwt`), the pending login saved
 *    encrypted, and the URL to open in a Custom Tab.
 * 2. [completeLogin], with the redirect the browser hands back: `state` and `iss` checks, code
 *    exchange, every id_token check (`nonce`, `at_hash`, and `ds_hash` plus `sid` when a device
 *    secret came back), userinfo for the name, then the [TokenBundle] saved encrypted.
 *
 * Blocking; call from a background thread. Failures are [OidcException]s or [LoginException]s,
 * and a failed login stores no tokens.
 */
class HubAuthenticator(
    private val config: HubConfig,
    private val http: OkHttpClient,
    private val dpopSigner: JwsSigner,
    clientSigner: JwsSigner,
    private val store: BlobStore,
    private val clock: Clock = Clock.systemUTC(),
    private val pendingLifetime: Duration = Duration.ofMinutes(10),
    private val log: (String) -> Unit = {},
    private val forgetDpopKey: () -> Unit = {},
) {
    private val clientAuth = ClientAssertionFactory(config.clientId, clientSigner, clock = clock)
    private val dpop = DpopProofFactory(dpopSigner, clock)

    @Volatile private var metadata: ProviderMetadata? = null
    @Volatile private var jwks: JwksSource? = null
    @Volatile private var client: OidcClient? = null

    /** Discovery, once per process (the document names exactly [HubConfig.issuer]). */
    @Synchronized
    private fun oidc(): OidcClient {
        client?.let { return it }
        val m = OidcClient.discover(http, config.issuer)
        metadata = m
        jwks = HttpJwksSource(http, m.jwksUri)
        return OidcClient(http, m, config.clientId, clientAuth).also { client = it }
    }

    /**
     * Pushes the authorization request and returns the URL for the browser.
     * [shareWithFamily] adds `device_sso`, so the token response carries a device secret.
     */
    fun startLogin(shareWithFamily: Boolean): String {
        val client = oidc()
        val session = LoginSession.create()
        val scopes = if (shareWithFamily) config.scopes + Scopes.DEVICE_SSO else config.scopes
        val par = client.pushAuthorizationRequest(
            AuthorizationRequest(
                redirectUri = config.redirectUri,
                scope = scopes,
                session = session,
                acrValues = config.acrValues,
                claims = config.claims,
            ),
            dpop,
        )
        val pending = PendingLogin(
            state = session.state,
            nonce = session.nonce,
            codeVerifier = session.pkce.verifier,
            redirectUri = config.redirectUri,
            deviceSso = shareWithFamily,
            createdAt = clock.instant(),
        )
        store.write(PENDING, pending.toJson())
        log("login started (device_sso=$shareWithFamily)")
        return client.authorizationUrl(par)
    }

    /** True when a browser login was started and not yet finished. */
    fun hasPendingLogin(): Boolean = store.contains(PENDING)

    /** True when [uri] is this app's redirect URI. */
    fun isRedirect(uri: String): Boolean = config.isRedirect(uri)

    /**
     * Finishes the login from the redirect URI the browser opened. The pending login is used once:
     * it is deleted before anything is sent, so a replayed redirect finds nothing.
     */
    fun completeLogin(redirect: String): TokenBundle {
        if (!config.isRedirect(redirect)) throw LoginException(LoginException.Reason.NOT_OUR_REDIRECT, "not this app's redirect URI")
        val pending = readPending() ?: throw LoginException(LoginException.Reason.NO_PENDING_LOGIN, "no login in progress")
        // A redirect with another state (sent by some other app, or an old browser tab) must not
        // cancel the login in progress: compare first, and use up the pending login only on a match.
        if (!sameState(redirect, pending.state)) {
            throw LoginException(LoginException.Reason.STATE_MISMATCH, "the redirect does not belong to the login in progress")
        }
        store.delete(PENDING)
        if (Duration.between(pending.createdAt, clock.instant()) > pendingLifetime) {
            throw LoginException(LoginException.Reason.PENDING_LOGIN_EXPIRED, "the login took too long; start again")
        }

        val client = oidc()
        val session = LoginSession(pending.state, pending.nonce, Pkce.fromVerifier(pending.codeVerifier))
        val code = client.parseAuthorizationResponse(redirect, session)
        val tokens = client.exchangeAuthorizationCode(code, pending.redirectUri, session, dpop)
        val rawIdToken = tokens.idToken ?: throw InvalidResponseException("token response: id_token is missing")
        if (tokens.deviceSecret != null && !pending.deviceSso) {
            throw InvalidResponseException("token response holds a device secret although device_sso was not asked for")
        }
        val jwks = checkNotNull(jwks)
        val idToken = IdTokenValidator(config.issuer, jwks, clock).validate(
            rawIdToken,
            IdTokenExpectations(
                audience = config.clientId,
                nonce = pending.nonce,
                accessToken = tokens.accessToken,
                deviceSecret = tokens.deviceSecret,
            ),
        )
        if (pending.deviceSso && tokens.deviceSecret == null) log("device_sso was asked for but no device secret came back")

        val name = userName(client, tokens.accessToken, idToken.subject, jwks)
        val now = clock.instant().truncatedTo(ChronoUnit.SECONDS) // stored with second precision
        val bundle = TokenBundle(
            issuer = config.issuer,
            clientId = config.clientId,
            subject = idToken.subject,
            sessionId = idToken.sessionId,
            accessToken = tokens.accessToken,
            tokenType = tokens.tokenType,
            accessTokenExpiresAt = tokens.expiresIn?.let { now.plusSeconds(it) },
            idToken = rawIdToken,
            idTokenExpiresAt = idToken.expiresAt,
            refreshToken = tokens.refreshToken,
            deviceSecret = tokens.deviceSecret,
            scope = tokens.scope,
            dpopKeyThumbprint = JwkThumbprint.compute(dpopSigner.publicJwk),
            displayName = name,
            signedInAt = now,
            tokenEndpoint = metadata?.tokenEndpoint,
            userinfoEndpoint = metadata?.userinfoEndpoint,
        )
        store.write(SESSION, bundle.toJson())
        log("signed in (device secret: ${if (bundle.deviceSecret != null) "yes" else "no"}, name: ${if (name != null) "yes" else "no"})")
        return bundle
    }

    /**
     * The name for the home screen from userinfo. Userinfo is a convenience here: when it fails the
     * login still stands, without a name. Claims whose `sub` differs from the id_token's are ignored
     * (OpenID Connect Core 5.3.2).
     */
    private fun userName(client: OidcClient, accessToken: String, subject: String, jwks: JwksSource): String? = try {
        val verifier = IssuerSignatureVerifier(jwks)
        val claims = client.userInfo(accessToken, dpop) { verifier.claimsOf(it) }
        if (claims["sub"] != subject) {
            log("userinfo sub differs from the id_token sub; ignoring userinfo")
            null
        } else {
            UserInfoNames.displayName(claims)
        }
    } catch (e: OidcException) {
        log("userinfo failed: ${e.javaClass.simpleName}: ${e.message}")
        null
    }

    /** The stored session, or null. A blob that cannot be read back is deleted and counts as signed out. */
    fun currentSession(): TokenBundle? = try {
        store.read(SESSION)?.let(TokenBundle::fromJson)
    } catch (e: SecureStoreException) {
        log("stored session unreadable, removed: ${e.message}")
        store.delete(SESSION)
        null
    } catch (e: ParseException) {
        log("stored session malformed, removed")
        store.delete(SESSION)
        null
    }

    /**
     * "Sign out of all apps": forgets the tokens, the device secret, any login in progress and the
     * family access list, and deletes the DPoP key ([forgetDpopKey]; a new one is made at the next
     * sign-in). Every device secret and every family app token bound to the old key stops working,
     * and the family service answers `ERROR_NOT_SIGNED_IN`.
     */
    fun signOut() {
        store.delete(SESSION)
        store.delete(PENDING)
        store.delete(FAMILY_ACCESS)
        forgetDpopKey()
        log("signed out")
    }

    private fun sameState(redirect: String, expected: String): Boolean {
        val query = try {
            URI(redirect).rawQuery
        } catch (e: URISyntaxException) {
            null
        } ?: return false
        val states = query.split('&').filter { it.substringBefore('=') == "state" }
            .map { URLDecoder.decode(it.substringAfter('=', ""), "UTF-8") }
        return states.size == 1 &&
            MessageDigest.isEqual(states[0].toByteArray(Charsets.UTF_8), expected.toByteArray(Charsets.UTF_8))
    }

    private fun readPending(): PendingLogin? = try {
        store.read(PENDING)?.let(PendingLogin::fromJson)
    } catch (e: SecureStoreException) {
        store.delete(PENDING)
        null
    } catch (e: ParseException) {
        store.delete(PENDING)
        null
    }

    companion object {
        /** Blob name of the [TokenBundle]. */
        const val SESSION: String = "session"

        /** Blob name of the [PendingLogin]. */
        const val PENDING: String = "pending-login"

        /** Blob name of the "Apps using this sign-in" list (`sso/FamilyAccessLog`). */
        const val FAMILY_ACCESS: String = "family-access"
    }
}

/** A login could not finish for a reason on the app's side (not an answer from the server). */
class LoginException(val reason: Reason, message: String) : Exception(message) {
    enum class Reason { NOT_OUR_REDIRECT, NO_PENDING_LOGIN, STATE_MISMATCH, PENDING_LOGIN_EXPIRED }
}
