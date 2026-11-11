import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter_tts/flutter_tts.dart';
import 'package:http/http.dart' as http;
import 'package:speech_to_text/speech_to_text.dart' as stt;
import 'package:shared_preferences/shared_preferences.dart';

import 'call_screen.dart';
import 'call_session.dart';

const ink = Color(0xFF111318);
const panel = Color(0xFF191B22);
const panelLight = Color(0xFF22252E);
const accent = Color(0xFF7588F5);
const muted = Color(0xFF9297A5);
const apiBase = String.fromEnvironment('CHATTERBOX_API',
    defaultValue: 'http://localhost:8080');

enum ColorVisionMode {
  standard,
  protanopia,
  deuteranopia,
  tritanopia,
  monochrome
}

extension ColorVisionModeLabel on ColorVisionMode {
  String get label => switch (this) {
        ColorVisionMode.standard => 'Standard',
        ColorVisionMode.protanopia => 'Red-weak (protanopia)',
        ColorVisionMode.deuteranopia => 'Green-weak (deuteranopia)',
        ColorVisionMode.tritanopia => 'Blue-weak (tritanopia)',
        ColorVisionMode.monochrome => 'Monochrome',
      };
}

void main() => runApp(const ChatterBoxApp());

@pragma('vm:entry-point')
Future<void> _firebaseMessagingBackgroundHandler(RemoteMessage message) async {
  await Firebase.initializeApp();
}

class ChatRoom {
  const ChatRoom(this.id, this.name);
  final String id;
  final String name;
  factory ChatRoom.fromJson(Map<String, dynamic> json) =>
      ChatRoom(json['id'] as String, json['name'] as String);
}

class ChatServer {
  const ChatServer(this.id, this.name);
  final String id;
  final String name;
  factory ChatServer.fromJson(Map<String, dynamic> json) =>
      ChatServer(json['id'] as String, json['name'] as String);
}

class ChatContact {
  const ChatContact(this.id, this.name, this.status, this.color);
  final String id;
  final String name;
  final String status;
  final Color color;
}

class GifLibraryItem {
  const GifLibraryItem(this.title, this.category, this.url);
  final String title;
  final String category;
  final String url;
}

class ChatMessage {
  const ChatMessage(
      {required this.id,
      required this.author,
      required this.content,
      required this.createdAt});
  final int id;
  final String author;
  final String content;
  final DateTime createdAt;
  factory ChatMessage.fromJson(Map<String, dynamic> json) => ChatMessage(
        id: json['id'] as int,
        author: json['author'] as String,
        content: json['content'] as String,
        createdAt: DateTime.parse(json['createdAt'] as String),
      );
}

class ChatterBoxApp extends StatelessWidget {
  const ChatterBoxApp({super.key});
  @override
  Widget build(BuildContext context) => MaterialApp(
        title: 'ChatterBox',
        debugShowCheckedModeBanner: false,
        theme: ThemeData(
            useMaterial3: true,
            brightness: Brightness.dark,
            scaffoldBackgroundColor: ink,
            colorScheme:
                const ColorScheme.dark(primary: accent, surface: panel),
            fontFamily: 'Roboto'),
        home: const ChatHome(),
      );
}

class ChatHome extends StatefulWidget {
  const ChatHome({super.key});
  @override
  State<ChatHome> createState() => _ChatHomeState();
}

class _ChatHomeState extends State<ChatHome> {
  final _scaffoldKey = GlobalKey<ScaffoldState>();
  final _input = TextEditingController();
  final _apiBaseController = TextEditingController();
  final _speech = stt.SpeechToText();
  final _tts = FlutterTts();
  List<ChatServer> _servers = const [
    ChatServer('hangout', 'The Hangout'),
    ChatServer('test', 'Test Server')
  ];
  static const _contacts = <ChatContact>[
    ChatContact(
        'maya', 'Maya Chen', 'Building something cool', Color(0xFFB478D3)),
    ChatContact('leo', 'Leo Park', 'Listening to music', Color(0xFF55B99A)),
    ChatContact('sam', 'Sam Rivera', 'Away', Color(0xFFE49A5A)),
  ];
  static const _gifLibrary = <GifLibraryItem>[
    GifLibraryItem('Excited', 'Reactions',
        'https://media.giphy.com/media/5GoVLqeAOo6PK/giphy.gif'),
    GifLibraryItem('Tumbleweed', 'Reactions',
        'https://media.giphy.com/media/1Zbeweu52ZaQE/giphy.gif'),
    GifLibraryItem('Nod', 'Reactions',
        'https://media.giphy.com/media/xKy2w6LehxxHa/giphy.gif'),
    GifLibraryItem('Mind blown', 'Reactions',
        'https://media.giphy.com/media/VAJvLpyrgr3Og/giphy.gif'),
    GifLibraryItem('Cat', 'Animals',
        'https://media.giphy.com/media/H2fORSKZw4SCQ/giphy.gif'),
    GifLibraryItem('Party', 'Celebration',
        'https://media.giphy.com/media/e5PpBi9XlQn8A/giphy.gif'),
    GifLibraryItem('Let’s go', 'Celebration',
        'https://media.giphy.com/media/3gNotAoIRZsb9UHPnj/giphy.gif'),
    GifLibraryItem('Classic reaction', 'Reactions',
        'https://media.giphy.com/media/cZ7rmKfFYOvYI/giphy.gif'),
    GifLibraryItem('American Psycho', 'Reactions',
        'https://media.giphy.com/media/Ggjwvmqktuvf2/giphy.gif'),
  ];
  List<ChatRoom> _rooms = const [
    ChatRoom('general', 'general'),
    ChatRoom('introductions', 'introductions'),
    ChatRoom('random', 'random')
  ];
  List<ChatMessage> _messages = const [];
  String _apiBase = apiBase;
  String _roomId = 'general';
  String _serverId = 'hangout';
  ChatContact? _activeDM;
  bool _showDirectMessages = false;
  Set<String> _favoriteContactIds = <String>{};
  bool _online = false;
  bool _sending = false;
  StreamSubscription<RemoteMessage>? _pushSubscription;
  StreamSubscription<String>? _pushTokenSubscription;
  bool _listening = false;
  bool _speechInitialized = false;
  bool _speechBusy = false;
  ColorVisionMode _visionMode = ColorVisionMode.standard;
  double _filterStrength = 1;
  bool _highContrast = false;
  bool _speakerphone = true;
  bool _frontCamera = true;
  int _videoHeight = 720;
  bool _noiseSuppression = true;
  bool _echoCancellation = true;
  bool _autoGainControl = true;
  double _microphoneGain = 1;
  String _profileName = 'You';
  String _profileStatus = 'Online';
  int _profileColorValue = 0xFFB478D3;
  int? _speakingMessageId;
  String _speechPrefix = '';
  Timer? _poller;
  ChatRoom get _room => _rooms.firstWhere((room) => room.id == _roomId,
      orElse: () => _rooms.first);
  ChatServer get _server =>
      _servers.firstWhere((server) => server.id == _serverId,
          orElse: () => _servers.first);
  String get _conversationId =>
      _activeDM == null ? _roomId : 'dm-${_activeDM!.id}';
  Color get _profileColor => Color(_profileColorValue);
  String get _profileInitials {
    final parts = _profileName.trim().split(RegExp(r'\s+'));
    if (parts.isEmpty || parts.first.isEmpty) return '?';
    return parts.length > 1
        ? '${parts.first[0]}${parts.last[0]}'.toUpperCase()
        : parts.first[0].toUpperCase();
  }

  @override
  void initState() {
    super.initState();
    _loadColorSettings();
    _initializePushNotifications();
    _poller = Timer.periodic(const Duration(seconds: 3), (_) => _refresh());
  }

  @override
  void dispose() {
    _poller?.cancel();
    _pushSubscription?.cancel();
    _pushTokenSubscription?.cancel();
    unawaited(_speech.stop());
    unawaited(_tts.stop());
    _input.dispose();
    _apiBaseController.dispose();
    super.dispose();
  }

  Future<void> _initializePushNotifications() async {
    try {
      await Firebase.initializeApp();
      FirebaseMessaging.onBackgroundMessage(
          _firebaseMessagingBackgroundHandler);
      final messaging = FirebaseMessaging.instance;
      final settings = await messaging.requestPermission(
          alert: true, badge: true, sound: true);
      if (settings.authorizationStatus == AuthorizationStatus.denied) return;
      _pushSubscription = FirebaseMessaging.onMessage.listen((message) {
        final title = message.notification?.title ?? 'New ChatterBox message';
        final body = message.notification?.body ?? '';
        if (mounted) _snack(body.isEmpty ? title : '$title: $body');
      });
      _pushTokenSubscription = messaging.onTokenRefresh
          .listen((token) => unawaited(_registerPushToken(token)));
      final token = await _getPushToken(messaging);
      if (token != null) unawaited(_registerPushToken(token));
    } catch (error) {
      debugPrint('Push notifications are not configured: $error');
    }
  }

  Future<void> _registerPushToken(String token) async {
    try {
      await http.post(Uri.parse('$_apiBase/api/push/register'),
          headers: {'Content-Type': 'application/json'},
          body: jsonEncode({'token': token, 'user': _profileName}));
    } catch (_) {
      // Registration is retried when Firebase rotates the token.
    }
  }

  Future<void> _registerCurrentPushToken() async {
    try {
      final token = await _getPushToken(FirebaseMessaging.instance);
      if (token != null) await _registerPushToken(token);
    } catch (_) {
      // Firebase configuration may be added after the app is installed.
    }
  }

  Future<String?> _getPushToken(FirebaseMessaging messaging) async {
    if (!kIsWeb && defaultTargetPlatform == TargetPlatform.iOS) {
      // FCM token requests can fail until iOS has supplied the APNs token.
      for (var attempt = 0; attempt < 10; attempt++) {
        if (await messaging.getAPNSToken() != null) break;
        await Future<void>.delayed(const Duration(seconds: 1));
      }
      if (await messaging.getAPNSToken() == null) return null;
    }
    return messaging.getToken();
  }

  Future<void> _toggleDictation() async {
    if (_listening) {
      await _speech.stop();
      if (mounted) setState(() => _listening = false);
      return;
    }
    if (_speechBusy) return;
    _speechBusy = true;
    try {
      if (!_speechInitialized) {
        _speechInitialized = await _speech.initialize(
          onStatus: (status) {
            if (mounted && (status == 'done' || status == 'notListening'))
              setState(() => _listening = false);
          },
          onError: (_) {
            if (mounted) {
              setState(() => _listening = false);
              _snack(
                  'Speech recognition is unavailable. Check microphone permissions.');
            }
          },
        );
      }
      if (!_speechInitialized) {
        _snack('Speech recognition is unavailable on this device.');
        return;
      }
      _speechPrefix = _input.text.trimRight();
      await _speech.listen(
        onResult: (result) {
          final words = result.recognizedWords;
          final prefix = _speechPrefix.isEmpty ? '' : '$_speechPrefix ';
          _input.value = TextEditingValue(
              text: '$prefix$words',
              selection:
                  TextSelection.collapsed(offset: ('$prefix$words').length));
        },
        listenFor: const Duration(seconds: 45),
        pauseFor: const Duration(seconds: 3),
        partialResults: true,
      );
      if (mounted) setState(() => _listening = _speech.isListening);
    } catch (_) {
      if (mounted)
        _snack('Could not start voice typing. Check microphone permissions.');
    } finally {
      _speechBusy = false;
    }
  }

  Future<void> _speakMessage(ChatMessage message) async {
    try {
      if (_speakingMessageId == message.id) {
        await _tts.stop();
        if (mounted) setState(() => _speakingMessageId = null);
        return;
      }
      await _speech.stop();
      await _tts.stop();
      if (mounted) setState(() => _speakingMessageId = message.id);
      await _tts.awaitSpeakCompletion(true);
      final result =
          await _tts.speak('${message.author} says: ${message.content}');
      if (result == 0 && mounted)
        _snack('Text to speech is unavailable on this device.');
    } catch (_) {
      if (mounted) _snack('Could not read this message aloud.');
    } finally {
      if (mounted) setState(() => _speakingMessageId = null);
    }
  }

  Future<void> _refresh() async {
    try {
      final serversResponse = await http
          .get(Uri.parse('$_apiBase/api/servers'))
          .timeout(const Duration(seconds: 3));
      if (serversResponse.statusCode == 200) {
        final servers = (jsonDecode(serversResponse.body) as List)
            .map((item) => ChatServer.fromJson(item as Map<String, dynamic>))
            .toList();
        if (servers.isNotEmpty) {
          _servers = servers;
          if (!servers.any((server) => server.id == _serverId)) {
            _serverId = servers.first.id;
          }
        }
      }
      final roomsResponse = await http
          .get(Uri.parse('$_apiBase/api/rooms?server=$_serverId'))
          .timeout(const Duration(seconds: 3));
      if (roomsResponse.statusCode != 200)
        throw Exception('Could not load rooms');
      final rooms = (jsonDecode(roomsResponse.body) as List)
          .map((item) => ChatRoom.fromJson(item as Map<String, dynamic>))
          .toList();
      final selected =
          rooms.any((r) => r.id == _roomId) ? _roomId : rooms.first.id;
      final roomWasRestored = selected != _roomId;
      final messageResponse = await http
          .get(Uri.parse('$_apiBase/api/messages?room=$_conversationId'))
          .timeout(const Duration(seconds: 3));
      if (messageResponse.statusCode != 200)
        throw Exception('Could not load messages');
      final messages = (jsonDecode(messageResponse.body) as List)
          .map((item) => ChatMessage.fromJson(item as Map<String, dynamic>))
          .toList();
      if (mounted)
        setState(() {
          _rooms = rooms;
          _roomId = selected;
          _messages = messages;
          _online = true;
        });
      if (roomWasRestored) unawaited(_saveLastChat());
    } catch (_) {
      if (mounted) setState(() => _online = false);
    }
  }

  void _selectServer(ChatServer server) {
    if (_serverId == server.id) {
      if (_showDirectMessages) setState(() => _showDirectMessages = false);
      return;
    }
    setState(() {
      _serverId = server.id;
      _roomId = '';
      _activeDM = null;
      _showDirectMessages = false;
      _messages = const [];
    });
    unawaited(_saveLastChat());
    if (MediaQuery.sizeOf(context).width < 720) {
      _scaffoldKey.currentState?.closeDrawer();
    }
    _refresh();
  }

  void _startDirectMessage(ChatContact contact) {
    setState(() {
      _activeDM = contact;
      _showDirectMessages = true;
      _messages = const [];
    });
    unawaited(_saveLastChat());
    if (MediaQuery.sizeOf(context).width < 720) {
      _scaffoldKey.currentState?.closeDrawer();
    }
    _refresh();
  }

  Future<void> _toggleFavorite(ChatContact contact) async {
    final updated = Set<String>.from(_favoriteContactIds);
    if (!updated.add(contact.id)) updated.remove(contact.id);
    setState(() => _favoriteContactIds = updated);
    final prefs = await SharedPreferences.getInstance();
    await prefs.setStringList('favoriteContacts', updated.toList());
  }

  Future<void> _saveLastChat() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('lastServerId', _serverId);
    await prefs.setString('lastRoomId', _roomId);
    if (_activeDM == null) {
      await prefs.remove('lastDirectMessageId');
    } else {
      await prefs.setString('lastDirectMessageId', _activeDM!.id);
    }
  }

  void _openDirectMessages() {
    setState(() => _showDirectMessages = true);
    if (MediaQuery.sizeOf(context).width < 720) {
      _scaffoldKey.currentState?.openDrawer();
    }
  }

  Future<void> _openServerSelection() async {
    await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        showDragHandle: true,
        backgroundColor: panel,
        builder: (sheetContext) => SafeArea(
            child: DraggableScrollableSheet(
                initialChildSize: .45,
                minChildSize: .3,
                maxChildSize: .9,
                expand: false,
                builder: (context, scrollController) => ListView(
                      controller: scrollController,
                      padding: const EdgeInsets.fromLTRB(16, 4, 16, 24),
                      children: [
                        const Padding(
                            padding: EdgeInsets.symmetric(vertical: 12),
                            child: Text('Switch server',
                                style: TextStyle(
                                    fontSize: 18,
                                    fontWeight: FontWeight.w800))),
                        ..._servers.map((server) => ListTile(
                              leading: CircleAvatar(
                                  backgroundColor: server.id == _serverId
                                      ? accent
                                      : panelLight,
                                  child:
                                      Text(server.id == 'hangout' ? 'H' : 'T')),
                              title: Text(server.name),
                              trailing: server.id == _serverId
                                  ? const Icon(Icons.check, color: accent)
                                  : null,
                              onTap: () {
                                Navigator.pop(sheetContext);
                                _selectServer(server);
                              },
                            ))
                      ],
                    ))));
  }

  Future<void> _send() async {
    final content = _input.text.trim();
    if (content.isEmpty || _sending) return;
    if (_listening) {
      await _speech.stop();
      setState(() => _listening = false);
    }
    setState(() => _sending = true);
    try {
      final response = await http
          .post(Uri.parse('$_apiBase/api/messages'),
              headers: {'Content-Type': 'application/json'},
              body: jsonEncode({
                'roomId': _conversationId,
                'author': _profileName,
                'content': content
              }))
          .timeout(const Duration(seconds: 5));
      if (response.statusCode < 300) {
        _input.clear();
        await _refresh();
      }
    } catch (_) {
      if (mounted) _snack('Can’t reach the chat server.');
    }
    if (mounted) setState(() => _sending = false);
  }

  void _snack(String text) =>
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(text)));

  void _insertEmoji(String emoji) {
    final value = _input.value;
    final selection = value.selection.isValid
        ? value.selection
        : TextSelection.collapsed(offset: value.text.length);
    final start = selection.start.clamp(0, value.text.length);
    final end = selection.end.clamp(start, value.text.length);
    final text = value.text.replaceRange(start, end, emoji);
    _input.value = TextEditingValue(
        text: text,
        selection: TextSelection.collapsed(offset: start + emoji.length));
  }

  Future<void> _openEmojiPicker() async {
    const emojis = <String>[
      '😀',
      '😃',
      '😄',
      '😁',
      '😆',
      '😅',
      '😂',
      '🤣',
      '😊',
      '😇',
      '🙂',
      '🙃',
      '😉',
      '😍',
      '🥰',
      '😘',
      '😋',
      '😎',
      '🤩',
      '🥳',
      '🤔',
      '🫡',
      '🤗',
      '🤭',
      '😴',
      '😭',
      '😤',
      '🥺',
      '😱',
      '🤯',
      '👍',
      '👎',
      '👏',
      '🙌',
      '🫶',
      '🤝',
      '🙏',
      '💪',
      '👀',
      '❤️',
      '🧡',
      '💛',
      '💚',
      '💙',
      '💜',
      '🖤',
      '💔',
      '💕',
      '🔥',
      '✨',
      '🎉',
      '🎊',
      '💯',
      '✅',
      '❌',
      '⭐',
      '🌟',
      '💡',
      '☕',
      '🍕',
      '🎮',
      '🎧',
      '🎵',
      '🚀',
      '🌈',
      '🐱',
      '🐶',
      '🌸',
      '☀️',
      '🌙',
      '⚽',
      '🏆',
    ];
    await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        showDragHandle: true,
        backgroundColor: panel,
        builder: (sheetContext) => SafeArea(
            child: SizedBox(
                height: MediaQuery.sizeOf(sheetContext).height * .55,
                child: Column(children: [
                  const Padding(
                      padding: EdgeInsets.symmetric(vertical: 8),
                      child: Text('Choose an emoji',
                          style: TextStyle(
                              fontWeight: FontWeight.w700, fontSize: 16))),
                  Expanded(
                      child: GridView.builder(
                          padding: const EdgeInsets.all(12),
                          gridDelegate:
                              SliverGridDelegateWithFixedCrossAxisCount(
                                  crossAxisCount:
                                      MediaQuery.sizeOf(sheetContext).width <
                                              400
                                          ? 6
                                          : 9),
                          itemCount: emojis.length,
                          itemBuilder: (context, index) => IconButton(
                              tooltip: 'Insert ${emojis[index]}',
                              onPressed: () {
                                _insertEmoji(emojis[index]);
                                Navigator.pop(sheetContext);
                              },
                              icon: Text(emojis[index],
                                  style: const TextStyle(fontSize: 25)))))
                ]))));
  }

  bool _isSupportedGifUrl(String value) {
    final uri = Uri.tryParse(value.trim());
    if (uri == null || !{'http', 'https'}.contains(uri.scheme)) return false;
    final host = uri.host.toLowerCase();
    final supportedHost = host == 'giphy.com' ||
        host.endsWith('.giphy.com') ||
        host == 'tenor.com' ||
        host.endsWith('.tenor.com');
    return supportedHost && uri.path.toLowerCase().endsWith('.gif');
  }

  Future<void> _openGifPicker() async {
    final searchController = TextEditingController();
    var selectedCategory = 'All';
    var searchText = '';
    final categories = [
      'All',
      ..._gifLibrary.map((gif) => gif.category).toSet()
    ];
    try {
      await showModalBottomSheet<void>(
          context: context,
          isScrollControlled: true,
          showDragHandle: true,
          backgroundColor: panel,
          builder: (sheetContext) =>
              StatefulBuilder(builder: (context, setSheetState) {
                final gifs = _gifLibrary.where((gif) {
                  final matchesCategory = selectedCategory == 'All' ||
                      gif.category == selectedCategory;
                  final matchesSearch = gif.title
                      .toLowerCase()
                      .contains(searchText.toLowerCase());
                  return matchesCategory && matchesSearch;
                }).toList();
                return SafeArea(
                    child: SizedBox(
                        height: MediaQuery.sizeOf(sheetContext).height * .84,
                        child: Column(children: [
                          const Padding(
                              padding: EdgeInsets.only(top: 4, bottom: 12),
                              child: Text('GIF Library',
                                  style: TextStyle(
                                      fontSize: 18,
                                      fontWeight: FontWeight.w800))),
                          Padding(
                              padding:
                                  const EdgeInsets.symmetric(horizontal: 16),
                              child: TextField(
                                  controller: searchController,
                                  decoration: const InputDecoration(
                                      prefixIcon: Icon(Icons.search),
                                      hintText: 'Search this library',
                                      border: OutlineInputBorder()),
                                  onChanged: (value) => setSheetState(
                                      () => searchText = value.trim()))),
                          SizedBox(
                              height: 52,
                              child: ListView(
                                  padding: const EdgeInsets.symmetric(
                                      horizontal: 12),
                                  scrollDirection: Axis.horizontal,
                                  children: categories
                                      .map((category) => Padding(
                                          padding: const EdgeInsets.symmetric(
                                              horizontal: 3, vertical: 7),
                                          child: ChoiceChip(
                                              label: Text(category),
                                              selected:
                                                  selectedCategory == category,
                                              onSelected: (_) => setSheetState(
                                                  () => selectedCategory =
                                                      category))))
                                      .toList())),
                          Expanded(
                              child: gifs.isEmpty
                                  ? const Center(
                                      child: Text('No GIFs match that search.',
                                          style: TextStyle(color: muted)))
                                  : GridView.builder(
                                      padding: const EdgeInsets.fromLTRB(
                                          12, 4, 12, 12),
                                      gridDelegate:
                                          const SliverGridDelegateWithFixedCrossAxisCount(
                                              crossAxisCount: 2,
                                              childAspectRatio: 1.35,
                                              crossAxisSpacing: 10,
                                              mainAxisSpacing: 10),
                                      itemCount: gifs.length,
                                      itemBuilder: (context, index) {
                                        final gif = gifs[index];
                                        return InkWell(
                                            onTap: () {
                                              _insertGifUrl(gif.url);
                                              Navigator.pop(sheetContext);
                                            },
                                            borderRadius:
                                                BorderRadius.circular(8),
                                            child: Card(
                                                margin: EdgeInsets.zero,
                                                color: panelLight,
                                                clipBehavior: Clip.antiAlias,
                                                child: Column(children: [
                                                  Expanded(
                                                      child: Image.network(
                                                          gif.url,
                                                          width:
                                                              double.infinity,
                                                          fit: BoxFit.cover,
                                                          errorBuilder: (_, __,
                                                                  ___) =>
                                                              const Center(
                                                                  child: Icon(
                                                                      Icons
                                                                          .broken_image_outlined,
                                                                      color:
                                                                          muted)))),
                                                  Padding(
                                                      padding: const EdgeInsets
                                                          .symmetric(
                                                          horizontal: 8,
                                                          vertical: 6),
                                                      child: Row(children: [
                                                        Expanded(
                                                            child: Text(
                                                                gif.title,
                                                                maxLines: 1,
                                                                overflow:
                                                                    TextOverflow
                                                                        .ellipsis,
                                                                style: const TextStyle(
                                                                    fontSize:
                                                                        12))),
                                                        const Icon(
                                                            Icons
                                                                .add_circle_outline,
                                                            size: 16,
                                                            color: muted)
                                                      ]))
                                                ])));
                                      })),
                          Padding(
                              padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
                              child: Row(children: [
                                const Expanded(
                                    child: Text('GIFs from GIPHY',
                                        style: TextStyle(
                                            color: muted, fontSize: 11))),
                                TextButton.icon(
                                    onPressed: () {
                                      Navigator.pop(sheetContext);
                                      _openGifUrlDialog();
                                    },
                                    icon: const Icon(Icons.link, size: 16),
                                    label: const Text('Paste a link'))
                              ]))
                        ])));
              }));
    } finally {
      searchController.dispose();
    }
  }

  void _insertGifUrl(String gifUrl) {
    final current = _input.text.trimRight();
    final insertion = current.isEmpty ? gifUrl : '$current $gifUrl';
    _input.value = TextEditingValue(
        text: insertion,
        selection: TextSelection.collapsed(offset: insertion.length));
  }

  Future<void> _openGifUrlDialog() async {
    final controller = TextEditingController();
    try {
      await showDialog<void>(
          context: context,
          builder: (dialogContext) => StatefulBuilder(
              builder: (context, setDialogState) => AlertDialog(
                    title: const Text('Add a GIF'),
                    content: SizedBox(
                        width: 420,
                        child:
                            Column(mainAxisSize: MainAxisSize.min, children: [
                          const Text(
                              'Paste a direct GIF link from Giphy or Tenor.'),
                          const SizedBox(height: 12),
                          TextField(
                              controller: controller,
                              keyboardType: TextInputType.url,
                              decoration: const InputDecoration(
                                  labelText: 'GIF URL',
                                  hintText: 'https://media.giphy.com/...gif',
                                  border: OutlineInputBorder()),
                              onChanged: (_) => setDialogState(() {})),
                          if (_isSupportedGifUrl(controller.text)) ...[
                            const SizedBox(height: 12),
                            ClipRRect(
                                borderRadius: BorderRadius.circular(8),
                                child: Image.network(controller.text.trim(),
                                    height: 150,
                                    fit: BoxFit.contain,
                                    errorBuilder: (_, __, ___) => const SizedBox(
                                        height: 80,
                                        child: Center(
                                            child: Text(
                                                'Could not load this GIF preview')))))
                          ]
                        ])),
                    actions: [
                      TextButton(
                          onPressed: () => Navigator.pop(dialogContext),
                          child: const Text('Cancel')),
                      FilledButton(
                          onPressed: _isSupportedGifUrl(controller.text)
                              ? () {
                                  final gifUrl = controller.text.trim();
                                  _insertGifUrl(gifUrl);
                                  Navigator.pop(dialogContext);
                                }
                              : null,
                          child: const Text('Insert GIF'))
                    ],
                  )));
    } finally {
      controller.dispose();
    }
  }

  Future<void> _loadColorSettings() async {
    final prefs = await SharedPreferences.getInstance();
    final modeName = prefs.getString('colorVisionMode');
    final savedApiBase = prefs.getString('apiBase');
    final savedDirectMessageId = prefs.getString('lastDirectMessageId');
    ChatContact? restoredDirectMessage;
    for (final contact in _contacts) {
      if (contact.id == savedDirectMessageId) {
        restoredDirectMessage = contact;
        break;
      }
    }
    if (!mounted) return;
    setState(() {
      _apiBase = savedApiBase ?? apiBase;
      _apiBaseController.text = _apiBase;
      _serverId = prefs.getString('lastServerId') ?? _serverId;
      _roomId = prefs.getString('lastRoomId') ?? _roomId;
      _activeDM = restoredDirectMessage;
      _showDirectMessages = restoredDirectMessage != null;
      _visionMode = ColorVisionMode.values.firstWhere(
          (mode) => mode.name == modeName,
          orElse: () => ColorVisionMode.standard);
      _filterStrength = prefs.getDouble('colorFilterStrength') ?? 1;
      _highContrast = prefs.getBool('highContrast') ?? false;
      _speakerphone = prefs.getBool('voiceSpeakerphone') ?? true;
      _frontCamera = prefs.getBool('voiceFrontCamera') ?? true;
      _videoHeight = prefs.getInt('videoHeight') ?? 720;
      _noiseSuppression = prefs.getBool('noiseSuppression') ?? true;
      _echoCancellation = prefs.getBool('echoCancellation') ?? true;
      _autoGainControl = prefs.getBool('autoGainControl') ?? true;
      _microphoneGain = prefs.getDouble('microphoneGain') ?? 1;
      _profileName = prefs.getString('profileName') ?? 'You';
      _profileStatus = prefs.getString('profileStatus') ?? 'Online';
      _profileColorValue = prefs.getInt('profileColor') ?? 0xFFB478D3;
      _favoriteContactIds =
          (prefs.getStringList('favoriteContacts') ?? <String>[]).toSet();
    });
    unawaited(_registerCurrentPushToken());
    _refresh();
  }

  Future<void> _saveColorSettings() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('colorVisionMode', _visionMode.name);
    await prefs.setDouble('colorFilterStrength', _filterStrength);
    await prefs.setBool('highContrast', _highContrast);
  }

  Future<void> _saveVoiceVideoSettings() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool('voiceSpeakerphone', _speakerphone);
    await prefs.setBool('voiceFrontCamera', _frontCamera);
    await prefs.setInt('videoHeight', _videoHeight);
    await prefs.setBool('noiseSuppression', _noiseSuppression);
    await prefs.setBool('echoCancellation', _echoCancellation);
    await prefs.setBool('autoGainControl', _autoGainControl);
    await prefs.setDouble('microphoneGain', _microphoneGain);
  }

  Future<void> _openSettings() async {
    _apiBaseController.text = _apiBase;
    final nameController = TextEditingController(text: _profileName);
    final statusController = TextEditingController(text: _profileStatus);
    var selectedColor = _profileColorValue;
    var speakerphone = _speakerphone;
    var frontCamera = _frontCamera;
    var videoHeight = _videoHeight;
    var noiseSuppression = _noiseSuppression;
    var echoCancellation = _echoCancellation;
    var autoGainControl = _autoGainControl;
    var microphoneGain = _microphoneGain;
    const colors = <int>[
      0xFFB478D3,
      0xFF7588F5,
      0xFF55B99A,
      0xFFE49A5A,
      0xFFE06C75,
      0xFF4CB6D6,
      0xFF9AAB57,
      0xFFE0C04B,
    ];
    try {
      await showDialog<void>(
          context: context,
          builder: (dialogContext) => StatefulBuilder(
              builder: (context, setDialogState) => AlertDialog(
                    title: const Text('Settings'),
                    content: SizedBox(
                        width: 520,
                        child: ConstrainedBox(
                            constraints: BoxConstraints(
                                maxHeight:
                                    MediaQuery.sizeOf(context).height * .68),
                            child: SingleChildScrollView(
                                child: Column(
                                    crossAxisAlignment:
                                        CrossAxisAlignment.start,
                                    children: [
                                  const Text('ACCOUNT',
                                      style: TextStyle(
                                          color: muted,
                                          fontWeight: FontWeight.w700,
                                          fontSize: 11,
                                          letterSpacing: .7)),
                                  const SizedBox(height: 10),
                                  TextField(
                                      controller: nameController,
                                      maxLength: 32,
                                      decoration: const InputDecoration(
                                          labelText: 'Display name',
                                          border: OutlineInputBorder())),
                                  TextField(
                                      controller: statusController,
                                      maxLength: 64,
                                      decoration: const InputDecoration(
                                          labelText: 'Status',
                                          border: OutlineInputBorder())),
                                  const SizedBox(height: 4),
                                  const Text('Profile color'),
                                  const SizedBox(height: 8),
                                  Wrap(
                                      spacing: 10,
                                      runSpacing: 10,
                                      children: colors
                                          .map((value) => InkWell(
                                                onTap: () => setDialogState(
                                                    () =>
                                                        selectedColor = value),
                                                customBorder:
                                                    const CircleBorder(),
                                                child: CircleAvatar(
                                                    radius: 17,
                                                    backgroundColor:
                                                        Color(value),
                                                    child: selectedColor ==
                                                            value
                                                        ? const Icon(
                                                            Icons.check,
                                                            size: 18,
                                                            color: Colors.white)
                                                        : null),
                                              ))
                                          .toList()),
                                  const SizedBox(height: 22),
                                  const Text('CHAT SERVER',
                                      style: TextStyle(
                                          color: muted,
                                          fontWeight: FontWeight.w700,
                                          fontSize: 11,
                                          letterSpacing: .7)),
                                  const SizedBox(height: 10),
                                  TextField(
                                      controller: _apiBaseController,
                                      keyboardType: TextInputType.url,
                                      decoration: const InputDecoration(
                                          labelText: 'Server URL',
                                          hintText: 'https://chat.example.com',
                                          border: OutlineInputBorder())),
                                  const Padding(
                                      padding: EdgeInsets.only(top: 6),
                                      child: Text(
                                          'For a local server, enter your computer’s LAN IP and port 8080.',
                                          style: TextStyle(
                                              color: muted, fontSize: 12))),
                                  const SizedBox(height: 22),
                                  const Text('VOICE',
                                      style: TextStyle(
                                          color: muted,
                                          fontWeight: FontWeight.w700,
                                          fontSize: 11,
                                          letterSpacing: .7)),
                                  SwitchListTile(
                                      contentPadding: EdgeInsets.zero,
                                      title: const Text('Use speaker'),
                                      subtitle: const Text(
                                          'Route call audio to speaker or earpiece'),
                                      value: speakerphone,
                                      onChanged: (value) => setDialogState(
                                          () => speakerphone = value)),
                                  SwitchListTile(
                                      contentPadding: EdgeInsets.zero,
                                      title: const Text('Noise suppression'),
                                      value: noiseSuppression,
                                      onChanged: (value) => setDialogState(
                                          () => noiseSuppression = value)),
                                  SwitchListTile(
                                      contentPadding: EdgeInsets.zero,
                                      title: const Text('Echo cancellation'),
                                      value: echoCancellation,
                                      onChanged: (value) => setDialogState(
                                          () => echoCancellation = value)),
                                  SwitchListTile(
                                      contentPadding: EdgeInsets.zero,
                                      title: const Text(
                                          'Automatic microphone gain'),
                                      value: autoGainControl,
                                      onChanged: (value) => setDialogState(
                                          () => autoGainControl = value)),
                                  Row(children: [
                                    const Expanded(
                                        child: Text('Microphone gain')),
                                    Text('${(microphoneGain * 100).round()}%')
                                  ]),
                                  Slider(
                                      value: microphoneGain,
                                      min: 0,
                                      max: 2,
                                      divisions: 20,
                                      label:
                                          '${(microphoneGain * 100).round()}%',
                                      onChanged: (value) => setDialogState(
                                          () => microphoneGain = value)),
                                  const Divider(),
                                  const Text('VIDEO',
                                      style: TextStyle(
                                          color: muted,
                                          fontWeight: FontWeight.w700,
                                          fontSize: 11,
                                          letterSpacing: .7)),
                                  RadioListTile<bool>(
                                      contentPadding: EdgeInsets.zero,
                                      title: const Text('Front camera'),
                                      value: true,
                                      groupValue: frontCamera,
                                      onChanged: (value) {
                                        if (value != null) {
                                          setDialogState(
                                              () => frontCamera = value);
                                        }
                                      }),
                                  RadioListTile<bool>(
                                      contentPadding: EdgeInsets.zero,
                                      title: const Text('Rear camera'),
                                      value: false,
                                      groupValue: frontCamera,
                                      onChanged: (value) {
                                        if (value != null) {
                                          setDialogState(
                                              () => frontCamera = value);
                                        }
                                      }),
                                  DropdownButtonFormField<int>(
                                      value: videoHeight,
                                      decoration: const InputDecoration(
                                          labelText: 'Video quality'),
                                      items: const [
                                        DropdownMenuItem(
                                            value: 720,
                                            child: Text('HD · 720p')),
                                        DropdownMenuItem(
                                            value: 480,
                                            child: Text('SD · 480p')),
                                        DropdownMenuItem(
                                            value: 360,
                                            child: Text('Low · 360p')),
                                      ],
                                      onChanged: (value) {
                                        if (value != null) {
                                          setDialogState(
                                              () => videoHeight = value);
                                        }
                                      }),
                                ])))),
                    actions: [
                      TextButton(
                          onPressed: () => Navigator.pop(dialogContext),
                          child: const Text('Cancel')),
                      FilledButton(
                          onPressed: () async {
                            final name = nameController.text.trim();
                            final entered = _apiBaseController.text.trim();
                            final uri = Uri.tryParse(entered);
                            if (name.isEmpty) {
                              _snack('Enter a display name.');
                              return;
                            }
                            if (uri == null ||
                                !uri.hasAuthority ||
                                !{'http', 'https'}.contains(uri.scheme)) {
                              _snack('Enter a valid http or https server URL.');
                              return;
                            }
                            final normalized =
                                entered.replaceFirst(RegExp(r'/+$'), '');
                            final status = statusController.text.trim();
                            final prefs = await SharedPreferences.getInstance();
                            await prefs.setString('profileName', name);
                            await prefs.setString('profileStatus', status);
                            await prefs.setInt('profileColor', selectedColor);
                            await prefs.setString('apiBase', normalized);
                            if (!mounted) return;
                            setState(() {
                              _profileName = name;
                              _profileStatus = status;
                              _profileColorValue = selectedColor;
                              _apiBase = normalized;
                              _speakerphone = speakerphone;
                              _frontCamera = frontCamera;
                              _videoHeight = videoHeight;
                              _noiseSuppression = noiseSuppression;
                              _echoCancellation = echoCancellation;
                              _autoGainControl = autoGainControl;
                              _microphoneGain = microphoneGain;
                            });
                            await _saveVoiceVideoSettings();
                            unawaited(_registerCurrentPushToken());
                            if (dialogContext.mounted) {
                              Navigator.pop(dialogContext);
                            }
                            _refresh();
                          },
                          child: const Text('Save changes'))
                    ],
                  )));
    } finally {
      nameController.dispose();
      statusController.dispose();
    }
  }

  Future<void> _openColorControls() async {
    await showDialog<void>(
        context: context,
        builder: (dialogContext) => StatefulBuilder(
            builder: (context, setDialogState) => AlertDialog(
                  title: const Text('Color vision controls'),
                  content: SizedBox(
                      width: 390,
                      child: SingleChildScrollView(
                          child:
                              Column(mainAxisSize: MainAxisSize.min, children: [
                        const Text(
                            'Choose a color filter that works best for you. These are approximations; adjust strength to suit your display.',
                            style: TextStyle(color: muted)),
                        const SizedBox(height: 8),
                        ...ColorVisionMode.values
                            .map((mode) => RadioListTile<ColorVisionMode>(
                                dense: true,
                                contentPadding: EdgeInsets.zero,
                                title: Text(mode.label),
                                value: mode,
                                groupValue: _visionMode,
                                onChanged: (value) {
                                  if (value == null) return;
                                  setState(() => _visionMode = value);
                                  setDialogState(() {});
                                  _saveColorSettings();
                                })),
                        Row(children: [
                          const Expanded(child: Text('Filter strength')),
                          Text('${(_filterStrength * 100).round()}%')
                        ]),
                        Slider(
                            value: _filterStrength,
                            min: 0,
                            max: 1,
                            divisions: 20,
                            label: '${(_filterStrength * 100).round()}%',
                            onChanged: (value) {
                              setState(() => _filterStrength = value);
                              setDialogState(() {});
                              _saveColorSettings();
                            }),
                        SwitchListTile(
                            contentPadding: EdgeInsets.zero,
                            title: const Text('Higher contrast'),
                            subtitle: const Text(
                                'Increase separation between text and backgrounds'),
                            value: _highContrast,
                            onChanged: (value) {
                              setState(() => _highContrast = value);
                              setDialogState(() {});
                              _saveColorSettings();
                            }),
                      ]))),
                  actions: [
                    TextButton(
                        onPressed: () {
                          setState(() {
                            _visionMode = ColorVisionMode.standard;
                            _filterStrength = 1;
                            _highContrast = false;
                          });
                          setDialogState(() {});
                          _saveColorSettings();
                        },
                        child: const Text('Reset')),
                    TextButton(
                        onPressed: () => Navigator.pop(dialogContext),
                        child: const Text('Done'))
                  ],
                )));
  }

  List<double> _visionMatrix() {
    const identity = <double>[
      1,
      0,
      0,
      0,
      0,
      0,
      1,
      0,
      0,
      0,
      0,
      0,
      1,
      0,
      0,
      0,
      0,
      0,
      1,
      0
    ];
    final target = switch (_visionMode) {
      ColorVisionMode.standard => identity,
      ColorVisionMode.protanopia => <double>[
          .567,
          .433,
          0,
          0,
          0,
          .558,
          .442,
          0,
          0,
          0,
          0,
          .242,
          .758,
          0,
          0,
          0,
          0,
          0,
          1,
          0
        ],
      ColorVisionMode.deuteranopia => <double>[
          .625,
          .375,
          0,
          0,
          0,
          .7,
          .3,
          0,
          0,
          0,
          0,
          .3,
          .7,
          0,
          0,
          0,
          0,
          0,
          1,
          0
        ],
      ColorVisionMode.tritanopia => <double>[
          .95,
          .05,
          0,
          0,
          0,
          0,
          .433,
          .567,
          0,
          0,
          0,
          .475,
          .525,
          0,
          0,
          0,
          0,
          0,
          1,
          0
        ],
      ColorVisionMode.monochrome => <double>[
          .2126,
          .7152,
          .0722,
          0,
          0,
          .2126,
          .7152,
          .0722,
          0,
          0,
          .2126,
          .7152,
          .0722,
          0,
          0,
          0,
          0,
          0,
          1,
          0
        ],
    };
    return List<double>.generate(
        20,
        (index) =>
            identity[index] +
            (target[index] - identity[index]) * _filterStrength);
  }

  Widget _applyColorSettings(Widget child) {
    final filtered = ColorFiltered(
        colorFilter: ColorFilter.matrix(_visionMatrix()), child: child);
    return _highContrast
        ? ColorFiltered(
            colorFilter: const ColorFilter.matrix(<double>[
              1.25,
              0,
              0,
              0,
              -.125,
              0,
              1.25,
              0,
              0,
              -.125,
              0,
              0,
              1.25,
              0,
              -.125,
              0,
              0,
              0,
              1,
              0
            ]),
            child: filtered)
        : filtered;
  }

  Future<void> _startCall({required bool video, String? roomId}) async {
    final session = CallSession(
        apiBase: _apiBase,
        roomId: roomId ?? _conversationId,
        video: video,
        speakerphone: _speakerphone,
        frontCamera: _frontCamera,
        videoHeight: _videoHeight,
        noiseSuppression: _noiseSuppression,
        echoCancellation: _echoCancellation,
        autoGainControl: _autoGainControl,
        microphoneGain: _microphoneGain);
    await Navigator.of(context).push(MaterialPageRoute<void>(
        builder: (_) => CallScreen(
            session: session,
            colorMatrix: _visionMatrix(),
            highContrast: _highContrast)));
  }

  @override
  Widget build(BuildContext context) {
    final phone = MediaQuery.sizeOf(context).width < 720;
    return Scaffold(
      key: _scaffoldKey,
      drawer: phone
          ? Drawer(child: SizedBox(width: 280, child: _channelPanel()))
          : null,
      body: _applyColorSettings(SafeArea(
          child: Row(children: [
        if (!phone) _serverRail(),
        if (!phone) SizedBox(width: 232, child: _channelPanel()),
        Expanded(child: _chatPanel(phone: phone)),
        if (!phone && MediaQuery.sizeOf(context).width > 1050)
          SizedBox(width: 220, child: _membersPanel()),
      ]))),
    );
  }

  Widget _serverRail() => Container(
      width: 72,
      color: ink,
      padding: const EdgeInsets.symmetric(vertical: 14),
      child: Column(children: [
        InkWell(
          onTap: _openDirectMessages,
          borderRadius: BorderRadius.circular(14),
          child: Container(
              width: 46,
              height: 46,
              decoration: BoxDecoration(
                  color: _showDirectMessages ? accent : panelLight,
                  borderRadius: BorderRadius.circular(14)),
              child: const Icon(Icons.forum_outlined, color: Colors.white)),
        ),
        const SizedBox(height: 12),
        _serverIcon('H',
            selected: _serverId == 'hangout', onTap: _openServerSelection),
        const SizedBox(height: 12),
        _divider(),
        const SizedBox(height: 12),
        _serverIcon('T',
            selected: _serverId == 'test', onTap: _openServerSelection),
        const SizedBox(height: 10),
        _serverIcon('+', selected: false, plus: true),
        const Spacer(),
        CircleAvatar(
            radius: 18,
            backgroundColor: const Color(0xFFB478D3),
            child:
                const Text('Y', style: TextStyle(fontWeight: FontWeight.bold))),
        const SizedBox(height: 12),
      ]));

  Widget _serverIcon(String label,
          {required bool selected, bool plus = false, VoidCallback? onTap}) =>
      InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(selected ? 16 : 22),
          child: Container(
              width: 46,
              height: 46,
              decoration: BoxDecoration(
                  color: selected ? accent : (plus ? panel : panelLight),
                  borderRadius: BorderRadius.circular(selected ? 16 : 22)),
              alignment: Alignment.center,
              child: Text(label,
                  style: TextStyle(
                      color: plus ? const Color(0xFF5AC28C) : Colors.white,
                      fontWeight: FontWeight.w700,
                      fontSize: 17))));
  Widget _divider() => Container(
      width: 30,
      height: 2,
      decoration: BoxDecoration(
          color: panelLight, borderRadius: BorderRadius.circular(2)));

  Widget _channelPanel() => Container(
      color: panel,
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        SizedBox(
            height: 58,
            child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 18),
                child: Row(children: [
                  Expanded(
                      child: Text(
                          _showDirectMessages
                              ? 'Direct Messages'
                              : _server.name,
                          style: const TextStyle(
                              fontWeight: FontWeight.w800, fontSize: 16))),
                  if (_showDirectMessages)
                    IconButton(
                        tooltip: 'Back to server channels',
                        onPressed: () =>
                            setState(() => _showDirectMessages = false),
                        icon: const Icon(Icons.dns_outlined, color: muted))
                  else ...[
                    IconButton(
                        tooltip: 'Direct messages',
                        onPressed: _openDirectMessages,
                        icon: const Icon(Icons.forum_outlined,
                            color: muted, size: 19)),
                    IconButton(
                        tooltip: 'Switch server',
                        onPressed: _openServerSelection,
                        icon: Icon(Icons.keyboard_arrow_down, color: muted))
                  ]
                ]))),
        const Divider(height: 1, color: ink),
        if (_showDirectMessages)
          Expanded(
              child:
                  ListView(padding: const EdgeInsets.only(top: 8), children: [
            _channelCategory('FAVORITES'),
            if (_contacts
                .where((contact) => _favoriteContactIds.contains(contact.id))
                .isEmpty)
              const Padding(
                  padding: EdgeInsets.fromLTRB(18, 4, 12, 12),
                  child: Text('Star a friend to add them here.',
                      style: TextStyle(color: muted, fontSize: 11))),
            ..._contacts
                .where((contact) => _favoriteContactIds.contains(contact.id))
                .map(_dmRow),
            _channelCategory('FRIENDS'),
            ..._contacts.map(_dmRow)
          ]))
        else ...[
          Padding(
              padding: const EdgeInsets.fromLTRB(16, 23, 12, 9),
              child: Row(children: [
                const Expanded(
                    child: Text('TEXT CHANNELS',
                        style: TextStyle(
                            color: muted,
                            fontWeight: FontWeight.w700,
                            fontSize: 11,
                            letterSpacing: .7))),
                Icon(Icons.add, color: muted, size: 18)
              ])),
          ..._rooms.map(_channelRow),
          _channelCategory('VOICE CHANNELS'),
          _voiceChannelRow(
              name: 'Lounge',
              icon: Icons.volume_up,
              onTap: () => _joinServerCall(
                  video: false,
                  roomId: '${_serverId == 'test' ? 'test-' : ''}voice-lounge')),
          _channelCategory('VIDEO CHANNELS'),
          _voiceChannelRow(
              name: 'Video Hangout',
              icon: Icons.videocam,
              onTap: () => _joinServerCall(
                  video: true,
                  roomId:
                      '${_serverId == 'test' ? 'test-' : ''}video-hangout')),
          const Spacer(),
        ],
        InkWell(
            onTap: _openSettings,
            borderRadius: BorderRadius.circular(8),
            child: Container(
                color: const Color(0xFF15171D),
                padding: const EdgeInsets.all(12),
                child: Row(children: [
                  CircleAvatar(
                      radius: 17,
                      backgroundColor: _profileColor,
                      child: Text(_profileInitials)),
                  const SizedBox(width: 9),
                  Expanded(
                      child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                        Text(_profileName,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                                fontWeight: FontWeight.w700, fontSize: 13)),
                        Text(_profileStatus,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(color: muted, fontSize: 11))
                      ])),
                  Icon(Icons.mic, color: muted, size: 19),
                  const SizedBox(width: 12),
                  Icon(Icons.headphones, color: muted, size: 19)
                ]))),
      ]));

  Widget _channelCategory(String title) => Padding(
      padding: const EdgeInsets.fromLTRB(16, 20, 12, 8),
      child: Text(title,
          style: const TextStyle(
              color: muted,
              fontWeight: FontWeight.w700,
              fontSize: 11,
              letterSpacing: .7)));

  Widget _voiceChannelRow(
          {required String name,
          required IconData icon,
          required VoidCallback onTap}) =>
      Padding(
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
          child: InkWell(
              onTap: onTap,
              borderRadius: BorderRadius.circular(5),
              child: Container(
                  height: 38,
                  padding: const EdgeInsets.symmetric(horizontal: 9),
                  child: Row(children: [
                    Icon(icon, size: 19, color: muted),
                    const SizedBox(width: 9),
                    Expanded(
                        child: Text(name,
                            style: const TextStyle(
                                color: muted, fontWeight: FontWeight.w500))),
                    Icon(Icons.arrow_forward_ios, size: 12, color: muted)
                  ]))));

  Widget _dmRow(ChatContact contact) {
    final selected = _activeDM?.id == contact.id;
    return Padding(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
        child: InkWell(
            onTap: () => _startDirectMessage(contact),
            borderRadius: BorderRadius.circular(5),
            child: Container(
                height: 38,
                padding: const EdgeInsets.symmetric(horizontal: 9),
                decoration: BoxDecoration(
                    color:
                        selected ? const Color(0xFF30333D) : Colors.transparent,
                    borderRadius: BorderRadius.circular(5)),
                child: Row(children: [
                  CircleAvatar(
                      radius: 12,
                      backgroundColor: contact.color,
                      child: Text(contact.name[0],
                          style: const TextStyle(
                              color: Colors.white, fontSize: 11))),
                  const SizedBox(width: 9),
                  Expanded(
                      child: Text(contact.name,
                          style: TextStyle(
                              color: selected ? Colors.white : muted,
                              fontWeight: selected
                                  ? FontWeight.w600
                                  : FontWeight.w500))),
                  if (selected)
                    const Icon(Icons.chat_bubble_outline,
                        size: 15, color: muted),
                  IconButton(
                      tooltip: _favoriteContactIds.contains(contact.id)
                          ? 'Remove from favorites'
                          : 'Add to favorites',
                      visualDensity: VisualDensity.compact,
                      constraints:
                          const BoxConstraints.tightFor(width: 34, height: 34),
                      onPressed: () => _toggleFavorite(contact),
                      icon: Icon(
                          _favoriteContactIds.contains(contact.id)
                              ? Icons.star
                              : Icons.star_border,
                          size: 17,
                          color: _favoriteContactIds.contains(contact.id)
                              ? const Color(0xFFF0C75E)
                              : muted))
                ]))));
  }

  void _joinServerCall({required bool video, required String roomId}) {
    if (MediaQuery.sizeOf(context).width < 720) {
      _scaffoldKey.currentState?.closeDrawer();
    }
    _startCall(video: video, roomId: roomId);
  }

  Widget _channelRow(ChatRoom room) {
    final selected = _roomId == room.id;
    return Padding(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
        child: InkWell(
            onTap: () {
              setState(() {
                _roomId = room.id;
                _activeDM = null;
              });
              unawaited(_saveLastChat());
              _refresh();
              if (MediaQuery.sizeOf(context).width < 720) {
                _scaffoldKey.currentState?.closeDrawer();
              }
            },
            borderRadius: BorderRadius.circular(5),
            child: Container(
                height: 38,
                padding: const EdgeInsets.symmetric(horizontal: 9),
                decoration: BoxDecoration(
                    color:
                        selected ? const Color(0xFF30333D) : Colors.transparent,
                    borderRadius: BorderRadius.circular(5)),
                child: Row(children: [
                  Text('#',
                      style: TextStyle(
                          fontSize: 21,
                          color: selected ? Colors.white : muted)),
                  const SizedBox(width: 9),
                  Expanded(
                      child: Text(room.name,
                          style: TextStyle(
                              color: selected ? Colors.white : muted,
                              fontWeight: selected
                                  ? FontWeight.w600
                                  : FontWeight.w500))),
                  if (selected)
                    Icon(Icons.person_add_alt_1, size: 16, color: muted)
                ]))));
  }

  Widget _chatPanel({required bool phone}) => Column(children: [
        _chatHeader(phone: phone),
        Expanded(
            child: _messages.isEmpty
                ? _emptyChat()
                : ListView(
                    padding: EdgeInsets.fromLTRB(
                        phone ? 14 : 22, 20, phone ? 14 : 22, 20),
                    children: [
                        SizedBox(height: phone ? 34 : 100),
                        _welcomeHeader(),
                        const SizedBox(height: 22),
                        ..._messages.map(_messageTile)
                      ])),
        Padding(
            padding: EdgeInsets.fromLTRB(phone ? 10 : 20, 8, phone ? 10 : 20,
                MediaQuery.paddingOf(context).bottom + 10),
            child: Container(
                decoration: BoxDecoration(
                    color: panelLight, borderRadius: BorderRadius.circular(9)),
                padding: EdgeInsets.symmetric(
                    horizontal: phone ? 8 : 14, vertical: 5),
                child: Row(children: [
                  if (!phone) ...[
                    Icon(Icons.add_circle, color: muted),
                    const SizedBox(width: 12),
                  ],
                  Expanded(
                      child: TextField(
                          controller: _input,
                          minLines: 1,
                          maxLines: 5,
                          textInputAction: TextInputAction.send,
                          onSubmitted: (_) => _send(),
                          decoration: InputDecoration(
                              hintText: _listening
                                  ? 'Listening… speak your message'
                                  : _activeDM == null
                                      ? 'Message #${_room.name}'
                                      : 'Message ${_activeDM!.name}',
                              hintStyle: const TextStyle(color: muted),
                              border: InputBorder.none))),
                  IconButton(
                      tooltip: 'Add a GIF',
                      visualDensity: VisualDensity.compact,
                      onPressed: _openGifPicker,
                      icon: const Icon(Icons.gif_box_outlined, color: muted)),
                  IconButton(
                      tooltip: 'Add emoji',
                      visualDensity: VisualDensity.compact,
                      onPressed: _openEmojiPicker,
                      icon: const Icon(Icons.emoji_emotions_outlined,
                          color: muted)),
                  IconButton(
                      tooltip: _listening ? 'Stop voice typing' : 'Voice type',
                      onPressed: _toggleDictation,
                      icon: Icon(_listening ? Icons.mic : Icons.mic_none,
                          color: _listening ? Colors.redAccent : muted)),
                  IconButton(
                      onPressed: _sending ? null : _send,
                      icon: Icon(Icons.send_rounded, color: accent))
                ]))),
      ]);

  Widget _chatHeader({required bool phone}) {
    if (phone) {
      return Container(
          height: 56,
          padding: const EdgeInsets.symmetric(horizontal: 4),
          decoration: const BoxDecoration(
              color: panel, border: Border(bottom: BorderSide(color: ink))),
          child: Row(children: [
            IconButton(
                tooltip: 'Open channels',
                onPressed: () => _scaffoldKey.currentState?.openDrawer(),
                icon: const Icon(Icons.menu, color: muted)),
            Expanded(
                child: Text(
                    _activeDM == null
                        ? '# ${_room.name}'
                        : '@ ${_activeDM!.name}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(fontWeight: FontWeight.w700))),
            IconButton(
                tooltip: 'Settings',
                onPressed: _openSettings,
                icon: const Icon(Icons.settings_outlined,
                    color: muted, size: 20)),
            IconButton(
                tooltip: 'Color vision controls',
                onPressed: _openColorControls,
                icon: const Icon(Icons.remove_red_eye_outlined,
                    color: muted, size: 20)),
            IconButton(
                tooltip: 'Start voice call',
                onPressed: () => _startCall(video: false),
                icon: const Icon(Icons.call, color: muted, size: 20)),
            IconButton(
                tooltip: 'Start video call',
                onPressed: () => _startCall(video: true),
                icon: const Icon(Icons.videocam, color: muted, size: 21)),
          ]));
    }
    return Container(
        height: 58,
        padding: const EdgeInsets.symmetric(horizontal: 20),
        decoration: const BoxDecoration(
            color: panel, border: Border(bottom: BorderSide(color: ink))),
        child: Row(children: [
          Text(_activeDM == null ? '#' : '@',
              style: const TextStyle(color: muted, fontSize: 22)),
          const SizedBox(width: 10),
          Text(_activeDM?.name ?? _room.name,
              style: const TextStyle(fontWeight: FontWeight.w700)),
          const SizedBox(width: 16),
          Container(width: 1, height: 22, color: panelLight),
          const SizedBox(width: 16),
          const Expanded(
              child: Text('A place for good conversations.',
                  style: TextStyle(color: muted, fontSize: 12))),
          Icon(_online ? Icons.wifi : Icons.wifi_off,
              size: 16, color: _online ? const Color(0xFF58C58D) : muted),
          const SizedBox(width: 7),
          Text(_online ? 'Connected' : 'Offline',
              style: const TextStyle(color: muted, fontSize: 11)),
          IconButton(
              tooltip: 'Settings',
              onPressed: _openSettings,
              icon:
                  const Icon(Icons.settings_outlined, color: muted, size: 20)),
          IconButton(
              tooltip: 'Color vision controls',
              onPressed: _openColorControls,
              icon: const Icon(Icons.remove_red_eye_outlined,
                  color: muted, size: 20)),
          IconButton(
              tooltip: 'Start voice call',
              onPressed: () => _startCall(video: false),
              icon: const Icon(Icons.call, color: muted, size: 20)),
          IconButton(
              tooltip: 'Start video call',
              onPressed: () => _startCall(video: true),
              icon: const Icon(Icons.videocam, color: muted, size: 21)),
          Icon(Icons.search, color: muted)
        ]));
  }

  Widget _welcomeHeader() {
    final directMessage = _activeDM;
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Container(
          width: 68,
          height: 68,
          decoration: BoxDecoration(
              color: directMessage?.color ?? panelLight,
              borderRadius: BorderRadius.circular(34)),
          alignment: Alignment.center,
          child: Text(directMessage?.name[0] ?? '#',
              style:
                  const TextStyle(fontSize: 42, fontWeight: FontWeight.w800))),
      const SizedBox(height: 14),
      Text(
          directMessage == null
              ? 'Welcome to #${_room.name}!'
              : 'Message ${directMessage.name}',
          style: const TextStyle(fontSize: 27, fontWeight: FontWeight.w800)),
      const SizedBox(height: 5),
      Text(
          directMessage == null
              ? 'This is the beginning of this channel.'
              : 'This is your direct conversation with ${directMessage.name}.',
          style: const TextStyle(color: muted))
    ]);
  }

  Widget _emptyChat() => Center(
          child: Column(mainAxisSize: MainAxisSize.min, children: [
        _welcomeHeader(),
        const SizedBox(height: 20),
        Text(
            _online
                ? 'Start the conversation.'
                : 'Start the Go server to connect.',
            style: const TextStyle(color: muted))
      ]));

  Widget _messageTile(ChatMessage message) {
    final initials =
        message.author.isEmpty ? '?' : message.author[0].toUpperCase();
    const authorPalette = <Color>[
      Color(0xFF56B4E9), // sky blue
      Color(0xFFE69F00), // orange
      Color(0xFF009E73), // bluish green
      Color(0xFFCC79A7), // reddish purple
      Color(0xFFD55E00), // vermillion
      Color(0xFF7BADE2), // light blue
      Color(0xFFF0E442), // yellow
    ];
    final color = message.author == _profileName
        ? _profileColor
        : authorPalette[message.author.hashCode.abs() % authorPalette.length];
    final localTime = message.createdAt.toLocal();
    final time =
        '${localTime.hour % 12 == 0 ? 12 : localTime.hour % 12}:${localTime.minute.toString().padLeft(2, '0')} ${localTime.hour >= 12 ? 'PM' : 'AM'}';
    return Padding(
        padding: const EdgeInsets.only(bottom: 20),
        child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
          CircleAvatar(
              radius: 19,
              backgroundColor: color,
              child: Text(initials,
                  style: const TextStyle(fontWeight: FontWeight.bold))),
          const SizedBox(width: 13),
          Expanded(
              child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                Row(children: [
                  Text(message.author,
                      style:
                          TextStyle(color: color, fontWeight: FontWeight.w700)),
                  const SizedBox(width: 8),
                  Text(time,
                      style: const TextStyle(color: muted, fontSize: 10)),
                  const Spacer(),
                  IconButton(
                      tooltip: _speakingMessageId == message.id
                          ? 'Stop reading'
                          : 'Read message aloud',
                      visualDensity: VisualDensity.compact,
                      onPressed: () => _speakMessage(message),
                      icon: Icon(
                          _speakingMessageId == message.id
                              ? Icons.volume_up
                              : Icons.volume_up_outlined,
                          size: 17,
                          color: _speakingMessageId == message.id
                              ? accent
                              : muted))
                ]),
                const SizedBox(height: 1),
                if (_isSupportedGifUrl(message.content))
                  ClipRRect(
                      borderRadius: BorderRadius.circular(8),
                      child: Image.network(message.content,
                          width: 300,
                          height: 190,
                          fit: BoxFit.contain,
                          alignment: Alignment.centerLeft,
                          errorBuilder: (_, __, ___) => Text(message.content,
                              style: const TextStyle(color: muted))))
                else
                  Text(message.content,
                      style: const TextStyle(
                          height: 1.35, color: Color(0xFFD7D9DF)))
              ]))
        ]));
  }

  Widget _membersPanel() => Container(
      color: panel,
      padding: const EdgeInsets.fromLTRB(16, 22, 12, 12),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        const Text('ONLINE — 3',
            style: TextStyle(
                color: muted,
                fontSize: 11,
                fontWeight: FontWeight.w700,
                letterSpacing: .6)),
        const SizedBox(height: 14),
        _member('Maya Chen', 'Building something cool', const Color(0xFFB478D3),
            onTap: () => _startDirectMessage(_contacts[0])),
        _member('Leo Park', 'Listening to music', const Color(0xFF55B99A),
            onTap: () => _startDirectMessage(_contacts[1])),
        _member(_profileName, _profileStatus, _profileColor),
        const SizedBox(height: 24),
        const Text('OFFLINE — 1',
            style: TextStyle(
                color: muted,
                fontSize: 11,
                fontWeight: FontWeight.w700,
                letterSpacing: .6)),
        const SizedBox(height: 14),
        _member('Sam Rivera', 'Away', const Color(0xFFE49A5A),
            online: false, onTap: () => _startDirectMessage(_contacts[2]))
      ]));
  Widget _member(String name, String status, Color color,
          {bool online = true, VoidCallback? onTap}) =>
      InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(6),
          child: Padding(
              padding: const EdgeInsets.only(bottom: 13),
              child: Row(children: [
                Stack(children: [
                  CircleAvatar(
                      radius: 17, backgroundColor: color, child: Text(name[0])),
                  Positioned(
                      right: 0,
                      bottom: 0,
                      child: Container(
                          width: 10,
                          height: 10,
                          decoration: BoxDecoration(
                              color: online ? const Color(0xFF58C58D) : muted,
                              border: const Border.fromBorderSide(
                                  BorderSide(color: panel, width: 2)),
                              borderRadius: BorderRadius.circular(9))))
                ]),
                const SizedBox(width: 9),
                Expanded(
                    child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                      Text(name,
                          style: const TextStyle(
                              fontWeight: FontWeight.w600, fontSize: 12)),
                      Text(status,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(color: muted, fontSize: 10))
                    ]))
              ])));
}
