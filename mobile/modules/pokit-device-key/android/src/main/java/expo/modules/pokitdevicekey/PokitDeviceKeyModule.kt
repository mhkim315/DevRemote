package expo.modules.pokitdevicekey

import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

// M3-auth-1C Android stub — returns not_implemented for every operation.
// The production Keystore provider arrives in M3-auth-1A.

class PokitDeviceKeyModule : Module() {
  override fun definition() = ModuleDefinition {
    Name("PokitDeviceKey")

    AsyncFunction("getSupport") {
      "not_implemented"
    }

    AsyncFunction("hasKey") {
      false
    }

    AsyncFunction("ensureKey") {
      throw UnsupportedOperationException("PokitDeviceKey is not implemented on this platform")
    }

    AsyncFunction("getKeyInfo") {
      throw UnsupportedOperationException("PokitDeviceKey is not implemented on this platform")
    }

    AsyncFunction("getPublicKeySpki") {
      throw UnsupportedOperationException("PokitDeviceKey is not implemented on this platform")
    }

    AsyncFunction("sign") { _: String ->
      throw UnsupportedOperationException("PokitDeviceKey is not implemented on this platform")
    }

    AsyncFunction("deleteKey") {
      throw UnsupportedOperationException("PokitDeviceKey is not implemented on this platform")
    }
  }
}
