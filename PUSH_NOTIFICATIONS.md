# Push notifications

ChatterBox uses Firebase Cloud Messaging (FCM). The app requests notification permission, registers its FCM token with the Go API, and displays foreground messages in the app. The Go API sends a notification for a new message to registered devices other than the sender.

## Firebase setup

1. Create a Firebase project and register the Android app with application ID `app.chatterbox.mobile` and the iOS app with the bundle ID configured in Xcode.
2. Add the Android `google-services.json` to `client/android/app/` and the iOS `GoogleService-Info.plist` to `client/ios/Runner/` (include it in the Runner target).
3. Enable Firebase Cloud Messaging. For iOS, add an APNs authentication key in Firebase, then enable Push Notifications and Background Modes → Background fetch and Remote notifications for the Runner target in Xcode. iOS push delivery requires Apple Developer signing and a physical Apple device for setup/validation.
4. Run `flutterfire configure` from `client` to configure both Firebase apps, then run `flutter pub get` and rebuild the app.
5. Give the Go API Application Default Credentials for the same Firebase project. For local development, set `GOOGLE_APPLICATION_CREDENTIALS` to a Firebase service account JSON file. In production, use the hosting platform's workload identity or Application Default Credentials.

Without Firebase app configuration, the app continues to run and logs that push is not configured. Without backend credentials, chat messages still work but FCM delivery is disabled. Never bundle a service account key in the mobile app.
