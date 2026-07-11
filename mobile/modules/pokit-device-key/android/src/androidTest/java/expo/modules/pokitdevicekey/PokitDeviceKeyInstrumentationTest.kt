package expo.modules.pokitdevicekey

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.MessageDigest
import java.security.Signature
import java.security.spec.ECGenParameterSpec
import java.security.spec.X509EncodedKeySpec
import java.security.KeyFactory
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

// M3-auth-1A instrumentation tests for the real Android Keystore provider.
//
// M-track for a *physical* device (TEE/StrongBox proof), but E-track on an
// emulator via the internal test override, which exercises the full functional
// contract against the real AndroidKeyStore.
//   ./gradlew :pokit-device-key:connectedAndroidTest
@RunWith(AndroidJUnit4::class)
class PokitDeviceKeyInstrumentationTest {
  private val alias = "pokit.device.identity.test"
  private lateinit var store: PokitDeviceKeyStore

  @Before
  fun setUp() {
    PokitDeviceKeyStore.allowSoftwareKeystoreForTest = true
    store = PokitDeviceKeyStore(alias)
    deleteAlias(alias)
    deleteAlias("pokit.test.p384")
    deleteAlias("pokit.test.sha512")
  }

  @After
  fun tearDown() {
    deleteAlias(alias)
    deleteAlias("pokit.test.p384")
    deleteAlias("pokit.test.sha512")
    PokitDeviceKeyStore.allowSoftwareKeystoreForTest = false
  }

  @Test
  fun supportDetected() {
    assertEquals("supported", store.getSupport())
  }

  @Test
  fun hasKeyBeforeAndAfterCreate() {
    assertFalse(store.hasKey())
    store.ensureKey()
    assertTrue(store.hasKey())
  }

  @Test
  fun ensureKeyIsIdempotent() {
    val a = store.ensureKey()
    val b = store.ensureKey()
    assertEquals(a["publicKeySpkiHex"], b["publicKeySpkiHex"])
    assertEquals(a["deviceId"], b["deviceId"])
    assertEquals(a["keyVersion"], b["keyVersion"])
    assertEquals(a["provider"], b["provider"])
    assertEquals(a["securityLevel"], b["securityLevel"])
  }

  @Test
  fun concurrentEnsureKeyResolvesToOneIdentity() {
    val pool = Executors.newFixedThreadPool(8)
    val spkis = java.util.Collections.synchronizedList(mutableListOf<String>())
    val ids = java.util.Collections.synchronizedList(mutableListOf<String>())
    val tasks = (0 until 16).map {
      pool.submit {
        val info = store.ensureKey()
        spkis.add(info["publicKeySpkiHex"] as String)
        ids.add(info["deviceId"] as String)
      }
    }
    tasks.forEach { it.get() }
    pool.shutdown()
    pool.awaitTermination(15, TimeUnit.SECONDS)
    assertEquals(16, spkis.size)
    assertEquals("one SPKI for all callers", 1, spkis.toSet().size)
    assertEquals("one deviceId for all callers", 1, ids.toSet().size)
  }

  @Test
  fun newStoreInstancePersistsIdentity() {
    val created = store.ensureKey()
    val spki = created["publicKeySpkiHex"] as String
    val id = created["deviceId"] as String

    // A brand-new store object (module reconstruction) reading the same alias.
    val restored = PokitDeviceKeyStore(alias)
    val info = restored.getKeyInfo()
    assertEquals(spki, info["publicKeySpkiHex"])
    assertEquals(id, info["deviceId"])
    // And it can sign with the restored identity.
    val sig = restored.sign(hexOf("persist-check".toByteArray()))
    assertTrue(verify(spki, "persist-check".toByteArray(), hex(sig)))
  }

  @Test
  fun spkiIsCanonicalP256AndDeviceIdMatches() {
    val info = store.ensureKey()
    val spkiHex = info["publicKeySpkiHex"] as String
    assertEquals(91 * 2, spkiHex.length)
    val spki = hex(spkiHex)
    val expected = hexOf(MessageDigest.getInstance("SHA-256").digest(spki))
    assertEquals(expected, info["deviceId"])
  }

  @Test
  fun signatureVerifiesWrongKeyAndTamperFail() {
    val info = store.ensureKey()
    val spki = info["publicKeySpkiHex"] as String
    val message = "pokit native instrumentation challenge".toByteArray()
    val sig = hex(store.sign(hexOf(message)))

    // Correct production public key verifies.
    assertTrue(verify(spki, message, sig))
    // Wrong (unrelated) key fails.
    val wrong = KeyPairGenerator.getInstance("EC").apply {
      initialize(ECGenParameterSpec("secp256r1"))
    }.generateKeyPair()
    val v = Signature.getInstance("SHA256withECDSA")
    v.initVerify(wrong.public)
    v.update(message)
    assertFalse(v.verify(sig))
    // Modified message fails.
    assertFalse(verify(spki, "tampered".toByteArray(), sig))
    // Modified signature fails.
    val mutated = sig.copyOf(); mutated[mutated.size - 1] = (mutated[mutated.size - 1].toInt() xor 0x01).toByte()
    assertFalse(verify(spki, message, mutated))
  }

  @Test
  fun deleteRemovesIdentityAndSignFails() {
    store.ensureKey()
    assertTrue(store.hasKey())
    store.deleteKey()
    assertFalse(store.hasKey())
    try {
      store.sign(hexOf("x".toByteArray()))
      fail("sign after delete must fail")
    } catch (e: DeviceKeyException) {
      assertEquals("key_missing", e.code)
    }
  }

  @Test
  fun privateKeyIsNonExportable() {
    val info = store.ensureKey()
    assertEquals(true, info["nonExportable"])
    assertNotNull(store.getKeyInfo()["publicKeySpkiHex"])
  }

  @Test
  fun getKeyInfoBeforeCreateFailsClosed() {
    try {
      store.getKeyInfo()
      fail("getKeyInfo without a key must fail closed")
    } catch (e: DeviceKeyException) {
      assertEquals("key_missing", e.code)
    }
  }

  @Test
  fun hardwarePolicyFailsClosedWithoutOverride() {
    PokitDeviceKeyStore.allowSoftwareKeystoreForTest = false
    deleteAlias(alias)
    try {
      store.ensureKey()
      // Succeeds on a real hardware-backed device; on a software-only emulator
      // it must throw hardware_unavailable.
    } catch (e: DeviceKeyException) {
      assertEquals("hardware_unavailable", e.code)
    } finally {
      PokitDeviceKeyStore.allowSoftwareKeystoreForTest = true
    }
  }

  // ── existing-key contract rejection (build incompatible aliases directly) ──

  @Test
  fun rejectsNonP256ExistingKey() {
    // Create a P-384 key under a test alias, then point the store at it.
    KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, "AndroidKeyStore").apply {
      initialize(
        KeyGenParameterSpec.Builder("pokit.test.p384", KeyProperties.PURPOSE_SIGN)
          .setAlgorithmParameterSpec(ECGenParameterSpec("secp384r1"))
          .setDigests(KeyProperties.DIGEST_SHA256)
          .build()
      )
    }.generateKeyPair()
    val s = PokitDeviceKeyStore("pokit.test.p384")
    try {
      s.getKeyInfo()
      fail("P-384 key must be rejected")
    } catch (e: DeviceKeyException) {
      assertEquals("key_invalidated", e.code)
    }
  }

  @Test
  fun rejectsKeyWithoutSha256Authorization() {
    // A P-256 key authorized only for SHA-512 must be rejected.
    KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, "AndroidKeyStore").apply {
      initialize(
        KeyGenParameterSpec.Builder("pokit.test.sha512", KeyProperties.PURPOSE_SIGN)
          .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
          .setDigests(KeyProperties.DIGEST_SHA512)
          .build()
      )
    }.generateKeyPair()
    val s = PokitDeviceKeyStore("pokit.test.sha512")
    try {
      s.getKeyInfo()
      fail("key without SHA-256 authorization must be rejected")
    } catch (e: DeviceKeyException) {
      assertEquals("key_invalidated", e.code)
    }
  }

  // ── Android→Go interoperability fixture export ──

  @Test
  fun exportGoInteropFixture() {
    val info = store.ensureKey()
    val spkiHex = info["publicKeySpkiHex"] as String
    val deviceId = info["deviceId"] as String
    val message = "pokit-android-go-interop-v1".toByteArray()
    val sigHex = store.sign(hexOf(message))

    // Sanity: verifies locally with SHA256withECDSA (same as Go sha256+VerifyASN1).
    assertTrue(verify(spkiHex, message, hex(sigHex)))

    val json = JSONObject()
      .put("version", 1)
      .put("messageHex", hexOf(message))
      .put("publicKeySpkiHex", spkiHex)
      .put("signatureHex", sigHex)
      .put("deviceId", deviceId)
      .toString()
    // Written to the app files dir; pull with `adb pull` and place in
    // companion-daemon/internal/devicetrust/testdata/ for the Go fixture test.
    val ctx = InstrumentationRegistry.getInstrumentation().targetContext
    File(ctx.filesDir, "android_signature_fixture.json").writeText(json)
  }

  // ── helpers ──

  private fun verify(spkiHex: String, message: ByteArray, sig: ByteArray): Boolean {
    val pub = KeyFactory.getInstance("EC").generatePublic(X509EncodedKeySpec(hex(spkiHex)))
    val v = Signature.getInstance("SHA256withECDSA")
    v.initVerify(pub)
    v.update(message)
    return v.verify(sig)
  }

  private fun deleteAlias(a: String) {
    try {
      KeyStore.getInstance("AndroidKeyStore").apply { load(null) }.deleteEntry(a)
    } catch (_: Exception) {}
  }

  private fun hex(s: String): ByteArray {
    val out = ByteArray(s.length / 2)
    var i = 0
    while (i < s.length) { out[i / 2] = ((Character.digit(s[i], 16) shl 4) or Character.digit(s[i + 1], 16)).toByte(); i += 2 }
    return out
  }
  private fun hexOf(b: ByteArray): String {
    val sb = StringBuilder(b.size * 2)
    for (x in b) sb.append(String.format("%02x", x))
    return sb.toString()
  }
}
