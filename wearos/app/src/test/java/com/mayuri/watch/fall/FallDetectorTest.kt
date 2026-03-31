package com.mayuri.watch.fall

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class FallDetectorTest {

    private val detector = FallDetector()

    // ─── Hard fall ────────────────────────────────────────────────────────────

    @Test
    fun `hard fall detected on high g-force and large orientation change`() {
        val snapshot = SensorSnapshot(gForce = 3.0f, orientationDeltaDeg = 60f)
        assertEquals(FallType.HARD, detector.classify(snapshot))
    }

    @Test
    fun `hard fall detected at exact threshold values`() {
        val snapshot = SensorSnapshot(gForce = 2.5f, orientationDeltaDeg = 45f)
        assertEquals(FallType.HARD, detector.classify(snapshot))
    }

    @Test
    fun `hard fall not triggered when g-force just below threshold`() {
        val snapshot = SensorSnapshot(gForce = 2.49f, orientationDeltaDeg = 60f)
        assertNull(detector.classify(snapshot))
    }

    @Test
    fun `hard fall not triggered when orientation change insufficient`() {
        val snapshot = SensorSnapshot(gForce = 3.0f, orientationDeltaDeg = 44f)
        assertNull(detector.classify(snapshot))
    }

    @Test
    fun `very high g-force with sufficient orientation is hard fall`() {
        val snapshot = SensorSnapshot(gForce = 5.0f, orientationDeltaDeg = 90f)
        assertEquals(FallType.HARD, detector.classify(snapshot))
    }

    // ─── Soft fall ────────────────────────────────────────────────────────────

    @Test
    fun `soft fall detected on low sustained g-force and slow orientation change`() {
        val snapshot = SensorSnapshot(gForce = 0.5f, orientationDeltaDeg = 30f)
        assertEquals(FallType.SOFT, detector.classify(snapshot))
    }

    @Test
    fun `soft fall detected at lower bound of g-force range`() {
        val snapshot = SensorSnapshot(gForce = 0.2f, orientationDeltaDeg = 20f)
        assertEquals(FallType.SOFT, detector.classify(snapshot))
    }

    @Test
    fun `soft fall detected at upper bound of g-force range`() {
        val snapshot = SensorSnapshot(gForce = 0.8f, orientationDeltaDeg = 20f)
        assertEquals(FallType.SOFT, detector.classify(snapshot))
    }

    @Test
    fun `soft fall not triggered when g-force above soft range`() {
        val snapshot = SensorSnapshot(gForce = 0.81f, orientationDeltaDeg = 30f)
        assertNull(detector.classify(snapshot))
    }

    @Test
    fun `soft fall not triggered when g-force below soft range`() {
        val snapshot = SensorSnapshot(gForce = 0.19f, orientationDeltaDeg = 30f)
        assertNull(detector.classify(snapshot))
    }

    @Test
    fun `soft fall not triggered when orientation change below threshold`() {
        val snapshot = SensorSnapshot(gForce = 0.5f, orientationDeltaDeg = 19f)
        assertNull(detector.classify(snapshot))
    }

    // ─── Normal activity (no false positives) ─────────────────────────────────

    @Test
    fun `normal walking does not trigger fall`() {
        val snapshot = SensorSnapshot(gForce = 1.2f, orientationDeltaDeg = 10f)
        assertNull(detector.classify(snapshot))
    }

    @Test
    fun `arm swing does not trigger fall`() {
        val snapshot = SensorSnapshot(gForce = 1.5f, orientationDeltaDeg = 35f)
        assertNull(detector.classify(snapshot))
    }

    @Test
    fun `high g-force without orientation change does not trigger hard fall`() {
        // e.g. banging watch on desk
        val snapshot = SensorSnapshot(gForce = 3.0f, orientationDeltaDeg = 5f)
        assertNull(detector.classify(snapshot))
    }

    @Test
    fun `resting at 1G does not trigger fall`() {
        val snapshot = SensorSnapshot(gForce = 1.0f, orientationDeltaDeg = 0f)
        assertNull(detector.classify(snapshot))
    }

    // ─── Custom thresholds ────────────────────────────────────────────────────

    @Test
    fun `strict custom thresholds prevent false hard fall classification`() {
        val strictDetector = FallDetector(
            FallThresholds(hardFallImpactG = 4.0f, hardFallOrientationDeg = 60f)
        )
        // Would be HARD with defaults, but not with strict thresholds.
        val snapshot = SensorSnapshot(gForce = 3.0f, orientationDeltaDeg = 60f)
        assertNull(strictDetector.classify(snapshot))
    }

    @Test
    fun `lenient custom thresholds can detect lower-impact hard fall`() {
        val lenientDetector = FallDetector(
            FallThresholds(hardFallImpactG = 2.0f, hardFallOrientationDeg = 30f)
        )
        val snapshot = SensorSnapshot(gForce = 2.1f, orientationDeltaDeg = 35f)
        assertEquals(FallType.HARD, lenientDetector.classify(snapshot))
    }

    // ─── Face-down flag (used by service for auto-SOS, not by classifier) ─────

    @Test
    fun `face-down flag does not affect fall classification on its own`() {
        // The isFaceDown flag is consumed by the service, not the classifier.
        val snapshot = SensorSnapshot(gForce = 1.0f, orientationDeltaDeg = 5f, isFaceDown = true)
        assertNull(detector.classify(snapshot))
    }
}
