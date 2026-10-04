// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import com.nimbusds.jose.util.JSONObjectUtils
import java.security.MessageDigest
import java.util.Base64

/** Base64url without padding (RFC 7515 section 2), the encoding of every JOSE part. */
internal object B64 {
    private val encoder = Base64.getUrlEncoder().withoutPadding()
    private val decoder = Base64.getUrlDecoder()

    fun encode(bytes: ByteArray): String = encoder.encodeToString(bytes)

    fun encode(text: String): String = encode(text.toByteArray(Charsets.UTF_8))

    fun decode(text: String): ByteArray = decoder.decode(text)
}

internal fun sha256(bytes: ByteArray): ByteArray = MessageDigest.getInstance("SHA-256").digest(bytes)

internal fun sha256(text: String): ByteArray = sha256(text.toByteArray(Charsets.US_ASCII))

/** Compact JSON for JOSE headers and claim sets. */
internal fun toJson(map: Map<String, Any?>): String = JSONObjectUtils.toJSONString(map.filterValues { it != null })

/** Parses a JSON object; anything else is a protocol error. */
internal fun parseJsonObject(body: String, what: String): Map<String, Any?> =
    try {
        JSONObjectUtils.parse(body)
    } catch (e: java.text.ParseException) {
        throw InvalidResponseException("$what is not a JSON object", e)
    }

internal fun Map<String, Any?>.requireString(name: String, what: String): String =
    when (val value = this[name]) {
        is String -> value.ifEmpty { throw InvalidResponseException("$what: \"$name\" is empty") }
        null -> throw InvalidResponseException("$what: \"$name\" is missing")
        else -> throw InvalidResponseException("$what: \"$name\" is not a string")
    }

internal fun Map<String, Any?>.optString(name: String, what: String): String? =
    when (val value = this[name]) {
        null -> null
        is String -> value
        else -> throw InvalidResponseException("$what: \"$name\" is not a string")
    }

internal fun Map<String, Any?>.optLong(name: String, what: String): Long? =
    when (val value = this[name]) {
        null -> null
        is Number -> value.toLong()
        else -> throw InvalidResponseException("$what: \"$name\" is not a number")
    }

internal fun Map<String, Any?>.optStringList(name: String, what: String): List<String>? =
    when (val value = this[name]) {
        null -> null
        is List<*> -> value.map { it as? String ?: throw InvalidResponseException("$what: \"$name\" holds a non-string") }
        else -> throw InvalidResponseException("$what: \"$name\" is not an array")
    }
