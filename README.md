# esp32-go-server

A personal Italian voice server for Xiaozhi ESP32 devices, built as a fork of [hackers365/xiaozhi-esp32-server-golang](https://github.com/hackers365/xiaozhi-esp32-server-golang) (MIT).

> **Status: personal install.** The upstream code is imported at commit [`21f1a2e`](https://github.com/hackers365/xiaozhi-esp32-server-golang/commit/21f1a2e71ff383723f1464ea9b137016e6feab8d) with its history. Docker Compose runs PocketBase (the console) and the device server. Redis and the voice server are optional profiles.

## Fork of upstream

This repository is a fork of [hackers365/xiaozhi-esp32-server-golang](https://github.com/hackers365/xiaozhi-esp32-server-golang). It keeps upstream history and authorship. The original project README (Chinese, at the imported commit) is [here](https://github.com/hackers365/xiaozhi-esp32-server-golang/blob/21f1a2e71ff383723f1464ea9b137016e6feab8d/README.md). To pull later upstream fixes, see [`docs/upstream-sync.md`](docs/upstream-sync.md).

## What it is

One install for one owner: one admin, with their agents and devices.

- **Device server (Go)**, kept from upstream: OTA, six-digit activation, WebSocket and MQTT+UDP, Opus, MCP, and the streaming ASR → LLM → TTS pipeline as the only dialogue engine.
- **PocketBase** for data, console login and console → server commands, replacing the upstream manager and its database.
- **React/Vite console** as the only product UI.
- **Optional, off by default:** voiceprint (who is speaking), mem0 memory, RAGFlow knowledge base, MQTT push, OpenClaw.

The voice loop is Italian, hold-to-talk, over the protocol the Eye firmware already speaks. The goal is that it feels as fluid as the hosted xiaozhi.me; there is no latency tooling, acceptance is by ear on the device.

## What changes from upstream

- Removed: the Gin/GORM manager and its Vue console, voice clone, role switching, persisted chat history.
- Added: a PocketBase config provider, a `commands` channel, the React console, an Italian default stack and Italian prompts.
- Fixed: WebSocket token check, no default secrets, no open inject route.

## Install

A clean host needs Docker with Compose. The PocketBase and device-server images build for `linux/amd64` and `linux/arm64` (a Raspberry Pi is arm64).

```sh
cp .env.example .env
# fill the values; see Environment below
docker compose up --build -d
```

PocketBase serves the console at <http://localhost:8090> (dashboard at `/_/`). The device server listens on `:8989` (OTA and WebSocket), `:2883` (MQTT) and UDP `:8990`.

First boot creates the one console user from `ADMIN_EMAIL` and `ADMIN_PASSWORD` (password at least 8 characters) and a PocketBase superuser with the same pair. Data stays in the `pb_data` volume. `docker compose down` keeps it; `docker compose down -v` deletes it.

The server logs in to PocketBase as that superuser. Compose sets `POCKETBASE_URL` to `http://pocketbase:8090`, and sets `POCKETBASE_EMAIL` and `POCKETBASE_PASSWORD` from `ADMIN_EMAIL` and `ADMIN_PASSWORD`. If PocketBase is down at startup the server still comes up and retries in the background.

Optional profiles stay off unless you name them:

```sh
docker compose --profile redis up --build -d
docker compose --profile voice up --build -d
```

`redis` starts Redis for short memory. Set `REDIS_ENABLE=true` and `REDIS_HOST=redis`. `REDIS_PASSWORD` overrides the password in `config/config.yaml` (`ticket_dev` when unset) on both the server and the Redis container. `REDIS_PORT` overrides `6379`.

`voice` starts Qdrant (`qdrant/qdrant:v1.15.4`) and the upstream voice server image `ghcr.io/hackers365/voice_server:0.1.2`. This repository does not build that image. In the console, under Settings, set `voice_identify.enable` and `voice_identify.base_url` to `http://voice-server:8080`. Confirm the upstream image has an arm64 build before using the profile on a Pi.

`docker compose up --build` builds the host architecture. To build both:

```sh
docker buildx build --platform linux/amd64,linux/arm64 -f docker/Dockerfile.pocketbase -t esp32-pocketbase:local .
docker buildx build --platform linux/amd64,linux/arm64 -f docker/Dockerfile.server -t esp32-server:local .
```

## Environment

Required. Compose stops when one of these is missing. The server also refuses a known upstream default for the four tokens.

| Variable | Purpose |
|---|---|
| `ADMIN_EMAIL` | Console user and superuser email |
| `ADMIN_PASSWORD` | Password, at least 8 characters |
| `WEBSOCKET_TOKEN` | WebSocket auth; the OTA response sends it as `websocket.token` |
| `ENDPOINT_AUTH_TOKEN` | Signs MCP and OpenClaw endpoint JWTs |
| `MQTT_SERVER_PASSWORD` | Embedded broker password |
| `VISION_TOKEN` | Vision endpoint token |
| `DASHSCOPE_API_KEY` | DashScope key for the seeded Italian ASR, LLM and TTS. The stack boots with it empty; a voice turn needs it |

Compose sets these on the server, so you do not put them in `.env` unless you run the binary outside Compose:

| Variable | Value |
|---|---|
| `POCKETBASE_URL` | `http://pocketbase:8090` |
| `POCKETBASE_EMAIL` | same as `ADMIN_EMAIL` |
| `POCKETBASE_PASSWORD` | same as `ADMIN_PASSWORD` |

Optional:

| Variable | When |
|---|---|
| `MQTT_SERVER_SIGNATURE_KEY` | Only when `mqtt_server.enable_auth` is on |
| `MEM0_API_KEY` | Long memory (`memory_mode: long`) |
| `REDIS_ENABLE` | `true` to use the Redis profile for short memory |
| `REDIS_HOST` | `redis` on the Compose network |
| `REDIS_PASSWORD` | Overrides the password in `config/config.yaml` |
| `REDIS_PORT` | Overrides `6379` |

## Bind a device

1. Point the device at `http://<host>:8989/xiaozhi/ota/`. The OTA response carries a six-digit `activation.code`, and a `devices` record appears in PocketBase with `activated` false.
2. Log in to the console with `ADMIN_EMAIL` and `ADMIN_PASSWORD`. Open Devices, choose Add device, enter that code, pick the seeded agent, and add a note. That sets `agent` and `activated`.
3. The next OTA response has no `activation` block and the device can talk. Edits to the agent (prompt, providers, voice) apply to the next session.
4. The check-in returns `websocket.url` from the `ota` setting: `test.websocket.url` when the device is on 192.168, 10, or 127, otherwise `external.websocket.url`. When that value is empty, the server builds `ws://<Host>/xiaozhi/v1/` from the request Host (`wss://` when the check-in itself is TLS). Set the field in the console, under Settings, only when the device should connect to a different host. An empty setting leaves a URL from the config file in place.

The device must send `WEBSOCKET_TOKEN` on the WebSocket upgrade. The OTA response includes that token.

## Build, vet and test

The cgo dependencies (opus, onnxruntime) are in the builder stage of `docker/test/Dockerfile.server`. Build it once, then run Go from it with the source mounted (`.dockerignore` hides every `test/` directory from the image build, so do not rely on `COPY`):

```sh
docker build --target builder -t esp32-go-builder -f docker/test/Dockerfile.server .
go() { docker run --rm -v "$PWD":/app -v esp32-gomod:/go/pkg/mod -w /app esp32-go-builder go "$@"; }
go build ./... && go vet ./... && go test ./...
go test -race ./internal/app/... ./internal/data/...
```

## Local test stack (Docker)

Developer stacks, separate from the install above. Each one starts PocketBase (with `pb_migrations/` mounted), Redis and the server, and carries the migration check. Pick the one that matches your host:

| Host | Compose file | Images | VAD |
|------|--------------|--------|-----|
| Mac (Apple Silicon) | `docker/test/mac/docker-compose.yml` | native `linux/arm64` | `silero_vad` (upstream ships no ten-vad for Linux arm64) |
| Linux x86-64 | `docker/test/linux/docker-compose.yml` | native `linux/amd64` | `silero_vad`, or `ten_vad` by editing `docker/test/config.yaml` |

```sh
F=docker/test/mac/docker-compose.yml   # or docker/test/linux/docker-compose.yml
docker compose -f $F --profile check run --rm check   # runs pb_migrations/check.sh, prints OK
docker compose -f $F up --build                       # PocketBase at http://localhost:8090 (admin UI at /_/)
docker compose -f $F down -v                          # reset all data
```

The first boot creates the console user (one record in the `users` collection) from `ADMIN_EMAIL` and `ADMIN_PASSWORD` (defaults `owner@example.com` / `console-pass-123`; override them in your shell). The admin UI at `/_/` only accepts a superuser, so every start also upserts a superuser with the same `ADMIN_EMAIL` and `ADMIN_PASSWORD` (password of at least 8 characters); it survives `down -v`. The server logs in to PocketBase as that superuser (`POCKETBASE_URL`, `POCKETBASE_EMAIL`, `POCKETBASE_PASSWORD`), reads the `settings` collection at startup and the agent of each device at the start of every session. If PocketBase is down at startup the server still comes up and retries in the background. The ASR/LLM/TTS keys in `docker/test/config.yaml` are placeholders, so the stack checks that it boots and activates devices, not that a voice turn works.

## Console

```sh
cd console
npm install
npm run dev    # http://localhost:5173, proxies /api to PocketBase on :8090
npm test
npm run build  # writes ../pb_public; the test stack serves that directory
```

Log in with `ADMIN_EMAIL` and `ADMIN_PASSWORD`. While logged out, only the login screen is reachable. The console edits agents and devices in PocketBase. It does not store API keys and it does not call a vendor. The install image builds this console itself. Binding a device is described in [Bind a device](#bind-a-device).

## Roadmap

- Spec: [#8](https://github.com/mcpll/esp32-go-server/issues/8)
- Tickets: the sub-issues of #8, starting at [#9](https://github.com/mcpll/esp32-go-server/issues/9)
- Research notes: [`docs/research/`](docs/research/)

## License

MIT, as upstream. See [`LICENSE`](LICENSE): copyright hackers365 and mcpll.
