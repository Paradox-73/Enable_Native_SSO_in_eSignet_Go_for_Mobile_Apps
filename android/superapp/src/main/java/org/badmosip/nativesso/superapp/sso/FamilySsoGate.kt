// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.sso

import org.badmosip.nativesso.contract.FamilySsoContract
import org.badmosip.nativesso.oidc.DpopProofSource
import org.badmosip.nativesso.oidc.DpopProofs
import org.badmosip.nativesso.oidc.DpopRequest
import org.badmosip.nativesso.superapp.auth.TokenBundle
import java.net.URI
import java.net.URISyntaxException
import java.time.Clock

/** One package that runs under a calling uid, with the SHA-256 digests of its current signing certificates. */
class CallerPackage(val packageName: String, val certificateSha256: List<ByteArray>) {
    override fun toString(): String = "CallerPackage($packageName, ${certificateSha256.size} certificate(s))"
}

/** Finds the packages behind a binder caller's uid (on Android: `PackageManager`, see [AndroidCallerLookup]). */
fun interface CallerLookup {
    /** Every package installed under [uid]; empty when the uid is unknown. */
    fun packagesFor(uid: Int): List<CallerPackage>
}

/**
 * What Hub hands a family app: the Native SSO inputs of the token exchange (Native SSO 1.0 section 4.1)
 * and nothing else. [toString] hides both tokens.
 */
data class SsoShare(
    val issuer: String,
    val idToken: String,
    val deviceSecret: String,
    val scope: String,
    val expiresAtEpochSec: Long,
) {
    override fun toString(): String = "SsoShare(issuer=$issuer, scope=$scope)"
}

/** A refused call; [code] is one of the `FamilySsoContract.ERROR_*` values sent back to the caller. */
class FamilySsoException(val code: Int, message: String) : Exception(message)

/**
 * The decisions behind Hub's family single sign-on service, without Android types so every rule
 * runs in plain JVM tests. `FamilySsoService` only turns the binder call into a call here with
 * `Binder.getCallingUid()` and turns [FamilySsoException] into the contract's coded exception.
 *
 * Rules, checked on every call in this order:
 * 1. Caller: at least one package under the calling uid is on [allowedPackages] and is signed with
 *    one of Hub's own certificates ([familyCertificates]). The signature permission on the service
 *    already guards the bind; this check still runs because a permission can be held by an app of
 *    another signer after a custom permission takeover (the uninstall sequence in docs/changes.md, findings, WP-11).
 * 2. Rate: at most [rateLimit] calls per caller uid in any [rateWindowSeconds] seconds.
 * 3. Session: a sign-in with a device secret, that is, one the user agreed to share
 *    ("Share this sign-in with my other apps"). Otherwise [bundle] returns null and [signProof] refuses
 *    with `ERROR_NOT_SIGNED_IN`.
 * 4. Proofs only: `POST` to the token endpoint, `GET` or `POST` to the userinfo endpoint, both as
 *    discovered at Hub's login; Hub builds every claim itself (fresh `jti` and `iat`) and the caller
 *    supplies only `ath` and `nonce`, both checked for shape.
 *
 * Every successful [bundle] call is written to [accessLog] (package and time, never a token).
 */
class FamilySsoGate(
    private val callers: CallerLookup,
    private val familyCertificates: () -> List<ByteArray>,
    private val session: () -> TokenBundle?,
    private val proofs: DpopProofSource,
    private val accessLog: FamilyAccessLog,
    private val clock: Clock = Clock.systemUTC(),
    private val allowedPackages: Set<String> = setOf(FamilySsoContract.CHILD_APP_PACKAGE),
    rateLimit: Int = DEFAULT_RATE_LIMIT,
    rateWindowSeconds: Long = DEFAULT_RATE_WINDOW_SECONDS,
    private val log: (String) -> Unit = {},
) {
    private val limiter = RateLimiter(rateLimit, rateWindowSeconds, clock)

    /** [FamilySsoContract.VERSION] for an allowed caller. */
    fun version(uid: Int): Int {
        admit(uid)
        return FamilySsoContract.VERSION
    }

    /** The shared sign-in, or null when nobody is signed in or the sign-in was not shared. */
    fun bundle(uid: Int): SsoShare? {
        val caller = admit(uid)
        val current = sharedSession() ?: run {
            log("bundle for $caller: nothing shared")
            return null
        }
        accessLog.record(caller, clock.instant())
        log("bundle handed to $caller")
        return SsoShare(
            issuer = current.issuer,
            idToken = current.idToken,
            deviceSecret = checkNotNull(current.deviceSecret),
            scope = current.scope.orEmpty(),
            expiresAtEpochSec = 0, // the device secret is encrypted for the server; Hub cannot read its exp
        )
    }

    /** A DPoP proof signed with Hub's key, for an allowed method and endpoint only. */
    fun signProof(uid: Int, htm: String?, htu: String?, ath: String?, nonce: String?): String {
        val caller = admit(uid)
        val current = sharedSession() ?: throw FamilySsoException(FamilySsoContract.ERROR_NOT_SIGNED_IN, "nothing shared")
        val method = htm ?: throw IllegalArgumentException("htm is missing")
        val target = htu ?: throw IllegalArgumentException("htu is missing")
        if (!endpointAllowed(current, method, target)) {
            log("proof refused for $caller: $method to an endpoint that is not allowed")
            throw FamilySsoException(FamilySsoContract.ERROR_HTU_NOT_ALLOWED, "Hub signs proofs only for its token and userinfo endpoints")
        }
        require(ath == null || ATH.matches(ath)) { "ath must be 43 base64url characters" }
        require(nonce == null || NONCE.matches(nonce)) { "nonce has characters or a length a DPoP nonce cannot have" }
        log("proof signed for $caller: $method")
        return proofs.proof(DpopRequest(htm = method, htu = target, ath = ath, nonce = nonce))
    }

    private fun admit(uid: Int): String {
        val own = familyCertificates()
        val match = callers.packagesFor(uid).firstOrNull { pkg ->
            pkg.packageName in allowedPackages &&
                pkg.certificateSha256.any { theirs -> own.any { it.contentEquals(theirs) } }
        }
        if (own.isEmpty() || match == null) {
            log("caller uid $uid refused: not a family app")
            throw FamilySsoException(FamilySsoContract.ERROR_CALLER_NOT_ALLOWED, "caller is not an app of this family")
        }
        if (!limiter.tryAcquire(uid)) {
            log("caller ${match.packageName} rate limited (${limiter.inWindow(uid)} calls in the last window)")
            throw FamilySsoException(FamilySsoContract.ERROR_RATE_LIMITED, "too many calls; try again later")
        }
        return match.packageName
    }

    private fun sharedSession(): TokenBundle? = session()?.takeIf { it.deviceSecret != null }

    private fun endpointAllowed(current: TokenBundle, htm: String, htu: String): Boolean {
        val target = exactUrl(htu) ?: return false
        val token = current.tokenEndpoint?.let(::exactUrl)
        val userinfo = current.userinfoEndpoint?.let(::exactUrl)
        return when (htm) {
            "POST" -> target == token || target == userinfo
            "GET" -> target == userinfo
            else -> false
        }
    }

    /** The URL as `htu` carries it, or null when it has a query, a fragment, user info or is not absolute http(s). */
    private fun exactUrl(url: String): String? = try {
        val uri = URI(url)
        val scheme = uri.scheme?.lowercase()
        if ((scheme != "https" && scheme != "http") || uri.rawQuery != null || uri.rawFragment != null ||
            uri.rawUserInfo != null || uri.host.isNullOrEmpty()
        ) {
            null
        } else {
            DpopProofs.normalizeHtu(url).let { normalized ->
                // Host names compare case-insensitively; the path stays exact.
                val parsed = URI(normalized)
                parsed.scheme + "://" + parsed.rawAuthority.lowercase() + (parsed.rawPath ?: "")
            }
        }
    } catch (e: URISyntaxException) {
        null
    } catch (e: IllegalArgumentException) {
        null
    }

    companion object {
        /** Calls per caller uid in one window; one silent sign-in takes about five. */
        const val DEFAULT_RATE_LIMIT: Int = 30

        /** Length of the rate window. */
        const val DEFAULT_RATE_WINDOW_SECONDS: Long = 60

        private val ATH = Regex("[A-Za-z0-9_-]{43}")

        // RFC 9449 section 8.1: nonce = 1*NQCHAR (printable ASCII without quote and backslash); capped.
        private val NONCE = Regex("[\\x21\\x23-\\x5B\\x5D-\\x7E]{1,512}")
    }
}

/** Sliding-window limit per caller uid. */
internal class RateLimiter(private val limit: Int, private val windowSeconds: Long, private val clock: Clock) {
    private val calls = HashMap<Int, ArrayDeque<Long>>()

    init {
        require(limit > 0 && windowSeconds > 0) { "limit and window must be positive" }
    }

    /** Calls of [uid] still inside the window. */
    @Synchronized
    fun inWindow(uid: Int): Int = calls[uid]?.size ?: 0

    @Synchronized
    fun tryAcquire(uid: Int): Boolean {
        val now = clock.millis()
        val windowStart = now - windowSeconds * 1000
        val times = calls.getOrPut(uid) { ArrayDeque() }
        while (times.isNotEmpty() && times.first() <= windowStart) times.removeFirst()
        if (times.size >= limit) return false
        times.addLast(now)
        return true
    }
}
