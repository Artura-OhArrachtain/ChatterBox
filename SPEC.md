# ChatterBox → Discord replacement: gap spec

Reviewed 2026-11-20 against `server/main.go` (400 lines) and `client/lib/*.dart` (~2,950 lines).

Items are ordered by priority. Earlier items are foundations that later items depend on.
Status: `[ ]` todo · `[~]` in progress · `[x]` done.

## Where the app stands today

ChatterBox has a polished Discord-style **UI shell**: server rail, channel list, chat pane,
emoji/GIF picker, accessibility colour filters, STT/TTS and 1:1 WebRTC calls. Most of the
**social backend is placeholder data**:

| Area | Current behaviour | Evidence |
|---|---|---|
| Storage | ~~Everything in memory~~ → SQLite (item 1 done) | `server/store.go` |
| Identity | Client sends any `author` string; no accounts | `server/main.go:338-351` |
| DMs | Any `dm-<anything>` room is accepted and readable by anyone | `server/main.go:364-368` |
| Contacts/members | Hard-coded "Maya / Leo / Sam"; "ONLINE — 3" is a literal | `client/lib/main.dart:131`, `:2035` |
| Push | Every message (incl. DMs) goes to *every* registered device | `server/main.go:186-214` |
| Sync | Client re-fetches servers, rooms and the room's *entire* history every 3 s | `client/lib/main.dart:213`, `:355` |
| Servers/channels | Static `server/config.json`; restart to change | `server/main.go:120` |
| Calls | 1:1 only, STUN-only (no TURN), signaling polled every 800 ms | `client/lib/call_session.dart:86,107` |
| Tests | None (server or client) | — |

---

## P0: Foundations (without these it isn't usable as a real chat app)

### 1. [x] Persistent storage
- SQLite database (pure-Go driver, single file, e.g. `/var/lib/chatterbox/chatterbox.db`).
- Tables so far: servers, rooms, messages, push_tokens (versioned migrations via `PRAGMA user_version` in `server/store.go`). Users/sessions/memberships/DMs arrive with items 2 and 6.
- Seed servers/channels from `config.json` on first run only; afterwards the DB is the source of truth.
- Nightly backup: `chatterbox-backup.timer` → `/var/backups/chatterbox/`, 14-day retention.
- **Done when:** messages, servers and channels survive `systemctl restart chatterbox-api` and a reboot.

### 2. [ ] Accounts & authentication
- Register / log in / log out with username + password (argon2id or bcrypt), session tokens (opaque, stored hashed).
- Server sets `author` from the session; the client can no longer choose it.
- All `/api/*` endpoints except `/health`, register and login require auth.
- Profile (display name, status, colour) stored server-side instead of SharedPreferences.
- Client: login/register screen; remove the "API base URL" field from end-user settings (or hide behind a debug flag).
- **Done when:** two browsers can log in as different users and cannot post as each other.

### 3. [ ] Security baseline
- Restrict CORS to the deployed web origin (configurable, plus localhost in dev).
- Rate-limit login and message posting per user/IP (respect `CF-Connecting-IP`).
- Bind API to `127.0.0.1` (configurable) so it isn't exposed on 0.0.0.0:8080.
- Push: send only to devices of users who are members of the channel / participants in the DM, excluding the sender; drop tokens FCM reports as invalid.
- **Done when:** a non-member never receives a notification or content for a channel/DM they can't see.

### 4. [ ] Real-time delivery (WebSocket gateway)
- `GET /api/gateway` WebSocket authenticated by session; events: `message.create/update/delete`, `typing`, `presence`, `channel.*`, `call.signal`.
- Client replaces the 3 s full refresh with: initial fetch + live events + reconnect with backoff and gap-fill (`after=<lastId>`).
- Move call signaling onto the socket (removes 800 ms polling).
- **Done when:** a message appears in another browser in <500 ms with no polling traffic.

### 5. [ ] Message history pagination
- `GET /api/channels/{id}/messages?before=<id>&limit=50`; client loads older pages on scroll-up.
- **Done when:** a channel with 10k messages opens instantly and scroll-back works.

### 6. [ ] Real direct messages
- DM channels between real users (1:1 and small group), created on demand, access restricted to participants.
- DM list shows real conversations ordered by last activity; remove hard-coded contacts.
- **Done when:** user A can DM user B, user C cannot read it, and the hard-coded contacts are gone.

## P1: Core Discord parity

### 7. [ ] Servers, channels & invites managed in-app
- Create server (creator = owner), create/rename/delete/reorder text & voice channels, channel categories.
- Invite links (`/invite/<code>`, optional expiry/max uses); join/leave server.
- Server icon/initials derived from name (today the code assumes `hangout`→H / else T).

### 8. [ ] Members list & presence
- Real per-server member list; online/idle/offline via gateway heartbeat; custom status.

### 9. [ ] Edit & delete messages
- Author can edit (shows "(edited)") and delete; moderators can delete any message.

### 10. [ ] Unread state & mentions
- Per-user read markers; bold unread channels, mention badge counts, "new messages" divider.
- `@username` / `@everyone` mentions with highlight and push notification.

### 11. [ ] Replies & reactions
- Reply-to (quoted preview, jump to original). Emoji reactions with counts and who-reacted.

### 12. [ ] Attachments & rich content
- Upload images/files (size limit, stored on disk or object storage), inline image preview.
- Markdown (bold/italic/code/code blocks/spoilers/links) and link previews (server-side unfurl with SSRF protection).
- Replace the fixed 9-GIF list with search (Tenor/Giphy API key on server) or keep URL paste.

### 13. [ ] Roles, permissions & moderation
- Roles (owner/admin/mod/member + custom), per-channel permission overrides (view/send/manage).
- Kick, ban, timeout; audit log of moderation actions.

### 14. [ ] Voice channels that work like Discord
- Persistent voice rooms: see who is connected, join/leave freely.
- Group calls (>2 people): mesh for ≤4, or an SFU (LiveKit/Pion) beyond that.
- TURN server (coturn) so calls work behind restrictive NATs/mobile carriers.
- Screen sharing, push-to-talk, per-user volume, speaking indicator.

### 15. [ ] Typing indicators
- "X is typing…" via gateway, throttled.

### 16. [ ] Search
- Full-text search in a server/DM (SQLite FTS5), filters: from:, in:, has:file, date range.

### 17. [ ] Notification settings & web push
- Mute server/channel/DM, "mentions only" mode. Web push for the browser/PWA build (FCM web or VAPID).

## P2: Nice to have

- [ ] 18. Avatars (upload & crop) and profile popouts
- [ ] 19. Friends list, friend requests, blocking
- [ ] 20. Pinned messages
- [ ] 21. Threads
- [ ] 22. Custom server emoji
- [ ] 23. Webhooks & bot API (incoming webhooks first)
- [ ] 24. Slash commands
- [ ] 25. Two-factor auth, password reset (email), session management
- [ ] 26. Data export / account deletion

## Engineering hygiene (runs alongside the items above)

- [ ] Split `main.dart` (2,100 lines) into screens/widgets/services; split `server/main.go` into packages.
- [ ] Go handler tests for every endpoint added; a widget smoke test for the client.
- [ ] CI: `go test ./...` and `flutter analyze` on push.
- [ ] Structured logging; `/health` also checks the DB.
