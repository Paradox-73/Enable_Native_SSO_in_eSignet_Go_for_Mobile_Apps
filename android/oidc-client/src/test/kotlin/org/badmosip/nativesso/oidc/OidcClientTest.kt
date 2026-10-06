// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import com.nimbusds.jose.JWSObject
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.SocketPolicy
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.util.concurrent.TimeUnit

/** The HTTP messages, against MockWebServer playing eSignet. */
class OidcClientTest {
    private val server = MockWebServer()
    private val http = OkHttpClient.Builder().readTimeout(2, TimeUnit.SECONDS).build()
    private val dpopSigner = SoftwareEcSigner.generate()
    private val dpop = DpopProofFactory(dpopSigner, FIXED_CLOCK)
    private val clientSigner = SoftwareEcSigner.generate(keyId = "hub-key")
    private val redirectUri = "org.badmosip.nativesso.superapp://oauth/callback"
    private lateinit var client: OidcClient

    @Before
    fun setUp() {
        server.start()
        val metadata = ProviderMetadata.parse(discoveryJson(server))
        client = OidcClient(http, metadata, "hub-client", ClientAssertionFactory("hub-client", clientSigner, clock = FIXED_CLOCK))
    }

    @After
    fun tearDown() = server.shutdown()

    private fun json(code: Int, body: String) =
        MockResponse().setResponseCode(code).setHeader("Content-Type", "application/json").setBody(body)

    private fun proofClaims(header: String?): Map<String, Any?> =
        verifiedJws(requireNotNull(header) { "DPoP header missing" }, dpopSigner.publicJwk).payload.toJSONObject()

    private fun session() = LoginSession.create()

    // --- discovery -------------------------------------------------------------------------------

    @Test
    fun `discovery reads the metadata of the requested issuer`() {
        server.enqueue(json(200, discoveryJson(server)))
        val metadata = OidcClient.discover(http, server.issuer())
        assertEquals(server.url("/oauth2/par").toString(), metadata.pushedAuthorizationRequestEndpoint)
        assertTrue(metadata.nativeSsoSupported)
        assertTrue(Scopes.DEVICE_SSO in metadata.scopesSupported)
        assertEquals("/.well-known/openid-configuration", server.takeRequest().path)
    }

    @Test
    fun `discovery naming another issuer is refused`() {
        server.enqueue(json(200, discoveryJson(server, issuer = "https://evil.example")))
        assertThrows(InvalidResponseException::class.java) { OidcClient.discover(http, server.issuer()) }
    }

    @Test
    fun `discovery without a required endpoint or with HTTP 500 fails with a typed error`() {
        server.enqueue(json(200, """{"issuer":"${server.issuer()}","authorization_endpoint":"a","jwks_uri":"j"}"""))
        assertThrows(InvalidResponseException::class.java) { OidcClient.discover(http, server.issuer()) }
        server.enqueue(MockResponse().setResponseCode(500))
        val e = assertThrows(HttpStatusException::class.java) { OidcClient.discover(http, server.issuer()) }
        assertEquals(500, e.httpStatus)
    }

    @Test
    fun `network failure is a NetworkException`() {
        server.enqueue(MockResponse().setSocketPolicy(SocketPolicy.DISCONNECT_AT_START))
        assertThrows(NetworkException::class.java) { OidcClient.discover(http, server.issuer()) }
    }

    // --- PAR and the browser step ------------------------------------------------------------------

    @Test
    fun `PAR sends PKCE, device_sso, private_key_jwt and a DPoP proof for the PAR endpoint`() {
        server.enqueue(json(201, """{"request_uri":"urn:ietf:params:oauth:request_uri:abc","expires_in":60}"""))
        val session = session()
        val request = AuthorizationRequest(
            redirectUri = redirectUri,
            scope = linkedSetOf("openid", "profile", "email", Scopes.DEVICE_SSO),
            session = session,
            acrValues = "mosip:idp:acr:generated-code",
        )
        val par = client.pushAuthorizationRequest(request, dpop)

        assertEquals("urn:ietf:params:oauth:request_uri:abc", par.requestUri)
        assertEquals(60, par.expiresIn)
        val recorded = server.takeRequest()
        assertEquals("POST", recorded.method)
        assertEquals("/oauth2/par", recorded.path)
        val form = recorded.form()
        assertEquals("code", form["response_type"])
        assertEquals("hub-client", form["client_id"])
        assertEquals("openid profile email device_sso", form["scope"])
        assertEquals(session.pkce.challenge, form["code_challenge"])
        assertEquals("S256", form["code_challenge_method"])
        assertEquals(session.state, form["state"])
        assertEquals(session.nonce, form["nonce"])
        assertEquals("mosip:idp:acr:generated-code", form["acr_values"])
        assertFalse("the verifier never goes to the PAR endpoint", form.containsValue(session.pkce.verifier))
        assertEquals(ClientAssertion.TYPE_JWT_BEARER, form["client_assertion_type"])
        val assertion = verifiedJws(form.getValue("client_assertion"), clientSigner.publicJwk).payload.toJSONObject()
        assertEquals(server.issuer(), assertion["aud"])
        assertEquals(server.url("/oauth2/par").toString(), proofClaims(recorded.getHeader("DPoP"))["htu"])
    }

    @Test
    fun `PAR error is typed, for example invalid_scope for a client not allowed device_sso`() {
        server.enqueue(json(400, """{"error":"invalid_scope","error_description":"device_sso not allowed"}"""))
        val request = AuthorizationRequest(redirectUri, setOf("openid", Scopes.DEVICE_SSO), session())
        val e = assertThrows(OAuthErrorException::class.java) { client.pushAuthorizationRequest(request, dpop) }
        assertEquals(OAuthError.INVALID_SCOPE, e.error)
        assertEquals(400, e.httpStatus)
        assertEquals("device_sso not allowed", e.description)
    }

    @Test
    fun `authorization request without openid is a programming error`() {
        assertThrows(IllegalArgumentException::class.java) { AuthorizationRequest(redirectUri, setOf("profile"), session()) }
    }

    @Test
    fun `authorization URL carries only client_id and request_uri`() {
        val url = client.authorizationUrl(ParResponse("urn:ietf:params:oauth:request_uri:a b&c", 60)).toHttpUrl()
        assertEquals(server.url("/authorize").encodedPath, url.encodedPath)
        assertEquals(setOf("client_id", "request_uri"), url.queryParameterNames)
        assertEquals("hub-client", url.queryParameter("client_id"))
        assertEquals("urn:ietf:params:oauth:request_uri:a b&c", url.queryParameter("request_uri"))
    }

    @Test
    fun `redirect parsing checks state and iss and returns the code`() {
        val s = session()
        val ok = "$redirectUri?code=c-1&state=${s.state}&iss=${java.net.URLEncoder.encode(server.issuer(), "UTF-8")}"
        assertEquals("c-1", client.parseAuthorizationResponse(ok, s))
        assertThrows(InvalidResponseException::class.java) {
            client.parseAuthorizationResponse("$redirectUri?code=c-1&state=forged", s)
        }
        assertThrows(InvalidResponseException::class.java) {
            client.parseAuthorizationResponse("$redirectUri?code=c-1&state=${s.state}&iss=https%3A%2F%2Fevil.example", s)
        }
    }

    @Test
    fun `redirect with an error becomes a typed error`() {
        val s = session()
        val e = assertThrows(OAuthErrorException::class.java) {
            client.parseAuthorizationResponse("$redirectUri?error=access_denied&error_description=User+cancelled&state=${s.state}", s)
        }
        assertEquals(OAuthError.ACCESS_DENIED, e.error)
        assertEquals("User cancelled", e.description)
        assertEquals(0, e.httpStatus)
    }

    // --- token endpoint ----------------------------------------------------------------------------

    @Test
    fun `code exchange sends the verifier and returns the device secret`() {
        server.enqueue(json(200, tokenJson(idToken = "id.token.value", deviceSecret = "ds-jwe")))
        val s = session()
        val tokens = client.exchangeAuthorizationCode("c-1", redirectUri, s, dpop)

        assertEquals("at-123", tokens.accessToken)
        assertEquals("ds-jwe", tokens.deviceSecret)
        assertEquals("id.token.value", tokens.idToken)
        assertFalse("tokens never appear in toString", tokens.toString().contains("ds-jwe"))
        val recorded = server.takeRequest()
        val form = recorded.form()
        assertEquals("authorization_code", form["grant_type"])
        assertEquals("c-1", form["code"])
        assertEquals(redirectUri, form["redirect_uri"])
        assertEquals(s.pkce.verifier, form["code_verifier"])
        assertEquals(server.url("/oauth2/token").toString(), proofClaims(recorded.getHeader("DPoP"))["htu"])
    }

    @Test
    fun `a Bearer token where DPoP was asked for is refused`() {
        server.enqueue(json(200, tokenJson(tokenType = "Bearer")))
        assertThrows(InvalidResponseException::class.java) { client.exchangeAuthorizationCode("c", redirectUri, session(), dpop) }
    }

    @Test
    fun `use_dpop_nonce is answered once with the server nonce and a fresh assertion`() {
        server.enqueue(json(400, """{"error":"use_dpop_nonce"}""").setHeader("DPoP-Nonce", "nonce-1"))
        server.enqueue(json(200, tokenJson()).setHeader("DPoP-Nonce", "nonce-2"))
        client.exchangeAuthorizationCode("c", redirectUri, session(), dpop)

        val first = server.takeRequest()
        val second = server.takeRequest()
        assertFalse("nonce" in proofClaims(first.getHeader("DPoP")))
        assertEquals("nonce-1", proofClaims(second.getHeader("DPoP"))["nonce"])
        val jti = { r: okhttp3.mockwebserver.RecordedRequest -> JWSObject.parse(r.form().getValue("client_assertion")).payload.toJSONObject()["jti"] }
        assertNotEquals(jti(first), jti(second))

        // The latest nonce is remembered for the next request to the same server.
        server.enqueue(json(200, tokenJson()))
        client.exchangeAuthorizationCode("c2", redirectUri, session(), dpop)
        assertEquals("nonce-2", proofClaims(server.takeRequest().getHeader("DPoP"))["nonce"])
    }

    @Test
    fun `a second use_dpop_nonce is not retried again`() {
        repeat(2) { server.enqueue(json(400, """{"error":"use_dpop_nonce"}""").setHeader("DPoP-Nonce", "n$it")) }
        val e = assertThrows(OAuthErrorException::class.java) { client.exchangeAuthorizationCode("c", redirectUri, session(), dpop) }
        assertEquals(OAuthError.USE_DPOP_NONCE, e.error)
        assertEquals(2, server.requestCount)
    }

    @Test
    fun `non-JSON error body is an HttpStatusException`() {
        server.enqueue(MockResponse().setResponseCode(502).setBody("<html>bad gateway</html>"))
        val e = assertThrows(HttpStatusException::class.java) { client.exchangeAuthorizationCode("c", redirectUri, session(), dpop) }
        assertEquals(502, e.httpStatus)
    }

    // --- Native SSO token exchange -----------------------------------------------------------------

    @Test
    fun `token exchange sends the RFC 8693 profile of Native SSO section 4_1`() {
        server.enqueue(json(200, tokenJson(idToken = "child.id.token", issuedTokenType = TokenTypes.ACCESS_TOKEN)))
        val exchange = DeviceSecretExchange("hub.id.token", "ds-jwe", linkedSetOf("openid", "email"))
        val tokens = client.exchangeDeviceSecret(exchange, dpop)

        assertEquals("child.id.token", tokens.idToken)
        val recorded = server.takeRequest()
        val form = recorded.form()
        assertEquals("urn:ietf:params:oauth:grant-type:token-exchange", form["grant_type"])
        assertEquals("hub.id.token", form["subject_token"])
        assertEquals("urn:ietf:params:oauth:token-type:id_token", form["subject_token_type"])
        assertEquals("ds-jwe", form["actor_token"])
        assertEquals("urn:openid:params:token-type:device-secret", form["actor_token_type"])
        assertEquals(server.issuer(), form["audience"])
        assertEquals("openid email", form["scope"])
        assertEquals("hub-client", form["client_id"])
        assertTrue(form.containsKey("client_assertion"))
        assertEquals("POST", proofClaims(recorded.getHeader("DPoP"))["htm"])
    }

    @Test
    fun `token exchange never asks for device_sso and always for openid`() {
        assertThrows(IllegalArgumentException::class.java) {
            DeviceSecretExchange("id", "ds", setOf("openid", Scopes.DEVICE_SSO))
        }
        assertThrows(IllegalArgumentException::class.java) { DeviceSecretExchange("id", "ds", setOf("email")) }
        assertThrows(IllegalArgumentException::class.java) { DeviceSecretExchange("id", " ", setOf("openid")) }
    }

    @Test
    fun `scope beyond the original consent gives interaction_required`() {
        server.enqueue(json(400, """{"error":"interaction_required","error_description":"consent needed for: phone"}"""))
        val e = assertThrows(OAuthErrorException::class.java) {
            client.exchangeDeviceSecret(DeviceSecretExchange("id", "ds", setOf("openid", "phone")), dpop)
        }
        assertTrue(e.requiresInteraction)
        assertEquals(OAuthError.INTERACTION_REQUIRED, e.error)
        assertEquals(null, e.consentUri)
    }

    @Test
    fun `interaction_required with a consent page carries consent_uri`() {
        server.enqueue(
            json(
                400,
                """{"error":"interaction_required","error_description":"user consent is needed for: health_card_share",""" +
                    """"consent_uri":"https://esignet.example/oauth2/native-sso/consent?ticket=t","consent_expires_in":300}""",
            ),
        )
        val e = assertThrows(OAuthErrorException::class.java) {
            client.exchangeDeviceSecret(DeviceSecretExchange("id", "ds", setOf("openid", "health_card_share")), dpop)
        }
        assertTrue(e.requiresInteraction)
        assertEquals("https://esignet.example/oauth2/native-sso/consent?ticket=t", e.consentUri)
        assertFalse(e.message!!.contains("ticket"))
    }

    @Test
    fun `rejected device secret maps to typed errors`() {
        server.enqueue(json(400, """{"error":"invalid_grant","error_description":"session ended"}"""))
        val e = assertThrows(OAuthErrorException::class.java) {
            client.exchangeDeviceSecret(DeviceSecretExchange("id", "ds", setOf("openid")), dpop)
        }
        assertEquals(OAuthError.INVALID_GRANT, e.error)
        assertFalse(e.requiresInteraction)
    }

    @Test
    fun `exchange that issues something other than an access token is refused`() {
        server.enqueue(json(200, tokenJson(issuedTokenType = "urn:ietf:params:oauth:token-type:refresh_token")))
        assertThrows(InvalidResponseException::class.java) {
            client.exchangeDeviceSecret(DeviceSecretExchange("id", "ds", setOf("openid")), dpop)
        }
    }

    // --- revocation (RFC 7009) ----------------------------------------------------------------------

    @Test
    fun `device secret revocation sends the token, the hint and private_key_jwt, without a DPoP proof`() {
        server.enqueue(MockResponse().setResponseCode(200))
        assertTrue(client.revokeToken("ds-jwe", TokenTypes.DEVICE_SECRET_HINT))

        val recorded = server.takeRequest()
        assertEquals("/oauth2/revoke", recorded.path)
        assertEquals(null, recorded.getHeader("DPoP"))
        val form = recorded.form()
        assertEquals("ds-jwe", form["token"])
        assertEquals("device_secret", form["token_type_hint"])
        assertEquals("hub-client", form["client_id"])
        assertEquals(ClientAssertion.TYPE_JWT_BEARER, form["client_assertion_type"])
        val assertion = verifiedJws(form.getValue("client_assertion"), clientSigner.publicJwk).payload.toJSONObject()
        assertEquals(server.issuer(), assertion["aud"])
    }

    @Test
    fun `revocation refused by the server is a typed error`() {
        server.enqueue(json(400, """{"error":"invalid_grant","error_description":"not issued to this client"}"""))
        val e = assertThrows(OAuthErrorException::class.java) { client.revokeToken("ds-jwe", TokenTypes.DEVICE_SECRET_HINT) }
        assertEquals(OAuthError.INVALID_GRANT, e.error)

        server.enqueue(MockResponse().setResponseCode(503))
        assertThrows(HttpStatusException::class.java) { client.revokeToken("ds-jwe") }
    }

    @Test
    fun `without a revocation endpoint nothing is sent`() {
        val metadata = ProviderMetadata.parse(discoveryJson(server)).copy(revocationEndpoint = null)
        val bare = OidcClient(http, metadata, "hub-client", ClientAssertionFactory("hub-client", clientSigner, clock = FIXED_CLOCK))
        assertFalse(bare.revokeToken("ds-jwe", TokenTypes.DEVICE_SECRET_HINT))
        assertEquals(0, server.requestCount)
    }

    // --- userinfo ----------------------------------------------------------------------------------

    @Test
    fun `userinfo sends the DPoP scheme and a proof with ath`() {
        server.enqueue(json(200, """{"sub":"psut-123","email":"a@example.org"}"""))
        val claims = client.userInfo("at-123", dpop)

        assertEquals("a@example.org", claims["email"])
        val recorded = server.takeRequest()
        assertEquals("GET", recorded.method)
        assertEquals("DPoP at-123", recorded.getHeader("Authorization"))
        val proof = proofClaims(recorded.getHeader("DPoP"))
        assertEquals("GET", proof["htm"])
        assertEquals(DpopProofs.accessTokenHash("at-123"), proof["ath"])
    }

    @Test
    fun `userinfo retries once on a resource-server nonce challenge`() {
        server.enqueue(
            MockResponse().setResponseCode(401)
                .setHeader("WWW-Authenticate", """DPoP error="use_dpop_nonce", error_description="nonce required"""")
                .setHeader("DPoP-Nonce", "rs-nonce"),
        )
        server.enqueue(json(200, """{"sub":"psut-123"}"""))
        assertEquals("psut-123", client.userInfo("at-123", dpop)["sub"])
        server.takeRequest()
        assertEquals("rs-nonce", proofClaims(server.takeRequest().getHeader("DPoP"))["nonce"])
    }

    @Test
    fun `userinfo token error from the WWW-Authenticate header is typed`() {
        server.enqueue(MockResponse().setResponseCode(401).setHeader("WWW-Authenticate", """DPoP error="invalid_token""""))
        val e = assertThrows(OAuthErrorException::class.java) { client.userInfo("at-123", dpop) }
        assertEquals(OAuthError.INVALID_TOKEN, e.error)
        assertEquals(401, e.httpStatus)
    }

    @Test
    fun `signed userinfo is verified with the issuer keys`() {
        val issuer = TestIssuer(server.issuer())
        val jws = issuer.signPs256(issuer.claims(nonce = null).claim("email", "a@example.org").build())
        val verifier = IssuerSignatureVerifier({ IssuerKeys.jwks })
        server.enqueue(MockResponse().setHeader("Content-Type", "application/jwt").setBody(jws))
        assertEquals("a@example.org", client.userInfo("at-123", dpop, verifier::claimsOf)["email"])

        val forged = issuer.signPs256(issuer.claims(nonce = null).build(), IssuerKeys.other)
        server.enqueue(MockResponse().setHeader("Content-Type", "application/jwt").setBody(forged))
        assertThrows(InvalidResponseException::class.java) { client.userInfo("at-123", dpop, verifier::claimsOf) }
    }
}
