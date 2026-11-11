import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';
import 'package:http/http.dart' as http;

class CallSession extends ChangeNotifier {
  CallSession(
      {required this.apiBase,
      required this.roomId,
      required this.video,
      this.speakerphone = true,
      this.frontCamera = true,
      this.videoHeight = 720,
      this.noiseSuppression = true,
      this.echoCancellation = true,
      this.autoGainControl = true,
      this.microphoneGain = 1});

  final String apiBase;
  final String roomId;
  final bool video;
  final bool speakerphone;
  final bool frontCamera;
  final int videoHeight;
  final bool noiseSuppression;
  final bool echoCancellation;
  final bool autoGainControl;
  final double microphoneGain;
  final String peerId =
      '${DateTime.now().microsecondsSinceEpoch}-${Random().nextInt(1 << 32)}';
  final localRenderer = RTCVideoRenderer();
  final remoteRenderer = RTCVideoRenderer();
  RTCPeerConnection? _connection;
  MediaStream? _localStream;
  Timer? _poller;
  Timer? _durationTimer;
  int _cursor = 0;
  bool _polling = false;
  bool _started = false;
  bool _closed = false;
  bool _muted = false;
  bool _cameraOff = false;
  String? remotePeer;
  String status = 'Starting call…';
  Duration duration = Duration.zero;
  final List<RTCIceCandidate> _earlyCandidates = [];

  bool get muted => _muted;
  bool get cameraOff => _cameraOff;

  Future<void> start() async {
    await localRenderer.initialize();
    await remoteRenderer.initialize();
    final constraints = <String, dynamic>{
      'audio': {
        'noiseSuppression': noiseSuppression,
        'echoCancellation': echoCancellation,
        'autoGainControl': autoGainControl,
      },
      'video': video
          ? <String, dynamic>{
              'facingMode': frontCamera ? 'user' : 'environment',
              'width': videoHeight * 16 ~/ 9,
              'height': videoHeight
            }
          : false,
    };
    try {
      await Helper.setSpeakerphoneOn(speakerphone);
    } catch (_) {
      // Speaker routing is only supported on some platforms.
    }
    _localStream = await navigator.mediaDevices.getUserMedia(constraints);
    for (final track in _localStream!.getAudioTracks()) {
      try {
        await Helper.setVolume(microphoneGain, track);
      } catch (_) {
        // Some platforms do not expose microphone track gain controls.
      }
    }
    localRenderer.srcObject = _localStream;
    _connection = await createPeerConnection({
      'iceServers': [
        {'urls': 'stun:stun.l.google.com:19302'},
      ],
    });
    _connection!.onTrack = (event) {
      if (event.streams.isNotEmpty) {
        remoteRenderer.srcObject = event.streams.first;
        status = 'Connected';
        notifyListeners();
      }
    };
    _connection!.onIceCandidate = (candidate) {
      if (candidate.candidate != null) {
        _send('candidate', to: remotePeer, candidate: candidate);
      }
    };
    for (final track in _localStream!.getTracks()) {
      await _connection!.addTrack(track, _localStream!);
    }
    _started = true;
    await _send('join');
    _poller = Timer.periodic(const Duration(milliseconds: 800), (_) => _poll());
    _durationTimer = Timer.periodic(const Duration(seconds: 1), (_) {
      duration += const Duration(seconds: 1);
      notifyListeners();
    });
  }

  Future<void> _send(String type,
      {String? to,
      RTCSessionDescription? description,
      RTCIceCandidate? candidate}) async {
    final body = <String, dynamic>{
      'roomId': roomId,
      'from': peerId,
      'type': type
    };
    if (to != null) body['to'] = to;
    if (description != null) body['sdp'] = description.sdp;
    if (candidate != null) {
      body['candidate'] = candidate.candidate;
      body['sdpMid'] = candidate.sdpMid;
      body['sdpMLineIndex'] = candidate.sdpMLineIndex;
    }
    await http
        .post(Uri.parse('$apiBase/api/calls/signals'),
            headers: {'Content-Type': 'application/json'},
            body: jsonEncode(body))
        .timeout(const Duration(seconds: 5));
  }

  Future<void> _poll() async {
    if (_polling || _closed) return;
    _polling = true;
    try {
      final uri = Uri.parse('$apiBase/api/calls/signals').replace(
          queryParameters: {
            'room': roomId,
            'peer': peerId,
            'after': '$_cursor'
          });
      final response = await http.get(uri).timeout(const Duration(seconds: 4));
      if (response.statusCode != 200) return;
      final events = jsonDecode(response.body) as List<dynamic>;
      for (final raw in events) {
        final event = raw as Map<String, dynamic>;
        _cursor = max(_cursor, event['id'] as int);
        await _handle(event);
      }
    } catch (_) {
      status = 'Reconnecting…';
      notifyListeners();
    } finally {
      _polling = false;
    }
  }

  Future<void> _handle(Map<String, dynamic> event) async {
    final type = event['type'] as String;
    final from = event['from'] as String;
    if (type == 'join') {
      remotePeer = from;
      status = 'Calling…';
      notifyListeners();
      if (peerId.compareTo(from) < 0) await _makeOffer(from);
    } else if (type == 'offer') {
      remotePeer = from;
      await _connection!.setRemoteDescription(
          RTCSessionDescription(event['sdp'] as String?, 'offer'));
      for (final candidate in _earlyCandidates) {
        await _connection!.addCandidate(candidate);
      }
      _earlyCandidates.clear();
      final answer = await _connection!.createAnswer();
      await _connection!.setLocalDescription(answer);
      await _send('answer', to: from, description: answer);
      status = 'Connecting…';
      notifyListeners();
    } else if (type == 'answer') {
      await _connection!.setRemoteDescription(
          RTCSessionDescription(event['sdp'] as String?, 'answer'));
      for (final candidate in _earlyCandidates) {
        await _connection!.addCandidate(candidate);
      }
      _earlyCandidates.clear();
      status = 'Connecting…';
      notifyListeners();
    } else if (type == 'candidate') {
      final candidate = RTCIceCandidate(event['candidate'] as String?,
          event['sdpMid'] as String?, event['sdpMLineIndex'] as int?);
      if (await _connection!.getRemoteDescription() == null) {
        _earlyCandidates.add(candidate);
      } else {
        await _connection!.addCandidate(candidate);
      }
    } else if (type == 'leave') {
      status = 'The other person left';
      notifyListeners();
    }
  }

  Future<void> _makeOffer(String to) async {
    final offer = await _connection!.createOffer();
    await _connection!.setLocalDescription(offer);
    await _send('offer', to: to, description: offer);
  }

  void toggleMute() {
    _muted = !_muted;
    for (final track
        in _localStream?.getAudioTracks() ?? <MediaStreamTrack>[]) {
      track.enabled = !_muted;
    }
    notifyListeners();
  }

  void toggleCamera() {
    _cameraOff = !_cameraOff;
    for (final track
        in _localStream?.getVideoTracks() ?? <MediaStreamTrack>[]) {
      track.enabled = !_cameraOff;
    }
    notifyListeners();
  }

  Future<void> switchCamera() async {
    final tracks = _localStream?.getVideoTracks() ?? <MediaStreamTrack>[];
    if (tracks.isNotEmpty) await Helper.switchCamera(tracks.first);
  }

  String get durationLabel {
    final minutes = duration.inMinutes.toString().padLeft(2, '0');
    final seconds = (duration.inSeconds % 60).toString().padLeft(2, '0');
    return '$minutes:$seconds';
  }

  Future<void> end() async {
    if (_closed) return;
    _closed = true;
    _poller?.cancel();
    _durationTimer?.cancel();
    if (_started) {
      try {
        await _send('leave', to: remotePeer);
      } catch (_) {}
    }
    await _connection?.close();
    await _localStream?.dispose();
    await localRenderer.dispose();
    await remoteRenderer.dispose();
  }

  @override
  void dispose() {
    unawaited(end());
    super.dispose();
  }
}
