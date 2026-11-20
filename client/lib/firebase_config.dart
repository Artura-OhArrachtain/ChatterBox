import 'package:flutter/foundation.dart';
import 'package:firebase_core/firebase_core.dart';

const _apiKey = String.fromEnvironment('FIREBASE_API_KEY');
const _projectId = String.fromEnvironment('FIREBASE_PROJECT_ID');
const _messagingSenderId =
    String.fromEnvironment('FIREBASE_MESSAGING_SENDER_ID');
const _webAppId = String.fromEnvironment('FIREBASE_WEB_APP_ID');
const _androidAppId = String.fromEnvironment('FIREBASE_ANDROID_APP_ID');
const _iosAppId = String.fromEnvironment('FIREBASE_IOS_APP_ID');
const _iosBundleId = String.fromEnvironment('FIREBASE_IOS_BUNDLE_ID');
const _authDomain = String.fromEnvironment('FIREBASE_AUTH_DOMAIN');

FirebaseOptions? firebaseOptionsForCurrentPlatform() {
  if (_apiKey.isEmpty || _projectId.isEmpty || _messagingSenderId.isEmpty) {
    return null;
  }

  if (kIsWeb) {
    if (_webAppId.isEmpty) return null;
    return FirebaseOptions(
      apiKey: _apiKey,
      appId: _webAppId,
      messagingSenderId: _messagingSenderId,
      projectId: _projectId,
      authDomain:
          _authDomain.isEmpty ? '$_projectId.firebaseapp.com' : _authDomain,
    );
  }

  switch (defaultTargetPlatform) {
    case TargetPlatform.android:
      if (_androidAppId.isEmpty) return null;
      return const FirebaseOptions(
        apiKey: _apiKey,
        appId: _androidAppId,
        messagingSenderId: _messagingSenderId,
        projectId: _projectId,
      );
    case TargetPlatform.iOS:
      if (_iosAppId.isEmpty || _iosBundleId.isEmpty) return null;
      return const FirebaseOptions(
        apiKey: _apiKey,
        appId: _iosAppId,
        messagingSenderId: _messagingSenderId,
        projectId: _projectId,
        iosBundleId: _iosBundleId,
      );
    default:
      return null;
  }
}

String firebaseConfigurationHelp() {
  if (_apiKey.isEmpty || _projectId.isEmpty || _messagingSenderId.isEmpty) {
    return 'Set FIREBASE_API_KEY, FIREBASE_PROJECT_ID, and '
        'FIREBASE_MESSAGING_SENDER_ID at build time.';
  }
  if (kIsWeb && _webAppId.isEmpty) {
    return 'Set FIREBASE_WEB_APP_ID at build time.';
  }
  if (!kIsWeb &&
      defaultTargetPlatform == TargetPlatform.android &&
      _androidAppId.isEmpty) {
    return 'Set FIREBASE_ANDROID_APP_ID at build time.';
  }
  if (!kIsWeb &&
      defaultTargetPlatform == TargetPlatform.iOS &&
      (_iosAppId.isEmpty || _iosBundleId.isEmpty)) {
    return 'Set FIREBASE_IOS_APP_ID and FIREBASE_IOS_BUNDLE_ID at build time.';
  }
  return 'This platform is not configured for Firebase.';
}
