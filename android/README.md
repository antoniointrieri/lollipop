# lollipop for Android

A companion app for [lollipop](../README.md): it shows your AI coding agents on your phone and brings one to the
front on the PC when you tap it.

- **Grid over the lock screen**: a Stream Deck style grid of colored squares, one per agent (🟡 working, 🟢 done,
  🔵 monitoring, 🔴 waiting for you). When an agent starts waiting for you, the phone turns the screen on and shows
  the grid over the lock screen, then dismisses it by itself.
- **Live Update**: the same state as an Android 16 promoted notification (the kind Maps uses for navigation), on
  the lock screen and the always-on display.
- **Tap to focus**: tapping a square tells lollipop on the PC to bring that agent's terminal or window to the
  front.

The app talks to lollipop over HTTP on the local network; the agent state never leaves the LAN.

## Requirements

- **Android 16 or later** (the grid and the Live Update use promoted notifications, `ProgressStyle`, added in
  Android 16). Developed and tested on a Pixel 10a with Android 17.
- lollipop running on a PC on the **same Wi-Fi network**, with the phone server enabled (see
  [Phone](../README.md#phone) in the main README).
- To build: **JDK 17+** and the **Android SDK** (platform 37, build-tools 37.0.0). The Gradle wrapper pulls Gradle
  itself. No Android Studio required.

## Build

```sh
cd android
./gradlew assembleDebug        # gradlew.bat on Windows
```

The APK lands in `app/build/outputs/apk/debug/app-debug.apk`.

## Install

With the phone connected over USB or wireless adb:

```sh
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

On first launch, grant the notification permission from the app. For the screen to wake over the lock screen on a
waiting agent, also allow full-screen notifications in Settings → Apps → Special app access → Notifications that
turn on the screen (or `adb shell appops set io.github.antoniointrieri.lollipop USE_FULL_SCREEN_INTENT allow`).

## Pairing

1. In lollipop on the PC, enable **Settings → Phone → Show agents on your phone**, and open the firewall port once
   (see the main README).
2. In the app, enter the **address** (`192.168.x.y:47600`, as shown by lollipop) and the **token**, then tap
   **Connetti**. The status line shows the connection state and the number of agents.

The app keeps a background connection (a foreground service) so it can update the Live Update and wake the screen
while the phone is idle.

## How it works

- `RemoteClient`: the network layer. It holds a Server-Sent Events connection to lollipop's `GET /events`, parses
  the state stream, exposes the current agent list, reconnects with backoff, and posts `POST /focus` on a tap.
  Only platform APIs (`HttpURLConnection`, `org.json`); no AndroidX.
- `AgentService`: a foreground service that keeps the connection alive, rebuilds the Live Update on each snapshot,
  and posts a full-screen alert (which turns on the screen and shows the grid) when an agent transitions to
  waiting.
- `GridActivity`: the lock-screen grid.
- `MainActivity`: pairing and connection status.

See the [HTTP contract](../remote.go) served by lollipop: `GET /state`, `GET /events` (SSE), `POST /focus`, all
authenticated with a `?token=` query parameter.

## Limitations

- The screen wakes while the phone is idle, but in deep Doze the connection can drop and reconnect with a delay.
  Exempting the app from battery optimization makes it more reliable.
- Bringing the PC window to the front depends on lollipop and on Windows' foreground rules; a focus triggered over
  the network may only flash the taskbar entry.
- Pairing is manual (address + token); there is no QR scan yet.
