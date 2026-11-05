# Envry

Envry is a local email testing server. It captures every email your application sends over SMTP, gives each recipient a mailbox, and serves those mailboxes back over POP3, IMAP, a REST API and a web UI. Nothing is ever relayed to the internet.

> **Status:** early development. The server, CLI and web UI shell are in place; SMTP capture, POP3 and IMAP are being built.

## Features

- **SMTP capture** on port 1025. Any username and password are accepted.
- **A mailbox per recipient**, created automatically, including BCC recipients.
- **POP3 and IMAP** access to every mailbox, so you can test code that receives mail.
- **Web UI** with three views: Sent, Receivers and Inbox.
- **REST API and live events** so automated tests can wait for and assert on emails.
- **One binary** with the UI embedded; no external services.

## Quick start

Build from source (requires Go 1.24+, Node.js 20+ and GNU Make):

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
  api/              REST API handlers
  app/              Wires the servers together and runs them
  config/           Flags and ENVRY_* environment variables
  version/          Build metadata
  webui/            Embedded web UI with SPA fallback
web/                React + TypeScript + Tailwind CSS + shadcn/ui
```

## License

[MIT](LICENSE)
