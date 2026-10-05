// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
package org.badmosip.nativesso.childapp.keys

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import org.badmosip.nativesso.oidc.EcSignatures
import org.badmosip.nativesso.oidc.JwkThumbprint
import org.badmosip.nativesso.oidc.JwsSigner
import org.badmosip.nativesso.oidc.ecP256PublicJwk
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PrivateKey
import java.security.Signature
import java.security.interfaces.ECPublicKey
import java.security.spec.ECGenParameterSpec

/**
 * Health Card's own EC P-256 keys in Android Keystore (the same scheme as Hub's `AndroidKeystoreKeys`;
 * the private key never enters the app process):
 * - the `private_key_jwt` client key whose public half is registered with eSignet, used for every
 *   request Health Card sends, including the token exchange;
 * - the DPoP key of Health Card's own browser login (re-consent). Tokens from the token exchange are
 *   bound to Hub's key instead, because the device secret is (statement task 9 iii.4).
 */
object CardKeys {
    private const val ANDROID_KEYSTORE = "AndroidKeyStore"

    /** Alias of the DPoP key of Health Card's own login. */
    const val DPOP_ALIAS: String = "card-dpop-es256"

    /** Alias of the client-authentication key. */
    const val CLIENT_AUTH_ALIAS: String = "card-client-auth-es256"

    /** Signer for [alias], creating the key on first use (StrongBox when the device has one). */
    @Synchronized
    fun signer(alias: String, keyId: String? = null): CardKeySigner {
        val keyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        (keyStore.getEntry(alias, null) as? KeyStore.PrivateKeyEntry)?.let {
            return CardKeySigner(it.privateKey, it.certificate.publicKey as ECPublicKey, keyId)
        }
        val (private, public) = try {
            generate(alias, strongBox = true)
        } catch (e: StrongBoxUnavailableException) {
            generate(alias, strongBox = false)
        }
        return CardKeySigner(private, public, keyId)
    }

    /** Deletes the key [alias] (no error when it does not exist). */
    @Synchronized
    fun delete(alias: String) {
        KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }.deleteEntry(alias)
    }

    private fun generate(alias: String, strongBox: Boolean): Pair<PrivateKey, ECPublicKey> {
        val spec = KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_SIGN)
            .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
            .setDigests(KeyProperties.DIGEST_SHA256)
            .setIsStrongBoxBacked(strongBox)
            .build()
        val pair = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, ANDROID_KEYSTORE)
            .apply { initialize(spec) }
            .generateKeyPair()
        return pair.private to pair.public as ECPublicKey
    }
}

/**
 * ES256 [JwsSigner] over a `java.security` private key: Keystore returns ASN.1 DER signatures and
 * JWS needs `R || S` (RFC 7518 section 3.4), so each one goes through [EcSignatures.derToJose].
 */
class CardKeySigner(
    private val privateKey: PrivateKey,
    private val publicKey: ECPublicKey,
    override val keyId: String? = null,
) : JwsSigner {
    init {
        require(publicKey.params.curve.field.fieldSize == 256) { "ES256 needs a P-256 key" }
    }

    override val algorithm: String = "ES256"
    override val publicJwk: Map<String, String> = ecP256PublicJwk(publicKey)

    /** RFC 7638 thumbprint of [publicJwk]: the client key's `kid` and the DPoP `jkt`. */
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
    fun withKeyId(keyId: String?): CardKeySigner = CardKeySigner(privateKey, publicKey, keyId)
}
