// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.auth

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
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.badmosip.nativesso.oidc.TokenHashes
import org.badmosip.nativesso.securestore.BlobStore
import java.net.URLDecoder
import java.time.Clock
import java.time.Instant
import java.time.ZoneId
import java.time.ZoneOffset
import java.util.Date
import java.util.concurrent.ConcurrentHashMap

/** The eSignet issuer's signing key, made once per test run (RSA generation is slow). */
internal object IssuerKey {
    val rsa: RSAKey = RSAKeyGenerator(2048).keyID("esignet-1").keyUse(KeyUse.SIGNATURE).generate()
}

/**
 * eSignet as far as Hub sees it: discovery, PAR, token, JWKS and userinfo on a MockWebServer.
 * It records each request's form and lets a test change what the token endpoint answers.
 */
internal class FakeEsignet(val clientId: String) : Dispatcher() {
    val server = MockWebServer()
    val issuer: String get() = server.url("/").toString().trimEnd('/')

    /** Form of the last PAR and token requests, and the DPoP headers seen per path. */
    @Volatile var parForm: Map<String, String> = emptyMap()
    @Volatile var tokenForm: Map<String, String> = emptyMap()
    @Volatile var revokeForm: Map<String, String>? = null
    val dpopHeaders = ConcurrentHashMap<String, String>()

    // What the token endpoint answers; tests change these.
    @Volatile var deviceSecret: String? = null
    @Volatile var dsHashOverride: String? = null
    @Volatile var nonceOverride: String? = null
    @Volatile var userinfoStatus: Int = 200
    @Volatile var userinfoName: String? = "Siddharth K Mansour"
    @Volatile var userinfoSub: String? = null
    @Volatile var revokeStatus: Int = 200
    @Volatile var advertiseRevocation: Boolean = true

    val subject = "psut-hub-123"
    val accessToken = "at-" + "x".repeat(40)
    val code = "code-abc"

    fun start(): FakeEsignet = apply {
        server.dispatcher = this
        server.start()
    }

    fun stop() = server.shutdown()

    /** The redirect the browser would open after a successful login. */
    fun redirect(redirectUri: String, state: String = parForm.getValue("state"), extra: String = ""): String =
        "$redirectUri?code=$code&state=${java.net.URLEncoder.encode(state, "UTF-8")}&iss=${java.net.URLEncoder.encode(issuer, "UTF-8")}$extra"

    override fun dispatch(request: RecordedRequest): MockResponse {
        request.getHeader("DPoP")?.let { dpopHeaders[request.requestUrl!!.encodedPath] = it }
        return when (request.requestUrl!!.encodedPath) {
            "/.well-known/openid-configuration" -> json(discovery())
            "/oauth2/par" -> {
                parForm = request.form()
                json("""{"request_uri":"urn:ietf:params:oauth:request_uri:abc","expires_in":60}""", 201)
            }
            "/oauth2/token" -> {
                tokenForm = request.form()
                json(tokenResponse())
            }
            "/oauth2/jwks" -> json(JWKSet(IssuerKey.rsa.toPublicJWK()).toString())
            "/oauth2/revoke" -> {
                revokeForm = request.form()
                if (revokeStatus == 200) MockResponse() else json("""{"error":"invalid_grant"}""", revokeStatus)
            }
            "/oidc/userinfo" -> if (userinfoStatus != 200) {
                MockResponse().setResponseCode(userinfoStatus)
            } else {
                MockResponse().setHeader("Content-Type", "application/jwt").setBody(sign(userinfoClaims()))
            }
            else -> MockResponse().setResponseCode(404)
        }
    }

    private fun discovery() = """
        {
          "issuer": "$issuer",
          "authorization_endpoint": "${server.url("/authorize")}",
          "token_endpoint": "${server.url("/oauth2/token")}",
          "pushed_authorization_request_endpoint": "${server.url("/oauth2/par")}",
          "userinfo_endpoint": "${server.url("/oidc/userinfo")}",
          "jwks_uri": "${server.url("/oauth2/jwks")}",${if (advertiseRevocation) "\n          \"revocation_endpoint\": \"${server.url("/oauth2/revoke")}\"," else ""}
          "scopes_supported": ["openid", "profile", "device_sso"],
          "dpop_signing_alg_values_supported": ["ES256"]
        }
    """.trimIndent()

    private fun tokenResponse(): String {
        val now = Instant.now()
        val claims = JWTClaimsSet.Builder()
            .issuer(issuer)
            .subject(subject)
            .audience(clientId)
            .issueTime(Date.from(now))
            .expirationTime(Date.from(now.plusSeconds(120)))
            .claim("nonce", nonceOverride ?: parForm["nonce"])
            .claim("at_hash", TokenHashes.halfHash(accessToken, "PS256"))
        val secret = deviceSecret
        if (secret != null) {
            claims.claim("sid", "sid-1").claim("ds_hash", dsHashOverride ?: TokenHashes.dsHash(secret))
        }
        val idToken = sign(claims.build())
        val ds = if (secret != null) ""","device_secret":"$secret"""" else ""
        return """{"access_token":"$accessToken","token_type":"DPoP","expires_in":300,""" +
            """"scope":"${parForm["scope"]}","id_token":"$idToken"$ds}"""
    }

    private fun userinfoClaims(): JWTClaimsSet {
        val b = JWTClaimsSet.Builder().subject(userinfoSub ?: subject).issuer(issuer).audience(clientId)
        userinfoName?.let { b.claim("name", it) }
        return b.build()
    }

    fun sign(claims: JWTClaimsSet): String =
        SignedJWT(JWSHeader.Builder(JWSAlgorithm.PS256).keyID(IssuerKey.rsa.keyID).type(JOSEObjectType.JWT).build(), claims)
            .apply { sign(RSASSASigner(IssuerKey.rsa)) }.serialize()

    private fun json(body: String, code: Int = 200) =
        MockResponse().setResponseCode(code).setHeader("Content-Type", "application/json").setBody(body)
}

/** Form parameters of an urlencoded request body. */
internal fun RecordedRequest.form(): Map<String, String> =
    body.readUtf8().split('&').filter { it.isNotEmpty() }.associate {
        URLDecoder.decode(it.substringBefore('='), "UTF-8") to URLDecoder.decode(it.substringAfter('='), "UTF-8")
    }

/** Checks a client-made ES256 JWS against the public [jwk] and returns it. */
internal fun verifiedJws(jws: String, jwk: Map<String, Any?>): JWSObject {
    val parsed = JWSObject.parse(jws)
    check(parsed.verify(ECDSAVerifier(ECKey.parse(jwk)))) { "JWS signature does not verify" }
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

/** A clock a test can move forward. */
internal class MutableClock(var now: Instant = Instant.now()) : Clock() {
    override fun getZone(): ZoneId = ZoneOffset.UTC
    override fun withZone(zone: ZoneId?): Clock = this
    override fun instant(): Instant = now
}
