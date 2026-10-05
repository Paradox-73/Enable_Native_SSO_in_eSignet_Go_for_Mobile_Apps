// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import com.nimbusds.jose.JOSEObjectType
import com.nimbusds.jose.JWSAlgorithm
import com.nimbusds.jose.JWSHeader
import com.nimbusds.jose.JWSObject
import com.nimbusds.jose.crypto.ECDSASigner
import com.nimbusds.jose.crypto.ECDSAVerifier
import com.nimbusds.jose.crypto.RSASSASigner
import com.nimbusds.jose.jwk.Curve
import com.nimbusds.jose.jwk.ECKey
import com.nimbusds.jose.jwk.JWK
import com.nimbusds.jose.jwk.JWKSet
import com.nimbusds.jose.jwk.KeyUse
import com.nimbusds.jose.jwk.RSAKey
import com.nimbusds.jose.jwk.gen.ECKeyGenerator
import com.nimbusds.jose.jwk.gen.RSAKeyGenerator
import com.nimbusds.jwt.JWTClaimsSet
import com.nimbusds.jwt.SignedJWT
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.Assert.assertTrue
import java.net.URLDecoder
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import java.util.Date

/** A fixed point in time, so every iat and exp in the tests is predictable. */
internal val NOW: Instant = Instant.parse("2026-10-04T10:00:00Z")
internal val FIXED_CLOCK: Clock = Clock.fixed(NOW, ZoneOffset.UTC)

/** Issuer keys generated once per test run (RSA generation is slow). */
internal object IssuerKeys {
    val rsa: RSAKey = RSAKeyGenerator(2048).keyID("rsa-1").keyUse(KeyUse.SIGNATURE).generate()
    val ec: ECKey = ECKeyGenerator(Curve.P_256).keyID("ec-1").keyUse(KeyUse.SIGNATURE).generate()
    val other: RSAKey = RSAKeyGenerator(2048).keyID("rsa-1").keyUse(KeyUse.SIGNATURE).generate()
    val jwks: JWKSet = JWKSet(listOf<JWK>(rsa.toPublicJWK(), ec.toPublicJWK()))
}

/** Signs id_tokens the way an issuer would; [claims] start from a valid default set. */
internal class TestIssuer(val issuer: String = "http://localhost:38088") {
    fun claims(
        audience: String = "hub-client",
        nonce: String? = "n-0S6_WzA2Mj",
        expiresAt: Instant = NOW.plusSeconds(120),
        issuedAt: Instant = NOW,
        extra: Map<String, Any?> = emptyMap(),
    ): JWTClaimsSet.Builder {
        val builder = JWTClaimsSet.Builder()
            .issuer(issuer)
            .subject("psut-123")
            .audience(audience)
            .expirationTime(Date.from(expiresAt))
            .issueTime(Date.from(issuedAt))
        if (nonce != null) builder.claim("nonce", nonce)
        extra.forEach { (k, v) -> builder.claim(k, v) }
        return builder
    }

    fun signPs256(claims: JWTClaimsSet, key: RSAKey = IssuerKeys.rsa): String =
        SignedJWT(JWSHeader.Builder(JWSAlgorithm.PS256).keyID(key.keyID).type(JOSEObjectType.JWT).build(), claims)
            .apply { sign(RSASSASigner(key)) }.serialize()

    fun signEs256(claims: JWTClaimsSet, key: ECKey = IssuerKeys.ec): String =
        SignedJWT(JWSHeader.Builder(JWSAlgorithm.ES256).keyID(key.keyID).build(), claims)
            .apply { sign(ECDSASigner(key)) }.serialize()
}

/** Discovery document for a MockWebServer that plays the issuer at its own URL. */
internal fun discoveryJson(server: MockWebServer, issuer: String = server.issuer()): String = """
    {
      "issuer": "$issuer",
      "authorization_endpoint": "${server.url("/authorize")}",
      "token_endpoint": "${server.url("/oauth2/token")}",
      "pushed_authorization_request_endpoint": "${server.url("/oauth2/par")}",
      "userinfo_endpoint": "${server.url("/oauth2/userinfo")}",
      "revocation_endpoint": "${server.url("/oauth2/revoke")}",
      "jwks_uri": "${server.url("/oauth2/jwks")}",
      "scopes_supported": ["openid", "profile", "email", "device_sso"],
      "grant_types_supported": ["authorization_code", "urn:ietf:params:oauth:grant-type:token-exchange"],
      "id_token_signing_alg_values_supported": ["PS256"],
      "dpop_signing_alg_values_supported": ["ES256", "PS256"],
      "native_sso_supported": true
    }
""".trimIndent()

/** Issuer identifier of a MockWebServer: its base URL without the trailing slash. */
internal fun MockWebServer.issuer(): String = url("/").toString().trimEnd('/')

/** Form parameters of a recorded urlencoded request body. */
internal fun RecordedRequest.form(): Map<String, String> =
    body.readUtf8().split('&').filter { it.isNotEmpty() }.associate {
        URLDecoder.decode(it.substringBefore('='), "UTF-8") to URLDecoder.decode(it.substringAfter('='), "UTF-8")
    }

/** Parses a compact JWS made by the client and checks its ES256 signature with [jwk]. */
internal fun verifiedJws(jws: String, jwk: Map<String, Any?>): JWSObject {
    val parsed = JWSObject.parse(jws)
    assertTrue("JWS signature must verify", parsed.verify(ECDSAVerifier(ECKey.parse(jwk))))
    return parsed
}

/** A token endpoint success body. */
internal fun tokenJson(
    idToken: String? = null,
    deviceSecret: String? = null,
    tokenType: String = "DPoP",
    issuedTokenType: String? = null,
): String = buildString {
    append("""{"access_token":"at-123","token_type":"$tokenType","expires_in":300,"scope":"openid profile"""")
    if (idToken != null) append(""","id_token":"$idToken"""")
    if (deviceSecret != null) append(""","device_secret":"$deviceSecret"""")
    if (issuedTokenType != null) append(""","issued_token_type":"$issuedTokenType"""")
    append("}")
}
