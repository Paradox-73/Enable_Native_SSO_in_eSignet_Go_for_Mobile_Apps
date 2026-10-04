// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.superapp.keys

import org.badmosip.nativesso.oidc.EcSignatures
import org.badmosip.nativesso.oidc.JwkThumbprint
import org.badmosip.nativesso.oidc.JwsSigner
import org.badmosip.nativesso.oidc.ecP256PublicJwk
import java.security.PrivateKey
import java.security.Signature
import java.security.interfaces.ECPublicKey

/**
 * ES256 [JwsSigner] over a `java.security` private key. On the phone the key is an Android Keystore
 * handle ([AndroidKeystoreKeys]): `Signature` passes the bytes to Keystore and only the signature
 * comes back, in ASN.1 DER. JWS needs the fixed 64-byte `R || S` form (RFC 7518 section 3.4), so
 * every signature goes through [EcSignatures.derToJose].
 *
 * @param keyId the JWS `kid`; null leaves it out (DPoP proofs carry the public `jwk` instead)
 */
class EcKeyJwsSigner(
    private val privateKey: PrivateKey,
    private val publicKey: ECPublicKey,
    override val keyId: String? = null,
) : JwsSigner {
    init {
        require(publicKey.params.curve.field.fieldSize == 256) { "ES256 needs a P-256 key" }
    }

    override val algorithm: String = "ES256"
    override val publicJwk: Map<String, String> = ecP256PublicJwk(publicKey)

    /** RFC 7638 thumbprint of [publicJwk]: the DPoP `jkt`, and the client key's `kid` in this project. */
    val thumbprint: String = JwkThumbprint.compute(publicJwk)

    override fun sign(signingInput: ByteArray): ByteArray {
        val der = Signature.getInstance("SHA256withECDSA").run {
            initSign(privateKey)
            update(signingInput)
            sign()
        }
        return EcSignatures.derToJose(der)
    }

    /** The same key with another `kid`. */
    fun withKeyId(keyId: String?): EcKeyJwsSigner = EcKeyJwsSigner(privateKey, publicKey, keyId)
}
