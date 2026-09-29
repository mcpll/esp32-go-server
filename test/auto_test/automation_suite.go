package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const defaultHelloTimeout = 10 * time.Second
const defaultNegativeReadTimeout = 2 * time.Second

const (
	protocolCaseNormal                   = "normal"
	protocolCaseMCP                      = "mcp"
	protocolCaseInvalidHello             = "invalid_hello"
	protocolCaseAbort                    = "abort"
	protocolCaseHelloMetadata            = "hello_metadata"
	protocolCaseInjectedMessage          = "injected_message"
	protocolCaseIot                      = "iot"
	protocolCaseTTSSentenceBoundaries    = "tts_sentence_boundaries"
	protocolCaseRealtimeInterrupt        = "realtime_interrupt"
	protocolCaseDuplicateHello           = "duplicate_hello"
	protocolCaseListenBeforeHello        = "listen_before_hello"
	protocolCaseAbortDuringTTS           = "abort_during_tts"
	protocolCaseRealtimeListenStop       = "realtime_listen_stop"
	protocolCaseNoMCP                    = "no_mcp"
	protocolCaseMCPDuplicateHello        = "mcp_duplicate_hello"
	protocolCaseRealtimeDuplicateStart   = "realtime_duplicate_start"
	protocolCaseGoodbyeThenResume        = "goodbye_then_resume"
	protocolCaseOTAMetadata              = "ota_metadata"
	protocolCaseOTAInvalidAlgorithm      = "ota_invalid_algorithm"
	protocolCaseOTAInvalidChallenge      = "ota_invalid_challenge"
	protocolCaseMqttUDPHello             = "mqtt_udp_hello"
	protocolCaseMqttUDPInjectedMessage   = "mqtt_udp_injected_message"
	protocolCaseInjectedMessageAuto      = "injected_message_auto"
	protocolCaseInjectedMessageCold      = "injected_message_cold"
	protocolCaseAgentWsEndpointMCP       = "agent_ws_endpoint_mcp"
	protocolCaseAgentWsEndpointKeepalive = "agent_ws_endpoint_mcp_keepalive"
)

var (
	runnerMode      = "manual"
	autoCasesFilter = "all"
	autoCaseTimeout = 20 * time.Second
	autoTurns       = 1
)

type protocolEvent struct {
	Direction   string
	Type        string
	State       string
	Mode        string
	Text        string
	SessionID   string
	Transport   string
	SampleRate  int
	Channels    int
	FrameMs     int
	AudioFormat string
	MCPMethod   string
	MCPID       string
	BinaryBytes int
	Note        string
	At          time.Time
}

type sessionRuntime struct {
	conn     *websocket.Conn
	deviceID string

	writeMu sync.Mutex
	eventMu sync.Mutex
	events  []protocolEvent

	helloAckCh chan ServerMessage
	ttsStartCh chan struct{}
	ttsStopCh  chan struct{}
	sttCh      chan ServerMessage
	outputCh   chan ServerMessage
	iotCh      chan ServerMessage
	mcpCh      chan protocolEvent
}

type listenExpectation struct {
	State string
	Mode  string
	Text  string
}

type protocolTestCase struct {
	Name                       string
	Description                string
	Kind                       string
	UseMqttUDP                 bool
	LocalMode                  string
	InputText                  string
	InterruptText              string
	Turns                      int
	Timeout                    time.Duration
	EnableMCP                  bool
	ExpectListenSequence       []listenExpectation
	ExpectListenCount          int
	ExpectHelloTransport       string
	ExpectHelloCount           int
	ExpectSTTText              string
	ExpectOutputText           bool
	ExpectMCPInitialize        bool
	ExpectMCPInitializeCount   int
	ExpectMCPToolsList         bool
	ExpectMCPToolsListCount    int
	ExpectMCPResponse          bool
	ExpectAbortSent            bool
	ExpectRestartAfterTTS      bool
	ExpectNoMCPTraffic         bool
	ExpectAbortStopsTTS        bool
	ExpectNoExtraAcceptedAudio bool
	ExpectHelloSessionID       bool
	ExpectHelloAudioParams     bool
	ExpectIotSuccess           bool
	ExpectSentenceEnd          bool
	ExpectNoServerGoodbye      bool
	ExplicitOnly               bool
}

type protocolTestResult struct {
	Name       string
	Mode       string
	Passed     bool
	Skipped    bool
	SkipReason string
	Duration   time.Duration
	Err        error
}

type skippedCaseError struct {
	Reason string
}

func (e *skippedCaseError) Error() string {
	return e.Reason
}

func skipCase(reason string) error {
	return &skippedCaseError{Reason: reason}
}

func newSessionRuntime(conn *websocket.Conn, deviceID string) *sessionRuntime {
	return &sessionRuntime{
		conn:       conn,
		deviceID:   deviceID,
		helloAckCh: make(chan ServerMessage, 1),
		ttsStartCh: make(chan struct{}, 4),
		ttsStopCh:  make(chan struct{}, 4),
		sttCh:      make(chan ServerMessage, 8),
		outputCh:   make(chan ServerMessage, 8),
		iotCh:      make(chan ServerMessage, 4),
		mcpCh:      make(chan protocolEvent, 16),
	}
}

func (rt *sessionRuntime) writeText(data []byte) error {
	rt.writeMu.Lock()
	defer rt.writeMu.Unlock()
	return rt.conn.WriteMessage(websocket.TextMessage, data)
}

func (rt *sessionRuntime) writeBinary(data []byte) error {
	rt.writeMu.Lock()
	defer rt.writeMu.Unlock()
	return rt.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (rt *sessionRuntime) recordEvent(event protocolEvent) {
	rt.eventMu.Lock()
	defer rt.eventMu.Unlock()
	rt.events = append(rt.events, event)
}

func (rt *sessionRuntime) snapshotEvents() []protocolEvent {
	rt.eventMu.Lock()
	defer rt.eventMu.Unlock()
	out := make([]protocolEvent, len(rt.events))
	copy(out, rt.events)
	return out
}

func (rt *sessionRuntime) recordOutgoingMessage(msg ClientMessage) {
	rt.recordEvent(protocolEvent{
		Direction: "send",
		Type:      msg.Type,
		State:     msg.State,
		Mode:      msg.Mode,
		Text:      msg.Text,
		At:        time.Now(),
	})
}

func (rt *sessionRuntime) recordIncomingMessage(msg ServerMessage) {
	rt.recordEvent(protocolEvent{
		Direction: "recv",
		Type:      msg.Type,
		State:     msg.State,
		SessionID: msg.SessionID,
		Transport: msg.Transport,
		Text:      msg.Text,
		SampleRate: func() int {
			if msg.AudioFormat == nil {
				return 0
			}
			return msg.AudioFormat.SampleRate
		}(),
		Channels: func() int {
			if msg.AudioFormat == nil {
				return 0
			}
			return msg.AudioFormat.Channels
		}(),
		FrameMs: func() int {
			if msg.AudioFormat == nil {
				return 0
			}
			return msg.AudioFormat.FrameDuration
		}(),
		AudioFormat: func() string {
			if msg.AudioFormat == nil {
				return ""
			}
			return msg.AudioFormat.Format
		}(),
		At: time.Now(),
	})
}

func (rt *sessionRuntime) recordIncomingMCP(msg ServerMessage) {
	event := newMCPEvent("recv", msg.PayLoad)
	rt.recordEvent(event)
	select {
	case rt.mcpCh <- event:
	default:
	}
}

func (rt *sessionRuntime) recordOutgoingMCP(data []byte) {
	payload := data
	var wrapped ServerMessage
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Type == MessageTypeMcp && len(wrapped.PayLoad) > 0 {
		payload = wrapped.PayLoad
	}
	event := newMCPEvent("send", payload)
	rt.recordEvent(event)
	select {
	case rt.mcpCh <- event:
	default:
	}
}

func (rt *sessionRuntime) recordIncomingBinary(size int, accepted bool) {
	note := "accepted"
	if !accepted {
		note = "ignored"
	}
	rt.recordEvent(protocolEvent{
		Direction:   "recv_binary",
		Type:        "binary",
		BinaryBytes: size,
		Note:        note,
		At:          time.Now(),
	})
}

func (rt *sessionRuntime) recordNote(note string) {
	rt.recordEvent(protocolEvent{
		Direction: "note",
		Type:      "note",
		Note:      note,
		At:        time.Now(),
	})
}

func (rt *sessionRuntime) notifyHelloAck(msg ServerMessage) {
	select {
	case rt.helloAckCh <- msg:
	default:
	}
}

func (rt *sessionRuntime) notifyTTSStart() {
	select {
	case rt.ttsStartCh <- struct{}{}:
	default:
	}
}

func (rt *sessionRuntime) notifyTTSStop() {
	select {
	case rt.ttsStopCh <- struct{}{}:
	default:
	}
}

func (rt *sessionRuntime) notifySTT(msg ServerMessage) {
	select {
	case rt.sttCh <- msg:
	default:
	}
}

func (rt *sessionRuntime) notifyOutput(msg ServerMessage) {
	select {
	case rt.outputCh <- msg:
	default:
	}
}

func (rt *sessionRuntime) notifyIot(msg ServerMessage) {
	select {
	case rt.iotCh <- msg:
	default:
	}
}

func newMCPEvent(direction string, payload []byte) protocolEvent {
	event := protocolEvent{
		Direction: direction,
		Type:      MessageTypeMcp,
		At:        time.Now(),
	}
	var msg struct {
		ID     interface{} `json:"id"`
		Method string      `json:"method"`
		Result interface{} `json:"result"`
		Error  interface{} `json:"error"`
	}
	if err := json.Unmarshal(payload, &msg); err == nil {
		event.MCPMethod = msg.Method
		switch id := msg.ID.(type) {
		case string:
			event.MCPID = id
		case float64:
			event.MCPID = fmt.Sprintf("%.0f", id)
		case nil:
		default:
			event.MCPID = fmt.Sprint(id)
		}
		if event.MCPMethod == "" && msg.Result != nil {
			event.Note = "result"
		}
		if msg.Error != nil {
			event.Note = "error"
		}
	}
	return event
}

func waitForHelloAck(rt *sessionRuntime, timeout time.Duration) error {
	select {
	case msg := <-rt.helloAckCh:
		if msg.Transport != "websocket" {
			return fmt.Errorf("hello 响应 transport 非 websocket: %s", msg.Transport)
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("等待 hello 响应超时")
	}
}

func drainSignal(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func drainServerMessages(ch chan ServerMessage) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func waitForOptionalTTSPlayback(ttsStartCh, ttsStopCh chan struct{}, outputCh chan ServerMessage, startTimeout, stopTimeout time.Duration, label string) (bool, error) {
	select {
	case <-ttsStartCh:
	case <-time.After(startTimeout):
		return false, nil
	}
	if err := waitForSignal(ttsStopCh, stopTimeout, label+" tts stop"); err != nil {
		return true, err
	}
	drainServerMessages(outputCh)
	time.Sleep(100 * time.Millisecond)
	return true, nil
}

func waitForSignal(ch chan struct{}, timeout time.Duration, label string) error {
	select {
	case <-ch:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("等待 %s 超时", label)
	}
}

func waitForMessage(ch chan ServerMessage, timeout time.Duration, label string) (ServerMessage, error) {
	select {
	case msg := <-ch:
		return msg, nil
	case <-time.After(timeout):
		return ServerMessage{}, fmt.Errorf("等待 %s 超时", label)
	}
}

func waitForNoMessage(ch chan ServerMessage, timeout time.Duration, label string) error {
	select {
	case msg := <-ch:
		return fmt.Errorf("%s 在负向断言窗口内收到消息: type=%s state=%s text=%s", label, msg.Type, msg.State, msg.Text)
	case <-time.After(timeout):
		return nil
	}
}

func waitForMCPEvent(ch chan protocolEvent, timeout time.Duration, match func(protocolEvent) bool, label string) error {
	deadline := time.After(timeout)
	for {
		select {
		case event := <-ch:
			if match(event) {
				return nil
			}
		case <-deadline:
			return fmt.Errorf("等待 %s 超时", label)
		}
	}
}

func waitForNoMCPEvent(ch chan protocolEvent, timeout time.Duration, label string) error {
	select {
	case event := <-ch:
		return fmt.Errorf("%s 在负向断言窗口内收到 MCP 消息: method=%s id=%s note=%s", label, event.MCPMethod, event.MCPID, event.Note)
	case <-time.After(timeout):
		return nil
	}
}

func drainProtocolEvents(ch chan protocolEvent) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func waitForMCPResultCount(rt *sessionRuntime, expected int, timeout time.Duration) error {
	if expected <= 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		count := 0
		for _, event := range rt.snapshotEvents() {
			if event.Direction == "send" && event.Type == MessageTypeMcp && event.Note == "result" {
				count++
			}
		}
		if count >= expected {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("等待 MCP result 数量达到 %d 超时", expected)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func buildProtocolTestCases() []protocolTestCase {
	manualListenCount := 2 * autoTurns
	auto1ListenCount := 2 + autoTurns
	auto2ListenCount := 3 + autoTurns
	manualMultiTurns := 3
	manualMultiListenCount := 2 * manualMultiTurns
	cases := []protocolTestCase{
		{
			Name:             "manual_roundtrip",
			Description:      "验证 manual 模式的 listen start/stop 与 tts 生命周期",
			Kind:             protocolCaseNormal,
			LocalMode:        LocalModeManual,
			InputText:        speectText,
			Turns:            autoTurns,
			Timeout:          autoCaseTimeout,
			ExpectHelloCount: 1,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
				{State: MessageStateStop, Mode: LocalModeManual},
			},
			ExpectListenCount:    manualListenCount,
			ExpectHelloTransport: "websocket",
			ExpectSTTText:        speectText,
			ExpectOutputText:     true,
		},
		{
			Name:             "auto1_roundtrip",
			Description:      "验证 auto1: detect -> start auto，tts stop 后重新 start auto",
			Kind:             protocolCaseNormal,
			LocalMode:        LocalModeAuto1,
			InputText:        speectText,
			Turns:            autoTurns,
			Timeout:          autoCaseTimeout,
			ExpectHelloCount: 1,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: "auto"},
				{State: MessageStateStart, Mode: "auto"},
			},
			ExpectListenCount:     auto1ListenCount,
			ExpectHelloTransport:  "websocket",
			ExpectSTTText:         speectText,
			ExpectOutputText:      true,
			ExpectRestartAfterTTS: true,
		},
		{
			Name:             "auto2_roundtrip",
			Description:      "验证 auto2: start auto -> detect，tts stop 后重新 start auto",
			Kind:             protocolCaseNormal,
			LocalMode:        LocalModeAuto2,
			InputText:        speectText,
			Turns:            autoTurns,
			Timeout:          autoCaseTimeout,
			ExpectHelloCount: 1,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: "auto"},
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: "auto"},
			},
			ExpectListenCount:     auto2ListenCount,
			ExpectHelloTransport:  "websocket",
			ExpectSTTText:         speectText,
			ExpectOutputText:      true,
			ExpectRestartAfterTTS: true,
		},
		{
			Name:             "realtime_roundtrip",
			Description:      "验证 realtime: detect -> start realtime，tts stop 后不追加 listen start",
			Kind:             protocolCaseNormal,
			LocalMode:        LocalModeRealtime,
			InputText:        speectText,
			Turns:            autoTurns,
			Timeout:          autoCaseTimeout,
			ExpectHelloCount: 1,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: LocalModeRealtime},
			},
			ExpectListenCount:    2,
			ExpectHelloTransport: "websocket",
			ExpectSTTText:        speectText,
			ExpectOutputText:     true,
		},
		{
			Name:                   "hello_metadata",
			Description:            "验证 hello 响应包含 session_id 与 audio_params",
			Kind:                   protocolCaseHelloMetadata,
			LocalMode:              LocalModeAuto1,
			Timeout:                defaultNegativeReadTimeout,
			ExpectHelloCount:       1,
			ExpectHelloTransport:   "websocket",
			ExpectHelloSessionID:   true,
			ExpectHelloAudioParams: true,
		},
		{
			Name:                 "injected_message_skip_llm",
			Description:          "验证服务端注入消息后 websocket 设备可直接进入 TTS 播报",
			Kind:                 protocolCaseInjectedMessage,
			LocalMode:            LocalModeAuto1,
			InputText:            "这是一条注入播报",
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "websocket",
			ExpectOutputText:     true,
			ExpectListenCount:    -1,
		},
		{
			Name:                 "iot_roundtrip",
			Description:          "验证 iot 请求能收到 success 响应",
			Kind:                 protocolCaseIot,
			LocalMode:            LocalModeAuto1,
			InputText:            "turn_on_light",
			Timeout:              defaultNegativeReadTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "websocket",
			ExpectIotSuccess:     true,
		},
		{
			Name:                 "tts_sentence_boundaries",
			Description:          "验证服务端会发送 tts sentence_end 边界事件",
			Kind:                 protocolCaseTTSSentenceBoundaries,
			LocalMode:            LocalModeManual,
			InputText:            speectText,
			Turns:                1,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectListenSequence: []listenExpectation{{State: MessageStateStart, Mode: LocalModeManual}, {State: MessageStateStop, Mode: LocalModeManual}},
			ExpectListenCount:    2,
			ExpectHelloTransport: "websocket",
			ExpectSTTText:        speectText,
			ExpectOutputText:     true,
			ExpectSentenceEnd:    true,
		},
		{
			Name:             "manual_multi_turn",
			Description:      "验证 manual 模式固定 3 轮连续发言的稳定性",
			Kind:             protocolCaseNormal,
			LocalMode:        LocalModeManual,
			InputText:        speectText,
			Turns:            manualMultiTurns,
			Timeout:          autoCaseTimeout,
			ExpectHelloCount: 1,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
				{State: MessageStateStop, Mode: LocalModeManual},
			},
			ExpectListenCount:    manualMultiListenCount,
			ExpectHelloTransport: "websocket",
			ExpectSTTText:        speectText,
			ExpectOutputText:     true,
		},
		{
			Name:                     "mcp_initialize",
			Description:              "验证 hello 启用 MCP 后服务端下发 initialize/tools/list，客户端按请求 ID 回传 result",
			Kind:                     protocolCaseMCP,
			LocalMode:                LocalModeAuto1,
			InputText:                speectText,
			Turns:                    1,
			Timeout:                  autoCaseTimeout,
			EnableMCP:                true,
			ExpectHelloCount:         1,
			ExpectListenSequence:     []listenExpectation{},
			ExpectListenCount:        0,
			ExpectHelloTransport:     "websocket",
			ExpectMCPInitialize:      true,
			ExpectMCPInitializeCount: 1,
			ExpectMCPToolsList:       true,
			ExpectMCPToolsListCount:  1,
			ExpectMCPResponse:        true,
		},
		{
			Name:                 "hello_without_mcp_no_initialize",
			Description:          "验证未声明 MCP feature 时不会下发任何 MCP initialize/tools/list",
			Kind:                 protocolCaseNoMCP,
			LocalMode:            LocalModeAuto1,
			Timeout:              defaultNegativeReadTimeout,
			ExpectHelloCount:     1,
			ExpectListenSequence: []listenExpectation{},
			ExpectListenCount:    0,
			ExpectHelloTransport: "websocket",
			ExpectNoMCPTraffic:   true,
		},
		{
			Name:                     "mcp_duplicate_hello_no_reinitialize",
			Description:              "验证健康 MCP runtime 下 duplicate hello 不会重复下发 initialize/tools/list",
			Kind:                     protocolCaseMCPDuplicateHello,
			LocalMode:                LocalModeAuto1,
			Timeout:                  autoCaseTimeout,
			EnableMCP:                true,
			ExpectHelloCount:         2,
			ExpectListenSequence:     []listenExpectation{},
			ExpectListenCount:        0,
			ExpectHelloTransport:     "websocket",
			ExpectMCPInitialize:      true,
			ExpectMCPInitializeCount: 1,
			ExpectMCPToolsList:       true,
			ExpectMCPToolsListCount:  1,
			ExpectMCPResponse:        true,
		},
		{
			Name:        "agent_ws_endpoint_mcp",
			Description: "验证智能体 WebSocket MCP endpoint 反连、initialize/tools/list 与短时保活",
			Kind:        protocolCaseAgentWsEndpointMCP,
			LocalMode:   LocalModeManual,
			Timeout:     autoCaseTimeout,
		},
		{
			Name:         "agent_ws_endpoint_mcp_keepalive",
			Description:  "验证智能体 WebSocket MCP endpoint 超过 2 分钟离线窗口与 10 分钟工具刷新边界后仍在线",
			Kind:         protocolCaseAgentWsEndpointKeepalive,
			LocalMode:    LocalModeManual,
			Timeout:      13 * time.Minute,
			ExplicitOnly: true,
		},
		{
			Name:             "duplicate_hello_rehandshake",
			Description:      "验证 websocket duplicate hello 后仍可继续正常 listen/stt/tts",
			Kind:             protocolCaseDuplicateHello,
			LocalMode:        LocalModeManual,
			InputText:        speectText,
			Turns:            1,
			Timeout:          autoCaseTimeout,
			ExpectHelloCount: 2,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
				{State: MessageStateStop, Mode: LocalModeManual},
			},
			ExpectListenCount:    2,
			ExpectHelloTransport: "websocket",
			ExpectSTTText:        speectText,
			ExpectOutputText:     true,
		},
		{
			Name:        "invalid_hello_missing_audio_params",
			Description: "验证 hello 缺少 audio_params 时服务端不会返回成功握手",
			Kind:        protocolCaseInvalidHello,
			LocalMode:   LocalModeManual,
			InputText:   "",
			Timeout:     defaultNegativeReadTimeout,
		},
		{
			Name:        "invalid_hello_unsupported_transport",
			Description: "验证 hello transport 非法时服务端不会返回成功握手",
			Kind:        protocolCaseInvalidHello,
			LocalMode:   LocalModeManual,
			InputText:   "bad_transport",
			Timeout:     defaultNegativeReadTimeout,
		},
		{
			Name:             "listen_before_hello_ignored",
			Description:      "验证 hello 前发送 listen 会被忽略，之后补 hello 可成功握手",
			Kind:             protocolCaseListenBeforeHello,
			LocalMode:        LocalModeManual,
			Timeout:          defaultNegativeReadTimeout,
			ExpectHelloCount: 1,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
			},
			ExpectListenCount:    1,
			ExpectHelloTransport: "websocket",
		},
		{
			Name:                 "abort_after_listen_start",
			Description:          "验证正常握手后 listen start 再 abort 的控制消息路径",
			Kind:                 protocolCaseAbort,
			LocalMode:            LocalModeManual,
			Timeout:              defaultNegativeReadTimeout,
			ExpectHelloCount:     1,
			ExpectListenSequence: []listenExpectation{{State: MessageStateStart, Mode: LocalModeManual}},
			ExpectListenCount:    1,
			ExpectHelloTransport: "websocket",
			ExpectAbortSent:      true,
		},
		{
			Name:                 "abort_during_tts",
			Description:          "验证收到 tts start 后发送 abort，服务端会尽快回 tts stop",
			Kind:                 protocolCaseAbortDuringTTS,
			LocalMode:            LocalModeManual,
			InputText:            speectText,
			Turns:                1,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectListenSequence: []listenExpectation{{State: MessageStateStart, Mode: LocalModeManual}, {State: MessageStateStop, Mode: LocalModeManual}},
			ExpectListenCount:    2,
			ExpectHelloTransport: "websocket",
			ExpectSTTText:        speectText,
			ExpectOutputText:     true,
			ExpectAbortSent:      true,
			ExpectAbortStopsTTS:  true,
		},
		{
			Name:                 "realtime_interrupt",
			Description:          "验证 realtime 模式下第二轮语音可在首轮 TTS 期间打断并触发新识别",
			Kind:                 protocolCaseRealtimeInterrupt,
			LocalMode:            LocalModeRealtime,
			InputText:            speectText,
			InterruptText:        speectText + "第二轮",
			Turns:                1,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectListenSequence: []listenExpectation{{State: MessageStateDetect, Text: defaultDetectText}, {State: MessageStateStart, Mode: LocalModeRealtime}},
			ExpectListenCount:    2,
			ExpectHelloTransport: "websocket",
			ExpectSTTText:        speectText,
			ExpectOutputText:     true,
		},
		{
			Name:             "realtime_listen_stop",
			Description:      "验证 realtime 模式 listen stop 后可再次 listen start 并恢复识别",
			Kind:             protocolCaseRealtimeListenStop,
			LocalMode:        LocalModeRealtime,
			InputText:        speectText,
			Turns:            1,
			Timeout:          autoCaseTimeout,
			ExpectHelloCount: 1,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: LocalModeRealtime},
				{State: MessageStateStop},
				{State: MessageStateStart, Mode: LocalModeRealtime},
			},
			ExpectListenCount:    4,
			ExpectHelloTransport: "websocket",
			ExpectSTTText:        speectText,
			ExpectOutputText:     true,
		},
		{
			Name:             "realtime_duplicate_start_ignored",
			Description:      "验证 realtime 已启动时重复 listen start 不会破坏后续识别",
			Kind:             protocolCaseRealtimeDuplicateStart,
			LocalMode:        LocalModeRealtime,
			InputText:        speectText,
			Turns:            1,
			Timeout:          autoCaseTimeout,
			ExpectHelloCount: 1,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: LocalModeRealtime},
				{State: MessageStateStart, Mode: LocalModeRealtime},
			},
			ExpectListenCount:    3,
			ExpectHelloTransport: "websocket",
			ExpectSTTText:        speectText,
			ExpectOutputText:     true,
		},
		{
			Name:             "goodbye_then_resume",
			Description:      "验证 websocket goodbye 后无需重连即可再次进入 listen/tts",
			Kind:             protocolCaseGoodbyeThenResume,
			LocalMode:        LocalModeManual,
			InputText:        speectText,
			Turns:            1,
			Timeout:          autoCaseTimeout,
			ExpectHelloCount: 1,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
				{State: MessageStateStop, Mode: LocalModeManual},
			},
			ExpectListenCount:     2,
			ExpectHelloTransport:  "websocket",
			ExpectSTTText:         speectText,
			ExpectOutputText:      true,
			ExpectNoServerGoodbye: true,
		},
		{
			Name:        "ota_metadata",
			Description: "验证 OTA 接口返回 websocket/server_time/firmware 元信息，并在启用时携带 activation/mqtt",
			Kind:        protocolCaseOTAMetadata,
			LocalMode:   LocalModeManual,
			Timeout:     autoCaseTimeout,
		},
		{
			Name:        "ota_activate_invalid_algorithm",
			Description: "验证激活接口对不支持的 algorithm 返回 400",
			Kind:        protocolCaseOTAInvalidAlgorithm,
			LocalMode:   LocalModeManual,
			Timeout:     autoCaseTimeout,
		},
		{
			Name:        "ota_activate_invalid_challenge_if_required",
			Description: "当 OTA 返回 activation challenge 时，验证错误 challenge 返回 202",
			Kind:        protocolCaseOTAInvalidChallenge,
			LocalMode:   LocalModeManual,
			Timeout:     autoCaseTimeout,
		},
		{
			Name:        "mqtt_udp_hello",
			Description: "验证 MQTT hello 握手返回 UDP 配置，并且 duplicate hello 会重建 UDP session",
			Kind:        protocolCaseMqttUDPHello,
			LocalMode:   LocalModeManual,
			Timeout:     autoCaseTimeout,
		},
		{
			Name:        "mqtt_udp_injected_message",
			Description: "验证 MQTT 主动播报的 speak_request/speak_ready/UDP 音频/服务端 goodbye 链路",
			Kind:        protocolCaseMqttUDPInjectedMessage,
			LocalMode:   LocalModeManual,
			InputText:   "MQTT主动播报测试",
			Timeout:     autoCaseTimeout,
		},
	}
	return append(cases, buildMqttUDPTestCases(manualListenCount, auto1ListenCount, auto2ListenCount, manualMultiTurns, manualMultiListenCount)...)
}

func isMqttUDPCase(testCase protocolTestCase) bool {
	return testCase.UseMqttUDP ||
		testCase.Kind == protocolCaseMqttUDPHello ||
		testCase.Kind == protocolCaseMqttUDPInjectedMessage
}

func deriveMqttCaseDeviceID(baseDeviceID, caseName string, caseIndex int) string {
	base := strings.TrimSpace(baseDeviceID)
	if base == "" {
		base = "auto-test"
	}
	suffix := sanitizeDeviceIDSuffix(caseName)
	if suffix == "" {
		suffix = "mqtt-udp"
	}
	if len(suffix) > 40 {
		suffix = suffix[:40]
	}
	indexPart := fmt.Sprintf("%02d", caseIndex)
	maxBaseLen := 96 - len(indexPart) - len(suffix) - 2
	if maxBaseLen < 1 {
		maxBaseLen = 1
	}
	if len(base) > maxBaseLen {
		base = base[:maxBaseLen]
	}
	return fmt.Sprintf("%s-%s-%s", base, indexPart, suffix)
}

func sanitizeDeviceIDSuffix(input string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(input) {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func selectProtocolTestCases() ([]protocolTestCase, error) {
	allCases := buildProtocolTestCases()
	if strings.EqualFold(strings.TrimSpace(autoCasesFilter), "all") || strings.TrimSpace(autoCasesFilter) == "" {
		selected := make([]protocolTestCase, 0, len(allCases))
		for _, testCase := range allCases {
			if !testCase.ExplicitOnly {
				selected = append(selected, testCase)
			}
		}
		return selected, nil
	}

	lookup := make(map[string]protocolTestCase, len(allCases))
	for _, testCase := range allCases {
		lookup[testCase.Name] = testCase
	}

	names := strings.Split(autoCasesFilter, ",")
	selected := make([]protocolTestCase, 0, len(names))
	for _, name := range names {
		key := strings.TrimSpace(name)
		if key == "" {
			continue
		}
		testCase, ok := lookup[key]
		if !ok {
			return nil, fmt.Errorf("未知自动化用例: %s", key)
		}
		selected = append(selected, testCase)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("未选择任何自动化用例")
	}
	return selected, nil
}

func runAutomationSuite(serverAddr, deviceID, audioFile string) error {
	if audioFile != "" {
		return fmt.Errorf("自动化模式暂不支持 -audio，请改用文本驱动测试")
	}

	testCases, err := selectProtocolTestCases()
	if err != nil {
		return err
	}

	results := make([]protocolTestResult, 0, len(testCases))
	for i, testCase := range testCases {
		mode = testCase.LocalMode
		resetSignals()
		startedAt := time.Now()
		fmt.Printf("\n=== 开始自动化用例: %s (%s) ===\n", testCase.Name, testCase.Description)
		caseDeviceID := deviceID
		if len(testCases) > 1 && isMqttUDPCase(testCase) {
			caseDeviceID = deriveMqttCaseDeviceID(deviceID, testCase.Name, i+1)
			fmt.Printf("用例设备ID: %s\n", caseDeviceID)
		}
		err := runProtocolCase(serverAddr, caseDeviceID, audioFile, &testCase)
		result := protocolTestResult{
			Name:     testCase.Name,
			Mode:     testCase.LocalMode,
			Passed:   err == nil,
			Duration: time.Since(startedAt),
			Err:      err,
		}
		var skippedErr *skippedCaseError
		if errors.As(err, &skippedErr) {
			result.Passed = false
			result.Skipped = true
			result.SkipReason = skippedErr.Reason
			result.Err = nil
		}
		results = append(results, result)
		if result.Skipped {
			fmt.Printf("=== 用例跳过: %s, reason=%s ===\n", testCase.Name, skippedErr.Reason)
			continue
		}
		if err != nil {
			fmt.Printf("=== 用例失败: %s, err=%v ===\n", testCase.Name, err)
			continue
		}
		fmt.Printf("=== 用例通过: %s, 耗时=%s ===\n", testCase.Name, result.Duration)
	}

	failures := 0
	fmt.Println("\n自动化测试汇总:")
	for _, result := range results {
		status := "PASS"
		if result.Skipped {
			status = "SKIP"
		} else if !result.Passed {
			status = "FAIL"
			failures++
		}
		if result.Err != nil {
			fmt.Printf("- [%s] %s (%s): %v\n", status, result.Name, result.Mode, result.Err)
			continue
		}
		if result.Skipped {
			fmt.Printf("- [%s] %s (%s): %s\n", status, result.Name, result.Mode, result.SkipReason)
			continue
		}
		fmt.Printf("- [%s] %s (%s): %s\n", status, result.Name, result.Mode, result.Duration)
	}
	if failures > 0 {
		return fmt.Errorf("自动化测试失败: %d/%d", failures, len(results))
	}
	return nil
}

func runProtocolCase(serverAddr, deviceID, audioFile string, testCase *protocolTestCase) error {
	if testCase.UseMqttUDP {
		return runMqttUDPCase(serverAddr, deviceID, testCase)
	}
	switch testCase.Kind {
	case protocolCaseInvalidHello:
		return runInvalidHelloCase(serverAddr, deviceID, testCase)
	case protocolCaseListenBeforeHello:
		return runListenBeforeHelloCase(serverAddr, deviceID, testCase)
	case protocolCaseInjectedMessage:
		return runInjectedMessageCase(serverAddr, deviceID, audioFile, testCase)
	case protocolCaseOTAMetadata:
		return runOTAMetadataCase(serverAddr, deviceID, testCase)
	case protocolCaseOTAInvalidAlgorithm:
		return runOTAInvalidAlgorithmCase(serverAddr, deviceID, testCase)
	case protocolCaseOTAInvalidChallenge:
		return runOTAInvalidChallengeCase(serverAddr, deviceID, testCase)
	case protocolCaseMqttUDPHello:
		return runMqttUDPHelloCase(serverAddr, deviceID, testCase)
	case protocolCaseMqttUDPInjectedMessage:
		return runMqttUDPInjectedMessageCase(serverAddr, deviceID, testCase)
	case protocolCaseAgentWsEndpointMCP, protocolCaseAgentWsEndpointKeepalive:
		return runAgentWsEndpointMCPCase(serverAddr, deviceID, testCase)
	default:
		return runClient(serverAddr, deviceID, audioFile, testCase)
	}
}

func runInvalidHelloCase(serverAddr, deviceID string, testCase *protocolTestCase) error {
	conn, _, err := dialServer(serverAddr, deviceID)
	if err != nil {
		return err
	}
	defer conn.Close()

	transport := "websocket"
	audioParams := defaultAudioFormat()
	if strings.TrimSpace(testCase.InputText) == "bad_transport" {
		transport = "invalid_transport"
	} else {
		audioParams = nil
	}
	data, err := json.Marshal(buildHelloMessage(deviceID, false, transport, audioParams))
	if err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("发送非法 hello 失败: %v", err)
	}

	timeout := testCase.Timeout
	if timeout <= 0 {
		timeout = defaultNegativeReadTimeout
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	messageType, message, err := conn.ReadMessage()
	if err != nil {
		return nil
	}
	if messageType != websocket.TextMessage {
		return nil
	}
	var serverMsg ServerMessage
	if err := json.Unmarshal(message, &serverMsg); err != nil {
		return nil
	}
	if serverMsg.Type == MessageTypeHello {
		return fmt.Errorf("非法 hello 不应收到成功握手: %s", string(message))
	}
	return nil
}

func runListenBeforeHelloCase(serverAddr, deviceID string, testCase *protocolTestCase) error {
	conn, _, err := dialServer(serverAddr, deviceID)
	if err != nil {
		return err
	}
	defer conn.Close()

	rt := startSessionRuntime(conn, deviceID)
	if err := sendListenStart(rt, LocalModeManual); err != nil {
		return err
	}
	if err := waitForNoMessage(rt.sttCh, testCase.Timeout, "hello 前 stt"); err != nil {
		return err
	}
	if err := waitForNoMessage(rt.outputCh, testCase.Timeout, "hello 前 output"); err != nil {
		return err
	}
	if err := sendHello(rt, deviceID, false, "websocket", defaultAudioFormat()); err != nil {
		return err
	}
	if err := waitForHelloAck(rt, defaultHelloTimeout); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return evaluateProtocolCase(rt, testCase)
}

func dialServer(serverAddr, deviceID string) (*websocket.Conn, *http.Response, error) {
	header := http.Header{}
	header.Set("Device-Id", deviceID)
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+token)
	header.Set("Protocol-Version", "1")
	header.Set("Client-Id", clientId)

	conn, resp, err := websocket.DefaultDialer.Dial(serverAddr, header)
	if err != nil {
		return nil, resp, fmt.Errorf("连接失败: %v", err)
	}
	return conn, resp, nil
}

func buildInjectMessageURL(serverAddr string) (string, error) {
	parsed, err := neturl.Parse(serverAddr)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	}
	parsed.Path = "/admin/inject_msg"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func postInjectMessage(serverAddr, deviceID, message string, skipLlm bool, autoListen bool) error {
	injectURL, err := buildInjectMessageURL(serverAddr)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]interface{}{
		"device_id":   deviceID,
		"message":     message,
		"skip_llm":    skipLlm,
		"auto_listen": autoListen,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, injectURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("inject_msg 响应失败: status=%s body=%s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func runInjectedMessageCase(serverAddr, deviceID, audioFile string, testCase *protocolTestCase) error {
	if audioFile != "" {
		return fmt.Errorf("注入消息用例不支持 -audio")
	}
	conn, _, err := dialServer(serverAddr, deviceID)
	if err != nil {
		return err
	}
	defer conn.Close()

	rt := startSessionRuntime(conn, deviceID)
	if err := sendHello(rt, deviceID, false, "websocket", defaultAudioFormat()); err != nil {
		return err
	}
	if err := waitForHelloAck(rt, defaultHelloTimeout); err != nil {
		return err
	}
	if err := postInjectMessage(serverAddr, deviceID, testCase.InputText, true, false); err != nil {
		return err
	}
	if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "注入消息 tts start"); err != nil {
		return err
	}
	if testCase.ExpectOutputText {
		if _, err := waitForMessage(rt.outputCh, testCase.Timeout, "注入消息 output"); err != nil {
			return err
		}
	}
	if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "注入消息 tts stop"); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return evaluateProtocolCase(rt, testCase)
}

func runProtocolTestCase(rt *sessionRuntime, testCase *protocolTestCase, runTurn func(string) error) error {
	drainSignal(rt.ttsStartCh)
	drainSignal(rt.ttsStopCh)

	switch testCase.Kind {
	case protocolCaseMCP:
		if err := runMCPCase(rt, testCase); err != nil {
			return err
		}
		return evaluateProtocolCase(rt, testCase)
	case protocolCaseHelloMetadata:
		return evaluateProtocolCase(rt, testCase)
	case protocolCaseIot:
		return runIotCase(rt, testCase)
	case protocolCaseNoMCP:
		if err := waitForNoMCPEvent(rt.mcpCh, testCase.Timeout, "hello 后 MCP 流量"); err != nil {
			return err
		}
		return evaluateProtocolCase(rt, testCase)
	case protocolCaseMCPDuplicateHello:
		if err := runMCPCase(rt, testCase); err != nil {
			return err
		}
		drainProtocolEvents(rt.mcpCh)
		if err := sendHello(rt, rt.deviceID, true, "websocket", defaultAudioFormat()); err != nil {
			return err
		}
		if err := waitForHelloAck(rt, defaultHelloTimeout); err != nil {
			return err
		}
		if err := waitForNoMCPEvent(rt.mcpCh, defaultNegativeReadTimeout, "duplicate hello 后 MCP 重复初始化"); err != nil {
			return err
		}
		return evaluateProtocolCase(rt, testCase)
	case protocolCaseDuplicateHello:
		if err := sendHello(rt, rt.deviceID, false, "websocket", defaultAudioFormat()); err != nil {
			return err
		}
		if err := waitForHelloAck(rt, defaultHelloTimeout); err != nil {
			return err
		}
	case protocolCaseRealtimeInterrupt:
		return runRealtimeInterruptCase(rt, testCase, runTurn)
	case protocolCaseAbort:
		return runAbortAfterListenStartCase(rt, testCase)
	case protocolCaseAbortDuringTTS:
		return runAbortDuringTTSCase(rt, testCase, runTurn)
	case protocolCaseRealtimeListenStop:
		return runRealtimeListenStopCase(rt, testCase, runTurn)
	case protocolCaseRealtimeDuplicateStart:
		return runRealtimeDuplicateStartCase(rt, testCase, runTurn)
	case protocolCaseGoodbyeThenResume:
		return runGoodbyeThenResumeCase(rt, testCase, runTurn)
	}

	for turn := 0; turn < testCase.Turns; turn++ {
		if err := runTurn(testCase.InputText); err != nil {
			return fmt.Errorf("执行发言轮次失败: %v", err)
		}
		if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "stt"); err != nil {
			return err
		}
		if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "tts start"); err != nil {
			return err
		}
		if testCase.ExpectOutputText {
			if _, err := waitForMessage(rt.outputCh, testCase.Timeout, "tts sentence output"); err != nil {
				return err
			}
		}
		if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "tts stop"); err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}

	return evaluateProtocolCase(rt, testCase)
}

func runMCPCase(rt *sessionRuntime, testCase *protocolTestCase) error {
	if testCase.ExpectMCPInitialize {
		if err := waitForMCPEvent(rt.mcpCh, testCase.Timeout, func(event protocolEvent) bool {
			return event.Direction == "recv" && event.MCPMethod == "initialize"
		}, "MCP initialize"); err != nil {
			return err
		}
	}
	totalResults := 0
	if testCase.ExpectMCPInitialize {
		totalResults += maxInt(1, testCase.ExpectMCPInitializeCount)
	}
	if testCase.ExpectMCPToolsList {
		totalResults += maxInt(1, testCase.ExpectMCPToolsListCount)
	}
	if testCase.ExpectMCPResponse && totalResults > 0 {
		if err := waitForMCPEvent(rt.mcpCh, testCase.Timeout, func(event protocolEvent) bool {
			return event.Direction == "send" && event.Note == "result"
		}, "MCP result 响应"); err != nil {
			return err
		}
	}
	if testCase.ExpectMCPToolsList {
		if err := waitForMCPEvent(rt.mcpCh, testCase.Timeout, func(event protocolEvent) bool {
			return event.Direction == "recv" && event.MCPMethod == "tools/list"
		}, "MCP tools/list"); err != nil {
			return err
		}
	}
	if err := waitForMCPResultCount(rt, totalResults, testCase.Timeout); err != nil {
		return err
	}
	return nil
}

func runIotCase(rt *sessionRuntime, testCase *protocolTestCase) error {
	if err := sendIot(rt, testCase.InputText); err != nil {
		return err
	}
	if _, err := waitForMessage(rt.iotCh, testCase.Timeout, "iot success"); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	return evaluateProtocolCase(rt, testCase)
}

func runAbortAfterListenStartCase(rt *sessionRuntime, testCase *protocolTestCase) error {
	if err := sendListenStart(rt, LocalModeManual); err != nil {
		return err
	}
	if err := sendAbort(rt); err != nil {
		return err
	}
	time.Sleep(testCase.Timeout)
	return evaluateProtocolCase(rt, testCase)
}

func runAbortDuringTTSCase(rt *sessionRuntime, testCase *protocolTestCase, runTurn func(string) error) error {
	if err := runTurn(testCase.InputText); err != nil {
		return fmt.Errorf("执行发言失败: %v", err)
	}
	if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "abort 前 stt"); err != nil {
		return err
	}
	if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "abort 前 tts start"); err != nil {
		return err
	}
	if testCase.ExpectOutputText {
		if _, err := waitForMessage(rt.outputCh, testCase.Timeout, "abort 前 output"); err != nil {
			return err
		}
	}
	if err := sendAbort(rt); err != nil {
		return err
	}
	if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "abort 后 tts stop"); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return evaluateProtocolCase(rt, testCase)
}

func runRealtimeInterruptCase(rt *sessionRuntime, testCase *protocolTestCase, runTurn func(string) error) error {
	if err := runTurn(testCase.InputText); err != nil {
		return fmt.Errorf("执行首轮发言失败: %v", err)
	}
	if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "首轮 stt"); err != nil {
		return err
	}
	if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "首轮 tts start"); err != nil {
		return err
	}
	if err := runTurn(testCase.InterruptText); err != nil {
		return fmt.Errorf("执行打断发言失败: %v", err)
	}
	if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "打断轮 stt"); err != nil {
		return err
	}
	if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "tts stop after interrupt"); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return evaluateProtocolCase(rt, testCase)
}

func runRealtimeListenStopCase(rt *sessionRuntime, testCase *protocolTestCase, runTurn func(string) error) error {
	time.Sleep(200 * time.Millisecond)
	if err := sendListenStop(rt); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	if err := sendListenStart(rt, LocalModeRealtime); err != nil {
		return err
	}
	if err := runTurn(testCase.InputText); err != nil {
		return fmt.Errorf("listen stop 后恢复发言失败: %v", err)
	}
	if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "realtime stop 后 stt"); err != nil {
		return err
	}
	if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "realtime stop 后 tts start"); err != nil {
		return err
	}
	if testCase.ExpectOutputText {
		if _, err := waitForMessage(rt.outputCh, testCase.Timeout, "realtime stop 后 output"); err != nil {
			return err
		}
	}
	if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "realtime stop 后 tts stop"); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return evaluateProtocolCase(rt, testCase)
}

func runRealtimeDuplicateStartCase(rt *sessionRuntime, testCase *protocolTestCase, runTurn func(string) error) error {
	time.Sleep(200 * time.Millisecond)
	if err := sendListenStart(rt, LocalModeRealtime); err != nil {
		return err
	}
	if err := runTurn(testCase.InputText); err != nil {
		return fmt.Errorf("duplicate start 后发言失败: %v", err)
	}
	if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "duplicate start 后 stt"); err != nil {
		return err
	}
	if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "duplicate start 后 tts start"); err != nil {
		return err
	}
	if testCase.ExpectOutputText {
		if _, err := waitForMessage(rt.outputCh, testCase.Timeout, "duplicate start 后 output"); err != nil {
			return err
		}
	}
	if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "duplicate start 后 tts stop"); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return evaluateProtocolCase(rt, testCase)
}

func runGoodbyeThenResumeCase(rt *sessionRuntime, testCase *protocolTestCase, runTurn func(string) error) error {
	if err := sendGoodbye(rt); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	if err := runTurn(testCase.InputText); err != nil {
		return fmt.Errorf("goodbye 后恢复发言失败: %v", err)
	}
	if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "goodbye 后 stt"); err != nil {
		return err
	}
	if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "goodbye 后 tts start"); err != nil {
		return err
	}
	if testCase.ExpectOutputText {
		if _, err := waitForMessage(rt.outputCh, testCase.Timeout, "goodbye 后 output"); err != nil {
			return err
		}
	}
	if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "goodbye 后 tts stop"); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return evaluateProtocolCase(rt, testCase)
}

func evaluateProtocolCase(rt *sessionRuntime, testCase *protocolTestCase) error {
	events := rt.snapshotEvents()
	if err := assertHelloTransport(events, testCase.ExpectHelloTransport, testCase.ExpectHelloCount); err != nil {
		return err
	}
	if err := assertListenSequence(events, testCase.ExpectListenSequence, testCase.ExpectListenCount); err != nil {
		return err
	}
	if shouldExpectTTSLifecycle(testCase) {
		if err := assertTTSLifecycle(events); err != nil {
			return err
		}
	}
	if shouldExpectBinaryAudio(testCase) {
		if err := assertBinaryAcceptedAfterTTSStart(events); err != nil {
			return err
		}
	}
	if testCase.ExpectRestartAfterTTS {
		if err := assertRestartAfterTTSStop(events); err != nil {
			return err
		}
	}
	if testCase.ExpectSTTText != "" {
		if err := assertSTTText(events, testCase.ExpectSTTText); err != nil {
			return err
		}
	}
	if testCase.ExpectOutputText {
		if err := assertOutputText(events); err != nil {
			return err
		}
	}
	if testCase.ExpectMCPInitialize {
		if err := assertMCPInitialize(events, testCase.ExpectMCPInitializeCount); err != nil {
			return err
		}
	}
	if testCase.ExpectMCPResponse {
		if err := assertMCPResponse(events, testCase.ExpectMCPInitializeCount, testCase.ExpectMCPToolsListCount); err != nil {
			return err
		}
	}
	if testCase.ExpectMCPToolsList {
		if err := assertMCPToolsList(events, testCase.ExpectMCPToolsListCount); err != nil {
			return err
		}
	}
	if testCase.ExpectAbortSent {
		if err := assertAbortSent(events); err != nil {
			return err
		}
	}
	if testCase.ExpectAbortStopsTTS {
		if err := assertAbortLeadsToTTSStop(events); err != nil {
			return err
		}
	}
	if testCase.ExpectNoExtraAcceptedAudio {
		if err := assertNoAcceptedBinaryAfterTTSStop(events); err != nil {
			return err
		}
	}
	if testCase.ExpectHelloSessionID {
		if err := assertHelloSessionID(events); err != nil {
			return err
		}
	}
	if testCase.ExpectHelloAudioParams {
		if err := assertHelloAudioParams(events); err != nil {
			return err
		}
	}
	if testCase.ExpectIotSuccess {
		if err := assertIotSuccess(events, testCase.InputText); err != nil {
			return err
		}
	}
	if testCase.ExpectSentenceEnd {
		if err := assertTTSSentenceEnd(events); err != nil {
			return err
		}
	}
	if testCase.ExpectNoServerGoodbye {
		if err := assertNoServerGoodbye(events); err != nil {
			return err
		}
	}
	return nil
}

func shouldExpectTTSLifecycle(testCase *protocolTestCase) bool {
	switch testCase.Kind {
	case protocolCaseMCP, protocolCaseAbort, protocolCaseNoMCP, protocolCaseListenBeforeHello, protocolCaseMCPDuplicateHello, protocolCaseHelloMetadata, protocolCaseIot:
		return false
	default:
		return true
	}
}

func shouldExpectBinaryAudio(testCase *protocolTestCase) bool {
	return shouldExpectTTSLifecycle(testCase)
}

func assertHelloTransport(events []protocolEvent, expected string, expectedCount int) error {
	if expectedCount <= 0 {
		expectedCount = 1
	}
	count := 0
	for _, event := range events {
		if event.Direction == "recv" && event.Type == MessageTypeHello {
			count++
			if expected != "" && event.Transport != expected {
				return fmt.Errorf("hello transport 不符合预期: got=%s want=%s", event.Transport, expected)
			}
		}
	}
	if count != expectedCount {
		return fmt.Errorf("hello 响应数量不符合预期: got=%d want=%d", count, expectedCount)
	}
	if count == 0 {
		return fmt.Errorf("未收到 hello 响应")
	}
	return nil
}

func assertListenSequence(events []protocolEvent, expected []listenExpectation, expectedCount int) error {
	listenEvents := make([]protocolEvent, 0)
	for _, event := range events {
		if event.Direction == "send" && event.Type == MessageTypeListen {
			listenEvents = append(listenEvents, event)
		}
	}

	if expectedCount >= 0 && len(listenEvents) != expectedCount {
		return fmt.Errorf("listen 消息数量不符合预期: got=%d want=%d", len(listenEvents), expectedCount)
	}
	if len(listenEvents) < len(expected) {
		return fmt.Errorf("listen 消息数量不足: got=%d want>=%d", len(listenEvents), len(expected))
	}
	for idx, want := range expected {
		got := listenEvents[idx]
		if got.State != want.State {
			return fmt.Errorf("listen[%d] state 不符合预期: got=%s want=%s", idx, got.State, want.State)
		}
		if want.Mode != "" && got.Mode != want.Mode {
			return fmt.Errorf("listen[%d] mode 不符合预期: got=%s want=%s", idx, got.Mode, want.Mode)
		}
		if want.Text != "" && got.Text != want.Text {
			return fmt.Errorf("listen[%d] text 不符合预期: got=%s want=%s", idx, got.Text, want.Text)
		}
	}
	return nil
}

func assertTTSLifecycle(events []protocolEvent) error {
	startIndex := -1
	stopIndex := -1
	for idx, event := range events {
		if event.Direction != "recv" || event.Type != "tts" {
			continue
		}
		if startIndex == -1 && event.State == MessageStateStart {
			startIndex = idx
		}
		if event.State == MessageStateStop {
			stopIndex = idx
			break
		}
	}
	if startIndex == -1 {
		return fmt.Errorf("未收到 tts start")
	}
	if stopIndex == -1 {
		return fmt.Errorf("未收到 tts stop")
	}
	if stopIndex < startIndex {
		return fmt.Errorf("tts stop 早于 tts start")
	}
	return nil
}

func assertBinaryAcceptedAfterTTSStart(events []protocolEvent) error {
	startIndex := -1
	for idx, event := range events {
		if event.Direction == "recv" && event.Type == "tts" && event.State == MessageStateStart {
			startIndex = idx
			break
		}
	}
	if startIndex == -1 {
		return fmt.Errorf("未找到 tts start，无法验证音频接收顺序")
	}

	for idx, event := range events {
		if event.Direction != "recv_binary" || event.Note != "accepted" {
			continue
		}
		if idx < startIndex {
			return fmt.Errorf("在 tts start 之前接受到了音频二进制帧")
		}
		return nil
	}
	return fmt.Errorf("未接收到任何被接受的音频二进制帧")
}

func assertRestartAfterTTSStop(events []protocolEvent) error {
	stopIndex := -1
	for idx, event := range events {
		if event.Direction == "recv" && event.Type == "tts" && event.State == MessageStateStop {
			stopIndex = idx
			break
		}
	}
	if stopIndex == -1 {
		return fmt.Errorf("未找到 tts stop，无法验证重启监听")
	}
	for idx := stopIndex + 1; idx < len(events); idx++ {
		event := events[idx]
		if event.Direction == "send" && event.Type == MessageTypeListen && event.State == MessageStateStart && event.Mode == "auto" {
			return nil
		}
	}
	return fmt.Errorf("tts stop 之后未重新发送 listen start auto")
}

func assertSTTText(events []protocolEvent, expected string) error {
	expected = normalizeAssertText(expected)
	if expected == "" {
		return nil
	}
	for _, event := range events {
		if event.Direction != "recv" || event.Type != ServerMessageTypeSTT {
			continue
		}
		got := normalizeAssertText(event.Text)
		if strings.Contains(got, expected) || strings.Contains(expected, got) {
			return nil
		}
	}
	return fmt.Errorf("未收到匹配的 stt 文本: want 包含 %q", expected)
}

func assertOutputText(events []protocolEvent) error {
	for _, event := range events {
		if event.Direction == "recv" && event.Type == ServerMessageTypeTTS && event.State == MessageStateSentenceStart && strings.TrimSpace(event.Text) != "" {
			return nil
		}
		if event.Direction == "recv" && event.Type == ServerMessageTypeText && strings.TrimSpace(event.Text) != "" {
			return nil
		}
		if event.Direction == "recv" && event.Type == ServerMessageTypeLLM && strings.TrimSpace(event.Text) != "" {
			return nil
		}
	}
	return fmt.Errorf("未收到任何 LLM/TTS 文本输出")
}

func assertMCPInitialize(events []protocolEvent, expectedCount int) error {
	return assertMCPMethodCount(events, "initialize", expectedCount)
}

func assertMCPResponse(events []protocolEvent, expectedInitializeCount, expectedToolsListCount int) error {
	if err := assertMCPResultsMatchRequests(events, "initialize", expectedInitializeCount); err != nil {
		return err
	}
	if err := assertMCPResultsMatchRequests(events, "tools/list", expectedToolsListCount); err != nil {
		return err
	}
	return nil
}

func assertMCPToolsList(events []protocolEvent, expectedCount int) error {
	return assertMCPMethodCount(events, "tools/list", expectedCount)
}

func assertAbortSent(events []protocolEvent) error {
	for _, event := range events {
		if event.Direction == "send" && event.Type == MessageTypeAbort {
			return nil
		}
	}
	return fmt.Errorf("未发送 abort 消息")
}

func assertHelloSessionID(events []protocolEvent) error {
	for _, event := range events {
		if event.Direction == "recv" && event.Type == MessageTypeHello && strings.TrimSpace(event.SessionID) != "" {
			return nil
		}
	}
	return fmt.Errorf("hello 响应未携带 session_id")
}

func assertHelloAudioParams(events []protocolEvent) error {
	for _, event := range events {
		if event.Direction != "recv" || event.Type != MessageTypeHello {
			continue
		}
		if event.SampleRate > 0 && event.Channels > 0 && event.FrameMs > 0 && strings.TrimSpace(event.AudioFormat) != "" {
			return nil
		}
	}
	return fmt.Errorf("hello 响应未携带完整 audio_params")
}

func assertIotSuccess(events []protocolEvent, expectedText string) error {
	for _, event := range events {
		if event.Direction != "recv" || event.Type != MessageTypeIot {
			continue
		}
		if event.State != MessageStateSuccess {
			return fmt.Errorf("iot 响应状态异常: got=%s want=%s", event.State, MessageStateSuccess)
		}
		if strings.TrimSpace(expectedText) != "" && strings.TrimSpace(event.Text) != strings.TrimSpace(expectedText) {
			return fmt.Errorf("iot 响应文本不符合预期: got=%q want=%q", event.Text, expectedText)
		}
		return nil
	}
	return fmt.Errorf("未收到 iot success 响应")
}

func assertTTSSentenceEnd(events []protocolEvent) error {
	for _, event := range events {
		if event.Direction == "recv" && event.Type == ServerMessageTypeTTS && event.State == MessageStateSentenceEnd {
			return nil
		}
	}
	return fmt.Errorf("未收到 tts sentence_end")
}

func assertNoServerGoodbye(events []protocolEvent) error {
	for _, event := range events {
		if event.Direction == "recv" && event.Type == MessageTypeGoodBye {
			return fmt.Errorf("不应收到服务端 goodbye 回包")
		}
	}
	return nil
}

func assertAbortLeadsToTTSStop(events []protocolEvent) error {
	abortIndex := -1
	for idx, event := range events {
		if event.Direction == "send" && event.Type == MessageTypeAbort {
			abortIndex = idx
			break
		}
	}
	if abortIndex == -1 {
		return fmt.Errorf("未找到 abort 事件，无法验证 tts stop")
	}
	for idx := abortIndex + 1; idx < len(events); idx++ {
		event := events[idx]
		if event.Direction == "recv" && event.Type == ServerMessageTypeTTS && event.State == MessageStateStop {
			return nil
		}
	}
	return fmt.Errorf("abort 之后未收到 tts stop")
}

func assertNoAcceptedBinaryAfterTTSStop(events []protocolEvent) error {
	stopIndex := -1
	for idx, event := range events {
		if event.Direction == "recv" && event.Type == ServerMessageTypeTTS && event.State == MessageStateStop {
			stopIndex = idx
			break
		}
	}
	if stopIndex == -1 {
		return fmt.Errorf("未找到 tts stop，无法验证 stop 后无音频")
	}
	for idx := stopIndex + 1; idx < len(events); idx++ {
		event := events[idx]
		if event.Direction == "recv_binary" && event.Note == "accepted" {
			return fmt.Errorf("tts stop 之后仍收到了被接受的音频帧")
		}
	}
	return nil
}

func assertMCPMethodCount(events []protocolEvent, method string, expectedCount int) error {
	if expectedCount <= 0 {
		expectedCount = 1
	}
	count := 0
	for _, event := range events {
		if event.Direction == "recv" && event.Type == MessageTypeMcp && event.MCPMethod == method {
			count++
		}
	}
	if count != expectedCount {
		return fmt.Errorf("MCP %s 数量不符合预期: got=%d want=%d", method, count, expectedCount)
	}
	return nil
}

func assertMCPResultsMatchRequests(events []protocolEvent, method string, expectedCount int) error {
	if expectedCount <= 0 {
		return nil
	}
	requestIDs := make([]string, 0, expectedCount)
	for _, event := range events {
		if event.Direction == "recv" && event.Type == MessageTypeMcp && event.MCPMethod == method {
			if strings.TrimSpace(event.MCPID) == "" {
				return fmt.Errorf("MCP %s 请求缺少 id", method)
			}
			requestIDs = append(requestIDs, event.MCPID)
		}
	}
	if len(requestIDs) != expectedCount {
		return fmt.Errorf("MCP %s 请求数量不符合预期，无法校验 result: got=%d want=%d", method, len(requestIDs), expectedCount)
	}
	resultIDs := make(map[string]int, len(requestIDs))
	for _, event := range events {
		if event.Direction == "send" && event.Type == MessageTypeMcp && event.Note == "result" && strings.TrimSpace(event.MCPID) != "" {
			resultIDs[event.MCPID]++
		}
	}
	for _, requestID := range requestIDs {
		if resultIDs[requestID] != 1 {
			return fmt.Errorf("MCP %s 请求 id=%s 的 result 数量不符合预期: got=%d want=1", method, requestID, resultIDs[requestID])
		}
	}
	return nil
}

func normalizeAssertText(text string) string {
	replacer := strings.NewReplacer(" ", "", "\t", "", "\n", "", "\r", "", "，", "", "。", "", "？", "", "?", "", "！", "", "!", "")
	return replacer.Replace(strings.ToLower(strings.TrimSpace(text)))
}
