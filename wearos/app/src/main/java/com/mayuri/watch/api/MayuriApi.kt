package com.mayuri.watch.api

import retrofit2.Retrofit
import retrofit2.converter.gson.GsonConverterFactory
import retrofit2.http.Body
import retrofit2.http.Header
import retrofit2.http.POST
import retrofit2.http.Path

// ─── Request / response models ───────────────────────────────────────────────

data class ReportFallRequest(val fall_type: String)

data class ReportFallResponse(
    val fall_id: String,
    val confirmation_window_sec: Int,
)

data class TriggerSOSRequest(
    val triggered_by: String = "manual",
    val fall_event_id: String? = null,
)

data class CallingContact(
    val full_name: String,
    val phone: String,
    val tier: Int,
)

data class TriggerSOSResponse(
    val sos_id: String,
    val calling_contact: CallingContact,
)

// ─── Retrofit interface ───────────────────────────────────────────────────────

interface MayuriApi {

    /** POST /falls — report a detected fall, get back the confirmation window. */
    @POST("falls")
    suspend fun reportFall(
        @Header("Authorization") token: String,
        @Body request: ReportFallRequest,
    ): ReportFallResponse

    /** POST /falls/{id}/cancel — user tapped "I'm OK" during countdown. */
    @POST("falls/{id}/cancel")
    suspend fun cancelFall(
        @Header("Authorization") token: String,
        @Path("id") fallId: String,
    )

    /** POST /sos — trigger SOS (optionally with fall context). */
    @POST("sos")
    suspend fun triggerSOS(
        @Header("Authorization") token: String,
        @Body request: TriggerSOSRequest,
    ): TriggerSOSResponse
}

// ─── Singleton client ─────────────────────────────────────────────────────────

object MayuriApiClient {
    // Override in tests or via BuildConfig.
    var baseUrl: String = "http://10.0.2.2:8080/"

    val api: MayuriApi by lazy {
        Retrofit.Builder()
            .baseUrl(baseUrl)
            .addConverterFactory(GsonConverterFactory.create())
            .build()
            .create(MayuriApi::class.java)
    }
}
