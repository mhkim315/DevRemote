package expo.modules.pokitdevicekey

import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

// M3-auth-1C Android stub — returns not_implemented for every operation.
// The production Keystore provider arrives in M3-auth-1A.

class PokitDeviceKeyModule : Module() {
  // notImplemented has an explicit generic return type so each AsyncFunction
  // reifies a concrete R (a bare `throw` would infer the forbidden `Nothing`).
  private fun <T> notImplemented(): T =
    throw IllegalStateException("PokitDeviceKey is not implemented on this platform")

  override fun definition() = ModuleDefinition {
    Name("PokitDeviceKey")

    AsyncFunction("getSupport") {
      "not_implemented"
    }

    AsyncFunction("hasKey") {
      false
    }

    AsyncFunction("ensureKey") {
      notImplemented<Map<String, Any?>>()
    }

    AsyncFunction("getKeyInfo") {
      notImplemented<Map<String, Any?>>()
    }

    AsyncFunction("getPublicKeySpki") {
      notImplemented<String>()
    }

    AsyncFunction("sign") { _: String ->
      notImplemented<String>()
    }

    AsyncFunction("deleteKey") {
      notImplemented<Unit>()
    }
  }
}
