// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.oidc

import java.math.BigInteger
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.Signature
import java.security.interfaces.ECPublicKey
import java.security.spec.ECGenParameterSpec

/**
 * Signs JWS signing inputs with a private key the client never sees.
 *
 * The apps implement it over an Android Keystore key, so the DPoP key and the client-authentication
 * key never enter the app process; the tests use [SoftwareEcSigner]. Only ES256 on P-256 is used:
 * eSignet accepts it for DPoP and for client keys, and Android Keystore supports it on every device.
 */
public interface JwsSigner {
    /** JWS "alg" value, for example "ES256". */
    public val algorithm: String

    /** The public key as a JWK (RFC 7517) with public members only. */
    public val publicJwk: Map<String, String>

    /** Optional "kid" for the JWS header (client assertions); null leaves it out. */
    public val keyId: String?

    /**
     * Signs [signingInput] (ASCII of `base64url(header).base64url(payload)`) and returns the JWS
     * signature bytes, for ES256 the 64-byte `R || S` form of RFC 7518 section 3.4.
     */
    public fun sign(signingInput: ByteArray): ByteArray
}

/** Builds a compact JWS with [signer]; [header] gets "alg" (and "kid" when the signer has one). */
internal fun signCompactJws(signer: JwsSigner, header: Map<String, Any?>, claims: Map<String, Any?>): String {
    val fullHeader = LinkedHashMap<String, Any?>()
    fullHeader["alg"] = signer.algorithm
    fullHeader.putAll(header)
    if (signer.keyId != null && "kid" !in fullHeader) fullHeader["kid"] = signer.keyId
    val signingInput = B64.encode(toJson(fullHeader)) + "." + B64.encode(toJson(claims))
    val signature = signer.sign(signingInput.toByteArray(Charsets.US_ASCII))
    return signingInput + "." + B64.encode(signature)
}

/**
 * Converts ECDSA signatures between the ASN.1 DER form that Java and Android Keystore produce
 * (`SEQUENCE { INTEGER r, INTEGER s }`) and the fixed-length `R || S` form JWS needs
 * (RFC 7518 section 3.4).
 */
public object EcSignatures {
    /** DER to `R || S`, each value left-padded to [partLength] bytes (32 for P-256). */
    public fun derToJose(der: ByteArray, partLength: Int = 32): ByteArray {
        require(der.size >= 8 && der[0] == 0x30.toByte()) { "not a DER ECDSA signature" }
        var offset = 2
        val sequenceLength = der[1].toInt() and 0xff
        if (sequenceLength and 0x80 != 0) offset += sequenceLength and 0x7f
        val (r, afterR) = readInteger(der, offset)
        val (s, afterS) = readInteger(der, afterR)
        require(afterS == der.size) { "trailing bytes after the DER ECDSA signature" }
        return toFixed(r, partLength) + toFixed(s, partLength)
    }

    /** `R || S` to DER, the input of `java.security.Signature.verify` for ECDSA. */
    public fun joseToDer(jose: ByteArray): ByteArray {
        require(jose.isNotEmpty() && jose.size % 2 == 0) { "JWS ECDSA signature must have an even length" }
        val half = jose.size / 2
        val r = derInteger(BigInteger(1, jose.copyOfRange(0, half)))
        val s = derInteger(BigInteger(1, jose.copyOfRange(half, jose.size)))
        val body = r + s
        return byteArrayOf(0x30) + derLength(body.size) + body
    }

    private fun readInteger(der: ByteArray, offset: Int): Pair<BigInteger, Int> {
        require(offset + 2 <= der.size && der[offset] == 0x02.toByte()) { "DER INTEGER expected" }
        val length = der[offset + 1].toInt() and 0xff
        require(length in 1..0x7f && offset + 2 + length <= der.size) { "bad DER INTEGER length" }
        val value = BigInteger(1, der.copyOfRange(offset + 2, offset + 2 + length))
        return value to offset + 2 + length
    }

    private fun toFixed(value: BigInteger, length: Int): ByteArray {
        val raw = value.toByteArray().let { if (it.size > 1 && it[0] == 0.toByte()) it.copyOfRange(1, it.size) else it }
        require(raw.size <= length) { "ECDSA value longer than $length bytes" }
        return ByteArray(length - raw.size) + raw
    }

    private fun derInteger(value: BigInteger): ByteArray {
        val bytes = value.toByteArray() // two's complement: keeps a leading 0x00 when the top bit is set
        return byteArrayOf(0x02) + derLength(bytes.size) + bytes
    }

    private fun derLength(length: Int): ByteArray =
        if (length < 0x80) byteArrayOf(length.toByte()) else byteArrayOf(0x81.toByte(), length.toByte())
}

/** Public JWK members of an EC P-256 key (RFC 7518 section 6.2.1). */
public fun ecP256PublicJwk(key: ECPublicKey): Map<String, String> {
    fun coordinate(value: BigInteger): String {
        val raw = value.toByteArray().let { if (it.size > 32) it.copyOfRange(it.size - 32, it.size) else it }
        return B64.encode(ByteArray(32 - raw.size) + raw)
    }
    return linkedMapOf("kty" to "EC", "crv" to "P-256", "x" to coordinate(key.w.affineX), "y" to coordinate(key.w.affineY))
}

/**
 * ES256 signer over an in-memory key pair. For tests and tools only: on a phone the private key
 * belongs in Android Keystore, behind its own [JwsSigner].
 */
public class SoftwareEcSigner(
    private val keyPair: KeyPair,
    override val keyId: String? = null,
) : JwsSigner {
    override val algorithm: String = "ES256"
    override val publicJwk: Map<String, String> = ecP256PublicJwk(keyPair.public as ECPublicKey)

    override fun sign(signingInput: ByteArray): ByteArray {
        val der = Signature.getInstance("SHA256withECDSA").run {
            initSign(keyPair.private)
            update(signingInput)
            sign()
        }
        return EcSignatures.derToJose(der)
    }

    public companion object {
        /** A signer with a fresh P-256 key pair. */
        public fun generate(keyId: String? = null): SoftwareEcSigner {
            val generator = KeyPairGenerator.getInstance("EC").apply { initialize(ECGenParameterSpec("secp256r1")) }
            return SoftwareEcSigner(generator.generateKeyPair(), keyId)
        }
    }
}
