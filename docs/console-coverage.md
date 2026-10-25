# Console coverage report

What the platform can do that an owner **cannot fully operate from the React console** (Agents, Devices, Settings, Status). Use this when deciding what still needs a UI, or how to run a feature today without one.

The console is the app under `console/`, served from PocketBase `pb_public/`. It never holds vendor API keys and never calls a vendor directly; it writes PocketBase records and watches `commands`.

## Gaps

| Area | Platform behavior | Console today | How to operate without the console |
|---|---|---|---|
| Knowledge bases and documents | `knowledge_upload` / `knowledge_sync`; agent `knowledge_enabled` and relations | Settings → Knowledge has provider JSON only. No KB list, document upload, sync, or per-agent knowledge switch | PocketBase admin `/_/` (superuser): collections `knowledge_bases`, `knowledge_documents`, agent fields. Or create `commands` records of type `knowledge_upload` / `knowledge_sync` |
| Long memory (mem0) | Search and save during chat when `memory_mode` is `long` | Agent Memory control only | Set `MEM0_API_KEY` (and mem0 URL if needed) in env / `config.yaml`. Mode alone does nothing without the key |
| Short memory (Redis) | Session history in Redis when `memory_mode` is `short` | Short is disabled in the UI when Redis is off | Compose `--profile redis`, `REDIS_ENABLE=true`, `REDIS_HOST=redis`. Without Redis the server treats short as none |
| Voiceprint at runtime | Identify speaker, `identified_only` gate, group prompt and TTS voice | Settings: voice server URL. Agent: groups, samples, enroll | Spoken turns on the device. Voice server via Compose `--profile voice` |
| OpenClaw session | Enter / exit via spoken phrases | Edit allowed flag and phrases | `ENDPOINT_AUTH_TOKEN` in env only; Settings cannot override it |
| Global MCP in dialogue | LLM may call global MCP servers during a turn | Settings → MCP configures servers | Automatic in session once servers are up; no separate “run tool” screen for global MCP |
| Device MCP in dialogue | Firmware tools available to the LLM | Devices: manual list / call | Agent `mcp_service_names` (PocketBase) filters which tools the session sees; empty means all |
| Agent MCP service filter | Restrict which MCP services an agent gets | Not editable in the console | Edit `mcp_service_names` on the agent in PocketBase |
| Vision | Device camera frames to a vision URL | Enable auth + URL in Settings | Device firmware path; `VISION_TOKEN` in env |
| OTA firmware delivery | Not implemented | — | — |
| `settings_reload` command | Force reload of all settings | Saving a settings block already reloads via PocketBase realtime; no button for the command | Create a `commands` record `{ "type": "settings_reload", "status": "pending", "payload": {} }` |
| Secrets | WebSocket token, MQTT keys, ASR/LLM/TTS keys, endpoint token | Never shown or stored (by design) | `.env` / `config.yaml` — see README Environment |
| Local Kokoro TTS | OpenAI-compatible speech on Compose network | Agent TTS provider `openai` (known fields: URL, model, voice, format) | `docker compose --profile tts` — image `ghcr.io/remsky/kokoro-fastapi-cpu`, no API key |
| Pool stats emission | Server overwrites the `pool_stats` record every 5 s | Status screen reads the record | Set `pool_stats.report_enabled` true in server config; when false, Status stays empty |
| Redis availability flag | Console learns whether Short memory is allowed | Read-only from settings | Server writes `redis_available` when Redis is enabled |

## Escape hatch: PocketBase admin

`/_/` is the PocketBase dashboard. It uses the **superuser** (`ADMIN_EMAIL` / `ADMIN_PASSWORD` on first boot), not the console `users` record. Prefer the console for day-to-day agent and device work. Use `/_/` for collections the console does not cover (knowledge documents, raw `commands`, agent fields without a form).

## Out of scope (v1)

From the product spec ([#8](https://github.com/mcpll/esp32-go-server/issues/8)): voice clone, role switching, persisted chat history UI, more than one admin, latency measurement tooling, OTA firmware blobs.
