package expo.modules.pokitdevicekey

import expo.modules.kotlin.exception.CodedException
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

// M3-auth-1A — thin Expo wrapper delegating to PokitDeviceKeyStore.
//
// Every store call is routed through `coded {}` so a typed DeviceKeyException is
// re-thrown as an Expo CodedException. Expo surfaces CodedException.code as the
// JS error `code`, so callers (M3-auth-2 error handling) can distinguish
// key_missing / key_invalidated / hardware_unavailable / key_exportable, etc.
// All AsyncFunctions run off the JS/UI thread.
class PokitDeviceKeyModule : Module() {
  private val store = PokitDeviceKeyStore()

  // coded is the single native error-mapping boundary. A typed DeviceKeyException
  // becomes a CodedException carrying the same stable code; any other throwable
  // becomes a generic native_operation_failed with no raw provider details.
  private inline fun <T> coded(block: () -> T): T =
    try {
      block()
    } catch (e: DeviceKeyException) {
      throw CodedException(e.code, e.message ?: e.code, e)
    } catch (e: CodedException) {
      throw e
    } catch (e: Throwable) {
      throw CodedException(DeviceKeyCodes.NATIVE_OPERATION_FAILED, "device key operation failed", e)
    }

  override fun definition() = ModuleDefinition {
    Name("PokitDeviceKey")

    AsyncFunction("getSupport") { coded { store.getSupport() } }
    AsyncFunction("hasKey") { coded { store.hasKey() } }
    AsyncFunction("ensureKey") { coded { store.ensureKey() } }
    AsyncFunction("getKeyInfo") { coded { store.getKeyInfo() } }
    AsyncFunction("getPublicKeySpki") { coded { store.getPublicKeySpki() } }
    AsyncFunction("sign") { messageHex: String -> coded { store.sign(messageHex) } }
    AsyncFunction("deleteKey") {
      coded { store.deleteKey() }
      null
    }
  }
}
