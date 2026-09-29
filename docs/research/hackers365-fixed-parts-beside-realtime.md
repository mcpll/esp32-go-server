# hackers365 fixed parts beside a realtime provider: voiceprint, mem0, RAGFlow, MQTT

Research for [mcpll/esp32-go-server#5](https://github.com/mcpll/esp32-go-server/issues/5).
Question: for each of the four parts the map keeps as fixed, when does it run compared with the audio turn, what does it store and where, what would a realtime provider have to hand it, and does it depend on ASR text.
Decided elsewhere and not reopened: manual push-to-talk turn and the `IConn` + `App.OnNewConnection` seam ([#3](https://github.com/mcpll/esp32-go-server/issues/3), [xiaozhi-device-contract.md](./xiaozhi-device-contract.md)); provider returns user transcript, assistant transcript and tool calls by id, and instructions plus tool declarations are fixed for the connection ([#4](https://github.com/mcpll/esp32-go-server/issues/4), [qwen-realtime-push-to-talk.md](./qwen-realtime-push-to-talk.md)); manager coupling is [hackers365-manager-seams.md](./hackers365-manager-seams.md).
Facts and a placement recommendation only. No collections, no server code.

## 1. Sources read

| Repo | Local clone | `git rev-parse HEAD` |
|---|---|---|
| [hackers365/xiaozhi-esp32-server-golang](https://github.com/hackers365/xiaozhi-esp32-server-golang) | `.scratch/hackers365` | `21f1a2e71ff383723f1464ea9b137016e6feab8d` |
| [mcpll/esp32-go-server](https://github.com/mcpll/esp32-go-server) (this file) | workspace root | `aaed180438428ebe28dbc281e76e0474080e099b` |

Citation format: `hackers365:path:Lstart-Lend` (or `:Lline`). Paths are relative to the hackers365 root. Static reading only; nothing was run.
The voiceprint engine (`asr_server`) is a git submodule (`hackers365:.gitmodules`) that is **not checked out** in the clone (`.scratch/hackers365/asr_server` is empty). Everything about the engine below comes from `doc/speaker_identification.md` and from the Go client that talks to it.

## 2. Summary

| Part | Runs relative to the audio turn | Persistent state lives in | Needs ASR text? | Needs from the provider |
|---|---|---|---|---|
| Voiceprint | Beside the turn: fed decoded PCM while the user talks, result read at release | External voice-server + Qdrant; sample files and groups in the manager | **No** (audio only) | The same 16 kHz PCM the provider gets, plus a way to use the result (section 3.4) |
| mem0 write | After the turn, out of band, one event per message | External mem0 (cloud default) | **Yes**, user and assistant text | Both transcripts, in order |
| mem0 read | Before the LLM call, inside the system prompt | External mem0 | **Yes**, this turn's user text is the query | Cannot be per-turn under fixed instructions (section 4.4) |
| RAGFlow | Inside the LLM call, as the local tool `search_knowledge` | External RAGFlow; KB rows and docs in the manager | **No** (query is the model's tool argument) | Tool declaration, tool round trip by id, static routing text in instructions |
| MQTT | Below the turn: a device transport (control on MQTT, audio on UDP) | Nothing durable (clean sessions) | **No** | Nothing, if the bridge stays at `IConn`; the "push to speak" endpoint is the exception (section 6.3) |

Voiceprint does **not** depend on voice clone. The only link is that a speaker group can name a cloned voice for the pipeline TTS (section 3.5).

---

## 3. Voiceprint

### 3.1 When it runs

- Gate: `voice_identify.enable` in viper **and** the agent's config has at least one speaker group (`hackers365:internal/data/client/client.go:L163-173`, `hackers365:internal/app/server/chat/asr.go:L370-372`). The session builds a `SpeakerManager` only when `voice_identify.base_url` is set (`hackers365:internal/app/server/chat/session.go:L147-165`).
- **Manual mode feeds it.** `ProcessVadAudio` sets `skipVad`, `clientHaveVoice` and `haveVoice` to true for `ListenMode == "manual"` (`hackers365:internal/app/server/chat/asr.go:L219-222`). The speaker branch keys on `haveVoice`, so every decoded Opus frame is sent (`asr.go:L370-388`). Opening the stream happens on the first frame (`StartStreaming`, `asr.go:L374-378`). So the "skips server VAD" decision from #3 does not turn voiceprint off.
- Tap point: the float32 PCM right after `audioProcesser.DecoderFloat32`, before VAD (`asr.go:L233-256`). The client sends it as PCM float32 little-endian over a WebSocket, with `sample_rate`, `agent_id` and `threshold` as query parameters (`hackers365:internal/domain/speaker/streaming.go:L85-93`; the doc says the endpoint is `/api/v1/speaker/stream` at `doc/speaker_identification.md:L199`, the code dials `/api/v1/speaker/identify_ws` at `streaming.go:L55, L67`).
- Result is read at release. `listen stop` → `OnManualStop` → `OnVoiceSilence` (`hackers365:internal/app/server/chat/session.go:L1254`, `client.go:L659-683`). `OnVoiceSilence` calls the speaker callback, which runs `FinishAndIdentify` in a goroutine (`client.go:L681-683`, `session.go:L170-199`). When ASR final text arrives, `getSpeakerResult` waits **at most 200 ms** for it and otherwise uses whatever is stored, possibly nil (`asr.go:L1076-1106`, timer at `L1089`; call at `L812`).
- A second, realtime-mode-only path (`chat.realtime_mode == 3`) polls `PeekAndIdentify` during speech to interrupt playback (`asr.go:L393-452`). It applies only when the device sends `mode:realtime`, so it is out for push-to-talk.

### 3.2 What the result is used for

`IdentifyResult{Identified, SpeakerID, SpeakerName, Confidence, Threshold}` (`hackers365:internal/domain/speaker/types.go`). It is matched to a group by name in `DeviceConfig.VoiceIdentify` (`hackers365:internal/domain/config/types/types.go:L33-42, L69`). Three consumers, all after ASR final and inside `chat`:

| Consumer | What it does | Cite |
|---|---|---|
| Chat gate | `speaker_chat_mode = identified_only` drops the turn (no LLM) if no configured speaker matched | `hackers365:internal/app/server/chat/asr.go:L838`, `session.go:L1759-1774`, `client.go:L183-196` |
| LLM prompt | appends the group's `Prompt` to the system prompt for that request | `hackers365:internal/app/server/chat/llm.go:L1188-1198` |
| TTS voice | swaps the TTS config (provider config id, `voice`, and for `aliyun_qwen` a model override) for that turn | `hackers365:internal/app/server/chat/session.go:L1667, L1777-1880`, `tts.go:L1668-1720` |

`AddAsrResultToQueue(text, speakerResult)` carries the result to `actionDoChat` (`asr.go:L926`, `session.go:L1389-1438, L1544`). An `ASR_OUTPUT` hook also sees it (`asr.go:L819`).

### 3.3 What it stores and where

- Embeddings: Qdrant collection `speaker_embeddings`, 192-dim cosine, payload `uid, agent_id, speaker_id, speaker_name, uuid, sample_index` (`hackers365:doc/speaker_identification.md:L209-232`). Extraction uses sherpa-onnx (`doc:L331-335`). Both are inside the `asr_server` service, not in this repo's Go code.
- Enrolment is manager-only. The manager saves the WAV, calls `POST {voice-server}/api/v1/speaker/register` with `speaker_id`, name, uuid, agent id, user id, and keeps `speaker_groups` / `speaker_samples` rows (`hackers365:manager/backend/controllers/speaker_group.go:L906-960`; tables at `doc/speaker_identification.md:L244-286`). Delete and verify call the same service (`speaker_group.go:L803, L964-990`).
- The device server keeps no voiceprint data. It receives the group list (`name`, `prompt`, `uuids`, `tts_config_id`, `voice`) through `GetUserConfig` (`hackers365:internal/domain/config/manager/manager.go:L102-113, L153-166`). The redis provider never fills `VoiceIdentify` (`hackers365:internal/domain/config/redis/userconfig.go:L55-60`), so voiceprint is off without a manager-shaped provider.
- Configured through viper `voice_identify.{enable,base_url,threshold}` (`hackers365:config/config.yaml:L432-435`). The code default threshold is 0.4 (`hackers365:internal/domain/speaker/asr_server.go:L27`); the doc says 0.6 (`doc:L120-124`); `config.yaml` sets 0.4.

### 3.4 What a realtime provider changes

- **Audio, not text.** Voiceprint needs the user's speech, not a transcript. The bridge already has the 16 kHz PCM it sends to the provider (#4 section 9), so it can feed the same voice-server stream in parallel. It does not need anything from the provider.
- **Timing fits push-to-talk.** The bridge knows the release moment itself and can call `FinishAndIdentify` before it sends `commit` + `response.create`. Today the pipeline only waits 200 ms after ASR final; there is no ASR final to wait for in a realtime turn.
- **The three consumers do not all survive fixed instructions.**
  - Gate: the bridge can decide before `response.create`, so `identified_only` is still possible.
  - Prompt text and TTS voice: both are per-turn today. Under "instructions and tools fixed for the connection" (#4), the identity of the current speaker cannot reach `instructions`, and the output voice is a session setting. How (or whether) the result reaches the model would need something #4 did not cover, such as a text item before `response.create`. That is unverified for Qwen Omni and is a question for #6.
- Prompt text that hackers365 injects is hardcoded Chinese ("基于声纹识别到对话人信息", `llm.go:L1195`). The voice loop here is Italian.

### 3.5 Does voiceprint depend on voice clone?

No. Nothing under `internal/domain/speaker` or `speaker_group.go` refers to cloning (searched `clone|复刻`). The link is one-way: a speaker group can carry `voice` / `voice_model_override`, which the manager fills from `models.VoiceClone` when the chosen voice is a clone (`hackers365:manager/backend/controllers/admin.go:L65-70, L220-297`; `session.go:L1858-1865`), and only the pipeline TTS reads it.

---

## 4. mem0 (long memory)

### 4.1 Mode and selection

- Three modes per agent: `none`, `short`, `long` (`hackers365:internal/data/client/client.go:L50-66`, `types.go:L70`). Redis provider hardcodes `short` (`redis/userconfig.go:L58`). The manager column defaults to `short` (`hackers365:manager/backend/models/models.go:L63`).
- Only `long` builds a real provider; the provider type and JSON config come from `DeviceConfig.Memory` (`hackers365:internal/app/server/chat/session.go:L429-458`). Interface: `AddMessage, GetMessages, GetContext, Search, Flush, ResetMemory` (`hackers365:internal/domain/memory/base.go:L15-37`). Siblings: memobase, memos, nomemo (`base.go:L41-69`).
- `short` is not mem0: history comes from the redis/manager dialogue path ([manager seams](./hackers365-manager-seams.md) section 5).

### 4.2 When it runs

| Path | When | Cite |
|---|---|---|
| Init | at listen start (`InitAsrLlmTts`): build provider, call `GetContext(id, 500)`. mem0's `GetContext` returns `""`, so `MemoryContext` stays empty | `session.go:L429-458`, `hackers365:internal/domain/memory/mem0/mem0_client.go:L233-235` |
| Read | every LLM request: `MemoryProvider.Search(id, userMessage.Content, …)` with **this turn's ASR text**; the result is appended to the system prompt as "历史关联信息" | `llm.go:L1202-1210` |
| Write | every message published on `TopicAddMessage` (user, assistant and tool roles) is handed to `MemoryProvider.AddMessage` by the message worker, off the turn's critical path | `hackers365:internal/app/server/message_handle.go:L118-139, L143-160`; publishers `session.go:L1353-1365`, `llm.go:L1105-1140` |
| Session end | `Flush` on `TopicSessionEnd`; mem0's `Flush` is a no-op | `hackers365:internal/app/server/event_handle.go:L186-210`, `mem0_client.go:L260-262` |

`AddMessage` sends **one message** per call with `AsyncMode: true` (`mem0_client.go:L155-170`). The tool role is not filtered before mem0 (`message_handle.go:L143-160`, tool messages published at `llm.go:L1105-1118`). The mem0 key is the agent id, or the device id if there is none (`client.go:L199-204`).

### 4.3 What it stores and where

- Nothing local. The client is `github.com/hackers365/mem0-go`, default `https://api.mem0.ai`, overridable with `base_url`; `api_key` is required (`mem0_client.go:L45-140`, default at `L98`). The mem0-go source and mem0's own storage were not read.
- Config keys: `api_key`, `base_url`, `enable_search` (default true), `search_threshold` (0.5), `search_topk` (3) (`mem0_client.go:L46-77`). Search ignores the caller's `topK` and uses `SearchTopk` (`mem0_client.go:L241-250`).
- The client is a process-wide singleton built inside `configOnce.Do` (`mem0_client.go:L45`), so the first agent's config wins for every agent until restart.

### 4.4 What a realtime provider changes

- **Write needs text.** `AddMessage` takes `schema.Message` with text. A realtime provider returns a user transcript and an assistant transcript (#4), so the write path is satisfiable if the bridge publishes the same `AddMessageEvent` for both, with `Msg`, `MessageID`, `Timestamp` and the `ClientState`. That one event also feeds chat history (`message_handle.go:L118-139`). A bridge that skips it loses history and mem0 together.
- **Read needs this turn's text before the model answers.** In a realtime turn the model answers from audio on `response.create`; the transcript arrives alongside or after. So `Search(userMessage.Content)` cannot run before the answer, and per-turn injection into a fixed `instructions` is not available (#4).
- Available shapes, from what exists in code: (a) a memory-search tool declared at connect, with the query written by the model (mem0's `Search` already takes a query string); (b) a one-time fetch at connect for static context. Shape (b) needs new code because mem0's `GetContext` returns `""`. Shape (a) needs a new local tool because no `search_memory` tool exists in `local_mcp_tool.go`. Neither is built.
- The prompt strings are hardcoded Chinese (`llm.go:L1182, L1209`).

---

## 5. RAGFlow (knowledge base)

### 5.1 When it runs

- As a **local MCP tool** `search_knowledge`, registered with the other local tools (`hackers365:internal/app/server/chat/local_mcp_tool.go:L64-69`), so it comes back from `mcp.GetToolsByDeviceIdWithTransport` with device and global tools (`session.go:L1679-1687`). The tool is removed for the turn when the agent has no usable KB (`session.go:L1689-1694, L1725`).
- The LLM decides whether and when to call it. There is no retrieval before the model. A routing policy is appended to the system prompt on every request, listing up to 8 KB ids, names and descriptions and rules for when to call (`llm.go:L1213, L1251-1291`). It depends only on `DeviceConfig.KnowledgeBases`, so it is static per session.
- The handler parses `query`, `top_k` (default 5), optional `knowledge_base_ids`, then calls `ChatSessionOperator.LocalMcpSearchKnowledge` (`local_mcp_tool.go:L376-435`), which calls `rag.Search` with `clientState.DeviceConfig.KnowledgeBases` (`hackers365:internal/app/server/chat/session_mcp_tool.go:L189-194`). The operator is read from `ctx.Value("chat_session_operator")` (`local_mcp_tool.go:L376-435`, propagated at `tool.go:L147-149`).
- Timeouts: total 2.5 s and per-KB 2.5 s by default, max 8 parallel (`hackers365:internal/domain/rag/manager.go:L20-24`).

### 5.2 What it stores and where

- The index lives in the external provider. RAGFlow: `POST {base_url}/api/v1/retrieval` with `Authorization: Bearer <api_key>` and body `question, dataset_ids, top_k, page_size, similarity_threshold (default 0.2), vector_similarity_weight (0.3), keyword, highlight` (`hackers365:internal/domain/rag/ragflow_searcher.go:L33-50, L55, L164-181`). One request per dataset (`ExternalKBID`). Dify and WeKnora sit behind the same `Searcher` interface (`hackers365:internal/domain/rag/interface.go`, `manager.go:L134-146`).
- Provider settings are read from viper `knowledge.providers` (`manager.go:L151`), not from `UserConfigProvider`. The shipped `config/config.yaml` has no `knowledge` section; the manager delivers it in its `knowledge_search` system config ([manager seams](./hackers365-manager-seams.md) section 5.4).
- KB rows (`KnowledgeBaseRef{ID, Name, Description, Provider, ExternalKBID, RetrievalThreshold, Status}`, `types.go:L44-53`) come from the manager per agent (`manager.go:L112, L204`). Upload, text documents and async sync to the provider are manager features (`hackers365:doc/knowledge_base.md:L101-172, L223-248`). The redis provider never sets them, so without a manager-shaped provider `search_knowledge` is removed.

### 5.3 What a realtime provider changes

- **No dependency on ASR text.** The query is the model's own tool argument (`local_mcp_tool.go:L376-395`), and the routing text needs no turn data.
- Needs: (1) `search_knowledge` in the session's tool declarations (already produced by `GetToolsByDeviceIdWithTransport`; the same KB-empty filter applies); (2) the routing policy string in session instructions (static, fits "fixed for the connection"); (3) the id-keyed tool round trip #4 already lists.
- Coupling to fix: the handler needs a `ChatSessionOperator` in `ctx`. A bridge that replaces `ChatManager` must provide an equivalent, or call `rag.Search` directly with the agent's KB refs.
- The 2.5 s search budget sits inside the provider's tool round trip, with no audio playing meanwhile. The routing policy also forbids the assistant to mention retrieval (`llm.go:L1294`), which suits speech.

---

## 6. MQTT

### 6.1 What "MQTT" is in hackers365

It is a device **transport**, an alternative to WebSocket, not a separate notification feature. Control JSON goes over MQTT and audio over UDP with AES-CTR ([xiaozhi-device-contract.md](./xiaozhi-device-contract.md) section 2.5, 3.1; `hackers365:doc/mqtt_udp.md:L5-16, L187-268`).

- Three deployments: embedded broker (mochi-mqtt) started when `mqtt_server.enable` (`hackers365:internal/app/server/app.go:L63-70`, `internal/app/mqtt_server/mqtt_server.go:L24-92`); an external broker such as EMQX (`doc/mqtt_udp.md`, "对接外部 MQTT Broker"); or the official `xiaozhi-mqtt-gateway` bridging to `/xiaozhi/mqtt_udp/v1/` (`doc/mqtt_bridge.md`).
- The device server side is a client of the broker (`app.go:L117-150`, gated by `mqtt.enable`). Devices learn about MQTT from the OTA response's `mqtt` object (`doc/mqtt_bridge.md`, OTA section).
- Broker ACL: devices may publish only to `device-server` and subscribe only to `/p2p/device_sub/<mac>` (`hackers365:internal/app/mqtt_server/device_hook.go:L32-60`). Connect auth is HMAC of the OTA-issued credentials when `mqtt_server.enable_auth` and `signature_key` are set, else an AES fallback with a fixed key; there is also a super-admin with a default password (`hackers365:internal/app/mqtt_server/auth_hook.go:L28-72`, `admin_config.go:L10-11`).

### 6.2 When it runs and what it stores

- It carries the same turn that WebSocket carries: both call `App.OnNewConnection` (`app.go:L145` and `L181`). It runs beneath the seam #3 chose.
- Stores nothing durable. The embedded broker forces clean sessions for devices (`device_hook.go:L63-70`). Lifecycle events (online/offline) go on a reserved topic and feed transport creation and MCP warm-up (`doc/mqtt_udp.md:L187-232, L254-268`). All settings are local viper (`mqtt`, `mqtt_server`, `udp`, `ota`; `config/config.yaml:L68, L78, L95, L354`). Nothing here uses `UserConfigProvider` ([manager seams](./hackers365-manager-seams.md) section 11).

### 6.3 The "push" part

The server can start speech on a device without a device turn:

- `POST /admin/inject_msg` on the WebSocket server, or the manager event path (`hackers365:internal/app/server/websocket/websocket_server.go:L116, L242-280`, `app.go:L512-560`). Fields: `device_id, message, skip_llm, auto_listen`. The handler shown has no auth check.
- `ChatManager.InjectMessage` (`hackers365:internal/app/server/chat/chat.go:L1203-1220`): with `skip_llm` it queues the text on the **TTS manager**; otherwise it feeds the text in as if it were the user's ASR result (`AddAsrResultToQueueWithOptions`).
- On the MQTT transport it first sends a `speak_request` and waits for the device's `hello`, because the UDP session may not exist (`chat.go:L1222-1262`, `server_transport.go:L108`). On WebSocket it skips that step (`chat.go:L1225-1228`).
- Other callers use the same method: OpenClaw offline replay (`app.go:L395-415`).

### 6.4 What a realtime provider changes

- **No ASR-text dependency.**
- The transport itself needs nothing from the provider, provided the bridge stays at `IConn`; the bridge must accept the transport-specific bootstrap (`hello`, UDP session) the same way `ChatManager` does.
- `InjectMessage` is inside `ChatSession`, which the bridge replaces. The two modes need different things from the bridge: `skip_llm` needs a TTS voice independent of a realtime response (a realtime provider only speaks as part of a response), and the other mode needs a way to put text into the conversation as user input. #4 does not cover either. The `speak_request` handshake only applies on MQTT.

---

## 7. Placement recommendation

All four stay outside the provider and are driven by the bridge at the `IConn` / `OnNewConnection` cut. Reasons are in sections 3-6.

| Part | Where it goes | Consequence |
|---|---|---|
| Voiceprint | A sidecar the bridge feeds with the same decoded 16 kHz PCM it sends to the provider; the bridge finalises it at release, before `commit` + `response.create` | Works in manual mode without ASR. Only the chat gate maps onto fixed instructions; prompt and voice effects need a decision in #6 |
| mem0 write | Keep as the `TopicAddMessage` subscriber. The bridge publishes user and assistant transcripts as events | Free once transcripts are published; also keeps history |
| mem0 read | A tool the model calls, or a one-time fetch at connect. Not a per-turn injection | New code either way (section 4.4) |
| RAGFlow | Keep the `rag` package and the `search_knowledge` tool unchanged; put the routing text in session instructions | Only the `ChatSessionOperator` coupling changes |
| MQTT | Untouched, under `IConn`. `inject_msg` becomes a bridge API | Two inject modes need provider capabilities not researched yet |

For the fork, the manager-fed inputs of the four parts are `UConfig.VoiceIdentify`, `SpeakerChatMode`, `Memory`, `MemoryMode` and `KnowledgeBases`, plus system config `voice_identify` and `knowledge.providers`. The redis provider fills none of them, so the PocketBase `UserConfigProvider` must carry them if these parts are to run.

## 8. Open questions / not verified

- The `asr_server` submodule is empty in the clone, so the voiceprint engine (sherpa-onnx, Qdrant, the `identify_ws` protocol, the 200 ms debounce of `PeekAndIdentify` on its side) is known from the doc and the Go client only. The doc and code disagree on the WebSocket path and the default threshold (section 3).
- `hackers365/mem0-go` and mem0's storage were not read. Whether per-message `Add` with `AsyncMode` distils into good memories is not checked.
- Whether Qwen Omni accepts a text item (system or user) in the conversation, or a spoken-text-only response, is not documented in #4. Voiceprint prompt effects, mem0 shape (b), and both inject modes depend on it.
- The session-level pass-through of `ChatSessionOperator` in tool `ctx` was read only at the handler and at `tool.go:L147`; where the value is first set was not traced.
- Not runtime-verified: static reading of `21f1a2e` only.
