# Mayuri — Elderly Care Watch App
## Product Requirements Document

---

## Problem Statement

Aging parents and elderly individuals living alone or with limited supervision face life-threatening risks every day — unexpected falls, missed medications, cardiac events, and wandering due to dementia or Alzheimer's. Family members, especially adult children living away from their parents, have no reliable, real-time way to monitor their loved one's safety and health without being physically present.

Existing solutions are either too clinical (medical alert pendants), too complex for elderly users to operate, or too dependent on the user actively seeking help — which fails entirely when the user is unconscious, confused, or unable to reach a phone.

The core problem: **families need passive, always-on safety assurance for their elderly loved ones, without burdening the elderly person with technology they won't use.**

---

## Solution

**Mayuri** is a Wear OS smartwatch app and companion platform that provides continuous, passive safety monitoring for elderly users. The elderly person simply wears the watch — no interaction required for core safety features. Family members configure everything remotely through a companion app (iOS, Android, Web) and receive real-time alerts, health data, and location information when it matters most.

Mayuri is named after the founder's mother. It is built for families, by a family.

---

## User Stories

### Elderly Wearer (Dad / Mom)

1. As an elderly wearer, I want a simple watch face with large text and a visible SOS button, so that I can always find the emergency button without confusion.
2. As an elderly wearer, I want the watch to automatically detect when I fall, so that help is summoned even if I am unable to press a button.
3. As an elderly wearer, I want a 10-second window to cancel a false fall alert, so that minor stumbles don't trigger unnecessary alarms.
4. As an elderly wearer, I want to press a single SOS button to instantly call my family, so that I can get help without navigating menus.
5. As an elderly wearer, I want to speak directly through the watch during an emergency call, so that I don't need to find or hold a phone.
6. As an elderly wearer, I want to receive medication reminders on my watch with a single tap to confirm, so that I never miss a dose.
7. As an elderly wearer, I want a morning check-in prompt on my watch, so that my family knows I am awake and well without me having to call them.
8. As an elderly wearer, I want to send one-tap preset messages to my family ("I'm okay", "Call me", "Coming home soon"), so that I can communicate without typing.
9. As an elderly wearer, I want the watch to work without my phone nearby, so that I am protected even when I forget my phone.
10. As an elderly wearer, I want my family to be able to push reminders to my watch ("Lunch at 1pm"), so that I receive timely reminders without checking my phone.
11. As an elderly wearer, I want the watch screen to dim and vibration to reduce at night, so that I am not disturbed while sleeping.
12. As an elderly wearer, I want emergency responders to see my name, blood type, medical conditions, and emergency contacts on my watch without unlocking it, so that I receive appropriate care if found unconscious.
13. As an elderly wearer with dementia, I want the watch to alert my family if I leave home, so that I am found quickly if I wander.

### Family Admin (Adult Child)

14. As a family admin, I want to set up the watch remotely before shipping it to my parent, so that my parent only needs to power it on to be fully protected.
15. As a family admin, I want to configure priority tiers for SOS escalation, so that the right person is always reachable.
16. As a family admin, I want to set on-call schedules for family members, so that SOS calls route to whoever is available at that time of day.
17. As a family admin, I want SOS calls to escalate through contacts if unanswered, so that my parent is never left without a response.
18. As a family admin, I want to receive a push notification when SOS is triggered, containing my parent's location, heart rate, SpO2, blood pressure, steps, and fall status, so that I arrive informed.
19. As a family admin, I want an SMS fallback if I don't open the push notification within 30 seconds, so that I never miss a critical alert.
20. As a family admin, I want to see my parent's real-time GPS location on a map in the companion app, so that I can find them quickly in an emergency.
21. As a family admin, I want to see my parent's last known location with a timestamp when GPS is unavailable, so that I have a starting point to search.
22. As a family admin, I want to define a home geofence, so that I receive an alert whenever my parent leaves the safe zone.
23. As a family admin, I want to configure safe zone hours, so that nighttime departures trigger immediate high-priority alerts.
24. As a family admin, I want to add medications with names, dosages, and schedules, so that my parent's medication regimen is managed in the app.
25. As a family admin, I want to see my parent's medication compliance history, so that I can identify missed doses over time.
26. As a family admin, I want a notification when my parent misses a medication confirmation after 15 minutes, so that I can follow up without waiting for an SOS.
27. As a family admin, I want to view my parent's health metrics (HR, SpO2, BP, steps) history in charts, so that I can monitor trends over time.
28. As a family admin, I want an alert when my parent's watch battery drops below 20%, so that I can remind them to charge before the watch dies.
29. As a family admin, I want an urgent alert when the watch battery drops below 10%, so that I know protection may be interrupted imminently.
30. As a family admin, I want an alert if my parent's watch is removed for more than one hour during daytime, so that I know they may not be protected.
31. As a family admin, I want a notification if my parent does not respond to the morning wellness check-in by 9am, so that I can check on them proactively.
32. As a family admin, I want to invite other family members to the companion app, so that multiple people can share monitoring responsibilities.
33. As a family admin, I want to control which family members can view live GPS location, so that location access is limited to trusted people.
34. As a family admin, I want to send messages directly to my parent's watch, so that I can communicate reminders without calling.
35. As a family admin, I want to receive my parent's one-tap preset message responses in the app, so that I know they are okay.
36. As a family admin, I want a 30-day free trial before being charged, so that I can evaluate Mayuri before committing.
37. As a family admin, I want to choose between monthly ($19.99) and annual ($179) billing per elderly user, so that I can optimize for cost.
38. As a family admin, I want to manage multiple elderly parents under one account, so that I can monitor both parents with a single subscription per person.

### Family Member (Non-Admin)

39. As a family member, I want to receive SOS call escalations according to the on-call schedule, so that I am only alerted when I am designated as available.
40. As a family member, I want to see the health and location data payload when I receive an SOS call, so that I arrive informed.
41. As a family member, I want to initiate a two-way VoIP check-in call to my parent's watch from the companion app, so that I can speak with them directly without using their phone number.
42. As a family member, I want to receive one-tap preset messages from my parent's watch, so that I know they are safe.

---

## Implementation Decisions

### Platform

- **Watch app:** Wear OS (Kotlin, native) — v1 only
- **Companion app:** Flutter (iOS + Android + Web) — single codebase
- **Backend:** AWS (HIPAA-eligible services), multi-AZ deployment

### Watch App Modules

**Fall Detection Module**
- Threshold-based using accelerometer + gyroscope
- Detects both hard falls (high g-force) and soft falls (orientation change + low g-force, e.g. sliding from bed)
- On detection: vibrate + full-screen confirmation UI with countdown timer
- Timer: configurable 5–30 seconds, default 10 seconds
- Buttons: large "I'M OK" (cancels), smaller "CALL NOW" (immediate SOS)
- If no interaction or face-down watch: auto-trigger SOS
- Suspended when wear detection reports watch is off wrist

**Health Monitoring Module**
- Heart rate: continuous monitoring via Wear OS Health Services API
- SpO2: periodic sampling (every 15 minutes) via Health Services API
- Blood pressure: Samsung Galaxy Watch only, via Samsung Health Sensor SDK; feature disabled on non-Samsung devices
- Steps: daily step count via Health Services API
- All readings written to local buffer, synced to backend on connectivity

**SOS Module**
- Single hardware button + on-screen button trigger
- Assembles payload: GPS coordinates, HR, SpO2, BP (if Samsung), steps, fall detected flag, battery level, timestamp
- Sends payload to backend SOS Routing Engine
- Initiates native cellular call (LTE) to on-call contact returned by routing engine
- If no data connection: direct 911 cellular call fallback

**GPS + Geofence Module**
- Geofence-based operation: passive when inside safe zone, active tracking when outside
- Always-on during active SOS event
- Sends location to backend on geofence exit
- Local cache of last known location with timestamp

**Calling Module**
- SOS: native cellular call via LTE SIM — no server dependency
- Check-in: VoIP via WebRTC, signaled through backend

**Watch Face Module**
- Custom watch face: large time, large date, visible SOS button, battery indicator, connectivity indicator
- Night mode: 10pm–7am, dimmed display, reduced haptic intensity

**Messaging Module**
- Preset messages: configurable list of one-tap responses ("I'm okay", "Call me", "Coming home soon")
- Incoming: display family push messages as watch notifications

**Medication Reminder Module**
- Pull medication schedule from backend on sync
- Display medication name + dosage at scheduled time with vibration
- One-tap "Taken" confirmation
- Report confirmation to backend; missed dose detected server-side after 15 min

**Daily Wellness Module**
- Morning prompt at configurable time (default 8am): "Good morning! How are you feeling?"
- Three-button emoji response: 😊 😐 😟
- Report response to backend; no-response triggers family alert server-side at 9am

**Wear Detection Module**
- On-wrist detection via optical HR sensor presence
- Removal for > 1 hour during daytime (6am–10pm): notify backend for family alert
- Fall detection suspended when not on wrist

**Battery Monitor Module**
- Alert backend at 20% (family push notification) and 10% (urgent alert)
- Include battery level in all SOS payloads

**Offline Buffer Module**
- Local SQLite queue for health readings, events, and confirmations
- Automatic sync on LTE reconnect
- 24-hour rolling buffer

**Emergency Medical ID Module**
- Accessible from watch lock screen without PIN
- Displays: name, blood type, medical conditions, allergies, emergency contacts
- Configured remotely by family admin

### Companion App Modules (Flutter)

**Auth + Family Management Module**
- Email/password + social login (Google, Apple)
- Role-based: Admin (full access) vs. Member (view + receive alerts)
- Admin invites members via email

**Elder Profile Module**
- Wearer name, photo, DOB, medical conditions, blood type, allergies
- 4-digit watch PIN for wearer login
- Emergency Medical ID content managed here

**Escalation Routing Module**
- Define priority tiers (up to 5)
- Per-tier: contact, timeout (default 20 sec, configurable)
- On-call schedules: per-contact availability windows (days + hours)
- Auto-escalate to 911 toggle after all contacts exhausted
- Live preview of current on-call contact

**Alert Module**
- FCM push for all alert types
- SMS fallback via Twilio if push not opened in 30 seconds
- Alert types: SOS, fall detected, geofence exit, missed medication, missed wellness check-in, low battery, watch removed
- Alert history log with timestamps and resolution status

**Live Dashboard Module**
- Real-time HR, SpO2, BP (Samsung), steps
- GPS map with live pin + last-seen timestamp
- Watch connectivity and battery status
- Active SOS banner with one-tap call back

**Health History Module**
- Time-series charts for HR, SpO2, BP, steps
- Daily/weekly/monthly views
- Export to PDF (for doctor visits)

**Medication Management Module**
- Add/edit/delete medications (name, dosage, frequency, schedule)
- Compliance calendar: daily confirmation status
- Missed dose log

**Messaging Module**
- Compose and send push message to watch (up to 80 characters)
- Incoming one-tap preset messages displayed as chat bubbles
- Message history per wearer

**Geofence Configuration Module**
- Draw or set radius-based safe zone on map
- Safe zone hours configuration
- Wandering alert sensitivity settings

**Subscription + Billing Module**
- Stripe integration
- $19.99/month or $179/year per elderly user
- 30-day free trial, no credit card required to start
- Multi-user management (one account, multiple elderly profiles)

### Backend Modules (AWS)

**SOS Routing Engine**
- Evaluates on-call schedule at time of SOS
- Returns primary contact phone number to watch
- Manages escalation: if no answer in tier timeout, calls next tier
- Twilio Programmable Voice for call initiation
- Auto-escalates to 911 if all tiers exhausted (if enabled)
- Emits SOS event with full health payload to Notification Service

**Health Data Service**
- HIPAA-compliant storage in RDS PostgreSQL (encrypted at rest)
- Ingests health readings from watch sync
- Serves health history to companion app
- Data access audit logged

**Notification Service**
- FCM push to companion app
- Twilio SMS fallback (30-second timeout)
- SMS payload: "SOS triggered — [Name] needs help. Location: [Google Maps link]"
- Full health data in push notification only (not SMS, for privacy)

**Location Service**
- Receives GPS coordinates from watch
- Evaluates geofence rules
- Emits geofence exit events
- 30-day rolling retention, auto-purged

**VoIP Signaling Service**
- WebRTC signaling for check-in calls between companion app and watch
- TURN/STUN server configuration

**Medication Service**
- Stores medication schedules per wearer
- Pushes schedule to watch on sync
- Detects missed confirmations after 15-minute window
- Emits missed dose event to Notification Service

**Device Management Service**
- Watch registration and pairing
- Configuration sync (watch face settings, geofence, medication schedule, escalation contacts)
- Remote setup: companion app pushes full config before watch is activated
- Watch polls on first boot and on reconnect

**Wellness Check-in Service**
- Schedules morning prompts per wearer (configurable time)
- Processes emoji responses from watch
- Emits no-response alert at 9am if no response received

**Audit Log Service**
- HIPAA-required: logs all PHI access with user ID, timestamp, action
- Immutable append-only log (AWS CloudWatch + S3 archival)
- Supports data access requests and deletion requests (GDPR/CCPA)

### Data Architecture

- All PHI encrypted at rest (AES-256) and in transit (TLS 1.3)
- BAA agreements in place with AWS, Twilio, Firebase
- GPS history: 30-day rolling retention
- Health data: retained for duration of subscription + 90 days post-cancellation
- User-requested deletion: full data purge within 30 days

### API Design

- REST API (JSON) for companion app ↔ backend
- WebSocket for live dashboard (real-time health + location updates)
- Watch ↔ backend: lightweight REST over LTE, with local queue for offline

---

## Testing Decisions

### What Makes a Good Test

- Test external behavior and outcomes, not internal implementation details
- Tests should remain valid across refactors of the internal logic
- Each test should have a single, clear failure reason
- Prefer integration tests over unit tests for safety-critical paths

### Modules Requiring Tests

**Fall Detection Module** — highest priority
- Simulate hard fall (high g-force spike + orientation change) → SOS triggered
- Simulate soft fall (low g-force + slow orientation change) → SOS triggered
- Simulate normal activity (walking, sitting down quickly) → no trigger
- User taps "I'M OK" within 10 seconds → SOS cancelled
- User taps "CALL NOW" → immediate SOS, no countdown
- Watch face-down, no interaction → SOS triggered after countdown
- Watch not on wrist → fall detection suspended, no trigger

**SOS Routing Engine** — highest priority
- On-call contact available → call routed to correct tier
- Primary unanswered after timeout → escalates to next tier
- All tiers exhausted, 911 enabled → 911 called
- All tiers exhausted, 911 disabled → family admin receives alert
- SOS payload contains all required health fields
- SOS triggered with no LTE data → direct 911 cellular fallback

**Notification Service**
- Push notification delivered within 5 seconds of SOS trigger
- Push not opened in 30 seconds → SMS sent
- SMS contains location link, does not contain raw health data

**Medication Service**
- Confirmation received within 15 minutes → no alert
- No confirmation after 15 minutes → missed dose alert emitted
- Correct medication schedule delivered to watch on sync

**Wellness Check-in Service**
- Response received before 9am → no alert
- No response by 9am → family alert emitted

**Geofence / Location Service**
- Watch exits safe zone → geofence exit event emitted immediately
- Watch exits safe zone outside safe zone hours → high-priority alert emitted
- Watch re-enters safe zone → exit event resolved

**Wear Detection Module**
- Watch removed for > 1 hour during daytime → family alert
- Watch removed at night (10pm–6am) → no alert
- Watch replaced on wrist → removal alert resolved

**Battery Monitor Module**
- Battery reaches 20% → family push notification
- Battery reaches 10% → urgent family alert
- Battery level included in SOS payload

---

## Out of Scope (v1)

- **Apple Watch / watchOS support** — see v2 section below
- **ML-based hybrid fall detection** — threshold-based ships in v1; ML model requires training data from production usage
- **AFib / irregular heart rhythm detection**
- **Vitals trending alerts** — requires clinical validation
- **Bluetooth BP cuff integration** (Omron, Withings)
- **B2B / assisted living facility dashboard**
- **Smart pill dispenser integration**
- **Multi-language support**
- **Android phone companion app offline mode**

---

## v2: Apple Watch Support

Apple Watch support is planned for v2 following v1 market validation.

**Additional work required:**
- New native watchOS app in Swift (separate codebase from Wear OS)
- HealthKit integration for HR, SpO2, fall detection, steps
- Core Motion for supplemental fall detection
- CallKit for native cellular SOS
- Separate Xcode project and App Store submission

**Key constraints to address in v2 planning:**
- Blood pressure sensor not available on Apple Watch — BP feature will not be available for Apple Watch wearers
- Apple Watch requires iPhone pairing for initial eSIM activation — not fully standalone; elderly user must have an iPhone for initial setup
- watchOS background execution restrictions require careful architecture to maintain always-on health monitoring
- Apple Watch fall detection is built-in (watchOS 5+) but cannot be customized — Core Motion supplement required for soft fall detection

**Shared across both platforms (no changes needed):**
- Entire backend (platform-agnostic REST API)
- Flutter companion app (already supports iOS)
- All AWS services, Twilio, FCM

---

## Further Notes

- **Named after Mayuri** — the founder's mother. The product story (built for his father, named after his mother) is a core part of the brand narrative and should be reflected in marketing and onboarding.
- **Fall detection is the most critical feature** — soft falls (sliding from bed, slow bathroom collapse) must never be missed. This requires extensive real-world testing with elderly volunteers before launch. Consider a closed beta with 20–30 elderly users specifically to validate fall detection accuracy.
- **LTE carrier compatibility** — Wear OS LTE watches require carrier support for standalone operation. At launch, document supported carriers and watch models explicitly. Samsung Galaxy Watch LTE series on major US carriers (Verizon, AT&T, T-Mobile) should be the primary support target.
- **HIPAA BAA checklist** — AWS (signed at account level for HIPAA-eligible services), Twilio (BAA available on enterprise plan), Firebase/FCM (Google BAA required). All three must be in place before any PHI enters production.
- **Regulatory note** — Mayuri is a consumer safety application, not an FDA-regulated medical device. Do not make diagnostic claims in marketing. Heart rate, SpO2, and blood pressure features should be described as "wellness monitoring" not "medical monitoring."
