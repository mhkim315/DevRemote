package expo.modules.pokitdevicekey

import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

// M3-auth-1A — thin Expo wrapper delegating to PokitDeviceKeyStore (the
// AndroidKeyStore logic lives in the store so it is instrumentation-testable
// without the Expo bridge). All AsyncFunctions run off the JS/UI thread.
class PokitDeviceKeyModule : Module() {
  private val store = PokitDeviceKeyStore()

  override fun definition() = ModuleDefinition {
    Name("PokitDeviceKey")

    AsyncFunction("getSupport") { store.getSupport() }
    AsyncFunction("hasKey") { store.hasKey() }
    AsyncFunction("ensureKey") { store.ensureKey() }
    AsyncFunction("getKeyInfo") { store.getKeyInfo() }
    AsyncFunction("getPublicKeySpki") { store.getPublicKeySpki() }
    AsyncFunction("sign") { messageHex: String -> store.sign(messageHex) }
    AsyncFunction("deleteKey") {
      store.deleteKey()
      null
    }
  }
}
