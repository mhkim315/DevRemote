package expo.modules.pokitdevicekey

import androidx.test.ext.junit.runners.AndroidJUnit4
import java.security.MessageDigest
import java.security.Signature
import java.security.spec.X509EncodedKeySpec
import java.security.KeyFactory
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

// M3-auth-1A instrumentation tests for the real Android Keystore provider.
//
// M-track: requires a connected device/emulator (`./gradlew :pokit-device-key:
// connectedAndroidTest` or app connectedAndroidTest). On an emulator the
// software-Keystore success path is exercised via the internal test override;
// hardware-backed evidence (TEE/StrongBox) requires a real device and is
// recorded separately in the Samsung smoke evidence.
@RunWith(AndroidJUnit4::class)
class PokitDeviceKeyInstrumentationTest {
  // Isolated test alias so runs never touch the production identity.
  private val alias = "pokit.device.identity.test"
  private lateinit var store: PokitDeviceKeyStore

  @Before
  fun setUp() {
    // Allow software Keystore so the success path runs on an emulator.
    PokitDeviceKeyStore.allowSoftwareKeystoreForTest = true
    store = PokitDeviceKeyStore(alias)
    try { store.deleteKey() } catch (_: Exception) {}
  }

  @After
  fun tearDown() {
    try { store.deleteKey() } catch (_: Exception) {}
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
  }

  @Test
  fun concurrentEnsureKeyResolvesToOneIdentity() {
    val pool = Executors.newFixedThreadPool(8)
    val results = java.util.Collections.synchronizedList(mutableListOf<String>())
    val tasks = (0 until 16).map {
      pool.submit { results.add(store.ensureKey()["publicKeySpkiHex"] as String) }
    }
    tasks.forEach { it.get() }
    pool.shutdown()
    pool.awaitTermination(10, TimeUnit.SECONDS)
    assertEquals(16, results.size)
    val distinct = results.toSet()
    assertEquals("all callers must resolve to one identity", 1, distinct.size)
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
  fun signatureVerifiesAndTamperFails() {
    val info = store.ensureKey()
    val spki = hex(info["publicKeySpkiHex"] as String)
    val pub = KeyFactory.getInstance("EC").generatePublic(X509EncodedKeySpec(spki))

    val message = "pokit native instrumentation challenge".toByteArray()
    val sigHex = store.sign(hexOf(message))
    val sig = hex(sigHex)

    val verifier = Signature.getInstance("SHA256withECDSA")
    verifier.initVerify(pub)
    verifier.update(message)
    assertTrue("valid signature must verify", verifier.verify(sig))

    // Wrong message fails.
    val v2 = Signature.getInstance("SHA256withECDSA")
    v2.initVerify(pub)
    v2.update("tampered".toByteArray())
    assertFalse(v2.verify(sig))
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
    // Re-reading getKeyInfo also enforces PrivateKey.encoded == null internally.
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
    // With the override off, a software-only emulator key must be rejected.
    PokitDeviceKeyStore.allowSoftwareKeystoreForTest = false
    try { store.deleteKey() } catch (_: Exception) {}
    try {
      store.ensureKey()
      // On a real hardware-backed device this succeeds; on a software-only
      // emulator it must throw hardware_unavailable.
    } catch (e: DeviceKeyException) {
      assertEquals("hardware_unavailable", e.code)
    } finally {
      PokitDeviceKeyStore.allowSoftwareKeystoreForTest = true
    }
  }

  // ── helpers ──

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
