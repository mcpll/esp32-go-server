# Xiaozhi device contract and hackers365 coverage

Research for [mcpll/esp32-go-server#3](https://github.com/mcpll/esp32-go-server/issues/3).
Scope: what a server must implement so a current Xiaozhi device can activate, receive OTA, open the WebSocket, stream Opus, run a push-to-talk turn and expose its on-device MCP tools; and what `hackers365/xiaozhi-esp32-server-golang` already does.
This document describes; it does not propose a schema or server code.

## 1. Sources read

| Repo | Local clone | `git rev-parse HEAD` |
|---|---|---|
| [78/xiaozhi-esp32](https://github.com/78/xiaozhi-esp32) (firmware) | `.scratch/firmware` | `8ce50d27cd7c72c777673f46cbfc3ef7454d1b5c` |
| [hackers365/xiaozhi-esp32-server-golang](https://github.com/hackers365/xiaozhi-esp32-server-golang) | `.scratch/hackers365` | `21f1a2e71ff383723f1464ea9b137016e6feab8d` |
| [mcpll/sauron-ai](https://github.com/mcpll/sauron-ai) (Eye firmware, used only for PTT listen messages) | `.scratch/sauron` | `f04484c40e9f1329e0ec74a120ff5927ef7f49ee` |
| [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) `client/client.go` at tag `v0.36.0` (the version pinned by hackers365) | `.scratch/mcpgo_client_v0.36.0.go` | tag `v0.36.0` (single file fetched) |

Citation format: `firmware:path:Lstart-Lend`, `hackers365:path:L…`, `eye:path:L…`, `mcp-go:client/client.go:L…`. All paths are relative to the repo root.

The Eye firmware is a set of overlays applied on top of a 78/xiaozhi-esp32 clone (`eye:README.md`, "hybrid Xiaozhi + Eye/PTT overlays"), so its wire behaviour is the upstream firmware's plus the PTT input handling described in section 5.

---

## 2. Device contract (what the firmware requires)

### 2.1 OTA check-version (config fetch) and firmware upgrade

- **URL**: the `ota_url` setting, falling back to `CONFIG_OTA_URL` (`firmware:main/ota.cc:L48-56`).
- **Method**: `POST` with a JSON body when there is system info, otherwise `GET`; any status other than 200 is an error (`firmware:main/ota.cc:L79-116`). The body is the board's system-info JSON (`firmware:main/boards/common/board.cc:L101` onward).
- **Request headers** (`firmware:main/ota.cc:L58-74`): `Activation-Version` (`2` when the device has an eFuse serial number, else `1`), `Device-Id` (MAC), `Client-Id` (UUID), `Serial-Number` (if any), `User-Agent`, `Accept-Language`, `Content-Type: application/json`.
- **Response fields the firmware reads** (all optional):
  - `activation{message, code, challenge, timeout_ms}` (`firmware:main/ota.cc:L128-149`).
  - `mqtt{…}`: every key is copied verbatim into the `mqtt` NVS namespace (`firmware:main/ota.cc:L151-170`).
  - `websocket{…}`: every key is copied verbatim into the `websocket` NVS namespace (`firmware:main/ota.cc:L171-191`); the protocol reads `url`, `token`, `version` from it (`firmware:main/protocols/websocket_protocol.cc:L79-106`).
  - `server_time{timestamp, timezone_offset}` (`firmware:main/ota.cc:L193-216`).
  - `firmware{version, url, force}`: the device compares `version` against its own and upgrades if newer or if `force` is set (`firmware:main/ota.cc:L218-246`).
- **Firmware upgrade**: a plain HTTP `GET` of `firmware.url`, written to the next OTA partition (`firmware:main/ota.cc:L272-398`).
- **Retry**: up to 10 attempts with backoff (`firmware:main/application.cc:L442-529`).
- **Transport selection**: MQTT if the OTA response contained `mqtt`, else WebSocket if it contained `websocket`, else MQTT (`firmware:main/application.cc:L531-545`).

### 2.2 Activation

- Triggered when the OTA response carries `activation.code` or `activation.challenge` (`firmware:main/application.cc:L442-529`).
- The device shows the code and reads the digits aloud (`firmware:main/application.cc:L728-750`).
- It then calls `Ota::Activate()` up to 10 times. That method requires a challenge and `POST`s to `<ota_url>/activate` (`firmware:main/ota.cc:L496-534`):
  - **202** means "keep waiting" (retry after 3 s); **200** means activated; other codes retry after 10 s (`firmware:main/application.cc:L442-529`).
- **Body** (`firmware:main/ota.cc:L459-494`): with a serial number, a flat JSON object `{"algorithm":"hmac-sha256","serial_number":…,"challenge":…,"hmac":…}`, where `hmac` is HMAC-SHA256 of the challenge using eFuse key `HMAC_KEY0`. Without a serial number the body is the literal `{}`. There is no wrapping `Payload` object.
- After the activate loop the device re-runs check-version and loops until the response has no `activation` block (`firmware:main/application.cc:L442-529`). A server can therefore complete activation purely by dropping the `activation` block from the OTA response.
- `timeout_ms` is stored (default 30000, `firmware:main/ota.h:L52`; set at `firmware:main/ota.cc:L147`) but no other read of it was found in `main/`.

### 2.3 WebSocket connection

- **Handshake headers** (`firmware:docs/websocket.md:L17-20`, `firmware:main/protocols/websocket_protocol.cc:L79-106`): `Authorization: Bearer <token>` (the `Bearer ` prefix is added when the token has no space), `Protocol-Version: <1|2|3>` (default 1, `firmware:main/protocols/websocket_protocol.h:L27`), `Device-Id`, `Client-Id`.
- **Device hello** (`firmware:main/protocols/websocket_protocol.cc:L199-223`, `firmware:docs/websocket.md:L22-50`): `{"type":"hello","version":N,"features":{"mcp":true[,"aec":true]},"transport":"websocket","audio_params":{"format":"opus","sample_rate":16000,"channels":1,"frame_duration":60}}`.
- **Server hello**: must arrive within 10 s (`firmware:main/protocols/websocket_protocol.cc:L176-190`). It must have `type:"hello"` and `transport:"websocket"`, and may carry `session_id` and `audio_params.sample_rate/frame_duration` (`firmware:main/protocols/websocket_protocol.cc:L225-255`, `firmware:docs/websocket.md:L52-71`). If the server omits these, the device defaults to 24000 Hz and 60 ms (`firmware:main/protocols/protocol.h:L79-80`).
- **Close**: the device sends no goodbye on WebSocket; it just closes (`firmware:main/protocols/websocket_protocol.cc:L74-77`).
- **Liveness**: the channel counts as timed out 120 s after the last *incoming data* (`firmware:main/protocols/protocol.cc:L108-117`).

### 2.4 Opus streaming

- **Uplink**: Opus, 16 kHz mono, 60 ms frames (`firmware:main/audio/audio_service.h:L40`, `firmware:docs/websocket.md:L423-433`).
- **Downlink**: Opus at the server hello's `sample_rate`/`frame_duration`; 24 kHz is explicitly allowed (`firmware:docs/websocket.md:L423-433`).
- **Binary framing** by `Protocol-Version` (`firmware:docs/websocket.md:L103-134`, `firmware:main/protocols/protocol.h:L10-34`, `firmware:main/protocols/websocket_protocol.cc:L24-54, L107-158`):
  - v1 is raw Opus.
  - v2 is a 16-byte header (`version, type, reserved, timestamp, payload_size`).
  - v3 is a 4-byte header (`type, reserved, payload_size`).
- **Playback gating**: incoming audio is played only while the device is in the `speaking` state (`firmware:main/application.cc:L554-558`), so the server must send `tts start` before audio frames. Frames received while the device is listening are discarded (`firmware:docs/websocket.md:L228-316`).

### 2.5 MQTT + UDP transport (alternative to WebSocket)

- **MQTT connect** uses the OTA `mqtt` object's `endpoint`, `client_id`, `username`, `password`, `keepalive` (default 240) and `publish_topic` (`firmware:main/protocols/mqtt_protocol.cc:L80-86`). The default port is 8883 (`firmware:main/protocols/mqtt_protocol.cc:L151-169`, `firmware:docs/mqtt-udp.md:L63-70`).
- **Hello exchange**:
  - The device hello has `version:3` and `transport:"udp"` (`firmware:main/protocols/mqtt_protocol.cc:L387-411`).
  - The server hello must contain `udp{server, port, key, nonce}`, where `key` and `nonce` are hex strings that each decode to 16 bytes (`firmware:main/protocols/mqtt_protocol.cc:L413-503`, `firmware:docs/mqtt-udp.md:L74-131`).
  - The wait is 10 s (`firmware:main/protocols/mqtt_protocol.cc:L274-385`).
- **UDP packets** are AES-128-CTR encrypted. The device copies the server nonce, then overwrites bytes [2:4] with the payload length, [8:12] with a timestamp and [12:16] with a sequence number. The resulting 16 bytes are both the plaintext packet header and the CTR IV (`firmware:main/protocols/mqtt_protocol.cc:L204-247`, `firmware:docs/mqtt-udp.md:L212-235`).
- **Receive side** checks type `0x01` and the length, and drops packets whose sequence is ≤ the last one seen (`firmware:main/protocols/mqtt_protocol.cc:L274-385`, `firmware:docs/mqtt-udp.md:L237-243`).
- **Goodbye**: the device sends `goodbye` on close (`firmware:main/protocols/mqtt_protocol.cc:L249-272`) and honours a server `goodbye` that matches its session (`firmware:main/protocols/mqtt_protocol.cc:L129-143`).

### 2.6 JSON control messages

**Device → server** (`firmware:main/protocols/protocol.cc:L66-106`, `firmware:docs/websocket.md:L164-224`):

| Message | Fields |
|---|---|
| `listen` | `state:"start"` + `mode:"auto"\|"manual"\|"realtime"`; `state:"stop"`; `state:"detect"` + `text` (wake word) |
| `abort` | optional `reason:"wake_word_detected"`, sent only in that case (`firmware:main/protocols/protocol.cc:L66-73`) |
| `mcp` | `payload` holding a JSON-RPC message |
| `hello`, `goodbye` | see 2.3 and 2.5 |

**Server → device**, as handled by `OnIncomingJson` (`firmware:main/application.cc:L579-722`, `firmware:docs/websocket.md:L228-316`):

- `tts` with `state`:
  - `start`: the device enters speaking.
  - `sentence_start` + `text`: shown as the assistant's line.
  - `stop`: the device goes to idle in manual mode, back to listening otherwise.
- `stt` + `text`: shown as the user's line.
- `llm` + `emotion`: sets the face.
- `mcp` + `payload`: handed to `McpServer::ParseMessage`.
- `system` + `command:"reboot"`.
- `alert` with `status`, `message`, `emotion`.
- `custom`, `notify`.

**Listening modes**: the default is `auto` when device-side AEC is off and `realtime` when it is on (`firmware:main/application.cc:L1188-1190`). `listen start` carries the current mode (`firmware:main/application.cc:L1071-1080`, `firmware:main/protocols/protocol.cc:L82-94`).

### 2.7 On-device MCP tools

The device is the MCP *server* and the backend is the client, tunnelled inside `{"type":"mcp","payload":…}` (`firmware:docs/mcp-protocol.md:L7-35`).

- **Advertising MCP**: the device sets `features.mcp:true` in its hello (`firmware:main/protocols/websocket_protocol.cc:L199-223`).
- **Validation**: payloads need `jsonrpc:"2.0"` and a `method`. Notifications are ignored, and the `id` must be a JSON number (`firmware:main/mcp_server.cc:L382-415`).
- **`initialize`**: the device answers with protocolVersion `2024-11-05`. It reads optional `params.capabilities.vision{url, token}` for the camera tool (`firmware:main/mcp_server.cc:L363-380, L417-430`, `firmware:docs/mcp-protocol.md:L60-100`).
- **`tools/list`**: accepts `params.cursor` and `params.withUserTools`, and returns `nextCursor` when paginated (`firmware:main/mcp_server.cc:L431-444`, `firmware:docs/mcp-protocol.md:L102-146`). User-only tools are hidden unless `withUserTools` is true (`firmware:docs/mcp-protocol.md:L209-218`, `firmware:main/mcp_server.cc:L126` onward).
- **`tools/call`**: takes `params.name` and `params.arguments` (`firmware:main/mcp_server.cc:L445-468`). For an unknown tool the doc says error `-32601` (`firmware:docs/mcp-protocol.md:L148-189`), but the code returns `-32602` (`firmware:main/mcp_server.cc:L572`).
- **Common tools** include `self.get_device_status` and `self.audio_speaker.set_volume` (`firmware:main/mcp_server.cc:L40-126`).

---

## 3. hackers365 implementation status

**Where the transports live**

| Piece | Location |
|---|---|
| HTTP + WebSocket routes | `hackers365:internal/app/server/websocket/websocket_server.go:L106-115`: `/xiaozhi/v1/` (device WS), `/xiaozhi/mqtt_udp/v1/` (bridge for an external MQTT gateway), `/xiaozhi/ota/`, `/xiaozhi/ota/activate`, `/mcp`, `/xiaozhi/api/mcp/tools/`, `/xiaozhi/api/vision`, `/ws/openclaw`, `/admin/inject_msg`; default port 8989 (`hackers365:doc/websocket_server.md:L22-31`) |
| WS connection | `hackers365:internal/app/server/websocket/websocket_conn.go` |
| MQTT+UDP adapter | `hackers365:internal/app/server/mqtt_udp/` (`mqtt_udp_adapter.go`, `mqtt_udp_conn.go`, `udp_server.go`, `udp.go`) |
| Embedded MQTT broker | `hackers365:internal/app/mqtt_server/`; listens on 2883 when enabled (`hackers365:config/config.yaml:L67-99`) |
| Transport-neutral interface | `types.IConn` (`hackers365:internal/app/server/types/conn.go:L13-36`) |
| Wiring | `App.OnNewConnection` (`hackers365:internal/app/server/app.go:L314-363`); adapters built at `hackers365:internal/app/server/app.go:L131-150` (MQTT/UDP) and `L177-181` (WS) |

**Coverage**

| Capability | Status | Where / notes |
|---|---|---|
| OTA check-version response | **Yes** | `handleOta` (`hackers365:internal/app/server/websocket/ota.go:L21-110`) returns `websocket{url,token}`, `mqtt{…}` (only when `mqtt.enable`, `ota.go:L112-133`), `server_time` (offset 480) and `firmware`. Response type at `hackers365:internal/app/server/websocket/types.go:L164-202`. The test or external config block is chosen by client IP prefix (`192.168.`, `10.`, `127.0.0.1`) (`ota.go:L21-110`, `hackers365:doc/websocket_server.md:L46-70`). |
| Firmware binary delivery | **No** | `firmware.version` is hardcoded to `"0.9.9"` and `url` is empty (`hackers365:internal/app/server/websocket/ota.go:L97-100`). A grep for `firmware` in `manager/backend` and `internal` finds nothing beyond these OTA structs. |
| Activation: code in OTA response | **Partial** | Only when `auth.enable` is true (`ota.go:L21-110`); the shipped default is `false` (`hackers365:config/config.yaml:L9-10`). The code and challenge come from the config provider (section 6). |
| Activation: `POST /xiaozhi/ota/activate` | **Partial / mismatched** | `handleOtaActivate` decodes `{"Payload":{…}}` and returns 400 unless `algorithm == "hmac-sha256"` (`hackers365:internal/app/server/websocket/ota.go:L17-19, L135-177`). The real firmware sends the payload unwrapped, or `{}` without a serial number (section 2.2), so this endpoint always answers 400 to stock firmware. Only hackers365's own test client wraps it (`hackers365:test/mqtt_udp/ota.go:L85-94`). Activation still completes through the OTA re-poll path (section 6). |
| WS upgrade and headers | **Yes** | `internalHandleChat` reads `Device-Id`/`Client-Id` from headers or the query string (`hackers365:internal/app/server/websocket/websocket_server.go:L155-222`). |
| WS token check | **No** | The `Authorization` check is commented out (`hackers365:internal/app/server/websocket/websocket_server.go:L166-181`). |
| Hello exchange | **Yes** | `HandleHelloMessage` requires `audio_params`, creates a session, replies, and starts MCP when `features.mcp` is set (`hackers365:internal/app/server/chat/chat.go:L455-527`). The reply is built in `sendHelloResponse` (`chat.go:L596-644`). The `ServerMessage` JSON shape, with `audio_params` and `udp`, is at `hackers365:internal/data/msg/message_types.go:L76-89`. |
| Binary protocol v1 | **Yes** | Binary frames are treated as raw Opus (`hackers365:internal/app/server/websocket/websocket_conn.go:L75-108`). |
| Binary protocol v2/v3 | **No** | No header parsing exists, and a grep for `Protocol-Version` finds no handling. Any `websocket.version` 2 or 3 pushed through OTA would break audio. |
| Opus uplink 16k/60 ms | **Yes** | Frames enter `ChatSession.HandleAudioMessage` → `OpusAudioBuffer` (`hackers365:internal/app/server/chat/session.go:L462-470`). |
| Opus downlink | **Yes** | TTS for xiaozhi clients is 24000 Hz / 20 ms (`hackers365:internal/app/server/chat/chat.go:L279-291`). A paced sender loop emits `tts start`/`sentence_start`/`stop` around the audio (`hackers365:internal/app/server/chat/tts.go:L183-398`; message senders at `hackers365:internal/app/server/chat/server_transport.go:L60-106, L198-234`). |
| MQTT+UDP | **Yes** | Covers the hello with UDP config (`chat.go:L596-644`), a random 16-byte key and a nonce of connID+timestamp (`hackers365:internal/app/server/mqtt_udp/udp_server.go:L190-246`), AES-CTR (`udp.go:L95-149`), and topic routing (section 3.1). Anti-replay is **disabled**: the sequence check is commented out (`hackers365:internal/app/server/mqtt_udp/udp.go:L95-116`). |
| `listen` start/stop/detect | **Yes** | Dispatch at `hackers365:internal/app/server/chat/session.go:L473-487`; details in sections 4 and 5. |
| `abort` | **Yes** | `chat.go:L663-670` → `StopSpeaking` (`session.go:L1092-1107`). |
| `stt` | **Yes** | `server_transport.go:L181-196`. |
| `llm` emotion, `system`, `alert` | **No** | The constants exist (`hackers365:internal/data/msg/message_types.go:L27-57`) but no sender was found. |
| `goodbye` | **Yes** (MQTT) | Received at `chat.go:L684-696`. |
| MCP: initialize | **Yes** | `sendInitlize` (`hackers365:internal/domain/mcp/device_manager.go:L615-634`). The `vision{url, token}` capability is injected with url from `vision.vision_url` and a **hardcoded** token `"1234567890"` (`hackers365:internal/app/server/chat/mcp.go:L32-60`). |
| MCP: tools/list | **Partial** | Uses mcp-go `ListTools` with an empty request (`device_manager.go:L274`), which follows `nextCursor` (`mcp-go:client/client.go:L379-403`). `withUserTools` is never sent, so user-only tools stay hidden (by design, per the firmware doc). |
| MCP: tools/call | **Yes** | `RawCallTool` (`device_manager.go:L739-755`). Device tools reach the LLM as eino `tool.InvokableTool`s via `mcp.GetToolsByDeviceIdWithTransport` (`hackers365:internal/domain/mcp/mcp_client.go:L268`) and are executed at `hackers365:internal/app/server/chat/tool.go:L251`. mcp-go uses integer request ids (`mcp-go:client/client.go:L127-131`), which satisfies the firmware's numeric-id rule. |
| MCP: inbound routing | **Yes** | `chat.go:L680-682` → `HandleDeviceIotMcpMessage` (`hackers365:internal/domain/mcp/mcp_client.go:L210-230`). |

### 3.1 MQTT topic routing (hackers365)

- The OTA response tells the device to publish to `device-server` (`hackers365:internal/app/server/websocket/ota.go:L130`, constant at `hackers365:internal/data/msg/message_types.go:L10`).
- The embedded broker only lets devices publish to that topic (`hackers365:internal/app/mqtt_server/device_hook.go:L32-46`). It rewrites each device publish to `/p2p/device_public/<mac>` (`device_hook.go:L153-190`) and auto-subscribes the device to `/p2p/device_sub/<mac>` (`device_hook.go:L107-132, L240-242`). The MAC is parsed from the client id `GID@@@<mac>@@@<uuid>` (`device_hook.go:L201-208`).
- The server side subscribes to `/p2p/device_public/#` (`hackers365:internal/app/server/mqtt_udp/mqtt_udp_adapter.go:L190-241`) and publishes to `/p2p/device_sub/<mac>` (`mqtt_udp_adapter.go:L492-526`).
- Credentials for an external broker or xiaozhi-mqtt-gateway: the username is base64 of the IP and the password is HMAC-SHA256 of `clientId|username` (`hackers365:doc/ota_mqtt_auth.md:L80-112`).

---

## 4. Turn flow through `internal/app/server/chat`

1. **Connection.** The transport produces an `IConn`. `App.OnNewConnection` builds `chat.NewChatManager(deviceID, transport)` and calls `Start` (`hackers365:internal/app/server/app.go:L314-363`; `hackers365:internal/app/server/chat/chat.go:L175-220`). `Start` runs `cmdMessageLoop` and `audioMessageLoop` (`chat.go:L318-323`). The client state starts with `ListenMode:"auto"` (`chat.go:L252`).
2. **Hello.** `HandleHelloMessage` calls `ensureSessionInternal` → `NewChatSession` + `Start` (`chat.go:L455-527, L746-799`). `ChatSession` owns the ASR, LLM and TTS managers (`hackers365:internal/app/server/chat/session.go:L64-113`). `ChatSession.Start` initialises ASR/LLM/TTS and launches `ProcessVadAudio` and `processChatText` (`session.go:L263-300`). If `features.mcp` is set, MCP init runs (`chat.go:L455-527` → `hackers365:internal/app/server/chat/mcp.go:L80-86`).
3. **Command dispatch.** Handled message types are `hello`, `speak_ready`, `listen`, `abort`, `iot`, `mcp`, `goodbye` (`chat.go:L424-453`).
4. **Audio in.** Binary frames go through `HandleAudioMessage` into the Opus buffer (`session.go:L462-470`). If `auth.enable` is on and the device is not activated, audio is dropped (`chat.go:L377-422`).
5. **Listen start.** `HandleListenStart` (`session.go:L1149-1241`):
   - Records the mode.
   - For `auto`/`manual`, restarts ASR via `RestartAsrRecognition` → `AsrProvider.StreamingRecognize` (`session.go:L1220-1240`; `hackers365:internal/app/server/chat/asr.go:L551-617`).
   - `realtime` takes a separate branch (`session.go:L1185-1218`).
6. **VAD / end of utterance.** `ProcessVadAudio` decodes Opus and feeds PCM to ASR (`asr.go:L118-532`). VAD runs only when `needVad := !(AutoEnd || ListenMode == "manual")` (`asr.go:L151`), so manual mode skips server VAD (`asr.go:L219-223`).
7. **Listen stop.** `HandleListenStop` → `clientState.OnManualStop()` (`session.go:L1243-1257`) → `OnVoiceSilence`, which calls `Asr.StopWithReason` to finalise recognition (`hackers365:internal/data/client/client.go:L659-673`).
8. **ASR result.** The recognition loop sends `stt` (`asr.go:L895`) and queues the text (`asr.go:L1110-1114`). `processChatText` consumes it (`session.go:L1425-1444`).
9. **LLM.** `actionDoChat` handles exit words and OpenClaw routing, collects device MCP tools with `mcp.GetToolsByDeviceIdWithTransport`, and calls `llmManager.DoLLmRequest` (`session.go:L1544-1723`). Tool calls run through `tool.go:L251`. The provider contract is `LLMProvider.ResponseWithContext(ctx, sessionID, []*schema.Message, []*schema.ToolInfo) chan *schema.Message` (`hackers365:internal/domain/llm/base.go:L46-62`).
10. **TTS out.** Text goes to `TTSManager` (`hackers365:internal/app/server/chat/tts.go:L87-129`, `TextToSpeechStream` at `tts.go:L1777`). The paced `runSenderLoop` sends `tts start` → `sentence_start` → Opus frames → `sentence_end` → `tts stop` (`tts.go:L183-398`) through `ServerTransport` (`server_transport.go:L20-26, L60-106, L198-240`).
11. **Barge-in.** `abort` → `StopSpeaking` (`session.go:L1092-1107`). In `realtime_mode: 4`, the first ASR text also interrupts output (`session.go:L251-257`).
12. **Wake word.** `listen detect` → `HandleListenDetect` either plays a greeting (`enable_greeting`), ignores the event, or debounces the text into the LLM (`session.go:L724-788`). Only this detect path triggers the greeting.

---

## 5. Push-to-talk verdict

**What the device sends for PTT**

Upstream firmware:
- Press calls `StartListening` and release calls `StopListening` (e.g. `firmware:main/boards/doit-s3-aibox/doit_s3_aibox.cc:L62-69`).
- `HandleStartListeningEvent` puts the device in `kListeningModeManualStop`. If the device is speaking, it first sends `abort` with no reason (`firmware:main/application.cc:L844-878`).
- The start is sent as `listen start mode:"manual"` (`firmware:main/protocols/protocol.cc:L82-94`).
- Stop sends `listen stop` and the device goes idle (`firmware:main/application.cc:L880-895`).
- After `tts stop` in manual mode the device returns to idle, not to listening (`firmware:main/application.cc:L579-722`).

Eye firmware, the same messages with different timing:
- Hold calls `app.StartListening()`, after `AbortSpeaking(kAbortReasonNone)` if the device is speaking (`eye:firmware/ai-buddy/eye_ptt.cc:L143-170`).
- Release arms a 250 ms delayed `StopListening` (`eye_ptt.cc:L59-85, L171-198`).
- A 30 s cap also forces `StopListening` (`eye_ptt.cc:L93-130`; `eye:firmware/ai-buddy/listen_cap.h:L7`).
- `eye:firmware/ai-buddy/eye_ptt.h:L7-8` states "Listening mode is manual (PTT)".
- The Eye pre-opens the audio channel when MQTT connects (`eye:overlays/core/application.cc.patch`).

Resulting Eye sequence: `[abort]` → `listen start mode:manual` → Opus frames → `listen stop` (about 250 ms after release, or at 30 s).

**Verdict: hackers365 supports this manual-mode PTT turn today. `realtime_mode` is irrelevant to it.**

- `listen start mode:manual` restarts ASR (`session.go:L1220-1240`) and marks the client as having voice (`session.go:L1301-1303`). Server VAD is skipped (`asr.go:L151, L219-223`).
- `listen stop` finalises ASR through `OnManualStop` → `OnVoiceSilence` → `Asr.StopWithReason` (`session.go:L1243-1257`; `client.go:L659-673`). This feeds the LLM → TTS path in section 4.
- `abort` stops current output (`session.go:L1092-1107`).
- `chat.realtime_mode` (default `4`, `hackers365:config/config.yaml:L17`) is read only behind `IsRealTime()` (`hackers365:internal/data/client/client.go:L175-177`), i.e. only when the device sent `mode:"realtime"` (`asr.go:L325, L395, L789`; `session.go:L251`).

**Caveats**

- A `manual`/`auto` start is ignored while the welcome greeting is playing (`session.go:L546-548, L1167-1170`). The greeting is only triggered by `listen detect` (`session.go:L724-788`), which a PTT-only device does not send, so in practice this should not fire. Not verified at runtime.
- The server never sends `llm` emotion messages (section 3), so the device face does not follow the reply.
- Stock firmware only uses `realtime` when device AEC is on (`firmware:main/application.cc:L1188-1190`). A continuous "realtime" experience would therefore need both device config and the `realtime_mode` knobs; PTT does not.

---

## 6. OTA / activation split (device server vs. `manager/`)

**Device server (`internal/app/server/websocket/ota.go`)**
- Serves `POST /xiaozhi/ota/` and `POST /xiaozhi/ota/activate` (`websocket_server.go:L106-115`; `hackers365:doc/websocket_server.md:L90-97`).
- Stores no activation state. When `auth.enable` is true, `handleOta` calls the configured provider's `IsDeviceActivated`; if the device is not activated it calls `GetActivationInfo` and embeds `activation{code, message, challenge, timeout_ms}` (`ota.go:L21-110`).
- `handleOtaActivate` calls `VerifyChallenge` and returns 200 on success, 202 otherwise (`ota.go:L135-177`). As shown in section 3, stock firmware never gets past the 400 body check.
- The provider interface is at `hackers365:internal/domain/config/interface.go:L13-15`. The provider type is selected by `config_provider.type`, default `manager` (`hackers365:config/config.yaml:L31-40`).
- Unactivated devices that reach chat hear the code read out: "请在后台添加设备，激活码: <code>" (`session.go:L790-815`).

**Manager provider client (`internal/domain/config/manager/auth.go`)**
- Thin HTTP calls to the manager backend:
  - `IsDeviceActivated` → `GET /api/internal/device/check-activation` (`L51-61`)
  - `GetActivationInfo` → `GET /api/internal/device/activation-info` (`L64-92`)
  - `VerifyChallenge` → `POST /api/internal/device/activate` (`L95-116`)
- The local `verifyHMAC` uses an empty key and always passes (`L119-134`).
- `timeoutMs` is set to `300` while the comment says "5 min" (`L64-92`). The firmware appears not to use the value (section 2.2).

**Manager backend (`manager/backend`, separate Gin/GORM service)**
- **Internal routes**, behind `InternalServiceAuth` (`hackers365:manager/backend/router/router.go:L78-83`).
- **Activation-info** creates the device record plus a six-digit code and challenge (`hackers365:manager/backend/controllers/device_activation.go:L21-47, L93-182`).
- **Check-activation**: `device_activation.go:L51-89`.
- **Activate** requires the device to be bound to a user, a matching challenge, and an HMAC keyed by `PreSecretKey`. An empty key passes (`device_activation.go:L184-285`).
- **Binding** (the path that actually activates a stock device):
  1. The user enters the spoken code in the manager UI.
  2. The UI calls `POST /agents/:id/devices` (`router.go:L142` → `controllers/user.go:L336`).
  3. That handler calls `BindToAgent`, which finds the device by `device_code` (or MAC) and sets `Activated=true` (`hackers365:manager/backend/controllers/agent_device_service.go:L638-712`).
  4. On the device's next OTA re-poll, `IsDeviceActivated` is true, the `activation` block disappears, and the firmware exits its activation loop (`firmware:main/application.cc:L442-529`).

**Alternative provider**: the `redis` provider keeps codes and challenges in memory (random six-digit code, UUID challenge) and activates only on a challenge match (`hackers365:internal/domain/config/redis/auth.go:L22-57`). It has no binding UI.

**Summary**: the device server is the protocol façade. The activation state machine, code generation and user binding live in `manager/backend`. With the shipped default `auth.enable: false`, no activation happens at all. Firmware OTA (binary upgrade) is not implemented anywhere.

---

## 7. Realtime-provider seam

**There is no realtime (audio-in → audio-out) provider seam.** The pipeline is built on three separate interfaces:

- `AsrProvider{Process, StreamingRecognize(ctx, <-chan []float32) (chan types.StreamingResult, error), Close, IsValid}` (`hackers365:internal/domain/asr/base.go:L14-27`)
- `LLMProvider.ResponseWithContext(…)` (`hackers365:internal/domain/llm/base.go:L46-62`)
- `BaseTTSProvider` / `DualStreamProvider` / `TTSProvider` (`hackers365:internal/domain/tts/base.go:L25-45`)

A grep for a speech-to-speech or realtime provider type found none.

**Closest existing seams, from outermost to innermost:**

1. **`types.IConn` + `types.OnNewConnection`** (`hackers365:internal/app/server/types/conn.go:L13-36`). This is the cleanest cut. It exposes `SendCmd`/`RecvCmd` (JSON), `SendAudio`/`RecvAudio` (Opus), `GetDeviceID`, `GetTransportType`, `CloseAudioChannel`, `OnClose`, `Close` and `GetData`. `App.OnNewConnection` is where a different handler could replace `ChatManager` wholesale (`app.go:L314-363`), keeping hackers365's WS and MQTT/UDP transports.
2. **`chat.ChatSession`** (`hackers365:internal/app/server/chat/session.go:L64-113`). Its inputs are `HandleAudioMessage`, `HandleListenMessage` (start/stop/detect) and `HandleAbortMessage`. It owns `ASRManager`, `LLMManager` and `TTSManager` directly, as concrete fields rather than an interface.
3. **Output side**:
   - `TTSManager.EnqueueTtsStart` / `EnqueueMediaSentenceStart` / `EnqueueMediaFrame` / `EnqueueTtsStop` (`hackers365:internal/app/server/chat/tts.go:L1180-1247`) already accept pre-encoded media frames and pace them.
   - Alternatively, the `ServerTransport` `Send*` methods (`server_transport.go:L60-319`).
4. **Device tools**: `mcp.GetToolsByDeviceIdWithTransport` returns `map[string]tool.InvokableTool` (`hackers365:internal/domain/mcp/mcp_client.go:L268`). This is independent of the ASR/LLM/TTS chain and reusable by any engine that can call tools.

**What a realtime engine would have to accept and emit at that seam**, derived from sections 2 and 4:

- **Inputs**: 16 kHz/60 ms Opus frames, or decoded PCM; `listen start(mode)` / `stop` / `abort`; the device's MCP tool list.
- **Outputs**, which must reproduce the device-facing contract:
  - `stt` text
  - `tts start` before any audio (section 2.4)
  - `sentence_start` text
  - paced Opus at the negotiated output format (24 kHz / 20 ms today, `chat.go:L279-291`)
  - `tts stop`
  - optionally `llm` emotion
  - tool invocations routed back over `mcp`

---

## 8. Open questions / gaps

**Contract mismatches in hackers365**
- `/xiaozhi/ota/activate` expects a `Payload` wrapper that stock firmware does not send (sections 2.2 and 3). The device still activates via OTA re-poll after binding, but every activate call returns 400.
- WebSocket binary protocol v2/v3 is unsupported. Only v1 works (section 3).
- The WebSocket `Authorization` token is not validated (section 3).
- UDP anti-replay is disabled on the server (section 3).
- The server never emits `llm` (emotion), `system` or `alert` (section 3).
- The vision capability token is hardcoded (section 3).
- The HMAC secret is empty in both the device-server provider client and the manager (by default), so the challenge HMAC is not really checked (section 6).
- `firmware.version` is hardcoded to `0.9.9` with no URL; OTA firmware delivery is absent (section 3).
- `activation.timeout_ms` is sent as 300 while the comment says 5 min; the firmware does not appear to use it (sections 2.2 and 6).

**Firmware-side ambiguities**
- The unknown-tool error code is `-32601` in `docs/mcp-protocol.md` but `-32602` in `mcp_server.cc` (section 2.7).
- The device's 120 s "no incoming data" timeout (`firmware:main/protocols/protocol.cc:L108-117`) means an idle pre-opened channel, as the Eye keeps, is treated as closed after 2 minutes and reopened with a new hello. How that interacts with hackers365 session reuse was not tested.

**Not verified**
- Anything at runtime: every finding comes from reading source and docs at the SHAs above.
- The OTA protocol spec linked from `firmware:main/ota.cc:L76-78` (a Feishu document) could not be accessed.
- The exact semantics of the server `notify` message the firmware handles (`firmware:main/application.cc:L579-722`).
- Authentication inside the embedded broker (`hackers365:internal/app/mqtt_server/auth_hook.go`), beyond the ACL hook read in section 3.1.
- The path by which the external xiaozhi-mqtt-gateway uses `/xiaozhi/mqtt_udp/v1/` (the bridge header handling is at `hackers365:internal/app/server/websocket/websocket_conn.go:L113-143`).
- The exact JSON encoding of mcp-go request ids beyond `NewRequestId(int64)` (`mcp-go:client/client.go:L127-131`).
