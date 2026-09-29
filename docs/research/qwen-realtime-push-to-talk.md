# Qwen realtime push-to-talk session contract (issue #4)

Research ticket for [mcpll/esp32-go-server#4](https://github.com/mcpll/esp32-go-server/issues/4). Findings only: no bridge design and no Go code beyond naming existing types. Every claim cites a source ID from section 1. Anything marked **Inferred** or **Unverified** is not stated directly in a primary source.

## 1. Sources read

All web pages accessed **2026-09-28**. The "Last Updated" dates are as shown on each page.

**Alibaba Cloud Model Studio (official)**

| ID | URL | Page last updated |
| --- | --- | --- |
| Q-RT | https://www.alibabacloud.com/help/en/model-studio/realtime ("Qwen-Omni-Realtime") | Sep 28, 2026 |
| Q-CE | https://www.alibabacloud.com/help/en/model-studio/client-events | Sep 28, 2026 |
| Q-SE | https://www.alibabacloud.com/help/en/model-studio/server-events | Sep 28, 2026 |
| Q-FC | https://www.alibabacloud.com/help/en/model-studio/qwen-function-calling (section "Qwen-Omni-Realtime series") | Sep 28, 2026 |
| Q-VL | https://www.alibabacloud.com/help/en/model-studio/omni-voice-list | May 22, 2026 |
| Q-OM | https://www.alibabacloud.com/help/en/model-studio/qwen-omni | Sep 18, 2026 |
| Q-QA | https://help.aliyun.com/en/model-studio/fun-audiochat-realtime-websocket-api (Qwen-Audio Realtime WebSocket guide, Alibaba Cloud help center). Used for the domain-migration notice, and as a **sibling-model analog** for cancellation, which the Omni pages do not describe. | not shown |
| Q-QC | https://docs.qwencloud.com/developer-guides/tool-calling/function-calling (Alibaba QwenCloud docs). Corroboration only: its `qwen3.5-omni-plus-realtime` sample still uses `wss://dashscope-intl.aliyuncs.com/api-ws/v1/realtime`. | not shown |

**Google Gemini (official)**

| ID | URL | Page last updated |
| --- | --- | --- |
| G-OV | https://ai.google.dev/gemini-api/docs/live ("Gemini Live API overview") | not captured |
| G-GD | https://ai.google.dev/gemini-api/docs/live-guide ("Live API capabilities guide") | not captured |
| G-API | https://ai.google.dev/api/live ("Live API - WebSockets API reference") | 2026-09-04 |
| G-TL | https://ai.google.dev/gemini-api/docs/live-tools ("Tool use with Live API") | 2026-09-15 |

**Repositories**

- **H**: `hackers365/xiaozhi-esp32-server-golang` at `21f1a2e71ff383723f1464ea9b137016e6feab8d` (commit date 2026-07-23), read from the existing clone at `.scratch/hackers365/` (its origin is `https://github.com/hackers365/xiaozhi-esp32-server-golang`). Paths below are relative to that repo root.
- **F**: `78/xiaozhi-esp32` firmware at `8ce50d27cd7c72c777673f46cbfc3ef7454d1b5c`, read from `.scratch/firmware/`. Used only to confirm what the device sends by default.

## 2. Model names, endpoint, auth

- **Current recommended model.** Q-RT "Model selection" says to use `qwen3.8-omni-flash-realtime` for realtime audio/video. Q-RT "WebRTC" calls `qwen3.5-omni-plus-realtime` the "previous-model example". The issue's `qwen3.5-omni-plus-realtime` still appears throughout Q-RT, Q-CE, Q-SE and Q-FC. The snapshot `qwen3.5-omni-plus-realtime-2026-03-15` is listed in QwenCloud's model table; that was seen only in a search result, so it is **Unverified** on alibabacloud.com.
- **Endpoints (WebSocket).**
  - Q-RT "Native WebSocket" documents only workspace domains: Singapore `wss://{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com/api-ws/v1/realtime`, Beijing `wss://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/api-ws/v1/realtime`.
  - Q-RT: "`qwen3.8-omni-flash-realtime` requires the workspace-specific endpoint below."
  - Q-QA (top notice): the migration goes "from `dashscope-intl.aliyuncs.com` to `{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com` … The existing domains remain fully functional."
  - Q-QC still shows `wss://dashscope-intl.aliyuncs.com/api-ws/v1/realtime` with `qwen3.5-omni-plus-realtime`.
  - Conclusion: `dashscope-intl` is still valid for the 3.5 series. 3.8 requires the workspace domain.
- **Model selection** is done with the query parameter `?model=<name>` (Q-RT "Native WebSocket").
- **Auth** is the header `Authorization: Bearer $DASHSCOPE_API_KEY` (Q-RT "Native WebSocket"). Q-QA "Request headers" adds that auth is checked at the handshake, failures return HTTP 401/403, and `X-DashScope-WorkSpace` is an optional header. Singapore and Beijing need separate API keys (Q-RT intro).
- **Transport for push-to-talk must be WebSocket.** Q-RT §3: "WebRTC only supports server-side VAD mode … Manual mode is not supported." Q-RT "Interaction flow > VAD mode" repeats this.

## 3. Session lifecycle

1. Connect. The server sends `session.created` (Q-SE "session.created"; Q-RT minimal example waits for it).
2. Client sends `session.update`. The server replies `session.updated`, or `error` if parameters are invalid (Q-CE "session.update"; Q-SE "session.updated").
3. Audio in, then commit, then `response.create` (manual mode). See section 4.

`session.update` fields relevant to this issue (Q-CE "session.update", "Audio configuration", "Output voice"):

| Field | Notes |
| --- | --- |
| `modalities` | `["text"]` or `["text","audio"]` (default). `["audio"]` alone is rejected (Q-SE "error" example). |
| `instructions` | System message. |
| `voice` | Default `Tina` for Qwen3.5-Omni-Realtime. For 3.8, `session.audio.output.voice` takes precedence over `session.voice`. |
| `input_audio_format` / `output_audio_format` | Only `pcm`: 16 kHz in, 24 kHz out (legacy fields). |
| `audio.input.format.{type,sample_rate}` | For `qwen3.5-omni-plus/flash-realtime`: `pcm`/`wav`, input rate 8000/16000 (default)/24000/48000. Can only be set while IDLE, before the first audio. |
| `audio.output.format.{type,sample_rate}` | Output rate 8000/16000/24000 (default)/48000. Set at session start. |
| `turn_detection` | `null` disables VAD ("trigger model responses manually"). **If omitted, VAD is enabled with defaults**, so manual mode must send an explicit `null`. |
| `tools` | Array of `{type:"function", function:{name, description, parameters}}`. |
| `enable_search` | Mutually exclusive with `tools`. |
| `temperature`, `top_p`, `top_k`, `max_tokens`, `repetition_penalty`, `presence_penalty`, `seed` | Sampling parameters. |

- **`tool_choice` does not exist.** Q-FC: "The Qwen-Omni-Realtime series does not support the `tool_choice` and `parallel_tool_calls` parameters."
- **Input transcription.** Q-SE `session.created`/`session.updated` echo `input_audio_transcription.model` = `qwen3-asr-flash-realtime`, "Not configurable". Q-RT's VAD-mode table says input transcription "requires enabling input_audio_transcription in session.update", and the SDK example passes `enable_input_audio_transcription=True` (Q-RT "Manual mode" Python SDK). The raw-WebSocket field shape is **not** in Q-CE; see section 12.
- **Constraints.** From the sibling Qwen-Audio page (Q-QA "Mode operation constraints"): "`turn_detection` and `input_audio_format` can only be changed before the first audio is sent (IDLE state)". Q-CE states the same for `audio.input.format`.
- **Limits** (Q-RT "Limitations"):
  - One WebSocket session lasts at most **120 min**.
  - `qwen3.5-omni-plus-realtime` keeps 100 audio turns / 600 s of audio in context and drops the oldest beyond that.
  - Web search and tool calling are mutually exclusive.
  - 3.8 total input is capped at 196 608 tokens.

## 4. Push-to-talk mapping

Q-RT "Interaction flow > Manual mode": "Set `session.turn_detection` … to `null` … The client sends `input_audio_buffer.commit` and `response.create` to request a response. Suitable for push-to-talk". Q-QA "push-to-talk mode" describes the same flow for Qwen-Audio.

| Device action (Xiaozhi) | Qwen client events | Source |
| --- | --- | --- |
| Press: `listen` `state:"start"` `mode:"manual"` (F `main/protocols/protocol.cc` L82-94) | If a response is in progress, send `response.cancel` (see barge-in row). Optionally send `input_audio_buffer.clear` (server replies `input_audio_buffer.cleared`) to drop stale audio. Then stream `input_audio_buffer.append {audio: base64}`. | Q-CE "response.cancel", "input_audio_buffer.clear", "input_audio_buffer.append" |
| Release: `listen` `state:"stop"` (F `protocol.cc` L96-100) | `input_audio_buffer.commit`, wait for `input_audio_buffer.committed`, then `response.create`. | Q-CE "input_audio_buffer.commit": "Committing the buffer does not trigger a model response"; Q-CE "response.create"; Q-RT minimal example waits for `committed` before `response.create`. |
| Empty press (no audio) | Do not commit. "If the buffer is empty, the server returns an error." | Q-CE "input_audio_buffer.commit" |
| Barge-in / abort (`type:"abort"`, F `protocol.cc` L66-73) | `response.cancel`. "If no response is in progress, the server returns an error." | Q-CE "response.cancel" |

- **What changes versus `server_vad`.** In VAD mode the server emits `input_audio_buffer.speech_started`/`speech_stopped`, commits the buffer itself, and starts a response automatically. `commit` and `response.create` are then unnecessary (Q-RT VAD-mode flow; Q-CE "input_audio_buffer.commit", "response.create"). In manual mode none of the speech events are listed; the only server acknowledgement of user input is `input_audio_buffer.committed` (Q-RT manual-mode table).
- **Cancel result (analog only).** Q-QA, for Qwen-Audio push-to-talk barge-in: "The client sends `response.cancel` … the server returns `response.done` (with status `cancelled` and reason `client_cancelled`)". Q-QA also says `response.create` is "Not allowed while a response is generating", so a new turn needs cancel first. The Omni pages do not state either behaviour. **Inferred** to apply to Omni.
- **Truncation.** No `conversation.item.truncate` or equivalent appears in Q-CE. There is no documented way to tell the server how much of a cancelled reply was actually played. **Unverified / absent.**
- **Images/video.** `input_image_buffer.append` exists; at least one audio append must come first, and images are committed with the audio (Q-CE "input_image_buffer.append"). Not needed here.

## 5. Audio in and out

- **Input.** Base64-encoded audio in `input_audio_buffer.append.audio` (Q-CE; Q-RT §3). The format is PCM, 16-bit, mono, 16 kHz by default, and must be fixed before the first audio (Q-CE "Audio configuration"). Q-RT's minimal example requires "16 kHz, 16-bit little-endian, mono PCM … without a file header" and sends 3200-byte (100 ms) chunks. No minimum or maximum chunk size is documented.
- **Output.** `response.audio.delta.delta` is "Incremental audio data, Base64-encoded" (Q-SE). The default is PCM 24 kHz mono 16-bit (Q-CE `output_audio_format`). For 3.5 plus/flash, `audio.output.format.sample_rate` can be set to 16000 (Q-CE "Audio configuration"), which would match the device's default downlink rate (section 9).
- **Billing.** Input is 7 tokens/s and output 12.5 tokens/s for Qwen3.5-Omni-Realtime. "Audio durations of less than 1 second are billed as 1 second" (Q-RT "Billing").

## 6. Output events

Server events for text+audio output (Q-RT §4 and manual-mode table; Q-SE):

- `response.created`, `response.output_item.added`, `conversation.item.created`, `response.content_part.added`
- `response.audio.delta` / `response.audio.done`: audio, base64
- `response.audio_transcript.delta` / `.done`: assistant transcript
- `response.text.delta` / `.done`: text-only modality
- `response.content_part.done`, `response.output_item.done`
- `response.done`: carries `response.status`, `output[]` (message or function_call), and `usage` with `total_tokens`, `input_tokens`, `output_tokens`, and `*_tokens_details.{text_tokens,audio_tokens}`

User transcript, when input transcription is enabled (Q-SE):

- `conversation.item.input_audio_transcription.delta` carries `text` (confirmed) + `stash` (draft), plus `language` and `emotion`.
- `.completed` carries `transcript`, and is marked "for reference only".
- `.failed` reports transcription failures.

Errors (Q-SE "error"): `{type:"error", error:{type, code, message, param}}`. Q-QA "Error handling" (sibling model): `invalid_request_error` keeps the connection open, while `server_error` terminates it.

## 7. Function calling

- **Declare tools.** Put them in `session.update.session.tools` (Q-CE; Q-FC "Qwen-Omni-Realtime series"). Supported for 3.8-flash, 3.5-plus and 3.5-flash realtime (Q-FC).
- **The call.** Streamed as `response.function_call_arguments.delta` (fields `call_id`, `delta`), completed by `response.function_call_arguments.done` with `call_id`, `name`, and `arguments` as a JSON string (Q-SE). Q-SE says to use the `arguments` from `done`, not the concatenated deltas. The function call also appears as a `function_call` item in `response.output_item.added/done`, `conversation.item.created`, and `response.done.output[]` (Q-SE).
- **Return the result.** Send `conversation.item.create` with `item:{type:"function_call_output", call_id, output:<string>}`, then `response.create` to get the spoken answer (Q-CE "conversation.item.create", "response.create"; Q-FC Phase 2). The model does not continue on its own after a tool result.
- **Limits.**
  - No `tool_choice` or `parallel_tool_calls` (Q-FC).
  - `tools` and `enable_search` are mutually exclusive (Q-CE; Q-RT "Limitations").
  - Server-side MCP tools (`type:"mcp"`) exist only on 3.8 (Q-CE "MCP tool configuration").
- **Where tools execute today.** In H, the device's own tools are exposed over MCP. Device MCP messages are routed through `mcp.HandleDeviceIotMcpMessage` (H `internal/app/server/chat/chat.go` L681). A bridge would have to proxy Qwen function calls to that MCP path. **Inferred**; not traced further.

## 8. Languages (Italian)

- **Output / voices: yes.** Q-VL "Qwen3.5-Omni and Qwen3.5-Omni-Realtime" lists each voice's languages. For example, `Tina` (the default voice) and `Ethan` both list: "Chinese (Mandarin), English, French, German, Russian, **Italian**, Spanish, Portuguese, Japanese, Korean, Thai, Indonesian, Arabic, Vietnamese, Turkish, Finnish, Polish, Hindi, Dutch, Czech, Urdu, Tagalog, Swedish, Danish, Hebrew, Icelandic, Malay, Norwegian, Persian" (29 languages). Q-RT "Model selection" gives the counts for Qwen3.5-Omni-Realtime: "speech recognition for 113 languages and dialects and speech generation for 36 languages and dialects", and "55 voices, including 47 multilingual voices and 8 dialectal voices."
- **Input: yes, from the Qwen3.5-Omni family list.** Q-OM "Other models and specifications", row Qwen3.5-Omni: "113 — 74 languages and 39 dialects. Languages: Chinese, English, German, French, **Italian**, …". Q-RT gives the same 113 count for the realtime variant but no list, so the realtime list is **inferred** to be identical.
- **No language parameter.** Omni `session.update` has none (Q-CE). Steering to Italian would go through `instructions` (**Inferred**). H's ASR client sends `input_audio_transcription.language` (H `internal/domain/asr/aliyun_qwen3/protocol.go` L28-30), but that field is not documented for Omni.

## 9. Device ↔ Qwen audio conversion

**What the device sends.**

- Default hello `audio_params` from the Xiaozhi firmware: `{"format":"opus","sample_rate":16000,"channels":1,"frame_duration":60}` (F `main/protocols/websocket_protocol.cc` L212-217, `mqtt_protocol.cc` L400-405, `main/audio/audio_service.h` L40 `OPUS_FRAME_DURATION_MS 60`).
- The device adopts the server hello's `audio_params.sample_rate`/`frame_duration` for its decoder (F `websocket_protocol.cc` L242-250).
- H rejects a hello without `audio_params` and stores them as `ClientState.InputAudioFormat` (H `internal/app/server/chat/chat.go` L455-473).
- H's defaults are `SampleRate=16000, Channels=1, FrameDuration=60, Format="opus"` (H `internal/data/audio/audio.go` L3-15, type `AudioFormat`).
- H's server hello returns `OutputAudioFormat` (H `chat.go` L596-614). That is 16 kHz / 60 ms Opus by default, overridden to **24000 Hz / 20 ms** when the TTS provider is `xiaozhi` (H `chat.go` L279-291).

**Uplink (device → Qwen or Gemini)**

1. Opus 16 kHz mono, 60 ms frames.
2. Opus decode at the hello rate. H already does this with `audio.AudioProcesser` (`GetAudioProcesser`, `DecoderFloat32`; H `internal/domain/audio/audio_handler.go` L17-48), called from H `internal/app/server/chat/asr.go` L130-133 and L235. The library is `gopkg.in/hraban/opus.v2` (cgo libopus).
3. float32 to PCM16 little-endian via `util.Float32ToPCMBytes` (H `internal/util/voice.go` L20-25).
4. Base64, then `input_audio_buffer.append`.

**No resampling is needed** on the uplink: Qwen's default input is 16 kHz (Q-CE) and Gemini's native input is 16 kHz (G-GD "Audio formats"). One 60 ms frame is 960 samples, or 1920 bytes of PCM16.

H's manual listen mode already skips VAD: `needVad := !(state.Asr.AutoEnd || state.ListenMode == "manual")` (H `asr.go` L151).

**Downlink (Qwen or Gemini → device)**

1. Base64-decode `response.audio.delta` to get PCM16.
2. Re-frame into fixed Opus frames at the device's output rate and duration.
3. Opus encode.

H already has this pipeline in `util.AudioDecoder`:

- `CreateAudioDecoderWithSampleRate(ctx, reader, outChan, frameDurationMs, "pcm", targetSampleRate)`, followed by `WithFormat(beep.Format{SampleRate: <source>})`, routes to `RunWavDecoder(isRaw=true)` (H `internal/util/audio_utils.go` L157-192, L911-1011).
- That path buffers `frameSize` samples, resamples with `ResampleLinearFloat32` when source ≠ target, and encodes with `opus.NewEncoder`.
- Precedent: the `edge_offline` TTS feeds raw 24 kHz PCM through exactly this path (H `internal/domain/tts/edge_offline/edge_offline.go` L248-259).

Resampling is **linear interpolation** (H `internal/util/voice.go` L64-80), with no low-pass filter. There are two ways to avoid it:

- **Qwen:** request `audio.output.format.sample_rate: 16000` (3.5 series, Q-CE).
- **Gemini:** output is fixed at 24 kHz (G-OV "Technical specifications"; G-GD "Audio formats"). Announce 24 kHz in the server hello, as H already does for `xiaozhi` TTS (H `chat.go` L286-289).

**Context on H.** H has no speech-to-speech provider. `chat.realtime_mode` only selects the interruption trigger: 1 = VAD duration (H `asr.go` L325-337), 2 = ASR result (L789-798), 3 = speaker peek/voiceprint (L395-450). Listen start in `auto`/`manual` calls `StopSpeakingWithReason` (H `internal/app/server/chat/session.go` L1227-1231); listen stop calls `OnManualStop` (L1243-1256).

## 10. Is `internal/domain/asr/aliyun_qwen3` reusable for the connection?

**Partly, as a pattern. The types are not reusable.** H files are under `internal/domain/asr/aliyun_qwen3/`.

What matches the Omni contract:

- **WebSocket library:** `github.com/gorilla/websocket` v1.5.3 (H `go.mod` L24; `engine.go` L18, L83 `websocket.DefaultDialer`).
- **URL and auth:** `?model=` query parameter plus `Authorization: Bearer <key>`, with `DASHSCOPE_API_KEY` as the fallback (`engine.go` L97-116; `config.go` L66-68). This matches Q-RT.
- **Append event:** `NewAudioAppendEvent` base64-encodes PCM16 into `{type:"input_audio_buffer.append", audio}` (`protocol.go` L105-112). Same shape as Q-CE.
- **Commit and manual null:** `NewAudioCommitEvent` (`protocol.go` L114-120). `Session.TurnDetection` has no `omitempty` (`protocol.go` L24), so manual mode serialises `"turn_detection": null` (L81-89), which Q-CE requires.
- **Handshake sequencing:** dial, `session.update`, wait for `session.updated`, then stream (`engine.go` L405-445, L578-603).

What does not fit:

- **Default URL** is Beijing, `wss://dashscope.aliyuncs.com/api-ws/v1/realtime`, with model `qwen3-asr-flash-realtime` (`config.go` L10-20). The international endpoint needs `dashscope-intl` or a workspace domain.
- **`Session` struct** (`protocol.go` L19-25) has no `instructions`, `voice`, `tools`, `output_audio_format`, or `audio` object. `modalities` is hard-coded to `["text"]` (L75). It uses a top-level `sample_rate`, which is not an Omni field.
- **`ServerEvent`** (`protocol.go` L40-50) has no `delta`, `response`, `call_id`, `name`, or `arguments` fields. The receive loop handles only ASR events (`engine.go` L291-368).
- **ASR-only events.** It sends `session.finish` and waits for `session.finished` (`protocol.go` L122-128; `engine.go` L497-526). Neither appears in Q-CE/Q-SE for Omni. It also listens for `conversation.item.input_audio_transcription.text`, which is not in Q-SE.
- **Undocumented header.** It adds `OpenAI-Beta: realtime=v1` (`engine.go` L116). No Alibaba page read here documents it.
- **Lifecycle** is one recognition per `StreamingRecognize` call, serialised by `taskMu`, with float32 channel input and text-only `types.StreamingResult` output (`engine.go` L88-130). It enforces `SampleRate == 16000` (L63-72). A full-duplex, long-lived session with audio and tool output does not fit the ASR interface.

Types worth naming for reuse or reference: `aliyun_qwen3.AliyunQwen3ASR`, `aliyun_qwen3.Config`, `aliyun_qwen3.ClientEvent`, `aliyun_qwen3.Session`, `aliyun_qwen3.ServerEvent`, `audio.AudioProcesser`, `util.AudioDecoder`, and `types_audio.AudioFormat` (in `internal/data/audio`).

## 11. Gemini Live comparison and shared-interface obligations

**Gemini Live facts**

- **Endpoint.** `wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent` (G-API "WebSocket connection").
- **Models.** `gemini-3.8-live` is the default; `gemini-3.8-live-extended-thinking` also exists; `gemini-3.1-flash-live-preview` is legacy (G-GD "Model comparison"). Setup takes `model: "models/{model}"` (G-API "BidiGenerateContentSetup").
- **Auth for server-to-server raw WebSocket.** The pages read do not state the API-key parameter. Ephemeral tokens go in the `access_token` query parameter or `Authorization: Token …` (G-API "Ephemeral authentication tokens").
- **Config is set once.** The first message is `setup` with `generationConfig` (`responseModalities`, `speechConfig`), `systemInstruction`, `tools`, `realtimeInputConfig`, and `input/outputAudioTranscription`. The client must wait for `setupComplete`. "You cannot update the configuration while the connection is open" (G-API "Session configuration", "BidiGenerateContentSetup"). Qwen, by contrast, allows most `session.update` changes mid-session.
- **Manual turn.** Set `realtimeInputConfig.automaticActivityDetection.disabled: true`, then send `realtimeInput.activityStart`, `realtimeInput.audio`, and `realtimeInput.activityEnd`. `activityStart`/`activityEnd` "can only be sent if automatic … activity detection is disabled", and `audioStreamEnd` is not used in that mode (G-API "BidiGenerateContentRealtimeInput"; G-GD "Disable automatic VAD"). With manual VAD, the server adds no pre-speech buffer and no silence tolerance: it "acts immediately on your `activityEnd`" (G-GD "Best practices for manual (client-side) VAD").
- **Audio.**
  - In: raw 16-bit LE PCM, natively 16 kHz. Other rates are resampled if declared via the mime type `audio/pcm;rate=16000`.
  - Out: always 24 kHz 16-bit LE PCM (G-GD "Audio formats"; G-OV "Technical specifications").
  - Native-audio models support only the `AUDIO` response modality; use output transcription to get text (G-GD "Limitations").
- **Output events** arrive in `serverContent`:
  - `modelTurn` carries audio as inline data.
  - `inputTranscription` / `interimInputTranscription` / `outputTranscription` carry transcripts.
  - `generationComplete` and `turnComplete` mark the end of generation and turn.
  - `interrupted` means "stop and empty the current playback queue" (G-API "BidiGenerateContentServerContent").
  - `usageMetadata` reports token usage.
- **Barge-in / cancel.** With the default `activityHandling` of `START_OF_ACTIVITY_INTERRUPTS`, a new `activityStart` cuts off the current response (G-API "ActivityHandling"). A `clientContent` message also "will interrupt any current model generation" (G-API "BidiGenerateContentClientContent"). No standalone cancel message is documented.
- **Tools.**
  - Declare `tools:[{functionDeclarations:[…]}]` in setup.
  - The server sends `toolCall.functionCalls[]` with `id`, `name` and `args`.
  - The client replies `toolResponse.functionResponses[]`, matched by `id` (G-API "BidiGenerateContentToolCall/ToolResponse"; G-TL "Function calling").
  - `toolCallCancellation.ids[]` is sent when the client interrupted the turn (G-API).
  - Unlike Qwen, no follow-up "create response" is needed.
  - `gemini-3.8-live` defaults to asynchronous `NON_BLOCKING` with `scheduling` `INTERRUPT`/`WHEN_IDLE`/`SILENT`. On 3.1 Flash Live, calls are sequential and the model waits for the tool response (G-GD "Model comparison"; G-TL "Asynchronous function calling").
- **Languages.** G-GD "Supported languages": "Live API supports the following 99 languages", including "Italian | it". G-OV "Key features" says "70 supported languages", which conflicts with G-GD. Native-audio models "automatically choose the appropriate language and don't support explicitly setting the language code". Voices are set via `speechConfig.voiceConfig.prebuiltVoiceConfig.voiceName` (G-GD "Change voice and language").
- **Session limits.** Audio-only sessions are limited to 15 min (G-GD "Limitations"). The server sends `goAway.timeLeft` before disconnecting, and supports `sessionResumption` handles (G-API).

**Obligations for a provider behind one interface**

These are the minimum each provider must meet, with the Qwen and Gemini mechanism for each.

1. **Open with static config and signal ready.** Config covers instructions, voice, tool declarations, and output-audio rate. Qwen: `session.update` → `session.updated`. Gemini: `setup` → `setupComplete`. Treat config as fixed per connection, because Gemini cannot change it.
2. **Accept PCM16 LE mono 16 kHz input chunks** of any size. Both accept this natively (Q-CE; G-GD), so the bridge only Opus-decodes.
3. **Explicit turn begin and end, with no server VAD.**
   - Begin. Qwen: `response.cancel` if a response is active, optional `input_audio_buffer.clear`, then appends. Gemini: `activityStart`.
   - End. Qwen: `input_audio_buffer.commit` + `response.create`. Gemini: `activityEnd`.
   - The provider must tolerate or guard an empty turn. Qwen errors on an empty commit.
4. **Stream output audio as PCM16 with a declared sample rate.** Qwen: 16k or 24k, configurable. Gemini: 24k fixed. The bridge owns re-framing and Opus encoding.
5. **Emit transcripts and turn boundaries.** That means the user's final transcript, assistant transcript deltas, end of response (Qwen `response.done`; Gemini `generationComplete`/`turnComplete`), and cancelled/interrupted (Qwen `response.done` status `cancelled`, inferred; Gemini `interrupted`).
6. **Tool round-trip keyed by call id.** Emit `{id, name, argsJSON}`; accept `{id, output}`. Qwen must then send `response.create`; Gemini continues by itself. The provider must also surface cancellation of a pending call (Gemini `toolCallCancellation`; Qwen has no equivalent).
7. **Cancel / barge-in.** Qwen: `response.cancel`. Gemini: implicit through `activityStart` (or `clientContent`). A pure "stop talking" with no new turn is not documented for Gemini.
8. **Lifetime management.** Hide reconnects behind the interface: Qwen closes at 120 min; Gemini sends `goAway` at about 15 min and needs resumption.
9. **Error surface.** Recoverable request errors (Qwen `error` event, connection stays open) versus fatal ones (Qwen `server_error` closes, per Q-QA; Gemini WebSocket close).

## 12. Open questions / unverified

- **Omni input transcription over raw WebSocket.** The exact `session.update` shape (e.g. `input_audio_transcription: {model: "qwen3-asr-flash-realtime"}` or a boolean) is not in Q-CE. It appears only as an SDK flag and in the Q-SE echo.
- **Omni `response.cancel` outcome.** Whether it yields `response.done` with status `cancelled`, and whether `response.create` is rejected while a response is active, is documented only for Qwen-Audio (Q-QA).
- **No truncation API.** Nothing tells Qwen how much of a cancelled reply was played. It is unknown whether unplayed assistant audio stays in context.
- **Appending during an active response.** Whether Omni in manual mode accepts `input_audio_buffer.append` while a response is still streaming, without cancelling first, is not stated.
- **Italian on input for the realtime variant.** The realtime page gives only the count (113). The list naming Italian is on the Qwen3.5-Omni page (Q-OM). The language set of the separate transcription model `qwen3-asr-flash-realtime` was not checked.
- **`OpenAI-Beta: realtime=v1`.** Whether H's header is ignored or needed is undocumented on the Alibaba pages read.
- **`dashscope-intl` longevity.** It is stated as "remain fully functional" (Q-QA), but it is deprecated in favour of workspace domains, and `qwen3.8-omni-flash-realtime` already requires the workspace domain.
- **Gemini auth for server-to-server raw WebSocket** (API-key query parameter vs header) was not found on the pages read.
- **Gemini language count.** 99 (G-GD) vs 70 (G-OV).
- **Snapshot name.** `qwen3.5-omni-plus-realtime-2026-03-15` was seen only via QwenCloud search results.
