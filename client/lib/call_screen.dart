import 'package:flutter/material.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';

import 'call_session.dart';

class CallScreen extends StatefulWidget {
  const CallScreen(
      {super.key,
      required this.session,
      required this.colorMatrix,
      required this.highContrast});
  final CallSession session;
  final List<double> colorMatrix;
  final bool highContrast;

  @override
  State<CallScreen> createState() => _CallScreenState();
}

class _CallScreenState extends State<CallScreen> {
  @override
  void initState() {
    super.initState();
    widget.session.start().catchError((Object error) {
      widget.session.status = 'Could not start call: $error';
      widget.session.notifyListeners();
    });
  }

  @override
  void dispose() {
    widget.session.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        backgroundColor: const Color(0xFF101116),
        body: _applyColorSettings(
          SafeArea(
            child: AnimatedBuilder(
              animation: widget.session,
              builder: (context, _) => Stack(
                children: [
                  Positioned.fill(child: _remoteVideo()),
                  Positioned(
                      top: 18,
                      left: 22,
                      child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                                '${widget.session.video ? 'VIDEO' : 'VOICE'}  ·  ${widget.session.roomId.replaceAll('-', ' ').toUpperCase()}',
                                style: const TextStyle(
                                    fontSize: 12,
                                    fontWeight: FontWeight.w700,
                                    letterSpacing: 1)),
                            const SizedBox(height: 5),
                            Text(
                                '${widget.session.status}  ·  ${widget.session.durationLabel}',
                                style: const TextStyle(
                                    color: Color(0xFFB4B7C1), fontSize: 12))
                          ])),
                  if (widget.session.video)
                    Positioned(
                        right: 22,
                        top: 22,
                        child: ClipRRect(
                            borderRadius: BorderRadius.circular(12),
                            child: Container(
                                width: 190,
                                height: 132,
                                color: const Color(0xFF252832),
                                child: RTCVideoView(
                                    widget.session.localRenderer,
                                    mirror: true,
                                    objectFit: RTCVideoViewObjectFit
                                        .RTCVideoViewObjectFitCover)))),
                  Positioned(
                      left: 0,
                      right: 0,
                      bottom: 32,
                      child: Row(
                          mainAxisAlignment: MainAxisAlignment.center,
                          children: [
                            _control(
                                widget.session.muted
                                    ? Icons.mic_off
                                    : Icons.mic,
                                widget.session.muted
                                    ? Colors.redAccent
                                    : const Color(0xFF30333D),
                                widget.session.toggleMute),
                            if (widget.session.video) ...[
                              const SizedBox(width: 16),
                              _control(
                                  Icons.cameraswitch,
                                  const Color(0xFF30333D),
                                  widget.session.switchCamera),
                              const SizedBox(width: 16),
                              _control(
                                  widget.session.cameraOff
                                      ? Icons.videocam_off
                                      : Icons.videocam,
                                  widget.session.cameraOff
                                      ? Colors.redAccent
                                      : const Color(0xFF30333D),
                                  widget.session.toggleCamera)
                            ],
                            const SizedBox(width: 16),
                            _control(Icons.call_end, Colors.redAccent,
                                () => Navigator.of(context).pop(),
                                size: 58),
                          ])),
                ],
              ),
            ),
          ),
        ),
      );

  Widget _applyColorSettings(Widget child) {
    final filtered = ColorFiltered(
        colorFilter: ColorFilter.matrix(widget.colorMatrix), child: child);
    return widget.highContrast
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

  Widget _remoteVideo() {
    if (widget.session.video &&
        widget.session.remoteRenderer.srcObject != null) {
      return RTCVideoView(widget.session.remoteRenderer,
          objectFit: RTCVideoViewObjectFit.RTCVideoViewObjectFitCover);
    }
    return Container(
        color: const Color(0xFF191B22),
        alignment: Alignment.center,
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          CircleAvatar(
              radius: 58,
              backgroundColor: const Color(0xFF7588F5),
              child: const Icon(Icons.person, size: 64, color: Colors.white)),
          const SizedBox(height: 16),
          Text(widget.session.video ? 'Waiting for video…' : 'Voice call',
              style:
                  const TextStyle(fontWeight: FontWeight.w600, fontSize: 19)),
          const SizedBox(height: 7),
          Text(
              widget.session.remotePeer == null
                  ? 'Share this channel and join from another device'
                  : widget.session.status,
              style: const TextStyle(color: Color(0xFF9297A5)))
        ]));
  }

  Widget _control(IconData icon, Color color, VoidCallback onPressed,
          {double size = 52}) =>
      Padding(
          padding: const EdgeInsets.all(3),
          child: Material(
              color: color,
              shape: const CircleBorder(),
              child: InkWell(
                  customBorder: const CircleBorder(),
                  onTap: onPressed,
                  child: SizedBox(
                      width: size,
                      height: size,
                      child:
                          Icon(icon, color: Colors.white, size: size * .42)))));
}
