import XCTest
import CryptoKit
import Security
@testable import PokitDeviceKey

// M3-auth-1B XCTest for the real iOS Secure Enclave provider — the iOS mirror of
// PokitDeviceKeyInstrumentationTest.kt. It exercises PokitDeviceKeyStore directly
// (no Expo wrapper), like the Android instrumentation test.
//
// M-track: the Secure Enclave is unavailable on the Simulator, so the functional
// (happy-path) tests skip there and require a real device. The fail-closed path
// (no enclave -> hardware_unavailable) and the pure structural checks run
// everywhere. This test is NOT part of scripts/build-gate.sh; it runs via
// scripts/ios-native-gate.sh on a connected device.
final class PokitDeviceKeyStoreTests: XCTestCase {
  private let testTag = "pokit.device.identity.test"
  private var store: PokitDeviceKeyStore!

  override func setUp() {
    super.setUp()
    store = PokitDeviceKeyStore(tag: testTag)
    try? store.deleteKey()
  }

  override func tearDown() {
    try? store.deleteKey()
    super.tearDown()
  }

  private func requireEnclave() throws {
    try XCTSkipUnless(SecureEnclave.isAvailable, "Secure Enclave unavailable (Simulator) — real device required")
  }

  // ── support / lifecycle ──

  func testSupportDetected() {
    let s = store.getSupport()
    if SecureEnclave.isAvailable {
      XCTAssertEqual(s, "supported")
    } else {
      XCTAssertEqual(s, "hardware_unavailable")
    }
  }

  func testHasKeyBeforeAndAfterCreate() throws {
    try requireEnclave()
    XCTAssertFalse(store.hasKey())
    _ = try store.ensureKey()
    XCTAssertTrue(store.hasKey())
  }

  func testEnsureKeyIsIdempotent() throws {
    try requireEnclave()
    let a = try store.ensureKey()
    let b = try store.ensureKey()
    XCTAssertEqual(a["publicKeySpkiHex"] as? String, b["publicKeySpkiHex"] as? String)
    XCTAssertEqual(a["deviceId"] as? String, b["deviceId"] as? String)
    XCTAssertEqual(a["provider"] as? String, b["provider"] as? String)
    XCTAssertEqual(a["securityLevel"] as? String, b["securityLevel"] as? String)
  }

  func testConcurrentEnsureKeyResolvesToOneIdentity() throws {
    try requireEnclave()
    let group = DispatchGroup()
    let queue = DispatchQueue(label: "ensure", attributes: .concurrent)
    let lock = NSLock()
    var spkis = Set<String>()
    var ids = Set<String>()
    for _ in 0..<16 {
      group.enter()
      queue.async {
        if let info = try? self.store.ensureKey() {
          lock.lock()
          spkis.insert(info["publicKeySpkiHex"] as! String)
          ids.insert(info["deviceId"] as! String)
          lock.unlock()
        }
        group.leave()
      }
    }
    group.wait()
    XCTAssertEqual(spkis.count, 1, "one SPKI for all callers")
    XCTAssertEqual(ids.count, 1, "one deviceId for all callers")
  }

  func testNewStoreInstancePersistsIdentity() throws {
    try requireEnclave()
    let created = try store.ensureKey()
    let spki = created["publicKeySpkiHex"] as! String
    let id = created["deviceId"] as! String

    let restored = PokitDeviceKeyStore(tag: testTag)
    let info = try restored.getKeyInfo()
    XCTAssertEqual(spki, info["publicKeySpkiHex"] as? String)
    XCTAssertEqual(id, info["deviceId"] as? String)
    let sig = try restored.sign(hexOf(Data("persist-check".utf8)))
    XCTAssertTrue(verify(spkiHex: spki, message: Data("persist-check".utf8), sigHex: sig))
  }

  // ── SPKI / deviceId structure ──

  func testSpkiIsCanonicalP256AndDeviceIdMatches() throws {
    try requireEnclave()
    let info = try store.ensureKey()
    let spkiHex = info["publicKeySpkiHex"] as! String
    XCTAssertEqual(spkiHex.count, 91 * 2)
    let spki = hex(spkiHex)
    let expected = hexOf(Data(SHA256.hash(data: spki)))
    XCTAssertEqual(expected, info["deviceId"] as? String)
  }

  // ── sign / verify roundtrip ──

  func testSignatureVerifiesWrongKeyAndTamperFail() throws {
    try requireEnclave()
    let info = try store.ensureKey()
    let spki = info["publicKeySpkiHex"] as! String
    let message = Data("pokit ios enclave challenge".utf8)
    let sig = try store.sign(hexOf(message))

    XCTAssertTrue(verify(spkiHex: spki, message: message, sigHex: sig))
    // Wrong (unrelated) key fails.
    let wrong = P256.Signing.PrivateKey().publicKey
    let der = try P256.Signing.ECDSASignature(derRepresentation: hex(sig))
    XCTAssertFalse(wrong.isValidSignature(der, for: message))
    // Modified message fails.
    XCTAssertFalse(verify(spkiHex: spki, message: Data("tampered".utf8), sigHex: sig))
    // Modified signature fails.
    var mutated = [UInt8](hex(sig)); mutated[mutated.count - 1] ^= 0x01
    let tampered = (try? P256.Signing.ECDSASignature(derRepresentation: Data(mutated)))
    if let tampered = tampered {
      let pub = try P256.Signing.PublicKey(derRepresentation: hex(spki))
      XCTAssertFalse(pub.isValidSignature(tampered, for: message))
    }
  }

  // ── non-exportability / fail-closed ──

  func testPrivateKeyIsNonExportable() throws {
    try requireEnclave()
    let info = try store.ensureKey()
    XCTAssertEqual(info["nonExportable"] as? Bool, true)
    XCTAssertNotNil(try store.getKeyInfo()["publicKeySpkiHex"])
  }

  func testGetKeyInfoBeforeCreateFailsClosed() throws {
    try requireEnclave()
    XCTAssertThrowsError(try store.getKeyInfo()) { err in
      XCTAssertEqual((err as? DeviceKeyError)?.code, "key_missing")
    }
  }

  func testDeleteRemovesIdentityAndSignFails() throws {
    try requireEnclave()
    _ = try store.ensureKey()
    XCTAssertTrue(store.hasKey())
    try store.deleteKey()
    XCTAssertFalse(store.hasKey())
    XCTAssertThrowsError(try store.sign(hexOf(Data("x".utf8)))) { err in
      XCTAssertEqual((err as? DeviceKeyError)?.code, "key_missing")
    }
  }

  func testNoEnclaveFailsClosed() throws {
    // On the Simulator (no enclave), ensureKey must fail closed — never a
    // software key. On a real device this test is not meaningful, so skip.
    try XCTSkipIf(SecureEnclave.isAvailable, "device has an enclave — no-enclave path not exercised")
    XCTAssertThrowsError(try store.ensureKey()) { err in
      XCTAssertEqual((err as? DeviceKeyError)?.code, "hardware_unavailable")
    }
  }

  // ── iOS → Go interoperability fixture export ──

  func testExportGoInteropFixture() throws {
    try requireEnclave()
    let info = try store.ensureKey()
    let spkiHex = info["publicKeySpkiHex"] as! String
    let deviceId = info["deviceId"] as! String
    let message = Data("pokit-ios-go-interop-v1".utf8)
    let sigHex = try store.sign(hexOf(message))

    XCTAssertTrue(verify(spkiHex: spkiHex, message: message, sigHex: sigHex))

    let json: [String: Any] = [
      "version": 1,
      "messageHex": hexOf(message),
      "publicKeySpkiHex": spkiHex,
      "signatureHex": sigHex,
      "deviceId": deviceId,
    ]
    let data = try JSONSerialization.data(withJSONObject: json)
    let dir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
    let url = dir.appendingPathComponent("ios_signature_fixture.json")
    try data.write(to: url)
    // Pull with `xcrun simctl` / device container and place in
    // companion-daemon/internal/devicetrust/testdata/ for the Go fixture test.
    print("ios_signature_fixture.json written to \(url.path)")
  }

  // ── helpers ──

  private func verify(spkiHex: String, message: Data, sigHex: String) -> Bool {
    guard let pub = try? P256.Signing.PublicKey(derRepresentation: hex(spkiHex)),
          let sig = try? P256.Signing.ECDSASignature(derRepresentation: hex(sigHex)) else {
      return false
    }
    return pub.isValidSignature(sig, for: message)
  }

  private func hex(_ s: String) -> Data {
    var out = [UInt8](); out.reserveCapacity(s.count / 2)
    let chars = Array(s.utf8)
    var i = 0
    while i < chars.count {
      let hi = nibble(chars[i]); let lo = nibble(chars[i + 1])
      out.append((hi << 4) | lo); i += 2
    }
    return Data(out)
  }
  private func nibble(_ c: UInt8) -> UInt8 {
    switch c {
    case 0x30...0x39: return c - 0x30
    case 0x61...0x66: return c - 0x61 + 10
    case 0x41...0x46: return c - 0x41 + 10
    default: return 0
    }
  }
  private func hexOf(_ d: Data) -> String {
    let digits = Array("0123456789abcdef".utf8)
    var out = [UInt8](); out.reserveCapacity(d.count * 2)
    for b in d { out.append(digits[Int(b >> 4)]); out.append(digits[Int(b & 0x0f)]) }
    return String(decoding: out, as: UTF8.self)
  }
}
