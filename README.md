# esp32-go-server

A personal Italian voice server for Xiaozhi ESP32 devices, built as a fork of [hackers365/xiaozhi-esp32-server-golang](https://github.com/hackers365/xiaozhi-esp32-server-golang) (MIT).

> **Status: import.** The upstream code is imported at commit [`21f1a2e`](https://github.com/hackers365/xiaozhi-esp32-server-golang/commit/21f1a2e71ff383723f1464ea9b137016e6feab8d) with its history. Pruning and the PocketBase provider come next, so this tree still builds and behaves as upstream.

## Fork of upstream

This repository is a fork of [hackers365/xiaozhi-esp32-server-golang](https://github.com/hackers365/xiaozhi-esp32-server-golang). It keeps upstream history and authorship. To pull later upstream fixes, see [`docs/upstream-sync.md`](docs/upstream-sync.md).

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

MIT, as upstream. See [`LICENSE`](LICENSE): copyright hackers365 and mcpll.
