// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.auth

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.text.ParseException
import java.time.Instant

/** The records Hub writes to the encrypted store, the name rule, the config and the client_id rule. */
class StoredRecordsTest {
    private val bundle = TokenBundle(
        issuer = "http://localhost:38088",
        clientId = "ps03-hub-0123456789ab",
        subject = "psut-1",
        sessionId = "sid-1",
        accessToken = "access-token-value",
        tokenType = "DPoP",
        accessTokenExpiresAt = Instant.ofEpochSecond(1_800_000_300),
        idToken = "id-token-value",
        idTokenExpiresAt = Instant.ofEpochSecond(1_800_000_120),
        refreshToken = null,
        deviceSecret = "device-secret-value",
        scope = "openid profile device_sso",
        dpopKeyThumbprint = "jkt-1",
        displayName = "Siddharth K Mansour",
        signedInAt = Instant.ofEpochSecond(1_800_000_000),
    )

    @Test
    fun `token bundle round trips, also without optional fields`() {
        assertEquals(bundle, TokenBundle.fromJson(bundle.toJson()))
        val withEndpoints = bundle.copy(tokenEndpoint = "http://localhost:38088/token", userinfoEndpoint = "http://localhost:38088/userinfo")
        assertEquals(withEndpoints, TokenBundle.fromJson(withEndpoints.toJson()))
        val minimal = bundle.copy(sessionId = null, accessTokenExpiresAt = null, deviceSecret = null, scope = null, displayName = null)
        assertEquals(minimal, TokenBundle.fromJson(minimal.toJson()))
        assertFalse(String(minimal.toJson()).contains("device_secret"))
    }

    @Test
    fun `toString shows no token`() {
        val text = bundle.toString() + PendingLogin("st", "no", "v".repeat(43), "x://y/z", true, Instant.EPOCH).toString()
        for (secret in listOf("access-token-value", "id-token-value", "device-secret-value", "st,", "vvvv")) {
            assertFalse("$secret in $text", text.contains(secret))
        }
        assertTrue(text.contains("deviceSecret=true"))
    }

    @Test
    fun `pending login round trips`() {
        val pending = PendingLogin("state-1", "nonce-1", "v".repeat(43), "org.x://oauth/callback", true, Instant.ofEpochSecond(42))
        assertEquals(pending, PendingLogin.fromJson(pending.toJson()))
    }

    @Test
    fun `records of another version or shape are refused`() {
        val text = String(bundle.toJson())
        assertThrows(ParseException::class.java) { TokenBundle.fromJson(text.replace("\"v\":1", "\"v\":2").toByteArray()) }
        assertThrows(ParseException::class.java) { TokenBundle.fromJson(text.replace("\"sub\"", "\"subject\"").toByteArray()) }
        assertThrows(ParseException::class.java) { TokenBundle.fromJson(text.replace("\"psut-1\"", "7").toByteArray()) }
        assertThrows(ParseException::class.java) { PendingLogin.fromJson("""{"v":1,"state":"s"}""".toByteArray()) }
        assertThrows(ParseException::class.java) { PendingLogin.fromJson("[]".toByteArray()) }
    }

    @Test
    fun `display name comes from name, a tagged name, or given and family name`() {
        assertEquals("Ana", UserInfoNames.displayName(mapOf("name" to " Ana ")))
        assertEquals("Ana E", UserInfoNames.displayName(mapOf("name#fr" to "Ana F", "name#en" to "Ana E")))
        assertEquals("Ana F", UserInfoNames.displayName(mapOf("name#fr" to "Ana F")))
        assertEquals("Ana Lima", UserInfoNames.displayName(mapOf("given_name" to "Ana", "family_name" to "Lima")))
        assertEquals("Ana", UserInfoNames.displayName(mapOf("name" to "", "given_name" to "Ana")))
        assertNull(UserInfoNames.displayName(mapOf("sub" to "x", "name" to 5)))
    }

    @Test
    fun `redirect match needs scheme, host and path`() {
        val config = HubConfig("http://localhost:38088", "c", "org.badmosip.nativesso.superapp://oauth/callback")
        assertTrue(config.isRedirect("org.badmosip.nativesso.superapp://oauth/callback?code=1&state=2"))
        assertTrue(config.isRedirect("ORG.badmosip.nativesso.superapp://OAUTH/callback"))
        assertFalse(config.isRedirect("org.badmosip.nativesso.superapp://oauth/other?code=1"))
        assertFalse(config.isRedirect("org.badmosip.nativesso.superapp://evil/callback"))
        assertFalse(config.isRedirect("https://oauth/callback"))
        assertFalse(config.isRedirect("not a uri"))
    }

    @Test
    fun `config refuses device_sso in the base scopes and a redirect without host`() {
        assertThrows(IllegalArgumentException::class.java) {
            HubConfig("http://i", "c", "org.x://oauth/cb", scopes = setOf("openid", "device_sso"))
        }
        assertThrows(IllegalArgumentException::class.java) { HubConfig("http://i", "c", "org.x:/cb") }
        assertThrows(IllegalArgumentException::class.java) { HubConfig("http://i", "c", "org.x://oauth/cb", scopes = setOf("profile")) }
    }

    @Test
    fun `client_id is the prefix and 12 hex characters, stable per key`() {
        val a = ClientIdentity.clientIdFor("ps03-hub-", "thumb-a")
        assertTrue(a, Regex("ps03-hub-[0-9a-f]{12}").matches(a))
        assertEquals(a, ClientIdentity.clientIdFor("ps03-hub-", "thumb-a"))
        assertFalse(a == ClientIdentity.clientIdFor("ps03-hub-", "thumb-b"))
    }
}
