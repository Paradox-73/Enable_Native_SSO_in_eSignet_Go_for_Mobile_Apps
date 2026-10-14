// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp

import android.app.Application
import android.content.ComponentName
import com.nimbusds.jose.JOSEObjectType
import com.nimbusds.jose.JWSAlgorithm
import com.nimbusds.jose.JWSHeader
import com.nimbusds.jose.JWSObject
import com.nimbusds.jose.crypto.ECDSAVerifier
import com.nimbusds.jose.crypto.RSASSASigner
import com.nimbusds.jose.jwk.ECKey
import com.nimbusds.jose.jwk.JWKSet
import com.nimbusds.jose.jwk.KeyUse
import com.nimbusds.jose.jwk.RSAKey
import com.nimbusds.jose.jwk.gen.RSAKeyGenerator
import com.nimbusds.jwt.JWTClaimsSet
import com.nimbusds.jwt.SignedJWT
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.badmosip.nativesso.childapp.auth.CardAuthenticator
import org.badmosip.nativesso.childapp.auth.CardConfig
import org.badmosip.nativesso.contract.FamilySsoContract
import org.badmosip.nativesso.contract.IFamilySso
import org.badmosip.nativesso.contract.SsoBundle
import org.badmosip.nativesso.oidc.DpopProofFactory
import org.badmosip.nativesso.oidc.DpopRequest
import org.badmosip.nativesso.oidc.GrantTypes
import org.badmosip.nativesso.oidc.EcKeyJwsSigner
import org.badmosip.nativesso.oidc.TokenHashes
import org.badmosip.nativesso.securestore.BlobStore
import org.robolectric.Shadows.shadowOf
import java.net.URLDecoder
import java.net.URLEncoder
import java.time.Instant
import java.util.Date
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.atomic.AtomicInteger

/** The eSignet issuer's signing key, made once per test run (RSA generation is slow). */
internal object IssuerKey {
    val rsa: RSAKey = RSAKeyGenerator(2048).keyID("esignet-1").keyUse(KeyUse.SIGNATURE).generate()
}

/**
 * Hub's family service as Health Card sees it over the binder: a shared bundle, proofs signed with
 * "Hub's key" ([key]), and Hub's coded refusals.
 */
internal class FakeHub : IFamilySso.Stub() {
    val key: EcKeyJwsSigner = EcKeyJwsSigner.generate()
    private val proofs = DpopProofFactory(key)

    @Volatile var bundle: SsoBundle? = null
    @Volatile var refusal: RuntimeException? = null
    @Volatile var proofRefusal: RuntimeException? = null
    val proofRequests = CopyOnWriteArrayList<DpopRequest>()

    /** Every call made, as Hub's rate limit counts them. */
    val calls = AtomicInteger()

    override fun getVersion(): Int {
        calls.incrementAndGet()
        refusal?.let { throw it }
        return FamilySsoContract.VERSION
    }

    override fun getSsoBundle(): SsoBundle? {
        calls.incrementAndGet()
        refusal?.let { throw it }
        return bundle
    }

    override fun signDpopProof(htm: String?, htu: String?, ath: String?, nonce: String?): String {
        calls.incrementAndGet()
        refusal?.let { throw it }
        proofRefusal?.let { throw it }
        val request = DpopRequest(htm!!, htu!!, ath, nonce)
        proofRequests += request
        return proofs.proof(request)
    }

    /** Registers this fake as Hub's service with Robolectric, for exactly [HubConnection.bindIntent]. */
    fun install(app: Application) {
        val component = ComponentName(FamilySsoContract.SUPER_APP_PACKAGE, FamilySsoContract.SERVICE_CLASS)
        shadowOf(app).setComponentNameAndServiceForBindServiceForIntent(HubConnection.bindIntent(), component, this)
        shadowOf(app).setBindServiceCallsOnServiceConnectedDirectly(true)
    }
}

/**
 * eSignet as Health Card sees it: discovery, PAR, the token endpoint for both the Native SSO token
 * exchange and the authorization-code grant, JWKS and userinfo, on a MockWebServer. It records each
 * request and lets a test change the answers.
 */
internal class FakeEsignet(val cardClientId: String) : Dispatcher() {
    val server = MockWebServer()
    val issuer: String get() = server.url("/").toString().trimEnd('/')
    val tokenEndpoint: String get() = server.url("/oauth2/token").toString()
    val userinfoEndpoint: String get() = server.url("/oidc/userinfo").toString()

    val hubClientId = "ps03-hub-test"
    val hubSid = "sid-hub-1"
    val subject = "psut-family-123"
    val exchangeAccessToken = "at-exchange-" + "x".repeat(32)
    val ownAccessToken = "at-own-" + "y".repeat(32)
    val code = "code-abc"
    val deviceSecret = "ds-" + "z".repeat(48)

    @Volatile var parForm: Map<String, String> = emptyMap()
    @Volatile var exchangeForm: Map<String, String> = emptyMap()
    @Volatile var codeForm: Map<String, String> = emptyMap()
    val exchangeProofs = CopyOnWriteArrayList<String>()
    val parProofs = CopyOnWriteArrayList<String>()
    val codeProofs = CopyOnWriteArrayList<String>()
    val userinfoProofs = CopyOnWriteArrayList<String>()
    val userinfoAuthorizations = CopyOnWriteArrayList<String>()
    val calls = ConcurrentHashMap<String, Int>()

    // What the token endpoint answers; tests change these.
    @Volatile var exchangeError: MockResponse? = null
    @Volatile var nonceChallenges: Int = 0
    @Volatile var exchangeSid: String? = hubSid
    @Volatile var exchangeAudience: String? = null
    @Volatile var exchangeTokenType: String = "DPoP"
    @Volatile var userinfoStatus: Int = 200
    @Volatile var name: String? = "Siddharth K Mansour"
    @Volatile var email: String? = "siddharth@example.com"
    @Volatile var phone: String = "+919876543210"

    fun start(): FakeEsignet = apply {
        server.dispatcher = this
        server.start()
    }

    fun stop() = server.shutdown()

    /** Hub's id_token, as eSignet gave it to Hub: aud Hub, `sid`, `ds_hash`. */
    fun hubIdToken(sid: String? = hubSid): String {
        val now = Instant.now()
        val claims = JWTClaimsSet.Builder().issuer(issuer).subject(subject).audience(hubClientId)
            .issueTime(Date.from(now)).expirationTime(Date.from(now.plusSeconds(600)))
            .claim("ds_hash", TokenHashes.dsHash(deviceSecret))
        sid?.let { claims.claim("sid", it) }
        return sign(claims.build())
    }

    /** What Hub's service hands out when the user shared the sign-in. */
    fun sharedBundle(sid: String? = hubSid, issuer: String = this.issuer) = SsoBundle().apply {
        this.issuer = issuer
        idToken = hubIdToken(sid)
        deviceSecret = this@FakeEsignet.deviceSecret
        scope = "openid profile device_sso"
        expiresAtEpochSec = 0
    }

    /** The redirect the browser would open after the consent login. */
    fun redirect(redirectUri: String, state: String = parForm.getValue("state"), extra: String = "&code=$code"): String =
        "$redirectUri?state=${URLEncoder.encode(state, "UTF-8")}&iss=${URLEncoder.encode(issuer, "UTF-8")}$extra"

    override fun dispatch(request: RecordedRequest): MockResponse {
        val path = request.requestUrl!!.encodedPath
        calls.merge(path, 1, Int::plus)
        val proof = request.getHeader("DPoP")
        return when (path) {
            "/.well-known/openid-configuration" -> json(discovery())
            "/oauth2/par" -> {
                parForm = request.form()
                proof?.let { parProofs += it }
                json("""{"request_uri":"urn:ietf:params:oauth:request_uri:abc","expires_in":60}""", 201)
            }
            "/oauth2/token" -> token(request.form(), proof)
            "/oauth2/jwks" -> json(JWKSet(IssuerKey.rsa.toPublicJWK()).toString())
            "/oidc/userinfo" -> {
                proof?.let { userinfoProofs += it }
                val authorization = request.getHeader("Authorization").orEmpty()
                userinfoAuthorizations += authorization
                if (userinfoStatus != 200) {
                    MockResponse().setResponseCode(userinfoStatus)
                } else {
                    MockResponse().setHeader("Content-Type", "application/jwt").setBody(sign(userinfo(authorization)))
                }
            }
            else -> MockResponse().setResponseCode(404)
        }
    }

    private fun token(form: Map<String, String>, proof: String?): MockResponse = when (form["grant_type"]) {
        GrantTypes.TOKEN_EXCHANGE -> {
            exchangeForm = form
            proof?.let { exchangeProofs += it }
            when {
                nonceChallenges > 0 -> {
                    nonceChallenges--
                    json("""{"error":"use_dpop_nonce","error_description":"Authorization server requires nonce in DPoP proof"}""", 400)
                        .setHeader("DPoP-Nonce", "server-nonce-1")
                }
                exchangeError != null -> exchangeError!!
                else -> json(tokens(exchangeAccessToken, exchangeAudience ?: cardClientId, exchangeSid, null, form["scope"]))
            }
        }
        GrantTypes.AUTHORIZATION_CODE -> {
            codeForm = form
            proof?.let { codeProofs += it }
            json(tokens(ownAccessToken, cardClientId, "sid-card-own", parForm["nonce"], parForm["scope"]))
        }
        else -> json("""{"error":"unsupported_grant_type"}""", 400)
    }

    private fun tokens(accessToken: String, audience: String, sid: String?, nonce: String?, scope: String?): String {
        val now = Instant.now()
        val claims = JWTClaimsSet.Builder().issuer(issuer).subject(subject).audience(audience)
            .issueTime(Date.from(now)).expirationTime(Date.from(now.plusSeconds(300)))
            .claim("at_hash", TokenHashes.halfHash(accessToken, "PS256"))
        sid?.let { claims.claim("sid", it) }
        nonce?.let { claims.claim("nonce", it) }
        val tokenType = if (accessToken == exchangeAccessToken) exchangeTokenType else "DPoP"
        val issued = if (accessToken == exchangeAccessToken) {
            ""","issued_token_type":"urn:ietf:params:oauth:token-type:access_token""""
        } else {
            ""
        }
        return """{"access_token":"$accessToken","token_type":"$tokenType","expires_in":300,""" +
            """"scope":"$scope","id_token":"${sign(claims.build())}"$issued}"""
    }

    private fun userinfo(authorization: String): JWTClaimsSet {
        val b = JWTClaimsSet.Builder().subject(subject).issuer(issuer).audience(cardClientId)
        name?.let { b.claim("name", it) }
        email?.let { b.claim("email", it) }
        // Only Health Card's own login was granted the phone scope.
        if (authorization.endsWith(ownAccessToken)) b.claim("phone_number", phone)
        return b.build()
    }

    private fun discovery() = """
        {
          "issuer": "$issuer",
          "authorization_endpoint": "${server.url("/authorize")}",
          "token_endpoint": "$tokenEndpoint",
          "pushed_authorization_request_endpoint": "${server.url("/oauth2/par")}",
          "userinfo_endpoint": "$userinfoEndpoint",
          "jwks_uri": "${server.url("/oauth2/jwks")}",
          "scopes_supported": ["openid", "profile", "email", "phone", "device_sso"],
          "dpop_signing_alg_values_supported": ["ES256"]
        }
    """.trimIndent()

    fun sign(claims: JWTClaimsSet): String =
        SignedJWT(JWSHeader.Builder(JWSAlgorithm.PS256).keyID(IssuerKey.rsa.keyID).type(JOSEObjectType.JWT).build(), claims)
            .apply { sign(RSASSASigner(IssuerKey.rsa)) }.serialize()
}

internal fun json(body: String, code: Int = 200): MockResponse =
    MockResponse().setResponseCode(code).setHeader("Content-Type", "application/json").setBody(body)

/** Form parameters of an urlencoded request body. */
internal fun RecordedRequest.form(): Map<String, String> =
    body.readUtf8().split('&').filter { it.isNotEmpty() }.associate {
        URLDecoder.decode(it.substringBefore('='), "UTF-8") to URLDecoder.decode(it.substringAfter('='), "UTF-8")
    }

/** Checks a client-made ES256 JWS against the public [jwk] and returns it. */
internal fun verifiedJws(jws: String, jwk: Map<String, Any?>): JWSObject {
    val parsed = JWSObject.parse(jws)
    check(parsed.verify(ECDSAVerifier(ECKey.parse(jwk)))) { "JWS signature does not verify with the expected key" }
    return parsed
}

/** A BlobStore in memory, standing in for the encrypted store where encryption is not the point. */
internal class MemoryBlobStore : BlobStore {
    val blobs = ConcurrentHashMap<String, ByteArray>()
    override fun read(name: String): ByteArray? = blobs[name]?.copyOf()
    override fun write(name: String, data: ByteArray) {
        blobs[name] = data.copyOf()
    }
    override fun delete(name: String): Boolean = blobs.remove(name) != null
    override fun contains(name: String): Boolean = blobs.containsKey(name)
}

/** Health Card wired to a [FakeEsignet] and to Hub through the real [HubConnection] (Robolectric). */
internal class CardFixture(val app: Application) {
    val clientId = "ps03-card-test"
    val redirectUri = "org.badmosip.nativesso.childapp://oauth/callback"
    val esignet = FakeEsignet(clientId).start()
    val hub = FakeHub().also { it.install(app) }
    val store = MemoryBlobStore()
    val ownDpop: EcKeyJwsSigner = EcKeyJwsSigner.generate()
    val clientKey: EcKeyJwsSigner = EcKeyJwsSigner.generate("client-kid")
    val logs = CopyOnWriteArrayList<String>()
    val config = CardConfig(issuer = esignet.issuer, clientId = clientId, redirectUri = redirectUri)
    val link = HubConnectionLink(HubConnection(app, timeoutMillis = 500))

    fun authenticator() = CardAuthenticator(
        config = config,
        http = OkHttpClient(),
        hub = link,
        ownDpopSigner = ownDpop,
        clientSigner = clientKey,
        store = store,
        log = { logs += it },
    )

    fun shareHubSignIn(sid: String? = esignet.hubSid) {
        hub.bundle = esignet.sharedBundle(sid)
    }

    fun stop() = esignet.stop()
}
