# ChatterBox

A small Discord-inspired chat starter with a Go HTTP API and a Flutter client.

## Run the Go server

From the project root:

```sh
go run ./server
```

The API listens on `http://localhost:8080`.

## Run the Flutter client

```sh
cd client
flutter pub get
flutter run
```

By default the client connects to `http://localhost:8080`. On a phone, open Settings to enter an address the device can reach. Android emulators can use `http://10.0.2.2:8080`; a physical Android phone can use the computer's LAN IP while both are on the same network. For iPhone, use an HTTPS server URL.

Android and iOS project files are included. Run on Android with `flutter run -d <device-id>`. Build an Android install package with `flutter build apk --release`; the APK is written to `client/build/app/outputs/flutter-apk/app-release.apk`. iOS builds require macOS and Xcode. The iOS bundle ID is `app.chatterbox.mobile`, with a minimum deployment target of iOS 15. The `Build iOS app` GitHub Actions workflow checks unsigned release builds on macOS for pushes and pull requests. To distribute an iOS app, open `client/ios/Runner.xcworkspace` in Xcode, select your Apple development team, and configure signing.

## Publish the web app on GitHub Pages

The repository includes a GitHub Actions workflow at `.github/workflows/publish-pages.yml`. Push to the `main` branch (or run the workflow manually) to build and publish the Flutter web client. In the repository's **Settings → Pages**, set the build source to **GitHub Actions**. The workflow chooses the correct base path for a project site or an `owner.github.io` site.

GitHub Pages hosts static files only. Deploy the Go API separately to a public HTTPS host, then add a repository Actions variable named `CHATTERBOX_API_URL` with the API origin, for example `https://chat-api.example.com`. Both the Pages workflow and unsigned iOS build workflow use this variable, so the published clients connect to the same backend. The API must allow browser requests from the Pages site. If this variable is unset, the builds still complete but start disconnected; users can enter a reachable server URL in the app's Settings panel. Do not put service-account keys or other secrets in build variables.

The web app uses browser microphone/camera permissions for calls and requires HTTPS. For production voice/video calls, the signaling API needs a public HTTPS address and the call service needs a TURN server; the current Go API is an in-memory demo and is not production-ready for user data. Mobile FCM configuration does not enable browser push notifications; web push needs a Firebase web app and its service worker configured separately.

## Voice features

Tap the microphone beside the composer to dictate a message, then edit or send the recognized text. Tap the speaker on any message to have the device read it aloud; tap again to stop. These features use the target platform's speech recognition and text-to-speech services through Flutter plugins. Speech recognition is for short dictation sessions and may rely on the platform's configured recognition service.

On iOS, add `NSSpeechRecognitionUsageDescription` and `NSMicrophoneUsageDescription` to `ios/Runner/Info.plist`. On Android, add `RECORD_AUDIO` and `INTERNET` permissions to `android/app/src/main/AndroidManifest.xml` (and the speech recognition service `<queries>` entry for Android 11+). The app needs Android min SDK 21. The microphone permission already needed for calls is also used for dictation. On web, speech input depends on browser support and permission.

## Color vision controls

Open the eye icon in the channel header to choose a red-weak, green-weak, blue-weak, or monochrome color filter, adjust filter strength, or enable higher contrast. Message author labels use a colorblind-aware palette and retain readable names so color is not the only way to distinguish speakers. Preferences are saved on the device. The filters are visual approximations and may look different across displays.

## API

- `GET /health` — server health
- `GET /api/servers` — available chat servers, including The Hangout and Test Server
- `GET /api/rooms?server=hangout` — text channels for a server
- `GET /api/messages?room=general` — messages in a channel
- `POST /api/messages` — create a message with JSON fields `roomId`, `author`, and `content`
- `GET/POST /api/calls/signals` — WebRTC call signaling for offers, answers, and ICE candidates

Use the **Lounge** or **Video Hangout** voice/video channel in the server list (or phone drawer) to join that shared call room. The phone or camera button in a text channel header still starts a one-to-one voice or video call scoped to that text channel. Go relays WebRTC signaling and audio/video streams travel peer-to-peer. The sample uses a public STUN server and does not include TURN, so some NATs and corporate networks will block media. For production, configure a TURN service and serve the app/API over HTTPS.

Open Settings in the chat header or tap your profile to change voice/video settings. Speaker routing, microphone noise suppression, echo cancellation, automatic gain, manual microphone gain, camera preference, and video resolution are saved on the device and applied to new calls. The in-call camera switch button changes cameras during a video call.

Select your profile at the bottom of the channel list to change your display name, status, and avatar color. These profile choices are saved on the device and your display name is used for messages you send.

ChatterBox remembers the last server and text or direct conversation you opened, then restores it at startup.

Use the emoji button beside the composer to insert an emoji at the cursor. The GIF button opens a searchable, categorized GIPHY library; select a preview to add it to your message. You can also paste a direct GIF link. Sent GIF links display inline in chat.

Open the separate Direct Messages listing from the chat icon in the server rail or channel panel. It groups contacts into Favorites and Friends; use the star on a friend to add or remove them from Favorites. Favorites are saved on the device. Select Maya, Leo, or Sam to open a DM, or tap a contact in the desktop member list. Direct conversations have separate message histories and support voice/video calls. The sample has no accounts or authentication, so these demo conversations are not private.

The Flutter WebRTC plugin needs microphone and camera permissions on the target platform. Add `NSMicrophoneUsageDescription` and `NSCameraUsageDescription` to the iOS app's `Info.plist`, and request `RECORD_AUDIO` and `CAMERA` permissions in Android's manifest. On web, allow the browser permission prompt. The app currently uses the built-in Flutter project templates generated by `flutter create`.

This starter stores messages and call signaling in memory; restarting the Go server clears them. It has no accounts or access control yet.
