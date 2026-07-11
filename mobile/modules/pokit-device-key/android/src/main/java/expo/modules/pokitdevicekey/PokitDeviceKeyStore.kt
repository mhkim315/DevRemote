package expo.modules.pokitdevicekey

import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyInfo
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import java.security.KeyFactory
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.MessageDigest
import java.security.Signature
import java.security.interfaces.ECPrivateKey
import java.security.spec.ECGenParameterSpec

// Typed device-key failure. `code` mirrors the DeviceKeySupport / error vocabulary
// so callers (and the JS wrapper) can distinguish missing vs invalidated vs
// hardware-unavailable. Messages never contain key material or aliases.
class DeviceKeyException(val code: String, message: String) : Exception(message)

// PokitDeviceKeyStore holds the Android Keystore logic, free of any Expo/React
// dependency so it can be exercised directly by instrumentation tests. The
// private scalar is created inside AndroidKeyStore and never leaves it.
//
// Signing uses SHA256withECDSA (the Keystore performs the SHA-256 digest — no
// JS hashing), producing ASN.1 DER signatures the Go daemon verifies. The
// public key is returned as canonical 91-byte SPKI DER; deviceId is lowercase
// hex sha256(SPKI).
class PokitDeviceKeyStore(private val alias: String = DEFAULT_ALIAS) {

  companion object {
    // Fixed native-owned production alias. Never selected from JS/QR/network.
    const val DEFAULT_ALIAS = "pokit.device.identity.v1"
    private const val KEYSTORE = "AndroidKeyStore"
    private val LOCK = Any()

    // Test-only override. Production leaves this false so a software-only
    // Keystore (e.g. an emulator) fails closed. Instrumentation sets it true to
    // exercise the success path. Internal — unreachable from JS or release code.
    @JvmStatic
    internal var allowSoftwareKeystoreForTest: Boolean = false
  }

  private fun keyStore(): KeyStore = KeyStore.getInstance(KEYSTORE).also { it.load(null) }

  fun getSupport(): String = try {
    keyStore()
    "supported"
  } catch (e: Exception) {
    "not_implemented"
  }

  fun hasKey(): Boolean = keyStore().containsAlias(alias)

  // ensureKey is atomic: check-and-create under a process lock; an existing
  // alias is validated and returned (never replaced on a race).
  fun ensureKey(): Map<String, Any?> = synchronized(LOCK) {
    val ks = keyStore()
    if (ks.containsAlias(alias)) {
      return@synchronized readKeyInfoOrThrow()
    }
    generateKey()
    readKeyInfoOrThrow()
  }

  fun getKeyInfo(): Map<String, Any?> = readKeyInfoOrThrow()

  fun getPublicKeySpki(): String = readKeyInfoOrThrow()["publicKeySpkiHex"] as String

  fun sign(messageHex: String): String {
    val message = hexToBytes(messageHex)
    val entry = keyStore().getEntry(alias, null)
      ?: throw DeviceKeyException("key_missing", "device key is missing")
    if (entry !is KeyStore.PrivateKeyEntry) {
      throw DeviceKeyException("key_invalidated", "device key entry is not a private-key entry")
    }
    val sig = Signature.getInstance("SHA256withECDSA")
    sig.initSign(entry.privateKey)
    sig.update(message)
    return bytesToHex(sig.sign()) // ASN.1 DER
  }

  fun deleteKey() {
    keyStore().deleteEntry(alias)
  }

  // ── internal ──

  private fun generateKey() {
    val build: (Boolean) -> KeyGenParameterSpec = { strongBox ->
      val b = KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_SIGN)
        .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
        .setDigests(KeyProperties.DIGEST_SHA256)
      if (strongBox && Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
        b.setIsStrongBoxBacked(true)
      }
      b.build()
    }
    val kpg = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, KEYSTORE)
    try {
      kpg.initialize(build(true))
      kpg.generateKeyPair()
    } catch (e: StrongBoxUnavailableException) {
      kpg.initialize(build(false))
      kpg.generateKeyPair()
    }
  }

  private fun readKeyInfoOrThrow(): Map<String, Any?> {
    val ks = keyStore()
    if (!ks.containsAlias(alias)) {
      throw DeviceKeyException("key_missing", "device key is missing")
    }
    val entry = ks.getEntry(alias, null)
    if (entry !is KeyStore.PrivateKeyEntry) {
      throw DeviceKeyException("key_invalidated", "device key entry is not a private-key entry")
    }
    val priv = entry.privateKey
    if (priv !is ECPrivateKey) {
      throw DeviceKeyException("key_invalidated", "device key is not an EC key")
    }
    if (priv.encoded != null) {
      throw DeviceKeyException("key_exportable", "device key is unexpectedly exportable")
    }

    val keyInfo = keyInfoOf(priv)
      ?: throw DeviceKeyException("key_invalidated", "cannot read key info")
    val level = securityLevelString(keyInfo)
    val hardwareBacked = level == "strongbox" || level == "tee"
    if (!hardwareBacked && !allowSoftwareKeystoreForTest) {
      try { ks.deleteEntry(alias) } catch (_: Exception) {}
      throw DeviceKeyException("hardware_unavailable", "hardware-backed key unavailable on this device")
    }

    val cert = entry.certificate
      ?: throw DeviceKeyException("key_invalidated", "no public certificate")
    val spki = cert.publicKey.encoded
    if (spki == null || spki.size != 91) {
      throw DeviceKeyException("spki_invalid", "unexpected public key encoding")
    }
    val spkiHex = bytesToHex(spki)
    val deviceId = bytesToHex(MessageDigest.getInstance("SHA-256").digest(spki))

    return mapOf(
      "provider" to "android_keystore",
      "keyVersion" to 1,
      "deviceId" to deviceId,
      "publicKeySpkiHex" to spkiHex,
      "hardwareBacked" to hardwareBacked,
      "nonExportable" to true,
      "securityLevel" to level
    )
  }

  private fun keyInfoOf(priv: ECPrivateKey): KeyInfo? = try {
    KeyFactory.getInstance(priv.algorithm, KEYSTORE).getKeySpec(priv, KeyInfo::class.java)
  } catch (e: Exception) {
    null
  }

  @Suppress("DEPRECATION")
  private fun securityLevelString(keyInfo: KeyInfo): String {
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
      return when (keyInfo.securityLevel) {
        KeyProperties.SECURITY_LEVEL_STRONGBOX -> "strongbox"
        KeyProperties.SECURITY_LEVEL_TRUSTED_ENVIRONMENT -> "tee"
        KeyProperties.SECURITY_LEVEL_SOFTWARE -> "os_keystore"
        else -> "unknown"
      }
    }
    return if (keyInfo.isInsideSecureHardware) "tee" else "os_keystore"
  }

  private fun hexToBytes(s: String): ByteArray {
    if (s.length % 2 != 0) throw DeviceKeyException("bad_hex", "hex length must be even")
    val out = ByteArray(s.length / 2)
    var i = 0
    while (i < s.length) {
      val hi = Character.digit(s[i], 16)
      val lo = Character.digit(s[i + 1], 16)
      if (hi < 0 || lo < 0) throw DeviceKeyException("bad_hex", "invalid hex")
      out[i / 2] = ((hi shl 4) or lo).toByte()
      i += 2
    }
    return out
  }

  private fun bytesToHex(b: ByteArray): String {
    val sb = StringBuilder(b.size * 2)
    for (byte in b) sb.append(String.format("%02x", byte))
    return sb.toString()
  }
}
