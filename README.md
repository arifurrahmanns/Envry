# Envry

Envry is a local email testing server. It captures every email your application sends over SMTP, gives each recipient a mailbox, and serves those mailboxes back over POP3, IMAP, a REST API and a web UI. Nothing is ever relayed to the internet.

![Envry's Sent view with an HTML email open in the reader](docs/images/sent.png)

> **Status:** early development; expect breaking changes before 1.0. SMTP capture, POP3, IMAP (all with optional TLS), the web UI, the REST API, message checks and failure simulation work today.

## Features

- **SMTP capture** on port 1025. Any username and password are accepted.
- **A mailbox per recipient**, created automatically, including BCC recipients.
- **POP3 and IMAP** access to every mailbox, so you can test code that receives mail.
- **Web UI** with three views: Sent, Receivers and Inbox.
- **REST API and live events** so automated tests can wait for and assert on emails.
- **Message checks** for a missing plain-text part, images without alt text, Gmail clipping, broken links and spam-prone headers.
- **Failure simulation** to test how your application handles rejected recipients, temporary errors and slow servers.
- **`envry send`** to fire a test email from the command line.
- **One binary** with the UI embedded; no external services.

![Receivers view: one card per recipient with unread counts](docs/images/receivers.png)

## Installation

**Desktop app** (Windows, macOS and Linux)

| System | Download |
| --- | --- |
| Windows 10 and 11 | [Envry-setup-windows-x64.exe](https://github.com/arifurrahmanns/envry/releases/latest/download/Envry-setup-windows-x64.exe) |
| macOS 11 or later (Apple silicon and Intel) | [Envry-macos-universal.dmg](https://github.com/arifurrahmanns/envry/releases/latest/download/Envry-macos-universal.dmg) |
| Linux | [Envry-linux-x86_64.AppImage](https://github.com/arifurrahmanns/envry/releases/latest/download/Envry-linux-x86_64.AppImage) or [envry-desktop_amd64.deb](https://github.com/arifurrahmanns/envry/releases/latest/download/envry-desktop_amd64.deb) |

The app runs Envry in the background and shows the web UI in its own window. Its tray (menu-bar) icon shows whether Envry is live and how much mail arrived since you last looked; new mail raises a notification, and Envry can start at login. Closing the window keeps Envry catching mail; choose **Quit Envry** in the tray menu to stop it. If a port is taken, for example by Mailpit, which uses the same defaults, the app lists the busy ports and what holds them, and can move Envry to other ports. The app and the `envry` command share one database, `~/.envry/envry.db`. On Windows the installer needs no administrator rights, and Envry appears under *Settings › Apps*, where you can uninstall it.

The installers are not code-signed yet, so the first launch shows a warning. On Windows, SmartScreen says "Windows protected your PC": choose **More info › Run anyway**. On macOS, right-click Envry in Applications and choose **Open**, or run `xattr -dr com.apple.quarantine /Applications/Envry.app`.

**macOS and Linux**

```bash
curl -sSfL https://raw.githubusercontent.com/arifurrahmanns/envry/main/install.sh | sh
```

The script downloads the latest release for your system, verifies its checksum and installs `envry` to `/usr/local/bin`. Set `ENVRY_INSTALL_DIR` to install elsewhere, or `ENVRY_VERSION` (for example `v0.1.0`) to pin a release.

**Windows** (PowerShell)

```powershell
irm https://raw.githubusercontent.com/arifurrahmanns/envry/main/install.ps1 | iex
```

The script verifies the download, installs `envry.exe` to `%LOCALAPPDATA%\Programs\Envry`, adds that folder to your PATH and adds **Envry (console)** to the Start menu. It needs no administrator rights; run it again to upgrade. `ENVRY_VERSION` and `ENVRY_INSTALL_DIR` work as above, and `ENVRY_NO_MODIFY_PATH=1` leaves PATH and the Start menu untouched. To uninstall, delete that folder and the Start-menu shortcut, and remove the folder from your user PATH under *Settings › System › About › Advanced system settings › Environment Variables*.

**Homebrew (macOS)**

```bash
brew install arifurrahmanns/tap/envry
```

**Scoop (Windows)**

```bash
scoop bucket add envry https://github.com/arifurrahmanns/scoop-bucket
scoop install envry
```

**Manual download**: archives for Linux, macOS and Windows on amd64 and arm64 are on the [releases page](https://github.com/arifurrahmanns/envry/releases). Each contains a single `envry` binary.

**From source** (requires Go 1.25+, Node.js 20+ and GNU Make): run `make build`; the binary is written to `bin/envry`.

## Quick start

```bash
envry
```

Then open <http://localhost:8025> and point your application at `localhost:1025`.

On Windows you can also start Envry from the Start menu or by double-clicking `envry.exe`. It runs in its own window, lists its addresses and opens the web UI in your browser; close the window to stop it. If a port is taken, for example by Mailpit, which uses the same defaults, the window stays open and says which one.

| Service | Default port |
| --- | --- |
| SMTP | 1025 |
| POP3 | 1110 |
| IMAP | 1143 |
| Web UI and API | 8025 |

## Reading mail over IMAP

Connect any mail client (Thunderbird, Apple Mail, Outlook) or library to the IMAP port with a mailbox address as the username. Every mailbox has `INBOX`, `Sent`, `Drafts` and `Trash`; clients can create, rename and delete their own folders too.

- Changes show up everywhere at once: mail captured over SMTP appears in an idling client within milliseconds, and reading, flagging or deleting a message in the client updates the web UI live (and the other way round).
- When an application authenticates over SMTP as an account, a copy of what it sends is filed in that account's `Sent` folder.
- Supported: IMAP4rev1 with `IDLE`, `MOVE`, `UIDPLUS`, `NAMESPACE`, `SPECIAL-USE`, `CHILDREN`, `LIST-EXTENDED`, `LIST-STATUS`, `STATUS=SIZE`, `ESEARCH`, `SEARCHRES`, `UNSELECT` and `APPENDLIMIT`.
- `SEARCH` matches substrings case-insensitively in decoded headers and text parts, so partial words work.

## TLS

Plain connections are the default, because that is what most local test setups want. Start Envry with `--tls` to also test encrypted connections:

```bash
envry --tls
```

- **STARTTLS** is offered on the standard SMTP, IMAP and POP3 (`STLS`) ports.
- **Implicit TLS** is served on SMTPS `1465`, IMAPS `1993` and POP3S `1995` (`--smtps`, `--imaps`, `--pop3s` change them).
- Without `--tls-cert` and `--tls-key`, Envry generates a self-signed certificate for `localhost`, `127.0.0.1` and `::1` and keeps it in a `tls` folder next to the database, so a mail client only has to accept it once. It is renewed automatically before it expires. The SHA-256 fingerprint is logged at startup and shown in the web UI's connection settings.
- Authentication still works without TLS, so the same accounts can be used either way.

## Reading mail over POP3

Log in to the POP3 port with a mailbox address as the username, for example `dev@test.local`. Any password is accepted unless `--strict-auth` is set, in which case the address must be an account and the password must match. The maildrop is that mailbox's INBOX, oldest message first.

- `RETR` marks the message as read in the web UI; `TOP` does not.
- `DELE` removes a message from that mailbox only when the session ends with `QUIT`. The message stays in Sent and in other recipients' mailboxes.
- Supported commands: `USER`, `PASS`, `APOP`, `STAT`, `LIST`, `UIDL`, `RETR`, `TOP`, `DELE`, `NOOP`, `RSET`, `QUIT` and `CAPA`.

## Sending a test email

`envry send` delivers a message to a running Envry over SMTP, which is handy for checking a mail client setup or the web UI without writing code:

```bash
envry send --to dev@test.local --subject "Hello"
envry send --to dev@test.local --cc qa@test.local --bcc audit@test.local \
  --html "<h1>Welcome</h1>" --text "Welcome" --attach invoice.pdf
```

It uses the `--smtp` address (or `ENVRY_SMTP`), so it finds a server started with the same settings. With `--user` and `--password` it signs in with SMTP AUTH; when the user is an account, a copy is also filed in its Sent folder.

## Message checks

The reader's **Checks** section reviews every message for problems that affect how it renders or whether it reaches the inbox:

| Check | Flags |
| --- | --- |
| Plain-text version | HTML-only mail with no `text/plain` alternative |
| Gmail clipping | HTML larger than 102 KB, which Gmail cuts off |
| Image alt text | Images without an `alt` attribute (tracking pixels are ignored) |
| Links | `javascript:` and relative links, empty links, insecure `http://` links and links to local addresses |
| Required headers | Missing `From`, `Date` or `Message-ID` |
| Subject line | Empty, all-capitals or `!!!` subjects, common spam phrases and very long subjects |
| Unsubscribe header | Missing `List-Unsubscribe` and one-click `List-Unsubscribe-Post` headers |
| Message size | Messages over 10 MB |

**Check links** requests every `http(s)` link with `HEAD` (falling back to `GET`) and shows the status code, so broken links stand out. Envry only makes these requests when you click the button, and checks at most 50 links per message.

## Failure simulation

Start Envry with these flags to see how your application copes when mail delivery goes wrong:

```bash
envry --smtp-reject-rcpt "bounce@test.local" --smtp-reject-rcpt "*@blocked.test"
envry --smtp-reject-rate 20 --smtp-latency 3s
```

- `--smtp-reject-rcpt` refuses matching recipients at `RCPT TO` with `550 5.1.1`. Patterns are case-insensitive and support `*` and `?` wildcards.
- `--smtp-reject-rate` refuses that percentage of messages with `451 4.3.0` after `DATA`; nothing is stored.
- `--smtp-latency` waits before answering `DATA`, like a slow or overloaded mail server.

The web UI's Connection dialog shows which simulations are active.

## Using Envry in automated tests

`GET /api/v1/wait` blocks until a matching email exists and returns it, so a test can trigger an email and assert on its content without polling or sleeping:

```bash
curl "http://localhost:8025/api/v1/wait?to=user@test.local&subject=Reset&timeout=10s"
```

| Parameter | Meaning |
| --- | --- |
| `to`, `from` | Exact recipient or sender address |
| `subject` | Case-insensitive substring of the subject |
| `since` | RFC 3339 timestamp; ignore mail received before it |
| `timeout` | How long to wait, e.g. `10s` (default 10s, max 5m); `408` when it expires |

A message that already arrived matches immediately, so the test does not race the sender. The response is the full message, including headers, the text body, recipients and attachments.

For example, in a JavaScript test:

```js
const since = new Date().toISOString()
await requestPasswordReset('user@test.local') // your app sends the email

const res = await fetch(
  `http://localhost:8025/api/v1/wait?to=user@test.local&subject=Reset&since=${since}&timeout=10s`,
)
const email = await res.json()
expect(email.text).toContain('/reset?token=')
```

### In CI

Envry starts in milliseconds and needs no services, so CI jobs can run it next to the tests. In GitHub Actions:

```yaml
- name: Start Envry
  run: |
    curl -sSfL https://raw.githubusercontent.com/arifurrahmanns/envry/main/install.sh | sh
    nohup envry --db :memory: > envry.log 2>&1 &
    curl -sf --retry 20 --retry-connrefused --retry-delay 1 http://localhost:8025/api/v1/info > /dev/null

- name: Test
  run: npm test # the app sends mail to localhost:1025
```

`--db :memory:` keeps every run clean. Add `--smtp-reject-rcpt` or `--smtp-latency` to test how the app handles delivery failures.

## REST API

All endpoints live under `/api/v1` and return JSON. Lists are newest first and paginated with `?limit=` and `?before=<id>` (the `nextCursor` of the previous page).

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/messages` | Every captured message; filter with `q` (full-text), `from`, `to` |
| `GET` | `/messages/{id}` | Headers, text body, envelope recipients (To, Cc, Bcc) and attachments |
| `GET` | `/messages/{id}/raw` | The exact bytes received; `?download=1` saves an `.eml` |
| `GET` | `/messages/{id}/html` | HTML part for previewing; remote content only with `?remote=1` |
| `GET` | `/messages/{id}/attachments/{aid}` | Attachment download |
| `GET` | `/messages/{id}/checks` | Message checks and the list of links; makes no network requests |
| `POST` | `/messages/{id}/checks/links` | Request the message's links and return each status code |
| `DELETE` | `/messages/{id}`, `/messages` | Delete one message, or all of them |
| `GET` | `/mailboxes` | Recipient mailboxes with folders, totals and unread counts |
| `GET` | `/mailboxes/{address}/messages` | One mailbox folder (`?folder=INBOX`) |
| `PATCH` | `/deliveries/{id}` | Set `seen` / `flagged` on one mailbox's copy |
| `DELETE` | `/deliveries/{id}` | Remove one mailbox's copy |
| `POST` | `/accounts` | Create a test account: `{address, displayName?, password?}` |
| `GET`, `DELETE` | `/accounts/{address}` | Read or remove an account |
| `POST` | `/send` | Compose a message: `{from?, to, cc?, bcc?, subject, text, html?}` |
| `GET` | `/wait` | Long-poll for a matching message (see above) |
| `GET` | `/info` | Version, ports, settings and counts |

`GET /ws` streams live events (`message.created`, `message.deleted`, `messages.cleared`, `delivery.updated`, `delivery.deleted`, `mailbox.created`, `mailbox.updated`) as JSON `{type, data}` frames.

Request bodies must be `application/json`, and the WebSocket accepts same-origin connections only, so other websites open in your browser cannot read or change captured mail.

## Configuration

Every flag has a matching environment variable. Flags take precedence over environment variables.

| Flag | Environment variable | Default |
| --- | --- | --- |
| `--smtp` | `ENVRY_SMTP` | `0.0.0.0:1025` |
| `--pop3` | `ENVRY_POP3` | `0.0.0.0:1110` |
| `--imap` | `ENVRY_IMAP` | `0.0.0.0:1143` |
| `--http` | `ENVRY_HTTP` | `0.0.0.0:8025` |
| `--db` | `ENVRY_DB` | `~/.envry/envry.db` (`:memory:` allowed) |
| `--max-messages` | `ENVRY_MAX_MESSAGES` | `1000` (`0` = unlimited) |
| `--max-size` | `ENVRY_MAX_SIZE` | `25MB` |
| `--strict-auth` | `ENVRY_STRICT_AUTH` | `false` |
| `--account` (repeatable) | `ENVRY_ACCOUNTS` (comma-separated) | none, e.g. `dev@test.local:secret` |
| `--tls` | `ENVRY_TLS` | `false` |
| `--smtps` / `--imaps` / `--pop3s` | `ENVRY_SMTPS` / `ENVRY_IMAPS` / `ENVRY_POP3S` | `0.0.0.0:1465` / `0.0.0.0:1993` / `0.0.0.0:1995` (with `--tls`) |
| `--tls-cert` / `--tls-key` | `ENVRY_TLS_CERT` / `ENVRY_TLS_KEY` | self-signed when empty |
| `--ui-auth` | `ENVRY_UI_AUTH` | none, e.g. `admin:password` |
| `--smtp-reject-rcpt` (repeatable) | `ENVRY_SMTP_REJECT_RCPT` (comma-separated) | none, e.g. `*@blocked.test` |
| `--smtp-reject-rate` | `ENVRY_SMTP_REJECT_RATE` | `0` (percent) |
| `--smtp-latency` | `ENVRY_SMTP_LATENCY` | `0s` |
| `--log-level` | `ENVRY_LOG_LEVEL` | `info` |

Run `envry --help` for the full list of commands and flags.

## Development

| Command | Description |
| --- | --- |
| `make dev` | Go server with hot reload plus the Vite dev server at <http://localhost:5173> |
| `make build` | Build the web UI, then a single binary in `bin/` with the UI embedded |
| `make test` | Go tests with the race detector, plus a TypeScript type check |
| `make lint` | `go vet` and `oxlint` |
| `make fmt` | Format Go code |
| `make desktop-dev` | Run the desktop app from source (needs Rust; see [Tauri's prerequisites](https://v2.tauri.app/start/prerequisites/)) |
| `make desktop` | Build the desktop app installers into `desktop/src-tauri/target/release/bundle/` |

On Windows, run the same targets with `mingw32-make` or any GNU Make 4.x.

### Releasing

Releases are built by [GoReleaser](https://goreleaser.com) from a version tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The Release workflow runs the tests, then publishes the archives and `checksums.txt` to GitHub Releases, and builds the desktop installers on Windows, macOS and Linux and attaches them with `desktop-checksums.txt`. When the `TAP_GITHUB_TOKEN` secret is set (a token with write access to the `homebrew-tap` and `scoop-bucket` repositories), it also updates the Homebrew cask and the Scoop manifest. CI builds every archive from the same configuration on each push, without publishing, so release problems show up before tagging.

### Project layout

```
.github/workflows/  CI (tests, lint, release dry run) and the tag-triggered release
cmd/envry/          CLI entry point (serve, send, version)
internal/
  api/              REST API, /wait long-poll and /ws event stream
  app/              Wires the servers together and runs them
  checks/           Message checks and the on-demand link checker
  compose/          Builds outgoing messages for the API and envry send
  config/           Flags and ENVRY_* environment variables
  events/           In-process publish/subscribe bus
  imapd/            IMAP server (go-imap v2) kept in sync through the event bus
  mimeparse/        Raw message → addresses, bodies, attachments
  pop3d/            POP3 server (RFC 1939)
  smtpd/            SMTP capture server
  tlsutil/          Certificates: user-supplied or generated self-signed
  store/            SQLite storage, migrations and queries
  version/          Build metadata
  webui/            Embedded web UI with SPA fallback
web/                React + TypeScript + Tailwind CSS + shadcn/ui
desktop/            Desktop app (Tauri 2): runs envry as a sidecar, tray, notifications
  scripts/          Builds the envry sidecar for the Tauri target
  src/              Status page shown while Envry starts, is stopped or hits a port conflict
  src-tauri/        Rust side: server lifecycle, tray, window, capabilities, icons, bundling
docs/images/        Screenshots used in this README
install.sh          Installer for Linux and macOS releases
install.ps1         Installer for Windows releases
.goreleaser.yaml    Release builds, archives, Homebrew cask and Scoop manifest
```

## License

[MIT](LICENSE)
