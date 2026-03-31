package com.mayuri.watch.fall

/**
 * Configurable thresholds for fall classification.
 *
 * Defaults are tuned for a wrist-worn watch; they can be overridden via
 * remote config or tightened in tests.
 */
data class FallThresholds(
    /** Peak g-force (impact phase) above which a hard fall is suspected. */
    val hardFallImpactG: Float = 2.5f,
    /** Minimum orientation change (degrees) required for a hard fall. */
    val hardFallOrientationDeg: Float = 45f,
    /** G-force range for a soft fall (slow slide/collapse — gravity present but dampened). */
    val softFallGForceMin: Float = 0.2f,
    val softFallGForceMax: Float = 0.8f,
    /** Minimum orientation change (degrees) required for a soft fall. */
    val softFallOrientationDeg: Float = 20f,
)

enum class FallType { HARD, SOFT }

/**
 * A snapshot of sensor readings at a single moment in time.
 *
 * @param gForce           Magnitude of the acceleration vector in G
 *                         (1.0 = Earth gravity, 0 = free-fall, >2.5 = impact).
 * @param orientationDeltaDeg  Change in pitch/roll from the watch's resting
 *                             baseline orientation, in degrees.
 * @param isFaceDown       True when the watch face points toward the ground.
 *                         Used as an additional trigger for face-down auto-SOS.
 */
data class SensorSnapshot(
    val gForce: Float,
    val orientationDeltaDeg: Float,
    val isFaceDown: Boolean = false,
)

/**
 * Pure, Android-free fall classification logic.
 *
 * This class holds no Android framework dependencies so it can be exercised
 * with plain JVM unit tests without a device or emulator.
 *
 * The [FallDetectionService] feeds pre-processed sensor snapshots into
 * [classify] and acts on the result.
 */
class FallDetector(private val thresholds: FallThresholds = FallThresholds()) {

    /**
     * Classifies a sensor snapshot into a [FallType], or returns null if no
     * fall is detected.
     *
     * Priority: hard fall is checked before soft fall so a high-impact event
     * is never mis-classified as a soft one.
     */
    fun classify(snapshot: SensorSnapshot): FallType? = when {
        isHardFall(snapshot) -> FallType.HARD
        isSoftFall(snapshot) -> FallType.SOFT
        else -> null
    }

    private fun isHardFall(s: SensorSnapshot): Boolean =
        s.gForce >= thresholds.hardFallImpactG &&
            s.orientationDeltaDeg >= thresholds.hardFallOrientationDeg

    private fun isSoftFall(s: SensorSnapshot): Boolean =
        s.gForce in thresholds.softFallGForceMin..thresholds.softFallGForceMax &&
            s.orientationDeltaDeg >= thresholds.softFallOrientationDeg
}
