# Envry

Envry is a local email testing server. It captures every email your application sends over SMTP, gives each recipient a mailbox, and serves those mailboxes back over POP3, IMAP, a REST API and a web UI. Nothing is ever relayed to the internet.

> **Status:** early development. SMTP capture, POP3, the web UI and the REST API work today; IMAP is being built.

## Features

- **SMTP capture** on port 1025. Any username and password are accepted.
- **A mailbox per recipient**, created automatically, including BCC recipients.
- **POP3 and IMAP** access to every mailbox, so you can test code that receives mail.
- **Web UI** with three views: Sent, Receivers and Inbox.
- **REST API and live events** so automated tests can wait for and assert on emails.
- **One binary** with the UI embedded; no external services.

## Quick start

Build from source (requires Go 1.25+, Node.js 20+ and GNU Make):

```bash
make build
./bin/envry
```

Then open <http://localhost:8025> and point your application at `localhost:1025`.

| Service | Default port |
| --- | --- |
| SMTP | 1025 |
| POP3 | 1110 |
| IMAP | 1143 |
| Web UI and API | 8025 |

## Reading mail over POP3

Log in to the POP3 port with a mailbox address as the username, for example `dev@test.local`. Any password is accepted unless `--strict-auth` is set, in which case the address must be an account and the password must match. The maildrop is that mailbox's INBOX, oldest message first.

- `RETR` marks the message as read in the web UI; `TOP` does not.
- `DELE` removes a message from that mailbox only when the session ends with `QUIT`. The message stays in Sent and in other recipients' mailboxes.
- Supported commands: `USER`, `PASS`, `APOP`, `STAT`, `LIST`, `UIDL`, `RETR`, `TOP`, `DELE`, `NOOP`, `RSET`, `QUIT` and `CAPA`.

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

## REST API

All endpoints live under `/api/v1` and return JSON. Lists are newest first and paginated with `?limit=` and `?before=<id>` (the `nextCursor` of the previous page).

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/messages` | Every captured message; filter with `q` (full-text), `from`, `to` |
| `GET` | `/messages/{id}` | Headers, text body, envelope recipients (To, Cc, Bcc) and attachments |
| `GET` | `/messages/{id}/raw` | The exact bytes received; `?download=1` saves an `.eml` |
| `GET` | `/messages/{id}/html` | HTML part for previewing; remote content only with `?remote=1` |
| `GET` | `/messages/{id}/attachments/{aid}` | Attachment download |
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
| `--tls-cert` / `--tls-key` | `ENVRY_TLS_CERT` / `ENVRY_TLS_KEY` | self-signed when empty |
| `--ui-auth` | `ENVRY_UI_AUTH` | none, e.g. `admin:password` |
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

On Windows, run the same targets with `mingw32-make` or any GNU Make 4.x.

### Project layout

```
cmd/envry/          CLI entry point (serve, version)
internal/
  api/              REST API, /wait long-poll and /ws event stream
  app/              Wires the servers together and runs them
  config/           Flags and ENVRY_* environment variables
  events/           In-process publish/subscribe bus
  mimeparse/        Raw message → addresses, bodies, attachments
  pop3d/            POP3 server (RFC 1939)
  smtpd/            SMTP capture server
  store/            SQLite storage, migrations and queries
  version/          Build metadata
  webui/            Embedded web UI with SPA fallback
web/                React + TypeScript + Tailwind CSS + shadcn/ui
```

## License

[MIT](LICENSE)
