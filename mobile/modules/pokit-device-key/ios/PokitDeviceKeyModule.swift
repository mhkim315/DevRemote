import ExpoModulesCore

// M3-auth-1B iOS Secure Enclave provider — thin Expo wrapper over
// PokitDeviceKeyStore (Security framework), mirroring the Android
// PokitDeviceKeyModule.kt. All real logic lives in the Expo-free store so
// XCTest can exercise it directly.
//
// Error mapping (mirror of Android `coded { }`): a typed DeviceKeyError is
// rejected with its stable `code` preserved, so the JS wrapper's
// normalizeNativeError sees `error.code`. Any other error fails closed as
// native_operation_failed.

public class PokitDeviceKeyModule: Module {
  private let store = PokitDeviceKeyStore()

  public func definition() -> ModuleDefinition {
    ModuleDefinition {
      Name("PokitDeviceKey")

      AsyncFunction("getSupport") { () -> String in
        self.store.getSupport()
      }

      AsyncFunction("hasKey") { () -> Bool in
        self.store.hasKey()
      }

      AsyncFunction("ensureKey") { (promise: Promise) in
        self.resolveCoded(promise) { try self.store.ensureKey() }
      }

      AsyncFunction("getKeyInfo") { (promise: Promise) in
        self.resolveCoded(promise) { try self.store.getKeyInfo() }
      }

      AsyncFunction("getPublicKeySpki") { (promise: Promise) in
        self.resolveCoded(promise) { try self.store.getPublicKeySpki() }
      }

      AsyncFunction("sign") { (messageHex: String, promise: Promise) in
        self.resolveCoded(promise) { try self.store.sign(messageHex) }
      }

      AsyncFunction("deleteKey") { (promise: Promise) in
        self.resolveCoded(promise) { try self.store.deleteKey(); return nil }
      }
    }
  }

  // resolveCoded runs a throwing store op and maps a typed DeviceKeyError to a
  // coded promise rejection (JS receives error.code). This is the iOS analogue
  // of the Android `coded { }` boundary that re-throws as a CodedException.
  private func resolveCoded(_ promise: Promise, _ block: () throws -> Any?) {
    do {
      promise.resolve(try block())
    } catch let e as DeviceKeyError {
      promise.reject(e.code, e.message)
    } catch {
      promise.reject(DeviceKeyCodes.nativeOperationFailed, "device key operation failed")
    }
  }
}
