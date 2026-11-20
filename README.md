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

This beta uses Firebase Authentication and Firestore. Complete the Firebase and API setup before publishing a web or Android build.

1. Create a Firebase project. Enable **Authentication → Sign-in method → Email/Password**, create a Firestore database, and register a Web app and an Android app with application ID `app.chatterbox.mobile`. Enable email verification in the Firebase Authentication email templates. Add `artura-oharrachtain.github.io` to **Authentication → Settings → Authorized domains**.
2. Deploy the Firestore security rules and required query indexes from this repository. Install the [Firebase CLI](https://firebase.google.com/docs/cli), then run from the repository root:

   ```sh
   firebase deploy --only firestore --project YOUR_FIREBASE_PROJECT_ID
   ```

   The deployed rules deny direct client access; the authenticated Go API uses the Firebase Admin SDK.
3. In the Firebase project settings, copy the Web API key, project ID, messaging sender ID, Web app ID, and Android app ID. In GitHub, open **Settings → Secrets and variables → Actions → Variables** and add:

   - `FIREBASE_API_KEY`
   - `FIREBASE_PROJECT_ID`
   - `FIREBASE_MESSAGING_SENDER_ID`
   - `FIREBASE_WEB_APP_ID`
   - `FIREBASE_ANDROID_APP_ID`
   - `FIREBASE_AUTH_DOMAIN` (optional; defaults to `YOUR_FIREBASE_PROJECT_ID.firebaseapp.com`)

   These client settings are compiled into the app and are not service-account credentials. Do not put an Admin SDK key in GitHub variables or in the client.
4. Create a Firebase service account with access to Firestore and Firebase Cloud Messaging. Deploy the Go API to Render using [`render.yaml`](render.yaml); set `FIREBASE_PROJECT_ID` and upload the service-account JSON as a Render secret file. Set `GOOGLE_APPLICATION_CREDENTIALS` to its mounted path, for example `/etc/secrets/firebase-admin.json`. Copy the deployed API’s public HTTPS URL.
5. Add GitHub Actions repository variable `CHATTERBOX_API_URL` with the API URL. The Pages and Android workflows now stop with an explicit error if the required API or Firebase client settings are missing.
6. Create an Android signing key and store a secure backup of `chatterbox-release.jks`:

```sh
keytool -genkeypair -v -keystore chatterbox-release.jks -keyalg RSA -keysize 2048 -validity 10000 -alias chatterbox
base64 < chatterbox-release.jks | tr -d '\n'
```

7. Add the base64 output as GitHub Actions secret `ANDROID_KEYSTORE_BASE64`. Add the alias as `ANDROID_KEY_ALIAS`, and the key/store passwords as `ANDROID_KEY_PASSWORD` and `ANDROID_KEYSTORE_PASSWORD`.
8. Push a new version tag from the repository root. The tag version should match the app version in `client/pubspec.yaml` (without the leading `v`):

```sh
git tag v1.0.0
git push origin v1.0.0
```

9. Wait for the **Package Android app** workflow to finish. It creates a GitHub Release containing **ChatterBox-Android.apk** for direct installation and **ChatterBox-Android.aab** for Google Play. Future updates must be signed with the same keystore.

To attach an APK to a release without using git commands, open **Actions → Package Android app → Run workflow**, enter the existing release tag (for example, `v1.0.0`), and run it. The workflow builds the APK and App Bundle and attaches both to that release. Leave the tag field empty to build an APK artifact only.

To publish the web install page, enable **Settings → Pages → Build and deployment → GitHub Actions**. The Pages workflow deploys the site after pushes to `main`; first publication may take a few minutes.

Before sharing the beta, test with two verified accounts: create accounts, verify the email links, search for one another, exchange DM messages, sign out, and sign back in. Calls use WebRTC/STUN and may require a TURN service on restrictive networks. The API currently keeps call signaling and push-token registrations in memory; they are lost when it restarts. Open registration has no rate limiting or moderation tooling yet, so treat this as a small beta, not a production-scale launch.

## Run locally

Requirements: [Flutter](https://docs.flutter.dev/get-started/install) (stable), [Go](https://go.dev/dl/) 1.22 or later, and Git.

If Flutter is already available in this workspace at `.deps/flutter`, add its SDK and package cache before running the commands below. The Git settings trust only that local SDK checkout for this shell, which may be needed when the workspace is on a shared mount:

```sh
export PATH="$PWD/.deps/flutter/bin:$PATH"
export PUB_CACHE="$PWD/.deps/pub-cache"
export GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0=safe.directory
export GIT_CONFIG_VALUE_0="$PWD/.deps/flutter"
```

The Go API requires Firebase Admin credentials and a project configured for Authentication and Firestore. Keep the service-account JSON outside the repository. Start the API in one terminal:

```sh
export FIREBASE_PROJECT_ID=your-firebase-project-id
export GOOGLE_APPLICATION_CREDENTIALS=/absolute/path/to/firebase-admin.json
go run ./server
```

It listens on `http://localhost:8080`. In another terminal, run the Flutter client with the client app settings from Firebase project settings:

```sh
cd client
flutter pub get
flutter run -d web-server --web-port=8081 \
  --dart-define=CHATTERBOX_API=http://localhost:8080 \
  --dart-define=FIREBASE_API_KEY=your-web-api-key \
  --dart-define=FIREBASE_PROJECT_ID=your-firebase-project-id \
  --dart-define=FIREBASE_MESSAGING_SENDER_ID=your-sender-id \
  --dart-define=FIREBASE_WEB_APP_ID=your-web-app-id
```

Open the printed URL in a browser. Use `flutter run -d chrome` instead if Chrome is installed. For Android, use the same common Firebase defines, replace `FIREBASE_WEB_APP_ID` with `FIREBASE_ANDROID_APP_ID`, and run `flutter run -d <device>`.

For a production web or mobile build, set `CHATTERBOX_API_URL` as a GitHub Actions repository variable. The workflows pass it to Flutter as `CHATTERBOX_API`. The API must be reachable by the device/browser; on a physical phone, `localhost` means the phone itself, so use the development computer’s network address or a deployed API URL.

## Backend and notifications

Firebase Authentication requires a verified email before the API accepts requests. User profiles, DMs, and messages are stored in Firestore; deploy [firestore.rules](firestore.rules), [firestore.indexes.json](firestore.indexes.json), and [firebase.json](firebase.json) before starting the beta. The API uses Firebase Admin Application Default Credentials from `GOOGLE_APPLICATION_CREDENTIALS` locally and on Render. Push delivery requires those credentials to have Firebase Cloud Messaging access. See [PUSH_NOTIFICATIONS.md](PUSH_NOTIFICATIONS.md) for platform details. Never put Firebase service-account credentials in the client app.

### Add chat servers and rooms

Edit [`server/config.json`](server/config.json) to add or rename servers and their rooms. Each server needs a unique `id` and a display `name`. Each room needs a unique `id`, a display `name`, and a `serverId` that matches one of the server IDs. Use lowercase IDs with hyphens, for example:

```json
{ "id": "book-club", "name": "Book Club", "serverId": "hangout" }
```

The API reads this file at startup from the repository root. To use another file, set `CHATTERBOX_SERVER_CONFIG` to its path. Restart or redeploy the API after editing the config. Room IDs also identify message history, so keep existing IDs if you want existing messages to stay associated with the same rooms.

### Deploy the Go API to Render

The root [`render.yaml`](render.yaml) defines a Render web service for the Go API and prompts for `FIREBASE_PROJECT_ID` and `GOOGLE_APPLICATION_CREDENTIALS`. In Render, create a **Blueprint** from this GitHub repository and deploy the `chatterbox-api` service. Add the service-account JSON as a secret file, set `GOOGLE_APPLICATION_CREDENTIALS` to its mounted path, and ensure the service account can access Firestore and Firebase Cloud Messaging. Render starts the Go server on its assigned `PORT` and checks `/health`. Set GitHub Actions variable `CHATTERBOX_API_URL` to the service’s HTTPS URL.

The free service can sleep when idle. Firestore-backed profiles and messages persist across restarts, but call signaling and push-token registrations currently do not; calls may be interrupted and users may need to reopen the app to register push tokens after an API restart.

## Build commands

From the repository root:

```sh
cd client
flutter build web --release
flutter build apk --release
flutter build ios --release --no-codesign
```

The APK is written to `client/build/app/outputs/flutter-apk/app-release.apk`. An unsigned iOS build is not directly installable on an iPhone.
