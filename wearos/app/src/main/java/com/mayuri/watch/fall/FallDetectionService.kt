package com.mayuri.watch.fall

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.hardware.Sensor
import android.hardware.SensorEvent
import android.hardware.SensorEventListener
import android.hardware.SensorManager
import android.os.IBinder
import android.util.Log
import com.mayuri.watch.api.MayuriApiClient
import com.mayuri.watch.api.ReportFallRequest
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlin.math.sqrt

/**
 * FallDetectionService runs as a foreground service monitoring the accelerometer
 * and rotation vector sensors for fall events.
 *
 * Architecture:
 *  - Sensor readings are processed via [FallDetector] (pure, testable logic).
 *  - On a positive classification the service POSTs to /falls and starts
 *    [FallConfirmationActivity] with the returned fall_id.
 *  - While the confirmation window is open, sensor monitoring is paused to
 *    prevent double-triggers.
 *  - Monitoring resumes when [FallConfirmationActivity] finishes (via broadcast).
 *  - If wear detection reports the watch is off-wrist, monitoring is suspended.
 */
class FallDetectionService : Service() {

    companion object {
        private const val TAG = "FallDetectionService"
        private const val NOTIFICATION_CHANNEL_ID = "fall_detection"
        private const val NOTIFICATION_ID = 1001
        const val ACTION_RESUME_MONITORING = "com.mayuri.watch.ACTION_RESUME_MONITORING"

        /** Shared prefs key for the device token (written by the auth flow). */
        const val PREF_DEVICE_TOKEN = "device_token"

        fun start(context: Context) {
            context.startForegroundService(Intent(context, FallDetectionService::class.java))
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, FallDetectionService::class.java))
        }
    }

    private val serviceJob = SupervisorJob()
    private val scope = CoroutineScope(Dispatchers.IO + serviceJob)

    private lateinit var sensorManager: SensorManager
    private val detector = FallDetector()

    // Sliding window: last N gForce + orientation samples.
    private val gForceWindow = ArrayDeque<Float>(20)
    private var baselineOrientationDeg: Float? = null
    private var currentOrientationDeg: Float = 0f

    // True while the confirmation UI is visible — prevents double-triggers.
    private var confirmationInProgress = false

    // ─── Service lifecycle ────────────────────────────────────────────────────

    override fun onCreate() {
        super.onCreate()
        sensorManager = getSystemService(SENSOR_SERVICE) as SensorManager
        createNotificationChannel()
        startForeground(NOTIFICATION_ID, buildNotification())
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_RESUME_MONITORING) {
            confirmationInProgress = false
            registerSensors()
            return START_STICKY
        }
        registerSensors()
        return START_STICKY
    }

    override fun onDestroy() {
        super.onDestroy()
        sensorManager.unregisterListener(sensorListener)
        scope.cancel()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    // ─── Sensor registration ──────────────────────────────────────────────────

    private fun registerSensors() {
        val accel = sensorManager.getDefaultSensor(Sensor.TYPE_ACCELEROMETER)
        val rotation = sensorManager.getDefaultSensor(Sensor.TYPE_ROTATION_VECTOR)
        sensorManager.registerListener(sensorListener, accel, SensorManager.SENSOR_DELAY_GAME)
        if (rotation != null) {
            sensorManager.registerListener(sensorListener, rotation, SensorManager.SENSOR_DELAY_GAME)
        }
    }

    // ─── Sensor event processing ──────────────────────────────────────────────

    private val sensorListener = object : SensorEventListener {

        override fun onSensorChanged(event: SensorEvent) {
            if (confirmationInProgress) return

            when (event.sensor.type) {
                Sensor.TYPE_ACCELEROMETER -> processAccelerometer(event.values)
                Sensor.TYPE_ROTATION_VECTOR -> processRotation(event.values)
            }
        }

        override fun onAccuracyChanged(sensor: Sensor, accuracy: Int) = Unit
    }

    private fun processAccelerometer(values: FloatArray) {
        val (x, y, z) = values
        val gForce = sqrt(x * x + y * y + z * z) / SensorManager.GRAVITY_EARTH
        val isFaceDown = z < -SensorManager.GRAVITY_EARTH * 0.7f

        // Maintain a rolling window of recent g-force readings.
        if (gForceWindow.size >= 20) gForceWindow.removeFirst()
        gForceWindow.addLast(gForce)

        val peakGForce = gForceWindow.max()
        val orientationDelta = baselineOrientationDeg?.let { baseline ->
            kotlin.math.abs(currentOrientationDeg - baseline)
        } ?: 0f

        val snapshot = SensorSnapshot(
            gForce = peakGForce,
            orientationDeltaDeg = orientationDelta,
            isFaceDown = isFaceDown,
        )

        val fallType = detector.classify(snapshot)
        if (fallType != null) {
            onFallDetected(fallType, isFaceDown)
        }
    }

    private fun processRotation(values: FloatArray) {
        // Convert rotation vector to orientation angles.
        val rotMatrix = FloatArray(9)
        SensorManager.getRotationMatrixFromVector(rotMatrix, values)
        val orientation = FloatArray(3)
        SensorManager.getOrientation(rotMatrix, orientation)
        // orientation[1] = pitch in radians.
        val pitchDeg = Math.toDegrees(orientation[1].toDouble()).toFloat()

        if (baselineOrientationDeg == null) {
            baselineOrientationDeg = pitchDeg
        }
        currentOrientationDeg = pitchDeg
    }

    // ─── Fall response ────────────────────────────────────────────────────────

    private fun onFallDetected(type: FallType, isFaceDown: Boolean) {
        if (confirmationInProgress) return
        confirmationInProgress = true

        // Pause sensors during confirmation window.
        sensorManager.unregisterListener(sensorListener)
        gForceWindow.clear()
        baselineOrientationDeg = null

        Log.i(TAG, "Fall detected: $type, face-down=$isFaceDown")

        scope.launch {
            try {
                val token = getDeviceToken()
                val response = MayuriApiClient.api.reportFall(
                    token = "Bearer $token",
                    request = ReportFallRequest(fall_type = type.name.lowercase()),
                )
                // Start confirmation UI.
                val intent = FallConfirmationActivity.buildIntent(
                    context = this@FallDetectionService,
                    fallId = response.fall_id,
                    windowSec = response.confirmation_window_sec,
                    isFaceDown = isFaceDown,
                )
                startActivity(intent)
            } catch (e: Exception) {
                Log.e(TAG, "Failed to report fall: ${e.message}")
                // If the API call fails, resume monitoring rather than leaving
                // the wearer unprotected.
                confirmationInProgress = false
                registerSensors()
            }
        }
    }

    // ─── Helpers ──────────────────────────────────────────────────────────────

    private fun getDeviceToken(): String {
        val prefs = getSharedPreferences("mayuri", Context.MODE_PRIVATE)
        return prefs.getString(PREF_DEVICE_TOKEN, "") ?: ""
    }

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            NOTIFICATION_CHANNEL_ID,
            "Fall Detection",
            NotificationManager.IMPORTANCE_LOW,
        ).apply { description = "Monitoring for falls in the background" }
        val nm = getSystemService(NOTIFICATION_SERVICE) as NotificationManager
        nm.createNotificationChannel(channel)
    }

    private fun buildNotification(): Notification =
        Notification.Builder(this, NOTIFICATION_CHANNEL_ID)
            .setContentTitle("Mayuri")
            .setContentText("Monitoring for falls")
            .setSmallIcon(android.R.drawable.ic_dialog_alert)
            .setOngoing(true)
            .build()
}
