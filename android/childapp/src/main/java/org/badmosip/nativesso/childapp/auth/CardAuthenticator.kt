// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp.auth

import com.nimbusds.jwt.SignedJWT
import okhttp3.OkHttpClient
import org.badmosip.nativesso.childapp.HubException
import org.badmosip.nativesso.childapp.HubLink
import org.badmosip.nativesso.oidc.AuthorizationRequest
import org.badmosip.nativesso.oidc.ClientAssertionFactory
import org.badmosip.nativesso.oidc.DeviceSecretExchange
import org.badmosip.nativesso.oidc.DpopProofFactory
import org.badmosip.nativesso.oidc.DpopProofSource
import org.badmosip.nativesso.oidc.HttpJwksSource
import org.badmosip.nativesso.oidc.IdToken
import org.badmosip.nativesso.oidc.IdTokenExpectations
import org.badmosip.nativesso.oidc.IdTokenValidator
import org.badmosip.nativesso.oidc.InvalidResponseException
import org.badmosip.nativesso.oidc.IssuerSignatureVerifier
import org.badmosip.nativesso.oidc.JwksSource
import org.badmosip.nativesso.oidc.JwsSigner
import org.badmosip.nativesso.oidc.LoginSession
import org.badmosip.nativesso.oidc.OAuthErrorException
import org.badmosip.nativesso.oidc.OidcClient
import org.badmosip.nativesso.oidc.OidcException
import org.badmosip.nativesso.oidc.Pkce
import org.badmosip.nativesso.oidc.ScopeStrings
import org.badmosip.nativesso.oidc.Scopes
import org.badmosip.nativesso.oidc.TokenResponse
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
 * Health Card's sign-in, without Android code so every path runs in tests:
 *
 * - [silentSignIn]: Hub's id_token and device secret -> Native SSO token exchange (Native SSO 1.0
 *   section 4.1: `audience` = issuer, Health Card's own `private_key_jwt`, a DPoP proof signed by
 *   Hub) -> id_token checks (signature, `iss`, `aud` = Health Card, `at_hash`, and `sid` equal to the
 *   `sid` of Hub's id_token) -> userinfo with a Hub-signed proof carrying `ath` -> encrypted storage.
 *   No browser, no password, no OTP.
 * - [startConsentLogin] and [completeConsentLogin]: when the exchange asks for more than Hub's login
 *   granted, eSignet answers `interaction_required` (bonus B1); Health Card then runs its own PAR
 *   login with `prompt=consent` and its own DPoP key.
 *
 * Blocking; call from a background thread. Failures are [OidcException], [HubException] or
 * [CardException]; a failed sign-in stores nothing and keeps the earlier session.
 */
class CardAuthenticator(
    val config: CardConfig,
    private val http: OkHttpClient,
    private val hub: HubLink,
    private val ownDpopSigner: JwsSigner,
    clientSigner: JwsSigner,
    private val store: BlobStore,
    private val clock: Clock = Clock.systemUTC(),
    private val pendingLifetime: Duration = Duration.ofMinutes(10),
    private val log: (String) -> Unit = {},
) {
    private val clientAuth = ClientAssertionFactory(config.clientId, clientSigner, clock = clock)
    private val ownDpop = DpopProofFactory(ownDpopSigner, clock)

    @Volatile private var jwks: JwksSource? = null
    @Volatile private var client: OidcClient? = null

    /** Discovery, once per process (the document names exactly [CardConfig.issuer]). */
    @Synchronized
    private fun oidc(): OidcClient {
        client?.let { return it }
        val m = OidcClient.discover(http, config.issuer)
        jwks = HttpJwksSource(http, m.jwksUri)
        return OidcClient(http, m, config.clientId, clientAuth).also { client = it }
    }

    /**
     * Signs in with Hub's shared sign-in. [scope] defaults to [CardConfig.scopes]; with
     * [CardConfig.extraScope] added, eSignet answers `interaction_required`
     * ([OAuthErrorException.requiresInteraction]) and the caller offers the consent screen.
     */
    fun silentSignIn(scope: Set<String> = config.scopes): CardSession = hub.use { calls ->
        val share = calls.share() ?: throw HubException(HubException.Reason.NOT_SIGNED_IN, "Hub shares no sign-in")
        if (share.issuer != config.issuer) {
            throw CardException(CardException.Reason.OTHER_ISSUER, "Hub is signed in to another server")
        }
        val hubSid = sessionIdOf(share.idToken)
            ?: throw CardException(CardException.Reason.SESSION_MISMATCH, "Hub's id_token carries no sid")
        val client = oidc()
        log("token exchange (scope=${ScopeStrings.format(scope)})")
        val tokens = client.exchangeDeviceSecret(DeviceSecretExchange(share.idToken, share.deviceSecret, scope), calls.proofs)
        if (tokens.deviceSecret != null) throw InvalidResponseException("token exchange returned a device secret")
        val idToken = validate(tokens, nonce = null)
        val sid = idToken.sessionId
        if (sid == null || !MessageDigest.isEqual(sid.toByteArray(), hubSid.toByteArray())) {
            throw CardException(CardException.Reason.SESSION_MISMATCH, "the new id_token does not belong to Hub's login session")
        }
        val profile = profile(client, tokens.accessToken, idToken.subject, calls.proofs)
        save(tokens, idToken, Binding.HUB, hubSid, profile)
    }

    /**
     * Pushes Health Card's own authorization request with `prompt=consent` for [scope] and returns
     * the URL for the browser. eSignet keeps no browser session, so the user enters an OTP again.
     */
    fun startConsentLogin(scope: Set<String>): String {
        val client = oidc()
        val session = LoginSession.create()
        val par = client.pushAuthorizationRequest(
            AuthorizationRequest(
                redirectUri = config.redirectUri,
                scope = scope,
                session = session,
                acrValues = config.acrValues,
                claims = config.claims,
                prompt = PROMPT_CONSENT,
            ),
            ownDpop,
        )
        store.write(
            PENDING,
            PendingConsent(
                state = session.state,
                nonce = session.nonce,
                codeVerifier = session.pkce.verifier,
                redirectUri = config.redirectUri,
                scope = ScopeStrings.format(scope),
                createdAt = clock.instant(),
            ).toJson(),
        )
        log("consent login started (scope=${ScopeStrings.format(scope)})")
        return client.authorizationUrl(par)
    }

    /** True when [uri] is this app's redirect URI. */
    fun isRedirect(uri: String): Boolean = config.isRedirect(uri)

    /** True while a consent login waits for the browser. */
    fun hasPendingConsent(): Boolean = store.contains(PENDING)

    /** The user declined or left the consent login: forget it. */
    fun cancelConsent() {
        store.delete(PENDING)
    }

    /**
     * Finishes the consent login from the redirect. An `error` redirect (for example
     * `access_denied` when the user refuses on eSignet's page) is an [OAuthErrorException] with HTTP
     * status 0, and nothing changes.
     */
    fun completeConsentLogin(redirect: String): CardSession {
        if (!config.isRedirect(redirect)) throw CardException(CardException.Reason.NOT_OUR_REDIRECT, "not this app's redirect URI")
        val pending = readPending() ?: throw CardException(CardException.Reason.NO_PENDING_LOGIN, "no consent login in progress")
        if (!sameState(redirect, pending.state)) {
            throw CardException(CardException.Reason.STATE_MISMATCH, "the redirect does not belong to the login in progress")
        }
        store.delete(PENDING)
        if (Duration.between(pending.createdAt, clock.instant()) > pendingLifetime) {
            throw CardException(CardException.Reason.PENDING_LOGIN_EXPIRED, "the login took too long; start again")
        }
        val client = oidc()
        val session = LoginSession(pending.state, pending.nonce, Pkce.fromVerifier(pending.codeVerifier))
        val code = client.parseAuthorizationResponse(redirect, session)
        val tokens = client.exchangeAuthorizationCode(code, pending.redirectUri, session, ownDpop)
        if (tokens.deviceSecret != null) throw InvalidResponseException("token response holds a device secret Health Card did not ask for")
        val idToken = validate(tokens, nonce = pending.nonce)
        val profile = profile(client, tokens.accessToken, idToken.subject, ownDpop)
        return save(tokens, idToken, Binding.OWN, hubSessionId = null, profile = profile)
    }

    /** The stored session, or null. A blob that cannot be read back is deleted and counts as signed out. */
    fun currentSession(): CardSession? = try {
        store.read(SESSION)?.let(CardSession::fromJson)
    } catch (e: SecureStoreException) {
        log("stored session unreadable, removed")
        store.delete(SESSION)
        null
    } catch (e: ParseException) {
        log("stored session malformed, removed")
        store.delete(SESSION)
        null
    }

    /**
     * Forgets Health Card's tokens and any consent login in progress. [byUser] remembers that the
     * user chose to sign out, so Health Card does not sign in again on its own until asked; a sign-out
     * because Hub signed out leaves that mark as it is.
     */
    fun signOut(byUser: Boolean) {
        store.delete(SESSION)
        store.delete(PENDING)
        if (byUser) store.write(SIGNED_OUT_BY_USER, byteArrayOf(1))
        log(if (byUser) "signed out by the user" else "signed out")
    }

    /** True after [signOut] with `byUser`, until [allowAutomaticSignIn]. */
    fun signedOutByUser(): Boolean = store.contains(SIGNED_OUT_BY_USER)

    /** The user asked to sign in again: automatic sign-in is allowed once more. */
    fun allowAutomaticSignIn() {
        store.delete(SIGNED_OUT_BY_USER)
    }

    private fun validate(tokens: TokenResponse, nonce: String?): IdToken {
        val raw = tokens.idToken ?: throw InvalidResponseException("token response: id_token is missing")
        return IdTokenValidator(config.issuer, checkNotNull(jwks), clock).validate(
            raw,
            IdTokenExpectations(audience = config.clientId, nonce = nonce, accessToken = tokens.accessToken),
        )
    }

    private fun save(tokens: TokenResponse, idToken: IdToken, binding: Binding, hubSessionId: String?, profile: Profile): CardSession {
        val now = clock.instant().truncatedTo(ChronoUnit.SECONDS)
        val session = CardSession(
            issuer = config.issuer,
            clientId = config.clientId,
            subject = idToken.subject,
            sessionId = idToken.sessionId,
            accessToken = tokens.accessToken,
            accessTokenExpiresAt = tokens.expiresIn?.let { now.plusSeconds(it) },
            idToken = idToken.raw,
            scope = tokens.scope,
            binding = binding,
            hubSessionId = hubSessionId,
            name = profile.name,
            email = profile.email,
            phone = profile.phone,
            signedInAt = now,
        )
        store.write(SESSION, session.toJson())
        store.delete(SIGNED_OUT_BY_USER)
        log("signed in (binding=$binding, scope=${tokens.scope}, name=${if (profile.name != null) "yes" else "no"})")
        return session
    }

    private class Profile(val name: String?, val email: String?, val phone: String?)

    /**
     * Name, email and phone from userinfo, with a proof from [dpop] carrying `ath`. Userinfo is a
     * convenience: when it fails the sign-in still stands without them. Claims whose `sub` differs
     * from the id_token's are ignored (OpenID Connect Core 5.3.2). A refusal by Hub is not swallowed.
     */
    private fun profile(client: OidcClient, accessToken: String, subject: String, dpop: DpopProofSource): Profile = try {
        val verifier = IssuerSignatureVerifier(checkNotNull(jwks))
        val claims = client.userInfo(accessToken, dpop) { verifier.claimsOf(it) }
        if (claims["sub"] != subject) {
            log("userinfo sub differs from the id_token sub; ignoring userinfo")
            Profile(null, null, null)
        } else {
            Profile(UserInfoNames.displayName(claims), claims.text("email"), claims.text("phone_number"))
        }
    } catch (e: OidcException) {
        log("userinfo failed: ${e.javaClass.simpleName}: ${e.message}")
        Profile(null, null, null)
    }

    private fun Map<String, Any?>.text(name: String): String? = (this[name] as? String)?.trim()?.takeIf { it.isNotEmpty() }

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

    private fun readPending(): PendingConsent? = try {
        store.read(PENDING)?.let(PendingConsent::fromJson)
    } catch (e: SecureStoreException) {
        store.delete(PENDING)
        null
    } catch (e: ParseException) {
        store.delete(PENDING)
        null
    }

    companion object {
        /** Blob name of the [CardSession]. */
        const val SESSION: String = "card-session"

        /** Blob name of the [PendingConsent]. */
        const val PENDING: String = "card-pending-consent"

        /** Blob name of the "signed out by the user" mark. */
        const val SIGNED_OUT_BY_USER: String = "card-signed-out"

        const val PROMPT_CONSENT: String = "consent"

        /**
         * `sid` of Hub's id_token, read without verifying the signature: Health Card only compares
         * it, and eSignet verifies the token itself in the exchange. Null when absent or unreadable.
         */
        fun sessionIdOf(idToken: String?): String? = try {
            idToken?.let { SignedJWT.parse(it).jwtClaimsSet.getStringClaim("sid") }
        } catch (e: ParseException) {
            null
        }

        /**
         * The scope values eSignet says need consent, from the `interaction_required` description
         * ("... user consent is needed for: phone"). Falls back to the [requested] values Health Card
         * does not hold yet ([held]) when the description has no list.
         */
        fun missingScopes(e: OAuthErrorException, requested: Set<String>, held: Set<String>): Set<String> {
            val listed = e.description?.substringAfter(CONSENT_MARKER, missingDelimiterValue = "")
                ?.split(' ', ',')?.map { it.trim() }?.filter { it.isNotEmpty() }?.toSet().orEmpty()
            return listed.ifEmpty { requested - held - Scopes.OPENID }
        }

        private const val CONSENT_MARKER = "user consent is needed for:"
    }
}

/** A sign-in could not finish for a reason on Health Card's side (not an answer from the server). */
class CardException(val reason: Reason, message: String) : Exception(message) {
    enum class Reason {
        /** Hub's sign-in is for another eSignet server than Health Card's. */
        OTHER_ISSUER,

        /** The id_token from the exchange does not carry the `sid` of Hub's login. */
        SESSION_MISMATCH,
        NOT_OUR_REDIRECT,
        NO_PENDING_LOGIN,
        STATE_MISMATCH,
        PENDING_LOGIN_EXPIRED,
    }
}
