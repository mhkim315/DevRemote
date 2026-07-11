import ExpoModulesCore

// M3-auth-1C iOS stub — returns not_implemented for every operation.
// The Secure Enclave provider arrives in M3-auth-1B.

public class PokitDeviceKeyModule: Module {
  public func definition() -> ModuleDefinition {
    ModuleDefinition {
      Name("PokitDeviceKey")

      AsyncFunction("getSupport") { () -> String in
        "not_implemented"
      }

      AsyncFunction("hasKey") { () -> Bool in
        false
      }

      AsyncFunction("ensureKey") { () throws -> Any in
        throw NSError(domain: "PokitDeviceKey", code: 1,
          userInfo: [NSLocalizedDescriptionKey: "PokitDeviceKey is not implemented on this platform"])
      }

      AsyncFunction("getKeyInfo") { () throws -> Any in
        throw NSError(domain: "PokitDeviceKey", code: 1,
          userInfo: [NSLocalizedDescriptionKey: "PokitDeviceKey is not implemented on this platform"])
      }

      AsyncFunction("getPublicKeySpki") { () throws -> String in
        throw NSError(domain: "PokitDeviceKey", code: 1,
          userInfo: [NSLocalizedDescriptionKey: "PokitDeviceKey is not implemented on this platform"])
      }

      AsyncFunction("sign") { (messageHex: String) throws -> String in
        throw NSError(domain: "PokitDeviceKey", code: 1,
          userInfo: [NSLocalizedDescriptionKey: "PokitDeviceKey is not implemented on this platform"])
      }

      AsyncFunction("deleteKey") { () throws in
        throw NSError(domain: "PokitDeviceKey", code: 1,
          userInfo: [NSLocalizedDescriptionKey: "PokitDeviceKey is not implemented on this platform"])
      }
    }
  }
}
