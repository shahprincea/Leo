package com.mayuri.watch.fall

import android.content.Context
import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.wear.compose.material.Button
import androidx.wear.compose.material.ButtonDefaults
import androidx.wear.compose.material.MaterialTheme
import androidx.wear.compose.material.Text
import com.mayuri.watch.api.MayuriApiClient
import com.mayuri.watch.api.TriggerSOSRequest
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * FallConfirmationActivity shows a full-screen countdown after a fall is detected.
 *
 * The wearer has [windowSec] seconds to tap "I'M OK" before SOS is triggered
 * automatically. If [isFaceDown] is true the countdown is skipped and SOS fires
 * immediately (no interaction possible when face-down).
 *
 * Flow:
 *  - Countdown expires OR user taps "CALL NOW" → POST /sos with triggered_by=fall
 *  - User taps "I'M OK"                        → POST /falls/{id}/cancel
 *  - Either path resumes [FallDetectionService] monitoring when the activity closes.
 */
class FallConfirmationActivity : ComponentActivity() {

    companion object {
        private const val EXTRA_FALL_ID = "fall_id"
        private const val EXTRA_WINDOW_SEC = "window_sec"
        private const val EXTRA_FACE_DOWN = "is_face_down"

        fun buildIntent(
            context: Context,
            fallId: String,
            windowSec: Int,
            isFaceDown: Boolean = false,
        ): Intent = Intent(context, FallConfirmationActivity::class.java).apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
            putExtra(EXTRA_FALL_ID, fallId)
            putExtra(EXTRA_WINDOW_SEC, windowSec)
            putExtra(EXTRA_FACE_DOWN, isFaceDown)
        }
    }

    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val fallId = intent.getStringExtra(EXTRA_FALL_ID) ?: return finish()
        val windowSec = intent.getIntExtra(EXTRA_WINDOW_SEC, 10)
        val isFaceDown = intent.getBooleanExtra(EXTRA_FACE_DOWN, false)

        if (isFaceDown) {
            // Skip countdown entirely — face-down means no interaction possible.
            triggerSOS(fallId)
            return
        }

        setContent {
            MaterialTheme {
                FallConfirmationScreen(
                    windowSec = windowSec,
                    onImOk = { cancelFall(fallId) },
                    onCallNow = { triggerSOS(fallId) },
                    onCountdownExpired = { triggerSOS(fallId) },
                )
            }
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        scope.cancel()
        // Resume sensor monitoring.
        sendBroadcast(Intent(FallDetectionService.ACTION_RESUME_MONITORING).apply {
            setPackage(packageName)
        })
    }

    // ─── API actions ──────────────────────────────────────────────────────────

    private fun triggerSOS(fallId: String) {
        scope.launch {
            try {
                val token = getDeviceToken()
                MayuriApiClient.api.triggerSOS(
                    token = "Bearer $token",
                    request = TriggerSOSRequest(triggered_by = "fall", fall_event_id = fallId),
                )
            } catch (_: Exception) {
                // Best-effort; the watch may retry on reconnect.
            } finally {
                finish()
            }
        }
    }

    private fun cancelFall(fallId: String) {
        scope.launch {
            try {
                val token = getDeviceToken()
                MayuriApiClient.api.cancelFall(
                    token = "Bearer $token",
                    fallId = fallId,
                )
            } catch (_: Exception) {
                // Best-effort.
            } finally {
                finish()
            }
        }
    }

    private fun getDeviceToken(): String {
        val prefs = getSharedPreferences("mayuri", MODE_PRIVATE)
        return prefs.getString(FallDetectionService.PREF_DEVICE_TOKEN, "") ?: ""
    }
}

// ─── Composable UI ────────────────────────────────────────────────────────────

@Composable
fun FallConfirmationScreen(
    windowSec: Int,
    onImOk: () -> Unit,
    onCallNow: () -> Unit,
    onCountdownExpired: () -> Unit,
) {
    var remaining by remember { mutableIntStateOf(windowSec) }

    LaunchedEffect(Unit) {
        while (remaining > 0) {
            delay(1_000)
            remaining--
        }
        onCountdownExpired()
    }

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(Color(0xFF1A0000)),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
            modifier = Modifier.padding(horizontal = 12.dp),
        ) {
            Text(
                text = "FALL DETECTED",
                color = Color.White,
                fontSize = 14.sp,
                fontWeight = FontWeight.Bold,
                textAlign = TextAlign.Center,
            )

            Spacer(Modifier.height(4.dp))

            Text(
                text = remaining.toString(),
                color = Color(0xFFFF4444),
                fontSize = 36.sp,
                fontWeight = FontWeight.ExtraBold,
            )

            Spacer(Modifier.height(12.dp))

            // Primary action — large green "I'M OK" button.
            Button(
                onClick = onImOk,
                modifier = Modifier.fillMaxWidth(0.85f),
                colors = ButtonDefaults.buttonColors(backgroundColor = Color(0xFF2E7D32)),
                shape = CircleShape,
            ) {
                Text(
                    text = "I'M OK",
                    color = Color.White,
                    fontSize = 16.sp,
                    fontWeight = FontWeight.Bold,
                )
            }

            Spacer(Modifier.height(8.dp))

            // Secondary action — smaller red "CALL NOW" button.
            Button(
                onClick = onCallNow,
                modifier = Modifier.fillMaxWidth(0.7f),
                colors = ButtonDefaults.buttonColors(backgroundColor = Color(0xFFB71C1C)),
                shape = CircleShape,
            ) {
                Text(
                    text = "CALL NOW",
                    color = Color.White,
                    fontSize = 13.sp,
                )
            }
        }
    }
}
