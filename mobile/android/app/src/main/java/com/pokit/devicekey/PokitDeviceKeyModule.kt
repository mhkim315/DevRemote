package com.pokit.devicekey

import com.facebook.react.bridge.*
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.Signature
import java.security.spec.ECGenParameterSpec
import java.util.*

/**
 * Android Keystore non-exportable ECDSA P-256 device identity.
 *
 * - The private scalar is created inside the AndroidKeyStore and never leaves it.
 * - SIGN uses SHA256withECDSA (DIGEST_SHA256 / PURPOSE_SIGN), matching Go's
 *   single-hash-over-message semantics — no JS-side hashing.
 * - The public key is returned as the hex-encoded SPKI DER (91 bytes), exactly
 *   what the daemon's ParseP256PublicKey expects.
 * - Key validity is 100 years from generation to avoid clock-skew expiry.
 */
class PokitDeviceKeyModule(reactContext: ReactApplicationContext) :
    ReactContextBaseJavaModule(reactContext) {

    companion object {
        const val NAME = "PokitDeviceKey"
        private const val KEY_ALIAS = "pokit_device_identity"
        private const val ANDROID_KEYSTORE = "AndroidKeyStore"
    }

    override fun getName(): String = NAME

    private fun keyStore(): KeyStore =
        KeyStore.getInstance(ANDROID_KEYSTORE).also { it.load(null) }

    // ── hasKey ──

    @ReactMethod
    fun hasKey(promise: Promise) {
        try {
            promise.resolve(keyStore().containsAlias(KEY_ALIAS))
        } catch (e: Exception) {
            promise.reject("KEYSTORE_ERR", e.message, e)
        }
    }

    // ── generateKey ──

    @ReactMethod
    fun generateKey(promise: Promise) {
        try {
            val kpg = KeyPairGenerator.getInstance("EC", ANDROID_KEYSTORE)
            kpg.initialize(
                KeyGenParameterSpec.Builder(KEY_ALIAS, KeyProperties.PURPOSE_SIGN)
                    .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
                    .setDigests(KeyProperties.DIGEST_SHA256)
                    .setCertificateNotBefore(Date(0L)) // 1970
                    .setCertificateNotAfter(Date(4102444800000L)) // 2100
                    .build()
            )
            kpg.generateKeyPair()
            promise.resolve(getPublicKeyInternal())
        } catch (e: Exception) {
            promise.reject("KEYGEN_ERR", e.message, e)
        }
    }

    // ── getPublicKey ──

    @ReactMethod
    fun getPublicKey(promise: Promise) {
        try {
            val hex = getPublicKeyInternal()
            if (hex == null) promise.resolve(null)
            else promise.resolve(hex)
        } catch (e: Exception) {
            promise.reject("KEYSTORE_ERR", e.message, e)
        }
    }

    // ── sign ──

    @ReactMethod
    fun sign(messageHex: String, promise: Promise) {
        try {
            val msg = hexToBytes(messageHex)
            val sig = Signature.getInstance("SHA256withECDSA")
            sig.initSign(keyStore().getKey(KEY_ALIAS, null))
            sig.update(msg)
            promise.resolve(bytesToHex(sig.sign()))
        } catch (e: Exception) {
            promise.reject("SIGN_ERR", e.message, e)
        }
    }

    // ── deleteKey ──

    @ReactMethod
    fun deleteKey(promise: Promise) {
        try {
            keyStore().deleteEntry(KEY_ALIAS)
            promise.resolve(null)
        } catch (e: Exception) {
            promise.reject("KEYSTORE_ERR", e.message, e)
        }
    }

    // ── internal helpers ──

    private fun getPublicKeyInternal(): String? {
        val entry = keyStore().getEntry(KEY_ALIAS, null) ?: return null
        return bytesToHex((entry as KeyStore.PrivateKeyEntry).certificate.publicKey.encoded)
    }

    private fun hexToBytes(s: String): ByteArray {
        val len = s.length
        if (len % 2 != 0) throw IllegalArgumentException("hex string must have even length")
        val out = ByteArray(len / 2)
        var i = 0
        while (i < len) {
            val hi = Character.digit(s[i], 16)
            val lo = Character.digit(s[i + 1], 16)
            if (hi < 0 || lo < 0) throw IllegalArgumentException("invalid hex char at $i")
            out[i / 2] = ((hi shl 4) or lo).toByte()
            i += 2
        }
        return out
    }

    private fun bytesToHex(b: ByteArray): String {
        val sb = StringBuilder(b.size * 2)
        for (byte in b) {
            sb.append(String.format("%02x", byte))
        }
        return sb.toString()
    }
}
