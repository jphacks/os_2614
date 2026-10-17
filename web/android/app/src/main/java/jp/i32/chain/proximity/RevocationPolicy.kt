package jp.i32.chain.proximity

/**
 * F-13: decides what a reported permission revocation means for the persisted user intent.
 * Android-free so the decision is unit-testable; the runtime permission query lives in
 * ProximityService.
 */
object RevocationPolicy {

    fun shouldClearPersistedIntent(permissionGranted: Boolean): Boolean = !permissionGranted
}
