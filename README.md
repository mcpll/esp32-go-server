# esp32-go-server

A personal Italian voice server for Xiaozhi ESP32 devices, built as a fork of [hackers365/xiaozhi-esp32-server-golang](https://github.com/hackers365/xiaozhi-esp32-server-golang) (MIT).

> **Status: console skeleton.** The upstream code is imported at commit [`21f1a2e`](https://github.com/hackers365/xiaozhi-esp32-server-golang/commit/21f1a2e71ff383723f1464ea9b137016e6feab8d) with its history. The PocketBase config provider has replaced the upstream manager. The React console covers login, agents, and devices; the `commands` channel comes next.

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

## Build, vet and test

The cgo dependencies (opus, onnxruntime) are in the builder stage of `docker/test/Dockerfile.server`. Build it once, then run Go from it with the source mounted (`.dockerignore` hides every `test/` directory from the image build, so do not rely on `COPY`):

```sh
docker build --target builder -t esp32-go-builder -f docker/test/Dockerfile.server .
go() { docker run --rm -v "$PWD":/app -v esp32-gomod:/go/pkg/mod -w /app esp32-go-builder go "$@"; }
go build ./... && go vet ./... && go test ./...
go test -race ./internal/app/... ./internal/data/...
```

## Local test stack (Docker)

Each stack starts PocketBase (with `pb_migrations/` mounted), Redis and the server, and carries the migration check. Pick the one that matches your host:

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
npm run build  # writes ../pb_public, which PocketBase serves
```

Log in with `ADMIN_EMAIL` and `ADMIN_PASSWORD`. While logged out, only the login screen is reachable. The console edits agents and devices in PocketBase. It does not store API keys and it does not call a vendor.

Activating a device by hand:

1. Point the device at `http://<host>:8989/xiaozhi/ota/`. The OTA response carries a six-digit `activation.code`, and a `devices` record appears in PocketBase with `activated=false`.
2. In the console, open Devices, enter that code, pick the seeded agent, and add a note. That sets `agent` and `activated`.
3. The next OTA response has no `activation` block and the device can talk. Edits to the agent (prompt, providers, voice) apply to the next session.
4. In the `settings` collection, record `ota`, fill in `test.websocket.url` (for LAN clients) and `external.websocket.url` so the device gets a WebSocket address.

## Roadmap

- Spec: [#8](https://github.com/mcpll/esp32-go-server/issues/8)
- Tickets: the sub-issues of #8, starting at [#9](https://github.com/mcpll/esp32-go-server/issues/9)
- Research notes: [`docs/research/`](docs/research/)

## License

MIT, as upstream. See [`LICENSE`](LICENSE): copyright hackers365 and mcpll.
