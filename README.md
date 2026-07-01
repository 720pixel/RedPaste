<div align="center">

<img src="assets/banner.svg" alt="RedPaste" width="620">

### Encrypted on your device. Readable only by whoever holds the link.

RedPaste is an anonymous, end-to-end encrypted pastebin. Your paste is
encrypted in the browser before it ever leaves — the server only ever stores
ciphertext. No accounts, no logins, no trackers.

![go](https://img.shields.io/badge/built%20with-Go-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![react](https://img.shields.io/badge/frontend-React-149ECA?style=for-the-badge&logo=react&logoColor=white)
![encryption](https://img.shields.io/badge/encryption-AES--256--GCM-FF3B3B?style=for-the-badge)
![license](https://img.shields.io/badge/license-MIT-FF3B3B?style=for-the-badge)

</div>

---

RedPaste ships as a single self-contained binary with the frontend embedded —
one process, embedded SQLite by default, nothing else to run.

Use it to:

- Share code or text, always end-to-end encrypted, AES-256-GCM
- Set burn-after-read, view limits, expiry, and optional passphrases
- Collaborate in real time on an encrypted pasteboard (CRDT-based)
- Diff two texts or two existing pastes, computed entirely client-side
- Get syntax highlighting for 30+ languages with automatic detection
- Run the whole thing as one binary, or in Docker, with SQLite or Postgres

## Contents

- [Quick start](#quick-start)
- [Run it](#run-it)
- [Configuration](#configuration)
- [Security model](#security-model)
- [Project layout](#project-layout)
- [Build from source](#build-from-source)
- [License](#license)

## Quick start

**Docker, one command:**

```bash
docker run -d -p 8080:8080 -v redpaste:/data ghcr.io/720pixel/redpaste
```

Open http://localhost:8080. The `redpaste` volume keeps your data (and an
auto-generated capability key) across restarts.

**Prebuilt binary:**

```bash
curl -fsSL https://raw.githubusercontent.com/720pixel/RedPaste/main/install.sh | sh
redpaste-server
```

## Run it

### Docker Compose

A small production stack (RedPaste + Postgres). It won't start without a
pepper:

```bash
export REDPASTE_SERVER_PEPPER=$(openssl rand -hex 32)
docker compose up -d
```

Put a TLS reverse proxy in front for a real deployment — see `deploy/nginx`.

### From source

You'll need Go 1.25+ and Node 20+ (Node only builds the frontend; it isn't
needed at runtime).

```bash
make build   # builds the frontend and embeds it into the binary
./redpaste-server
```

Or run in dev mode with SQLite:

```bash
make dev
```

## Configuration

Everything is configured through `REDPASTE_*` environment variables — see
[`.env.example`](.env.example) for the full list with comments. The
essentials:

| Variable | Purpose | Default |
|---|---|---|
| `REDPASTE_BASE_URL` | Canonical public origin | `http://localhost:8080` |
| `REDPASTE_LISTEN_ADDR` | Listen address | `:8080` |
| `REDPASTE_DATABASE_URL` | Postgres DSN (empty = embedded SQLite) | *(empty)* |
| `REDPASTE_SERVER_PEPPER` | HMAC pepper for capability hashing (≥32 bytes in prod) | *(dev ephemeral)* |

Generate a pepper with `openssl rand -hex 32`.

## Security model

Your browser encrypts before upload. The server stores encrypted data and
never receives the decryption secret in your share link. Anyone with the
complete link can read or edit the paste according to its access type.
RedPaste **cannot** recover lost keys or passphrases, prevent screenshots, or
delete copies a recipient already saved.

See [`docs/threat-model.md`](docs/threat-model.md) for the full threat model,
[`docs/security/ddos.md`](docs/security/ddos.md) for the abuse/DDoS posture,
and [`SECURITY.md`](SECURITY.md) to report a vulnerability.

## Project layout

```
cmd/redpaste-server   the server binary
internal/             backend packages (config, store, crypto, http, live, …)
web/                  React + Vite frontend (built into the binary)
migrations/           reserved for versioned migrations; schema is currently
                      inline in internal/sqlite and internal/postgres
deploy/               nginx, systemd, docker, compose
docs/                 ADRs, threat model, security notes
```

## Build from source

```bash
git clone https://github.com/720pixel/RedPaste.git
cd RedPaste
make build
./redpaste-server
```

`make test` runs the Go test suite; `cd web && npm test` runs the frontend
suite.

## License

[MIT](LICENSE).
