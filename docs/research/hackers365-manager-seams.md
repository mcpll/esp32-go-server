# hackers365 device server ↔ manager seams

Research for [mcpll/esp32-go-server#7](https://github.com/mcpll/esp32-go-server/issues/7).
Scope: what the device server (`cmd/server`, `internal/`) reads from or writes to `manager/` today, through which transport, and whether it can already run from `config/config.yaml` alone.
OTA/activation behaviour is closed by [#3](https://github.com/mcpll/esp32-go-server/issues/3) / [xiaozhi-device-contract.md](./xiaozhi-device-contract.md); this note only names the provider interface those three calls sit on and covers everything else.
This document describes; it does not propose PocketBase collections or server code.

## 1. Sources read

| Repo | Local clone | `git rev-parse HEAD` |
|---|---|---|
| [hackers365/xiaozhi-esp32-server-golang](https://github.com/hackers365/xiaozhi-esp32-server-golang) | `.scratch/hackers365` | `21f1a2e71ff383723f1464ea9b137016e6feab8d` |
| [mcpll/esp32-go-server](https://github.com/mcpll/esp32-go-server) (this research file) | workspace root | `aaed180438428ebe28dbc281e76e0474080e099b` |

Citation format: `hackers365:path:Lstart-Lend`. Paths are relative to the hackers365 repo root.
Deepwiki overview was used only as a map; every claim below is cited to source or `doc/`.

---

## 2. The seam: `UserConfigProvider` (required)

The three OTA/activation calls (`IsDeviceActivated`, `GetActivationInfo`, `VerifyChallenge`) sit on **`UserConfigProvider`** in `internal/domain/config/interface.go` — not on `manager/backend/services/configprovider` (that package only normalises provider names when the Gin backend exports VAD/ASR/LLM/TTS/memory/vision rows; see section 3.1).

Full method set, grouped as in the source:

| Group | Methods |
|---|---|
| **Auth / activation** | `IsDeviceActivated(ctx, deviceId, clientId) (bool, error)` · `GetActivationInfo(ctx, deviceId, clientId) (code, challenge, message string, timeoutMs int)` · `VerifyChallenge(ctx, deviceId, clientId, activationPayload) (bool, error)` |
| **Per-device runtime config** | `GetUserConfig(ctx, userID) (types.UConfig, error)` |
| **Device role** | `SwitchDeviceRoleByName(ctx, deviceID, roleName) (string, error)` · `RestoreDeviceDefaultRole(ctx, deviceID) error` |
| **System config blob** | `GetSystemConfig(ctx) (string, error)` — JSON string of mqtt / mqtt_server / udp / ota / vision / … |
| **Events** | `NotifyDeviceEvent(ctx, eventType, eventData)` (device → manager) · `RegisterMessageEventHandler(ctx, eventType, handler)` (manager → device) |

(`hackers365:internal/domain/config/interface.go:L8-35`)

Factory: `GetProvider` / `GetUserConfigProvider` switch on `config_provider.type` and wire only **`redis`** or **`manager`** (`hackers365:internal/domain/config/base.go:L17-62`). `InitConfigSystem` also accepts `"memory"` (`hackers365:internal/domain/config/config_init.go:L27-52`), but `GetUserConfigProvider` has no `"memory"` case — that path cannot construct a usable provider for chat.

Implementations:

| `config_provider.type` | Type | Transport to manager |
|---|---|---|
| `manager` | `manager.ConfigManager` | HTTP + manager WebSocket (section 4) |
| `redis` | `redis_config.RedisUserConfigProvider` / embedded `UserConfig` | none (Redis + local viper / yaml) |

---

## 3. Switch / fallback: yaml alone without manager

### 3.1 The switch

- Bootstrap always loads a file into viper (`hackers365:cmd/server/config.go:L127-135`). Default path is `config/config.yaml` (`hackers365:cmd/server/defaults_config_standard.go:L5`).
- Provider selection key: `config_provider.type` (`hackers365:config/config.yaml:L31-34`). Stock sample sets `"manager"`.
- If the key is empty, `InitConfigSystem` defaults to **`redis`** (`hackers365:internal/domain/config/config_init.go:L29-32`).
- After yaml load, `updateConfigFromAPI` calls `GetProvider(type).GetSystemConfig` and merges the JSON into viper (`hackers365:cmd/server/config.go:L168-233`). On redis, `GetSystemConfig` returns `""` and the merge is a no-op (`hackers365:internal/domain/config/redis/userconfig.go:L200-204`) — so yaml stays authoritative for system keys.
- Periodic refresh is gated by `config_provider.enable_periodic_update` / `update_interval` (`hackers365:cmd/server/config.go:L78-116`).
- Embedding the Gin manager in-process (`-manager-enable`, build tag `manager`) is separate from the provider switch (`hackers365:cmd/server/main.go:L23-36`, `hackers365:cmd/server/manager_http.go:L29-68`). A binary without the tag stubs those calls (`hackers365:cmd/server/manager_http_stub.go:L7-12`).

**Verdict:** set `config_provider.type: redis` (or leave it unset so Init defaults to redis) and fill VAD/ASR/LLM/TTS/memory/mqtt/ota/mcp/… in `config/config.yaml`. The device server then does not need the Gin+GORM manager for runtime config or activation stubs. Redis is still initialised (`hackers365:cmd/server/config.go:L297-312`) and used for optional per-device overrides and chat memory when type is redis.

### 3.2 What redis mode actually reads

`GetUserConfig` HGets `xiaozhi:userconfig:{deviceId}` (prefix from `redis.key_prefix`), then for each of llm/asr/tts/memory merges redis fields onto **viper** sections; VAD always comes from yaml (`hackers365:internal/domain/config/redis/userconfig.go:L42-117`, `hackers365:doc/user_config.md`). System prompt: redis key or `system_prompt` in yaml (`hackers365:internal/domain/config/redis/userconfig.go:L184-198`).

Auth stubs are in-process maps (not Redis, not manager): first `GetActivationInfo` invents a code/challenge; `VerifyChallenge` matching challenge marks the device activated (`hackers365:internal/domain/config/redis/auth.go:L21-57`). `NotifyDeviceEvent` / `RegisterMessageEventHandler` are no-ops (`hackers365:internal/domain/config/redis/userconfig.go:L216-224`). Role switch/restore return errors (`hackers365:internal/domain/config/redis/userconfig.go:L206-214`).

### 3.3 `manager/backend/services/configprovider`

Not a device-server interface. The Gin admin uses `NormalizeProvider` / `ExportData` when building config responses and yaml exports for VAD/ASR/LLM/TTS/memory/vision (`hackers365:manager/backend/services/configprovider/provider.go:L31-114`, call sites e.g. `hackers365:manager/backend/controllers/admin.go:L4034-4093`). The device server never imports that package; it receives already-normalised `provider` + `json_data` over HTTP.

---

## 4. Manager mode transports

When `config_provider.type` is `manager`:

| Direction | Transport | Endpoint / channel |
|---|---|---|
| Device → manager (config, activation, history, pool stats) | **HTTP** (`http.ManagerClient`, token `manager.auth_token`) | see sections 5–8 |
| Device ↔ manager (events, MCP console, config test, system_config push) | **WebSocket** to `{backend_url}/ws` with JWT (`manager.endpoint_auth_token`) | `hackers365:internal/domain/config/manager/websocket_client.go:L120-180, L1216-1233` |
| Shared DB | **No** — device server does not open the manager GORM DB |
| File | yaml only as bootstrap; manager system config is merged over it |

Base URL: `BACKEND_URL` env, else `manager.backend_url` (`hackers365:internal/util/backend_url.go:L9-17`, default `http://localhost:8080` in `NewManagerUserConfigProvider`).

Manager `Init` dials the WS and starts reconnect (`hackers365:internal/domain/config/manager/websocket_client.go:L1216-1233`). Incoming `type:"system_config"` pushes merge into viper via registered handlers (`hackers365:internal/domain/config/config_init.go:L39-45`, `hackers365:internal/domain/config/manager/websocket_client.go:L634-677`, `hackers365:cmd/server/config.go:L138-145`). Manager broadcasts after system-config save (`hackers365:manager/backend/controllers/admin.go:L1198-1207`).

---

## 5. Runtime config (VAD / ASR / LLM / TTS / memory / RAG / speaker)

### 5.1 Per-device bundle (`GetUserConfig`)

**Manager:** HTTP `GET /api/configs?device_id=…` (internal auth) (`hackers365:internal/domain/config/manager/manager.go:L78-134`, route `hackers365:manager/backend/router/router.go:L85`). Response fields mapped into `types.UConfig`: vad/asr/llm/tts/memory providers+json, `voice_identify` groups, `knowledge_bases`, prompt, `agent_id`, `memory_mode`, `speaker_chat_mode`, `mcp_service_names`, openclaw keywords (`hackers365:internal/domain/config/manager/manager.go:L182-222`, `hackers365:internal/domain/config/types/types.go:L62-76`).

**Manager resolution:** look up `Device` by `device_name`; if missing, fall back to global default role; if present, load bound `Agent` for prompt/providers/KB/MCP/speaker modes (`hackers365:manager/backend/controllers/admin.go:L119-218`). That is the device/agent lookup seam — one HTTP call, not a separate API.

**Redis/yaml:** section 3.2. No agent_id / knowledge_bases / voice_identify groups / mcp_service_names from manager.

Chat loads this at session start via `GetProvider(…).GetUserConfig` (`hackers365:internal/app/server/chat/chat.go:L223-228`).

### 5.2 System-wide keys (`GetSystemConfig`)

**Manager:** HTTP `GET /api/system/configs` → JSON of mqtt, mqtt_server, udp, ota, mcp, local_mcp, voice_identify, tts, vad, asr, llm, vision, auth, chat, knowledge_search, … (`hackers365:internal/domain/config/manager/manager.go:L225-270`, `hackers365:manager/backend/controllers/admin.go:L1184-1195`). Merged into viper at startup and on poll/push.

**Redis:** empty string; yaml/`config.md` values remain (`hackers365:doc/config.md`).

### 5.3 Speaker (声纹)

- **Group/prompt/uuid bindings** come from per-device `GetUserConfig.VoiceIdentify` (manager) (`hackers365:internal/domain/config/manager/manager.go:L152-170`).
- **Service URL / threshold** come from system config / viper `voice_identify.*` (`hackers365:internal/app/server/chat/session.go:L146-167`; manager fills threshold default 0.4 if missing — `hackers365:internal/domain/config/manager/manager.go:L242-260`).
- Runtime identify calls an external voice server (`hackers365:doc/speaker_identification.md`), not the Gin manager HTTP API.

### 5.4 RAG / knowledge

- KB **refs** arrive on `UConfig.KnowledgeBases` from manager `GetDeviceConfigs` (`hackers365:internal/domain/config/manager/manager.go:L204`).
- Search does **not** go through manager: local MCP tool `search_knowledge` → `rag.Search` → Dify/RAGFlow/WeKnora using viper `knowledge.*` + refs (`hackers365:internal/app/server/chat/session_mcp_tool.go:L188-193`, `hackers365:internal/domain/rag/manager.go:L27-80`, `hackers365:doc/knowledge_base.md`).

### 5.5 Memory providers

Long-memory provider type/config is part of `UConfig.Memory` from GetUserConfig; short/none modes skip external memory (`hackers365:internal/app/server/chat/session.go:L428-439`). Provider implementations talk to Memobase/Mem0/etc., not to manager.

---

## 6. MQTT auth

MQTT broker auth is **local to the device server**, driven by viper — not an HTTP call into manager:

- Hook reads `mqtt_server.enable_auth` and `mqtt_server.signature_key` (`hackers365:internal/app/mqtt_server/auth_hook.go:L33-77`).
- HMAC credentials algorithm documented in `hackers365:doc/ota_mqtt_auth.md` (clientId `GID_…@@@mac@@@uuid`, username base64 JSON ip, password HMAC of `clientId|username`).
- OTA response mqtt password uses `ota.signature_key` (same key family) from system config / yaml (`hackers365:doc/ota_mqtt_auth.md:L9-35`).

In manager mode those keys are typically **pushed** into viper via `GetSystemConfig` / `system_config` WS. In redis/yaml mode they must be present in `config.yaml`. No shared DB with manager for MQTT auth.

---

## 7. MCP endpoints

Three different MCP surfaces; only one is a manager seam:

| Surface | Who talks | Transport | Manager involved? |
|---|---|---|---|
| Global MCP servers | Device server → external MCP | SSE / streamable HTTP from `mcp.global` in yaml or system config | Only as config source when type=manager (`hackers365:internal/domain/mcp/global_manage.go:L118-124`, `hackers365:config/config.yaml:L371-388`, `hackers365:doc/mcp.md`) |
| On-device / agent WS MCP | Device ↔ Xiaozhi device (or agent endpoint client) | Device-server WebSocket `/xiaozhi/mcp/…` | No (`hackers365:doc/mcp.md`, `hackers365:internal/app/server/websocket/mcp.go`) |
| Console remote MCP | Manager console → device server | **Manager WebSocket** paths `/api/mcp/tools`, `/api/mcp/status`, `/api/mcp/call` handled on the device (`hackers365:internal/domain/config/manager/websocket_client.go:L750-759`) | Yes — manager is the client |

Per-agent `mcp_service_names` filters which global servers apply (`hackers365:internal/domain/config/types/types.go:L73`, `hackers365:internal/domain/mcp/mcp_client.go:L264-281`). Agent MCP endpoint URLs for external tools are minted by the manager console (`/agents/:id/mcp-endpoint` in `hackers365:manager/backend/router/router.go:L190`), not by `cmd/server`.

Config test from the console also uses the manager WS (`/api/config/test`) (`hackers365:internal/domain/config/manager/websocket_client.go:L746-748`).

---

## 8. Chat history

| Mode | Load | Save |
|---|---|---|
| `manager` | HTTP `GET /api/internal/history/messages` (`hackers365:internal/app/server/chat/session.go:L335-341, L373-416`, `hackers365:internal/data/history/client.go:L125-151`) | HTTP `POST /api/internal/history/messages` (+ `PUT …/audio`) via `MessageWorker` (`hackers365:internal/app/server/message_handle.go:L211-333`, `hackers365:internal/data/history/client.go:L69-99`) |
| `redis` | Redis dialogue list via `llm_memory` (`hackers365:internal/app/server/chat/session.go:L323-334`) | Same path short-circuits to Redis AddMessage; does not call manager (`hackers365:internal/app/server/message_handle.go:L214-224`) |
| other / unset | Skip load (`hackers365:internal/app/server/chat/session.go:L343-346`) | — |

`App.initEventHandle` always constructs `HistoryClient` with `Enabled: true` and `manager.backend_url` (`hackers365:internal/app/server/app.go:L106-113`). In redis mode the worker never hits that client for text save; in manager mode history is pure HTTP to manager (manager stores DB + optional audio files under `storage/chat_history/audio` — `hackers365:manager/backend/router/router.go:L49-62, L87-89`).

---

## 9. Push / events the server sends (and receives)

### 9.1 Device → manager (via provider)

`App.DeviceOnline` / `DeviceOffline` → `NotifyDeviceEvent` with paths `/api/device/active` and `/api/device/inactive` (`hackers365:internal/app/server/app.go:L473-497`, `hackers365:internal/domain/config/types/event.go:L7-11`).

In manager mode that is a **WebSocket** POST-style request (`SendDeviceRequest` → `SendRequest`) (`hackers365:internal/domain/config/manager/manager.go:L360-365`, `hackers365:internal/domain/config/manager/websocket_client.go:L335-336`). Manager handles those paths on its WS controller (`hackers365:manager/backend/controllers/websocket.go:L378-381`). Redis provider no-ops.

### 9.2 Manager → device

`RegisterMessageEventHandler(EventHandleMessageInject=/api/device/inject_msg, HandleInjectMsg)` (`hackers365:internal/app/server/app.go:L499-508`, `hackers365:internal/domain/config/types/event.go:L13-16`). Manager sends inject on WS path `/api/device/inject_msg` (`hackers365:manager/backend/controllers/websocket.go:L1189`).

Also inbound WS: MCP tools/status/call, config test, openclaw, server ping/info (section 7); outbound system_config push (section 4).

### 9.3 Other HTTP writes to manager

- **Pool stats:** `POST /api/internal/pool/stats` on an interval (`hackers365:internal/pool/reporter.go:L26-123`, route `hackers365:manager/backend/router/router.go:L90`). Initialised whenever the reporter starts; intended for manager dashboards.
- **Role switch/restore** (local MCP tools): HTTP `POST /api/internal/devices/:id/switch-role` and `…/restore-default-role` (`hackers365:internal/domain/config/manager/manager.go:L295-358`, `hackers365:internal/app/server/chat/session_mcp_tool.go:L144-186`).

---

## 10. Activation methods (interface only; flow closed by #3)

Under manager: HTTP `GET …/check-activation`, `GET …/activation-info`, `POST …/activate` (`hackers365:internal/domain/config/manager/auth.go:L50-218`, routes `hackers365:manager/backend/router/router.go:L81-83`). Under redis: in-process stub (section 3.2). Do not re-derive OTA/activate firmware behaviour here.

---

## 11. What a replacement provider must implement vs what can be stubbed

Descriptive only — not a schema.

| Capability | Must implement for a manager-shaped product | Can stub / omit for yaml-first / minimal device server |
|---|---|---|
| `GetUserConfig` | Real per-device VAD/ASR/LLM/TTS/memory + prompt + agent id (+ KB refs, speaker groups, mcp_service_names if those features stay) | Return providers/config built from static yaml (redis provider pattern) |
| `GetSystemConfig` | Return mqtt/ota/udp/mcp/voice_identify/… JSON that merges into viper | Return `""` and keep yaml |
| `IsDeviceActivated` / `GetActivationInfo` / `VerifyChallenge` | Persist activation / binding state | In-memory or always-activated stub (redis already does this) |
| `NotifyDeviceEvent` | Persist online/offline for console | No-op |
| `RegisterMessageEventHandler` | Deliver inject (and keep a bidirectional event channel if console MCP remote-call stays) | No-op (no console inject) |
| `SwitchDeviceRoleByName` / `RestoreDeviceDefaultRole` | Only if role MCP tools remain | Return “unsupported” like redis |
| Chat history HTTP | If console history UI remains | Use Redis dialogue or skip (`Enabled: false` / redis branch) |
| Manager WebSocket client | Only if console needs live MCP/config-test/system_config push | Omit; redis Init does not connect |
| MQTT auth | Not part of `UserConfigProvider` — keep as local viper/signature_key | Same |
| RAG search / global MCP / speaker HTTP to voice-server | Not part of the provider interface | Configure via yaml; no manager required |
| `manager/backend/services/configprovider` normalisation | Manager-side only when exporting DB rows | Irrelevant to a PocketBase/React replacement of the console |

---

## 12. Open questions / gaps

- `event2Path` / `path2Event` in `hackers365:internal/domain/config/manager/types.go` map inject as `/api/device/message`, but registration and manager send use `/api/device/inject_msg`. The maps appear unused by the live register/send path; worth confirming before relying on them.
- `pool_stats.report_enabled` logic forces enabled when the flag is false (`hackers365:internal/pool/reporter.go:L35-39`), so pool POSTs may still hit a missing manager in redis mode.
- `HistoryClient.Enabled` is hardcoded true at App init even for redis (`hackers365:internal/app/server/app.go:L107-112`); harmless only because the redis save branch returns early.
- `"memory"` provider type is initialisable but not constructible via `GetUserConfigProvider`.
- Not runtime-verified: only static reading of `21f1a2e`.
