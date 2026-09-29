# esp32-go-server

A personal Italian voice server for Xiaozhi ESP32 devices, built as a fork of [hackers365/xiaozhi-esp32-server-golang](https://github.com/hackers365/xiaozhi-esp32-server-golang) (MIT).

> **Status: planning.** The design is written and the work is split into tickets. The upstream code is not imported yet, so there is nothing to build or run here today.

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

## Roadmap

- Spec: [#8](https://github.com/mcpll/esp32-go-server/issues/8)
- Tickets: the sub-issues of #8, starting at [#9](https://github.com/mcpll/esp32-go-server/issues/9)
- Research notes: [`docs/research/`](docs/research/)

## License

MIT, as upstream. See `LICENSE` once the upstream import lands.
