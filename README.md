# ChatterBox

ChatterBox is a Discord-inspired chat app with a Flutter client and a Go API. The client supports Android, iOS, and web builds. It includes server and direct-message views, voice/video calling, speech-to-text and text-to-speech, accessibility color controls, and push-notification integration.

## Download and install

### Android

1. Open [ChatterBox latest releases](https://github.com/Artura-OhArrachtain/ChatterBox/releases/latest) on your Android phone.
2. Under **Assets**, download **ChatterBox-Android.apk**.
3. Open the downloaded APK and tap **Install**. If Android asks, allow your browser or file manager to install apps, then return to the APK and tap **Install** again.

If there is no APK under **Assets** yet, a maintainer needs to publish the first release below.

### iPhone and iPad

Open the [ChatterBox install page](https://artura-oharrachtain.github.io/ChatterBox/downloads.html) in Safari, tap **Share**, then **Add to Home Screen**. This installs ChatterBox as a web app. A native iOS app requires Apple signing and distribution; the CI build in this repository is unsigned and cannot be installed directly.

### Desktop and mobile web

Open the [ChatterBox web app](https://artura-oharrachtain.github.io/ChatterBox/) in a browser. On supported mobile browsers, use the browser’s **Install app** or **Add to Home Screen** option. These links become available after the GitHub Pages workflow has published the site.

### Publish the first Android download

Complete these one-time steps as a repository maintainer:

1. Deploy the Go API to Render using [`render.yaml`](render.yaml). Copy the API’s public HTTPS URL.
2. In the GitHub repository, open **Settings → Secrets and variables → Actions**. Add repository variable `CHATTERBOX_API_URL` with the API URL.
3. Create an Android signing key and store a secure backup of `chatterbox-release.jks`:

```sh
keytool -genkeypair -v -keystore chatterbox-release.jks -keyalg RSA -keysize 2048 -validity 10000 -alias chatterbox
base64 < chatterbox-release.jks | tr -d '\n'
```

4. Add the base64 output as secret `ANDROID_KEYSTORE_BASE64`. Add the alias as `ANDROID_KEY_ALIAS`, and the key/store passwords as `ANDROID_KEY_PASSWORD` and `ANDROID_KEYSTORE_PASSWORD`.
5. Push a new version tag from the repository root. The tag version should match the app version in `client/pubspec.yaml` (without the leading `v`):

```sh
git tag v1.0.0
git push origin v1.0.0
```

6. Wait for the **Package Android app** workflow to finish. It creates a GitHub Release containing **ChatterBox-Android.apk** for direct installation and **ChatterBox-Android.aab** for Google Play. Future updates must be signed with the same keystore.

To attach an APK to a release without using git commands, open **Actions → Package Android app → Run workflow**, enter the existing release tag (for example, `v1.0.0`), and run it. The workflow builds the APK and App Bundle and attaches both to that release. Leave the tag field empty to build an APK artifact only.

To publish the web install page, enable **Settings → Pages → Build and deployment → GitHub Actions**. The Pages workflow deploys the site after pushes to `main`; first publication may take a few minutes.

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
