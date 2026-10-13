// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import okhttp3.FormBody
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import java.io.IOException
import java.net.URI
import java.util.concurrent.ConcurrentHashMap

/**
 * OpenID Connect client for one app and one issuer, following the FAPI 2.0 profile eSignet asks
 * of relying parties: pushed authorization requests, PKCE S256, DPoP-bound tokens and
 * `private_key_jwt` client authentication. On top it speaks the two Native SSO messages: the
 * authorization-code grant that returns a device secret, and the token exchange that turns the
 * Super App's id_token and device secret into the Child App's own tokens.
 *
 * All calls block; run them off the main thread. Errors are [OidcException] subtypes.
 *
 * @param clientId this app's client_id
 * @param clientAuth signs this app's client assertions
 */
public class OidcClient(
    private val http: OkHttpClient,
    public val metadata: ProviderMetadata,
    public val clientId: String,
    private val clientAuth: ClientAssertionFactory,
) {
    // Latest DPoP-Nonce per origin (RFC 9449 section 8): sent on the next request to that server.
    private val dpopNonces = ConcurrentHashMap<String, String>()

    /**
     * Sends the authorization request to the PAR endpoint (RFC 9126) with a DPoP proof, so the
     * authorization code is bound to the DPoP key from the start (RFC 9449 section 10).
     */
    public fun pushAuthorizationRequest(request: AuthorizationRequest, dpop: DpopProofSource): ParResponse {
        val endpoint = metadata.pushedAuthorizationRequestEndpoint
            ?: throw InvalidResponseException("the server has no pushed_authorization_request_endpoint")
        val params = linkedMapOf(
            "response_type" to "code",
            "redirect_uri" to request.redirectUri,
            "scope" to ScopeStrings.format(request.scope),
            "state" to request.session.state,
            "nonce" to request.session.nonce,
            "code_challenge" to request.session.pkce.challenge,
            "code_challenge_method" to Pkce.METHOD,
        )
        request.acrValues?.let { params["acr_values"] = it }
        request.claims?.let { params["claims"] = it }
        request.prompt?.let { params["prompt"] = it }
        request.uiLocales?.let { params["ui_locales"] = it }

        val body = postForm(endpoint, params, dpop)
        val what = "PAR response"
        return ParResponse(
            requestUri = body.requireString("request_uri", what),
            expiresIn = body.optLong("expires_in", what)
                ?: throw InvalidResponseException("$what: \"expires_in\" is missing"),
        )
    }

    /** The URL to open in the browser after PAR (RFC 9126 section 4). */
    public fun authorizationUrl(par: ParResponse): String =
        metadata.authorizationEndpoint.toHttpUrl().newBuilder()
            .addQueryParameter("client_id", clientId)
            .addQueryParameter("request_uri", par.requestUri)
            .build().toString()

    /**
     * Reads the redirect that ends the browser step and returns the authorization code.
     * Checks `state` (RFC 6749 section 10.12) and, when present, `iss` (RFC 9207); an `error`
     * parameter becomes an [OAuthErrorException] with HTTP status 0.
     */
    public fun parseAuthorizationResponse(redirect: String, session: LoginSession): String {
        val params = queryParameters(redirect)
        if (!constantTimeEquals(params["state"], session.state)) {
            throw InvalidResponseException("authorization response: state does not match the login")
        }
        params["iss"]?.let {
            if (it != metadata.issuer) throw InvalidResponseException("authorization response: iss is not the issuer")
        }
        params["error"]?.let { throw OAuthErrorException.of(it, params["error_description"], 0) }
        return params["code"]?.takeIf { it.isNotEmpty() }
            ?: throw InvalidResponseException("authorization response: code is missing")
    }

    /**
     * Exchanges the authorization code (RFC 6749 section 4.1.3, RFC 7636 section 4.5). When the login
     * asked for device_sso the response also holds a device secret (Native SSO 1.0 section 3.4).
     * Validate the id_token with [IdTokenValidator] before keeping anything.
     */
    public fun exchangeAuthorizationCode(
        code: String,
        redirectUri: String,
        session: LoginSession,
        dpop: DpopProofSource,
    ): TokenResponse {
        val params = linkedMapOf(
            "grant_type" to GrantTypes.AUTHORIZATION_CODE,
            "code" to code,
            "redirect_uri" to redirectUri,
            "code_verifier" to session.pkce.verifier,
        )
        return tokenResponse(postForm(metadata.tokenEndpoint, params, dpop))
    }

    /**
     * Native SSO token exchange (Native SSO 1.0 section 4.1, RFC 8693 section 2.1): the Super App's
     * id_token as subject_token, its device secret as actor_token, the issuer as audience.
     * [dpop] must sign with the Super App's key, the key the device secret is bound to. With
     * [DeviceSecretExchange.dpopJkt] the issued tokens are bound to that key instead of [dpop]'s.
     *
     * A server that wants the user to consent to more than the original login answers
     * interaction_required: [OAuthErrorException.requiresInteraction] is then true.
     */
    public fun exchangeDeviceSecret(exchange: DeviceSecretExchange, dpop: DpopProofSource): TokenResponse {
        val params = linkedMapOf(
            "grant_type" to GrantTypes.TOKEN_EXCHANGE,
            "subject_token" to exchange.idToken,
            "subject_token_type" to TokenTypes.ID_TOKEN,
            "actor_token" to exchange.deviceSecret,
            "actor_token_type" to TokenTypes.DEVICE_SECRET,
            "audience" to metadata.issuer,
            "scope" to ScopeStrings.format(exchange.scope),
        )
        exchange.dpopJkt?.let { params[DpopHttp.JKT_PARAM] = it }
        val response = tokenResponse(postForm(metadata.tokenEndpoint, params, dpop))
        if (response.issuedTokenType != null && response.issuedTokenType != TokenTypes.ACCESS_TOKEN) {
            throw InvalidResponseException("token exchange issued ${response.issuedTokenType}, expected an access token")
        }
        return response
    }

    /**
     * Revokes [token] at the revocation endpoint (RFC 7009 section 2.1) with this client's
     * `private_key_jwt` authentication. Hub uses it at "Sign out of all apps" to end its device
     * secret's session on the server. Returns false, sending nothing, when the server advertises no
     * revocation_endpoint. The server answers 200 for a token it does not know (RFC 7009 section
     * 2.2), so true means "the token is no longer usable", not "the token existed".
     */
    public fun revokeToken(token: String, tokenTypeHint: String? = null): Boolean {
        val endpoint = metadata.revocationEndpoint ?: return false
        val form = FormBody.Builder().add("token", token)
        tokenTypeHint?.let { form.add("token_type_hint", it) }
        clientAuth.formParameters(metadata.issuer, endpoint).forEach { (k, v) -> form.add(k, v) }
        val request = Request.Builder().url(endpoint).post(form.build()).header("Accept", "application/json").build()
        val (response, body) = try {
            http.newCall(request).execute().use { it to (it.body?.string().orEmpty()) }
        } catch (e: IOException) {
            throw NetworkException("POST ${stripQuery(endpoint)} failed: ${e.message}", e)
        }
        if (!response.isSuccessful) {
            throw errorOf(response, body) ?: HttpStatusException(response.code, "POST ${stripQuery(endpoint)}: HTTP ${response.code}")
        }
        return true
    }

    /**
     * Calls the userinfo endpoint with a DPoP-bound access token (RFC 9449 section 7.1) and returns
     * the claims. A JSON answer is returned as is; a signed JWT answer (application/jwt) is first
     * verified with [jwtVerifier], usually [IssuerSignatureVerifier.claimsOf] over the issuer's keys.
     */
    public fun userInfo(
        accessToken: String,
        dpop: DpopProofSource,
        jwtVerifier: ((String) -> Map<String, Any?>)? = null,
    ): Map<String, Any?> {
        val endpoint = metadata.userinfoEndpoint ?: throw InvalidResponseException("the server has no userinfo_endpoint")
        val ath = DpopProofs.accessTokenHash(accessToken)
        return execute(endpoint, "GET", ath, dpop, { proof ->
            Request.Builder().url(endpoint).get()
                .header("Authorization", "${DpopHttp.SCHEME} $accessToken")
                .header(DpopHttp.HEADER, proof)
                .header("Accept", "application/json, application/jwt")
                .build()
        }) { response, body ->
            val contentType = response.header("Content-Type").orEmpty().lowercase()
            when {
                contentType.startsWith("application/json") -> parseJsonObject(body, "userinfo response")
                contentType.startsWith("application/jwt") -> {
                    val verifier = jwtVerifier ?: throw InvalidResponseException("userinfo is a JWT but no verifier was given")
                    if (body.count { it == '.' } != 2) {
                        throw InvalidResponseException("userinfo is encrypted; this client reads signed userinfo only")
                    }
                    verifier(body.trim())
                }
                else -> throw InvalidResponseException("userinfo has unexpected content type \"$contentType\"")
            }
        }
    }

    // --- HTTP ------------------------------------------------------------------------------------

    private fun tokenResponse(body: Map<String, Any?>): TokenResponse {
        val response = TokenResponse.from(body)
        // RFC 9449 section 5: a client that sent a proof must check it got a DPoP-bound token back.
        if (!response.tokenType.equals(DpopHttp.SCHEME, ignoreCase = true)) {
            throw InvalidResponseException("token_type is \"${response.tokenType}\", expected DPoP")
        }
        return response
    }

    /** POST a form with client authentication and a DPoP proof; returns the JSON answer. */
    private fun postForm(endpoint: String, params: Map<String, String>, dpop: DpopProofSource): Map<String, Any?> =
        execute(endpoint, "POST", null, dpop, { proof ->
            // A new client assertion for every attempt: the server refuses a reused jti.
            val form = FormBody.Builder()
            (params + clientAuth.formParameters(metadata.issuer, endpoint)).forEach { (k, v) -> form.add(k, v) }
            Request.Builder().url(endpoint).post(form.build())
                .header(DpopHttp.HEADER, proof)
                .header("Accept", "application/json")
                .build()
        }) { _, body -> parseJsonObject(body, "response of $endpoint") }

    /**
     * Sends a request with a fresh DPoP proof. When the server answers use_dpop_nonce with a
     * DPoP-Nonce header (RFC 9449 sections 8 and 9), it retries once with that nonce.
     */
    private fun <T> execute(
        url: String,
        method: String,
        ath: String?,
        dpop: DpopProofSource,
        buildRequest: (proof: String) -> Request,
        onSuccess: (Response, String) -> T,
    ): T {
        val origin = originOf(url)
        var retried = false
        while (true) {
            val proof = dpop.proof(DpopRequest(method, url, ath, dpopNonces[origin]))
            val (response, body) = try {
                http.newCall(buildRequest(proof)).execute().use { it to (it.body?.string().orEmpty()) }
            } catch (e: IOException) {
                throw NetworkException("$method ${stripQuery(url)} failed: ${e.message}", e)
            }
            val newNonce = response.header(DpopHttp.NONCE_HEADER)
            if (!newNonce.isNullOrEmpty()) dpopNonces[origin] = newNonce
            if (response.isSuccessful) return onSuccess(response, body)

            val error = errorOf(response, body)
            if (error?.error == OAuthError.USE_DPOP_NONCE && !newNonce.isNullOrEmpty() && !retried) {
                retried = true
                continue
            }
            throw error ?: HttpStatusException(response.code, "$method ${stripQuery(url)}: HTTP ${response.code}")
        }
    }

    /**
     * The OAuth error of a failed response: the JSON body (RFC 6749 section 5.2) or, from a
     * resource server, the WWW-Authenticate header (RFC 6750 section 3, RFC 9449 section 7.1).
     */
    private fun errorOf(response: Response, body: String): OAuthErrorException? {
        val json = try {
            parseJsonObject(body, "error response")
        } catch (e: InvalidResponseException) {
            null
        }
        (json?.get("error") as? String)?.let {
            return OAuthErrorException.of(it, json["error_description"] as? String, response.code, json["consent_uri"] as? String)
        }
        val challenge = response.headers("WWW-Authenticate").firstOrNull { it.startsWith(DpopHttp.SCHEME, ignoreCase = true) }
            ?: response.header("WWW-Authenticate")
            ?: return null
        val attributes = AUTH_PARAM.findAll(challenge).associate { it.groupValues[1].lowercase() to it.groupValues[2] }
        return attributes["error"]?.let { OAuthErrorException.of(it, attributes["error_description"], response.code) }
    }

    public companion object {
        private val AUTH_PARAM = Regex("""([A-Za-z_]+)\s*=\s*"([^"]*)"""")

        /**
         * Reads `<issuer>/.well-known/openid-configuration` (OpenID Connect Discovery 1.0 section 4)
         * and checks that the document names exactly [issuer] (section 4.3), so a redirect or a
         * look-alike host cannot swap the endpoints.
         */
        public fun discover(http: OkHttpClient, issuer: String): ProviderMetadata {
            val url = issuer.trimEnd('/') + "/.well-known/openid-configuration"
            val body = try {
                http.newCall(Request.Builder().url(url).header("Accept", "application/json").get().build()).execute()
                    .use { response ->
                        if (!response.isSuccessful) throw HttpStatusException(response.code, "discovery: HTTP ${response.code}")
                        response.body?.string().orEmpty()
                    }
            } catch (e: IOException) {
                throw NetworkException("discovery failed: ${e.message}", e)
            }
            val metadata = ProviderMetadata.parse(body)
            if (metadata.issuer != issuer) {
                throw InvalidResponseException("discovery names issuer \"${metadata.issuer}\", expected \"$issuer\"")
            }
            return metadata
        }

        private fun originOf(url: String): String = URI(url).let { "${it.scheme}://${it.rawAuthority}" }

        private fun stripQuery(url: String): String = url.substringBefore('?')

        private fun queryParameters(url: String): Map<String, String> {
            val query = URI(url).rawQuery ?: return emptyMap()
            return query.split('&').filter { it.isNotEmpty() }.associate { pair ->
                val name = pair.substringBefore('=')
                val value = pair.substringAfter('=', "")
                decode(name) to decode(value)
            }
        }

        private fun decode(s: String): String = java.net.URLDecoder.decode(s, "UTF-8") // the Charset overload needs Android API 33
    }
}
