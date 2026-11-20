# Push notifications

ChatterBox uses Firebase Cloud Messaging (FCM) for native push delivery. The app requests notification permission and registers its device token with the Go API. The API sends a notification for a new message to registered devices other than the sender.

## Firebase app configuration

The client reads Firebase settings from build-time defines; it does not need a service-account key or a generated `firebase_options.dart`. Register the native app in the same Firebase project used by the API, then provide:

- `FIREBASE_API_KEY`
- `FIREBASE_PROJECT_ID`
- `FIREBASE_MESSAGING_SENDER_ID`
- `FIREBASE_ANDROID_APP_ID`
- `FIREBASE_IOS_APP_ID` and `FIREBASE_IOS_BUNDLE_ID` for a native iOS build

The Web build additionally needs `FIREBASE_WEB_APP_ID`; `FIREBASE_AUTH_DOMAIN` is optional. GitHub Actions uses repository variables with these names. For local builds, pass them as `--dart-define` arguments. Keep Firebase Admin service-account credentials out of the client and out of Git.

## Android

Enable Firebase Cloud Messaging for the Firebase project and register the Android app using application ID `app.chatterbox.mobile`. Set the Android app ID and shared project values in the Android build workflow variables. The Go API needs Firebase Admin credentials to send notifications. Token registrations are kept in API memory, so a service restart clears them; devices register their token again when the app initializes.

## iOS

Native iOS builds require an Apple Developer account and signing. Register the iOS app with its actual bundle ID, add an APNs authentication key in Firebase, and enable Push Notifications and Background Modes → Background fetch and Remote notifications for the Runner target in Xcode. Test on a signed physical Apple device. This repository's iOS workflow produces an unsigned artifact and is not installable on iPhones; the iOS web app does not provide native FCM push.

## API credentials

The Go API uses Firebase Admin Application Default Credentials. Locally, set `GOOGLE_APPLICATION_CREDENTIALS` to a service-account JSON file outside the repository. On Render, upload the JSON as a secret file and set the variable to its mounted path. The service account needs access to Firestore and Firebase Cloud Messaging. Never put this credential in the Flutter app, repository, or downloadable APK.

If FCM setup is incomplete, the API logs that push delivery is disabled or reports send failures; authentication and Firestore-backed messaging still require a correctly configured Firebase project and credentials.
