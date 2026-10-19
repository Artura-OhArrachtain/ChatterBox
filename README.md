# ChatterBox

ChatterBox is a Discord-inspired chat app with a Flutter client and a Go API. The client supports Android, iOS, and web builds. It includes server and direct-message views, voice/video calling, speech-to-text and text-to-speech, accessibility color controls, and push-notification integration.

## Download and install

### Android

After the first versioned GitHub Release is published, download **ChatterBox-Android.apk** from the [latest releases](https://github.com/Artura-OhArrachtain/ChatterBox/releases/latest). On Android, open the APK and follow the install prompts. Android may ask you to allow installs from the browser or file manager you used.

To create a release, push a version tag such as `v1.0.0`. GitHub Actions builds the APK and attaches it to the release:

```sh
git tag v1.0.0
git push origin v1.0.0
```

Before publishing, set the repository variable `CHATTERBOX_API_URL` to the deployed Go API’s public HTTPS URL. Create an Android signing keystore and keep a secure backup:

```sh
keytool -genkeypair -v -keystore chatterbox-release.jks -keyalg RSA -keysize 2048 -validity 10000 -alias chatterbox
base64 < chatterbox-release.jks | tr -d '\n'
```

Add the encoded output and keystore values as repository Actions secrets: `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEY_ALIAS`, `ANDROID_KEYSTORE_PASSWORD`, and `ANDROID_KEY_PASSWORD`. Future Android updates must use the same key. A versioned release includes an APK for direct installation and an Android App Bundle for Google Play.

### iPhone and iPad

The web app can be added to the Home Screen from Safari: open the published site, tap **Share**, then **Add to Home Screen**. Installing a native iOS app requires an Apple-signed build and distribution through Apple’s supported channels. The iOS workflow saves an unsigned app artifact for CI; it is not directly installable on an iPhone.

### Web

The GitHub Pages workflow publishes the web client when changes are pushed to `main`. Enable **Settings → Pages → Build and deployment → GitHub Actions** in the repository. The app is served from the project’s Pages URL; a separate install page is available at `/downloads.html`.

## Run locally

Requirements: [Flutter](https://docs.flutter.dev/get-started/install) (stable), [Go](https://go.dev/dl/) 1.22 or later, and Git.

Start the Go API in one terminal:

```sh
go run ./server
```

It listens on `http://localhost:8080`. In another terminal, run the Flutter client:

```sh
cd client
flutter pub get
flutter run
```

To run the web client with the local API, use:

```sh
flutter run -d chrome --dart-define=CHATTERBOX_API=http://localhost:8080
```

For a production web or mobile build, set `CHATTERBOX_API_URL` as a GitHub Actions repository variable. The workflows pass it to Flutter as `CHATTERBOX_API`. The API must be reachable by the device/browser; on a physical phone, `localhost` means the phone itself, so use the development computer’s network address or a deployed API URL.

## Backend and notifications

The Go API currently keeps chat messages, rooms, and call signaling in memory, so they reset when the server restarts. It is a demo backend and does not provide persistent storage or user authentication. For push notifications, configure Firebase for the Android/iOS apps and give the Go API Firebase Application Default Credentials. See [PUSH_NOTIFICATIONS.md](PUSH_NOTIFICATIONS.md) for setup. Never put Firebase service-account credentials in the client app.

### Deploy the Go API to Render

The root [`render.yaml`](render.yaml) defines a Render web service for the Go API. In Render, create a **Blueprint** from this GitHub repository and deploy the `chatterbox-api` service. Render builds the Go server, starts it on Render’s assigned `PORT`, and checks `/health`. After deployment, set the GitHub Actions repository variable `CHATTERBOX_API_URL` to the service’s HTTPS URL so the web and mobile release workflows point clients to that API.

The free service can sleep when idle, and this demo stores messages and call signaling in memory. A restart or redeploy clears that state; use persistent storage before relying on it for real conversations.

## Build commands

From the repository root:

```sh
cd client
flutter build web --release
flutter build apk --release
flutter build ios --release --no-codesign
```

The APK is written to `client/build/app/outputs/flutter-apk/app-release.apk`. An unsigned iOS build is not directly installable on an iPhone.
