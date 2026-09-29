package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/audio"
	"xiaozhi-esp32-server-golang/internal/domain/tts"
)

func buildMqttUDPTestCases(manualListenCount, auto1ListenCount, auto2ListenCount, manualMultiTurns, manualMultiListenCount int) []protocolTestCase {
	return []protocolTestCase{
		{
			Name:                 "mqtt_udp_manual_roundtrip",
			Description:          "验证 MQTT/UDP manual 模式 listen/stt/tts 主链路",
			Kind:                 protocolCaseNormal,
			UseMqttUDP:           true,
			LocalMode:            LocalModeManual,
			InputText:            speectText,
			Turns:                autoTurns,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
				{State: MessageStateStop, Mode: LocalModeManual},
			},
			ExpectListenCount: manualListenCount,
			ExpectSTTText:     speectText,
			ExpectOutputText:  true,
		},
		{
			Name:                 "mqtt_udp_auto1_roundtrip",
			Description:          "验证 MQTT/UDP auto1 模式 detect -> start auto -> tts stop 后重启监听",
			Kind:                 protocolCaseNormal,
			UseMqttUDP:           true,
			LocalMode:            LocalModeAuto1,
			InputText:            speectText,
			Turns:                autoTurns,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: "auto"},
				{State: MessageStateStart, Mode: "auto"},
			},
			ExpectListenCount:     auto1ListenCount,
			ExpectSTTText:         speectText,
			ExpectOutputText:      true,
			ExpectRestartAfterTTS: true,
		},
		{
			Name:                 "mqtt_udp_auto2_roundtrip",
			Description:          "验证 MQTT/UDP auto2 模式 start auto -> detect -> tts stop 后重启监听",
			Kind:                 protocolCaseNormal,
			UseMqttUDP:           true,
			LocalMode:            LocalModeAuto2,
			InputText:            speectText,
			Turns:                autoTurns,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: "auto"},
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: "auto"},
			},
			ExpectListenCount:     auto2ListenCount,
			ExpectSTTText:         speectText,
			ExpectOutputText:      true,
			ExpectRestartAfterTTS: true,
		},
		{
			Name:                 "mqtt_udp_realtime_roundtrip",
			Description:          "验证 MQTT/UDP realtime 模式 detect -> start realtime",
			Kind:                 protocolCaseNormal,
			UseMqttUDP:           true,
			LocalMode:            LocalModeRealtime,
			InputText:            speectText,
			Turns:                autoTurns,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: LocalModeRealtime},
			},
			ExpectListenCount: 2,
			ExpectSTTText:     speectText,
			ExpectOutputText:  true,
		},
		{
			Name:                 "mqtt_udp_manual_multi_turn",
			Description:          "验证 MQTT/UDP manual 模式固定 3 轮稳定性",
			Kind:                 protocolCaseNormal,
			UseMqttUDP:           true,
			LocalMode:            LocalModeManual,
			InputText:            speectText,
			Turns:                manualMultiTurns,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
				{State: MessageStateStop, Mode: LocalModeManual},
			},
			ExpectListenCount: manualMultiListenCount,
			ExpectSTTText:     speectText,
			ExpectOutputText:  true,
		},
		{
			Name:                     "mqtt_udp_mcp_initialize",
			Description:              "验证 MQTT/UDP hello 启用 MCP 后 initialize/tools/list 链路",
			Kind:                     protocolCaseMCP,
			UseMqttUDP:               true,
			LocalMode:                LocalModeManual,
			EnableMCP:                true,
			Timeout:                  autoCaseTimeout,
			ExpectHelloCount:         1,
			ExpectHelloTransport:     "udp",
			ExpectListenCount:        0,
			ExpectMCPInitialize:      true,
			ExpectMCPToolsList:       true,
			ExpectMCPResponse:        true,
			ExpectMCPInitializeCount: 1,
			ExpectMCPToolsListCount:  1,
		},
		{
			Name:                 "mqtt_udp_hello_without_mcp_no_initialize",
			Description:          "验证 MQTT/UDP 未声明 mcp feature 时无 MCP 流量",
			Kind:                 protocolCaseNoMCP,
			UseMqttUDP:           true,
			LocalMode:            LocalModeManual,
			Timeout:              defaultNegativeReadTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenCount:    0,
			ExpectNoMCPTraffic:   true,
		},
		{
			Name:                 "mqtt_udp_iot_roundtrip",
			Description:          "验证 MQTT/UDP iot 请求能收到 success 响应",
			Kind:                 protocolCaseIot,
			UseMqttUDP:           true,
			LocalMode:            LocalModeManual,
			InputText:            "turn_on_light",
			Timeout:              defaultNegativeReadTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectIotSuccess:     true,
		},
		{
			Name:                 "mqtt_udp_duplicate_hello_rehandshake",
			Description:          "验证 MQTT/UDP duplicate hello 会重建 UDP 会话且可继续对话",
			Kind:                 protocolCaseDuplicateHello,
			UseMqttUDP:           true,
			LocalMode:            LocalModeManual,
			InputText:            speectText,
			Turns:                1,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     2,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
				{State: MessageStateStop, Mode: LocalModeManual},
			},
			ExpectListenCount: 2,
			ExpectSTTText:     speectText,
			ExpectOutputText:  true,
		},
		{
			Name:        "mqtt_udp_invalid_hello_missing_audio_params",
			Description: "验证 MQTT/UDP hello 缺少 audio_params 时不会收到成功握手",
			Kind:        protocolCaseInvalidHello,
			UseMqttUDP:  true,
			LocalMode:   LocalModeManual,
			Timeout:     defaultNegativeReadTimeout,
		},
		{
			Name:        "mqtt_udp_invalid_hello_unsupported_transport",
			Description: "验证 MQTT/UDP hello transport 非法时不会收到成功握手",
			Kind:        protocolCaseInvalidHello,
			UseMqttUDP:  true,
			LocalMode:   LocalModeManual,
			InputText:   "bad_transport",
			Timeout:     defaultNegativeReadTimeout,
		},
		{
			Name:                 "mqtt_udp_listen_before_hello_ignored",
			Description:          "验证 MQTT/UDP hello 前 listen 会被忽略",
			Kind:                 protocolCaseListenBeforeHello,
			UseMqttUDP:           true,
			LocalMode:            LocalModeManual,
			Timeout:              defaultNegativeReadTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
			},
			ExpectListenCount: 1,
		},
		{
			Name:                 "mqtt_udp_abort_after_listen_start",
			Description:          "验证 MQTT/UDP listen start 后 abort 控制链路",
			Kind:                 protocolCaseAbort,
			UseMqttUDP:           true,
			LocalMode:            LocalModeManual,
			Timeout:              defaultNegativeReadTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
			},
			ExpectListenCount: 1,
			ExpectAbortSent:   true,
		},
		{
			Name:                 "mqtt_udp_abort_during_tts",
			Description:          "验证 MQTT/UDP TTS 期间 abort 会停止播报",
			Kind:                 protocolCaseAbortDuringTTS,
			UseMqttUDP:           true,
			LocalMode:            LocalModeManual,
			InputText:            speectText,
			Turns:                1,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
				{State: MessageStateStop, Mode: LocalModeManual},
			},
			ExpectListenCount:   2,
			ExpectSTTText:       speectText,
			ExpectOutputText:    true,
			ExpectAbortSent:     true,
			ExpectAbortStopsTTS: true,
		},
		{
			Name:                 "mqtt_udp_realtime_interrupt",
			Description:          "验证 MQTT/UDP realtime 下第二轮输入可打断首轮播报",
			Kind:                 protocolCaseRealtimeInterrupt,
			UseMqttUDP:           true,
			LocalMode:            LocalModeRealtime,
			InputText:            speectText,
			InterruptText:        speectText + "继续",
			Turns:                1,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: LocalModeRealtime},
			},
			ExpectListenCount: 2,
			ExpectSTTText:     speectText,
			ExpectOutputText:  true,
		},
		{
			Name:                 "mqtt_udp_realtime_listen_stop",
			Description:          "验证 MQTT/UDP realtime 显式 stop/start 后仍可继续对话",
			Kind:                 protocolCaseRealtimeListenStop,
			UseMqttUDP:           true,
			LocalMode:            LocalModeRealtime,
			InputText:            speectText,
			Turns:                1,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: LocalModeRealtime},
				{State: MessageStateStop},
				{State: MessageStateStart, Mode: LocalModeRealtime},
			},
			ExpectListenCount: 4,
			ExpectSTTText:     speectText,
			ExpectOutputText:  true,
		},
		{
			Name:                 "mqtt_udp_realtime_duplicate_start_ignored",
			Description:          "验证 MQTT/UDP realtime 重复 listen start 不破坏后续链路",
			Kind:                 protocolCaseRealtimeDuplicateStart,
			UseMqttUDP:           true,
			LocalMode:            LocalModeRealtime,
			InputText:            speectText,
			Turns:                1,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: LocalModeRealtime},
				{State: MessageStateStart, Mode: LocalModeRealtime},
			},
			ExpectListenCount: 3,
			ExpectSTTText:     speectText,
			ExpectOutputText:  true,
		},
		{
			Name:                 "mqtt_udp_goodbye_then_resume",
			Description:          "验证 MQTT/UDP 发送 goodbye 后 duplicate hello 并恢复对话",
			Kind:                 protocolCaseGoodbyeThenResume,
			UseMqttUDP:           true,
			LocalMode:            LocalModeManual,
			InputText:            speectText,
			Turns:                1,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     2,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
				{State: MessageStateStop, Mode: LocalModeManual},
			},
			ExpectListenCount:     2,
			ExpectSTTText:         speectText,
			ExpectOutputText:      true,
			ExpectNoServerGoodbye: true,
		},
		{
			Name:                 "mqtt_udp_tts_sentence_boundaries",
			Description:          "验证 MQTT/UDP 会发送 sentence_start/sentence_end",
			Kind:                 protocolCaseTTSSentenceBoundaries,
			UseMqttUDP:           true,
			LocalMode:            LocalModeManual,
			InputText:            speectText,
			Turns:                1,
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
				{State: MessageStateStop, Mode: LocalModeManual},
			},
			ExpectListenCount: 2,
			ExpectSTTText:     speectText,
			ExpectOutputText:  true,
			ExpectSentenceEnd: true,
		},
		{
			Name:                 "mqtt_udp_injected_message_warm_path",
			Description:          "验证 MQTT/UDP 已建链时 speak_request/speak_ready/UDP 下行播报/goodbye",
			Kind:                 protocolCaseInjectedMessage,
			UseMqttUDP:           true,
			LocalMode:            LocalModeManual,
			InputText:            "MQTT主动播报测试",
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     1,
			ExpectHelloTransport: "udp",
			ExpectOutputText:     true,
		},
		{
			Name:                  "mqtt_udp_injected_message_auto_listen",
			Description:           "验证 MQTT/UDP 主动播报 auto_listen=true 不会收到服务端 goodbye",
			Kind:                  protocolCaseInjectedMessageAuto,
			UseMqttUDP:            true,
			LocalMode:             LocalModeManual,
			InputText:             "MQTT主动播报自动续听",
			Timeout:               autoCaseTimeout,
			ExpectHelloCount:      1,
			ExpectHelloTransport:  "udp",
			ExpectOutputText:      true,
			ExpectNoServerGoodbye: true,
		},
		{
			Name:                 "mqtt_udp_injected_message_cold_rehello",
			Description:          "验证 MQTT/UDP 冷链路收到 speak_request 后通过重复 hello 重建 UDP 再回 speak_ready",
			Kind:                 protocolCaseInjectedMessageCold,
			UseMqttUDP:           true,
			LocalMode:            LocalModeManual,
			InputText:            "MQTT主动播报冷链路",
			Timeout:              autoCaseTimeout,
			ExpectHelloCount:     2,
			ExpectHelloTransport: "udp",
			ExpectOutputText:     true,
		},
	}
}

func runMqttUDPCase(serverAddr, deviceID string, testCase *protocolTestCase) error {
	switch testCase.Kind {
	case protocolCaseMqttUDPHello:
		return runMqttUDPHelloCase(serverAddr, deviceID, testCase)
	case protocolCaseInjectedMessage:
		return runMqttInjectedMessageCase(serverAddr, deviceID, testCase, false, false)
	case protocolCaseInjectedMessageAuto:
		return runMqttInjectedMessageCase(serverAddr, deviceID, testCase, true, false)
	case protocolCaseInjectedMessageCold:
		return runMqttInjectedMessageCase(serverAddr, deviceID, testCase, false, true)
	case protocolCaseInvalidHello:
		return runMqttInvalidHelloCase(serverAddr, deviceID, testCase)
	case protocolCaseListenBeforeHello:
		return runMqttListenBeforeHelloCase(serverAddr, deviceID, testCase)
	default:
		return runMqttConversationCase(serverAddr, deviceID, testCase)
	}
}

func runMqttConversationCase(serverAddr, deviceID string, testCase *protocolTestCase) error {
	resp, rt, err := newMqttCaseRuntime(serverAddr, deviceID)
	if err != nil {
		return err
	}
	_ = resp
	defer rt.close()

	enableMCP := testCase.EnableMCP || testCase.Kind == protocolCaseMCP
	helloMsg, err := sendMqttHelloAndBindUDP(rt, deviceID, enableMCP)
	if err != nil {
		return err
	}

	runTurn, cleanupRealtime := prepareMqttRunTurn(rt)
	defer cleanupRealtime()

	switch testCase.Kind {
	case protocolCaseMCP:
		if err := waitForMCPEvent(rt.mcpCh, testCase.Timeout, func(event protocolEvent) bool {
			return event.Direction == "recv" && event.MCPMethod == "initialize"
		}, "MQTT MCP initialize"); err != nil {
			return err
		}
		if err := waitForMCPEvent(rt.mcpCh, testCase.Timeout, func(event protocolEvent) bool {
			return event.Direction == "recv" && event.MCPMethod == "tools/list"
		}, "MQTT MCP tools/list"); err != nil {
			return err
		}
		if err := waitForMCPResultCountEvents(rt.snapshotEvents, 2, testCase.Timeout); err != nil {
			return err
		}
		return evaluateProtocolCaseFromEvents(rt.snapshotEvents(), testCase)
	case protocolCaseNoMCP:
		if err := waitForNoMCPEvent(rt.mcpCh, testCase.Timeout, "MQTT hello 后 MCP 流量"); err != nil {
			return err
		}
		return evaluateProtocolCaseFromEvents(rt.snapshotEvents(), testCase)
	case protocolCaseIot:
		if err := rt.publish(ClientMessage{Type: MessageTypeIot, DeviceID: deviceID, Text: testCase.InputText}); err != nil {
			return err
		}
		if _, err := waitForMessage(rt.iotCh, testCase.Timeout, "mqtt iot"); err != nil {
			return err
		}
		return evaluateProtocolCaseFromEvents(rt.snapshotEvents(), testCase)
	case protocolCaseDuplicateHello:
		prevNonce := helloMsg.Udp.Nonce
		secondHello, err := sendMqttHelloAndBindUDP(rt, deviceID, enableMCP)
		if err != nil {
			return err
		}
		if prevNonce == secondHello.Udp.Nonce {
			return fmt.Errorf("duplicate hello 后 UDP nonce 未变化")
		}
	case protocolCaseAbort:
		if err := rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateStart, Mode: LocalModeManual}); err != nil {
			return err
		}
		if err := rt.publish(ClientMessage{Type: MessageTypeAbort, SessionID: rt.sessionID}); err != nil {
			return err
		}
		time.Sleep(testCase.Timeout)
		return evaluateProtocolCaseFromEvents(rt.snapshotEvents(), testCase)
	case protocolCaseAbortDuringTTS:
		if err := runTurn(testCase.InputText); err != nil {
			return err
		}
		if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "mqtt stt before abort"); err != nil {
			return err
		}
		if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "mqtt tts start before abort"); err != nil {
			return err
		}
		if _, err := waitForMessage(rt.outputCh, testCase.Timeout, "mqtt output before abort"); err != nil {
			return err
		}
		if err := rt.publish(ClientMessage{Type: MessageTypeAbort, SessionID: rt.sessionID}); err != nil {
			return err
		}
		if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "mqtt tts stop after abort"); err != nil {
			return err
		}
		return evaluateProtocolCaseFromEvents(rt.snapshotEvents(), testCase)
	case protocolCaseRealtimeInterrupt:
		if err := runTurn(testCase.InputText); err != nil {
			return err
		}
		if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "mqtt realtime first stt"); err != nil {
			return err
		}
		if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "mqtt realtime first tts start"); err != nil {
			return err
		}
		if err := runTurn(testCase.InterruptText); err != nil {
			return err
		}
		if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "mqtt realtime interrupt stt"); err != nil {
			return err
		}
		if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "mqtt realtime interrupt tts stop"); err != nil {
			return err
		}
		return evaluateProtocolCaseFromEvents(rt.snapshotEvents(), testCase)
	case protocolCaseRealtimeListenStop:
		time.Sleep(200 * time.Millisecond)
		if err := rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateStop, Mode: LocalModeRealtime}); err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
		if err := rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateStart, Mode: LocalModeRealtime}); err != nil {
			return err
		}
		if err := runTurn(testCase.InputText); err != nil {
			return err
		}
		if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "mqtt realtime stop stt"); err != nil {
			return err
		}
		if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "mqtt realtime stop tts start"); err != nil {
			return err
		}
		if _, err := waitForMessage(rt.outputCh, testCase.Timeout, "mqtt realtime stop output"); err != nil {
			return err
		}
		if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "mqtt realtime stop tts stop"); err != nil {
			return err
		}
		return evaluateProtocolCaseFromEvents(rt.snapshotEvents(), testCase)
	case protocolCaseRealtimeDuplicateStart:
		time.Sleep(200 * time.Millisecond)
		if err := rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateStart, Mode: LocalModeRealtime}); err != nil {
			return err
		}
		if err := runTurn(testCase.InputText); err != nil {
			return err
		}
		if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "mqtt realtime duplicate start stt"); err != nil {
			return err
		}
		if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "mqtt realtime duplicate start tts start"); err != nil {
			return err
		}
		if _, err := waitForMessage(rt.outputCh, testCase.Timeout, "mqtt realtime duplicate start output"); err != nil {
			return err
		}
		if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "mqtt realtime duplicate start tts stop"); err != nil {
			return err
		}
		return evaluateProtocolCaseFromEvents(rt.snapshotEvents(), testCase)
	case protocolCaseGoodbyeThenResume:
		if err := rt.publish(ClientMessage{Type: MessageTypeGoodBye, SessionID: rt.sessionID}); err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
		if _, err := sendMqttHelloAndBindUDP(rt, deviceID, enableMCP); err != nil {
			return err
		}
	}

	for turn := 0; turn < testCase.Turns; turn++ {
		if err := runTurn(testCase.InputText); err != nil {
			return err
		}
		if _, err := waitForMessage(rt.sttCh, testCase.Timeout, "mqtt stt"); err != nil {
			return err
		}
		if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "mqtt tts start"); err != nil {
			return err
		}
		if testCase.ExpectOutputText {
			if _, err := waitForMessage(rt.outputCh, testCase.Timeout, "mqtt output"); err != nil {
				return err
			}
		}
		if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "mqtt tts stop"); err != nil {
			return err
		}
		maybeRestartMqttListenAfterTTSStop(rt)
		time.Sleep(200 * time.Millisecond)
	}

	return evaluateProtocolCaseFromEvents(rt.snapshotEvents(), testCase)
}

func runMqttInvalidHelloCase(serverAddr, deviceID string, testCase *protocolTestCase) error {
	_, rt, err := newMqttCaseRuntime(serverAddr, deviceID)
	if err != nil {
		return err
	}
	defer rt.close()

	transport := "udp"
	audioParams := defaultAudioFormat()
	if strings.TrimSpace(testCase.InputText) == "bad_transport" {
		transport = "invalid_transport"
	} else {
		audioParams = nil
	}
	if err := rt.publish(buildHelloMessage(deviceID, false, transport, audioParams)); err != nil {
		return err
	}
	select {
	case msg := <-rt.helloAckCh:
		return fmt.Errorf("非法 MQTT hello 不应收到握手: %+v", msg)
	case <-time.After(testCase.Timeout):
		return nil
	}
}

func runMqttListenBeforeHelloCase(serverAddr, deviceID string, testCase *protocolTestCase) error {
	_, rt, err := newMqttCaseRuntime(serverAddr, deviceID)
	if err != nil {
		return err
	}
	defer rt.close()

	if err := rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: "", State: MessageStateStart, Mode: LocalModeManual}); err != nil {
		return err
	}
	if err := waitForNoMessage(rt.sttCh, testCase.Timeout, "mqtt hello 前 stt"); err != nil {
		return err
	}
	if err := waitForNoMessage(rt.outputCh, testCase.Timeout, "mqtt hello 前 output"); err != nil {
		return err
	}
	if _, err := sendMqttHelloAndBindUDP(rt, deviceID, false); err != nil {
		return err
	}
	return evaluateProtocolCaseFromEvents(rt.snapshotEvents(), testCase)
}

func runMqttInjectedMessageCase(serverAddr, deviceID string, testCase *protocolTestCase, autoListen bool, coldPath bool) error {
	_, rt, err := newMqttCaseRuntime(serverAddr, deviceID)
	if err != nil {
		return err
	}
	defer rt.close()

	helloMsg, err := sendMqttHelloAndBindUDP(rt, deviceID, false)
	if err != nil {
		return err
	}
	if coldPath {
		if rt.udpClient != nil {
			rt.udpClient.Close()
			rt.udpClient = nil
		}
	} else if err := bindUDPRemoteAddr(rt); err != nil {
		return err
	}

	injectErrCh := make(chan error, 1)
	go func() {
		injectErrCh <- postInjectMessage(serverAddr, deviceID, testCase.InputText, true, autoListen)
	}()

	var speakRequest ServerMessage
	select {
	case speakRequest = <-rt.speakRequestCh:
	case err := <-injectErrCh:
		if err != nil {
			return err
		}
		return fmt.Errorf("inject_msg 在收到 speak_request 前已完成")
	case <-time.After(testCase.Timeout):
		return fmt.Errorf("等待 mqtt speak_request 超时")
	}
	if speakRequest.SessionID != helloMsg.SessionID {
		return fmt.Errorf("speak_request session_id 不匹配: got=%s want=%s", speakRequest.SessionID, helloMsg.SessionID)
	}
	if speakRequest.AutoListen == nil || *speakRequest.AutoListen != autoListen {
		return fmt.Errorf("speak_request auto_listen 不符合预期: got=%v want=%v", speakRequest.AutoListen, autoListen)
	}

	if coldPath {
		reHello, err := sendMqttHelloAndBindUDP(rt, deviceID, false)
		if err != nil {
			return err
		}
		if reHello.Udp == nil || reHello.Udp.Nonce == helloMsg.Udp.Nonce {
			return fmt.Errorf("cold-path speak_request duplicate hello 未重建 UDP 会话")
		}
	}
	if err := bindUDPRemoteAddr(rt); err != nil {
		return err
	}

	if err := rt.publish(ClientMessage{
		Type:      "speak_ready",
		SessionID: speakRequest.SessionID,
		State:     "ready",
		SpeakUDPConfig: &SpeakReadyUDPConfig{
			Ready:         true,
			ReuseExisting: !coldPath,
		},
	}); err != nil {
		return err
	}
	select {
	case err := <-injectErrCh:
		if err != nil {
			return err
		}
	case <-time.After(testCase.Timeout):
		return fmt.Errorf("等待 inject_msg 响应完成超时")
	}
	if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "mqtt injected tts start"); err != nil {
		return err
	}
	if _, err := waitForMessage(rt.outputCh, testCase.Timeout, "mqtt injected output"); err != nil {
		return err
	}
	select {
	case <-rt.udpAudioCh:
	case <-time.After(testCase.Timeout):
		return fmt.Errorf("等待 MQTT injected UDP 音频超时")
	}
	if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "mqtt injected tts stop"); err != nil {
		return err
	}
	if autoListen {
		select {
		case msg := <-rt.goodbyeCh:
			return fmt.Errorf("auto_listen=true 不应收到服务端 goodbye: %+v", msg)
		case <-time.After(defaultNegativeReadTimeout):
		}
	} else {
		if _, err := waitForMessage(rt.goodbyeCh, testCase.Timeout, "mqtt injected goodbye"); err != nil {
			return err
		}
	}
	return evaluateProtocolCaseFromEvents(rt.snapshotEvents(), testCase)
}

func newMqttCaseRuntime(serverAddr, deviceID string) (*otaResponse, *mqttProtocolRuntime, error) {
	resp, err := requestOTAConfig(serverAddr, deviceID)
	if err != nil {
		return nil, nil, err
	}
	if resp.Mqtt == nil {
		return nil, nil, skipCase("OTA 未返回 mqtt 配置，请先开启 ota.test/external.mqtt.enable")
	}
	rt, err := newMqttProtocolRuntime(deviceID, resp.Mqtt)
	if err != nil {
		return nil, nil, err
	}
	return resp, rt, nil
}

func sendMqttHelloAndBindUDP(rt *mqttProtocolRuntime, deviceID string, enableMCP bool) (ServerMessage, error) {
	if err := rt.publish(buildHelloMessage(deviceID, enableMCP, "udp", defaultAudioFormat())); err != nil {
		return ServerMessage{}, err
	}
	msg, err := waitForMessage(rt.helloAckCh, autoCaseTimeout, "mqtt hello ack")
	if err != nil {
		return ServerMessage{}, err
	}
	if err := assertMqttHelloMessage(msg); err != nil {
		return ServerMessage{}, err
	}
	if err := replaceRuntimeUDPClient(rt, msg); err != nil {
		return ServerMessage{}, err
	}
	return msg, nil
}

func replaceRuntimeUDPClient(rt *mqttProtocolRuntime, helloMsg ServerMessage) error {
	if helloMsg.Udp == nil {
		return fmt.Errorf("hello 缺少 udp 配置")
	}
	udpClient, err := newAutoUDPClient(helloMsg.Udp.Server, helloMsg.Udp.Port, helloMsg.Udp.Key, helloMsg.Udp.Nonce)
	if err != nil {
		return err
	}
	if err := udpClient.ReceiveAudioData(func(audioData []byte) {
		rt.recordIncomingBinary(len(audioData), true)
		select {
		case rt.udpAudioCh <- audioData:
		default:
		}
	}); err != nil {
		udpClient.Close()
		return err
	}
	if rt.udpClient != nil {
		rt.udpClient.Close()
	}
	rt.udpClient = udpClient
	return nil
}

func bindUDPRemoteAddr(rt *mqttProtocolRuntime) error {
	if rt.udpClient == nil {
		return fmt.Errorf("udp client 未初始化")
	}
	return rt.udpClient.SendAudioData(nil)
}

func prepareMqttRunTurn(rt *mqttProtocolRuntime) (func(string) error, func()) {
	emptyOpusData := genEmptyOpusData(SampleRate, 1, FrameDurationMs, 1000)
	realtimeStop := make(chan struct{}, 1)
	realtimeResume := make(chan struct{}, 1)
	realtimeQuit := make(chan struct{})

	if err := sendInitialMqttListenSequence(rt); err != nil {
		rt.recordEvent(protocolEvent{Direction: "note", Type: "note", Note: "sendInitialListenSequence_failed: " + err.Error(), At: time.Now()})
	}

	if mode == LocalModeRealtime && rt.udpClient != nil && emptyOpusData != nil {
		go func() {
			for {
				select {
				case <-realtimeQuit:
					return
				case <-realtimeStop:
					realtimeResume <- struct{}{}
					select {
					case <-realtimeResume:
					case <-realtimeQuit:
						return
					}
				default:
					if rt.udpClient != nil {
						_ = rt.udpClient.SendAudioData(emptyOpusData)
					}
					time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
				}
			}
		}()
	}

	runTurn := func(input string) error {
		if rt.udpClient == nil {
			return fmt.Errorf("udp client 未初始化")
		}
		if mode == LocalModeRealtime {
			realtimeStop <- struct{}{}
			<-realtimeResume
		}
		if mode == LocalModeManual {
			if err := rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateStart, Mode: protocolMode()}); err != nil {
				return err
			}
		}
		if err := streamTTSTextToMqttUDP(rt, input, 100); err != nil {
			if mode == LocalModeRealtime {
				realtimeResume <- struct{}{}
			}
			return err
		}
		if mode == LocalModeManual {
			if err := rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateStop, Mode: LocalModeManual}); err != nil {
				return err
			}
		}
		if mode == LocalModeRealtime {
			realtimeResume <- struct{}{}
		}
		return nil
	}
	cleanup := func() {
		close(realtimeQuit)
	}
	return runTurn, cleanup
}

func sendInitialMqttListenSequence(rt *mqttProtocolRuntime) error {
	switch mode {
	case LocalModeAuto1:
		if err := rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateDetect, Text: defaultDetectText}); err != nil {
			return err
		}
		if _, err := waitForOptionalTTSPlayback(rt.ttsStartCh, rt.ttsStopCh, rt.outputCh, 2*time.Second, autoCaseTimeout, "mqtt auto1 welcome"); err != nil {
			return err
		}
		return rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateStart, Mode: protocolMode()})
	case LocalModeAuto2:
		if err := rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateStart, Mode: protocolMode()}); err != nil {
			return err
		}
		if err := rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateDetect, Text: defaultDetectText}); err != nil {
			return err
		}
		if _, err := waitForOptionalTTSPlayback(rt.ttsStartCh, rt.ttsStopCh, rt.outputCh, 2*time.Second, autoCaseTimeout, "mqtt auto2 welcome"); err != nil {
			return err
		}
		return rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateStart, Mode: protocolMode()})
	case LocalModeRealtime:
		if err := rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateDetect, Text: defaultDetectText}); err != nil {
			return err
		}
		if _, err := waitForOptionalTTSPlayback(rt.ttsStartCh, rt.ttsStopCh, rt.outputCh, 2*time.Second, autoCaseTimeout, "mqtt realtime welcome"); err != nil {
			return err
		}
		return rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateStart, Mode: protocolMode()})
	default:
		return nil
	}
}

func maybeRestartMqttListenAfterTTSStop(rt *mqttProtocolRuntime) {
	switch mode {
	case LocalModeAuto1, LocalModeAuto2:
		_ = rt.publish(ClientMessage{Type: MessageTypeListen, SessionID: rt.sessionID, State: MessageStateStart, Mode: protocolMode()})
	}
}

func streamTTSTextToMqttUDP(rt *mqttProtocolRuntime, msg string, silenceTailCount int) error {
	if rt.udpClient == nil {
		return fmt.Errorf("udp client 未初始化")
	}
	ttsProvider, err := newAutoTestTTSProvider()
	if err != nil {
		return err
	}
	audioChan, err := ttsProvider.TextToSpeechStream(context.Background(), msg, SampleRate, 1, FrameDurationMs)
	if err != nil {
		return fmt.Errorf("生成语音失败: %v", err)
	}
	for audioData := range audioChan {
		if err := rt.udpClient.SendAudioData(audioData); err != nil {
			return fmt.Errorf("发送 UDP 音频帧失败: %v", err)
		}
		time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
	}
	detectStartTs = time.Now().UnixMilli()
	emptyOpusData := genEmptyOpusData(SampleRate, 1, FrameDurationMs, 1000)
	if emptyOpusData == nil {
		return fmt.Errorf("生成静音 Opus 数据失败")
	}
	for i := 0; i <= silenceTailCount; i++ {
		if err := rt.udpClient.SendAudioData(emptyOpusData); err != nil {
			return fmt.Errorf("发送 UDP 静音帧失败: %v", err)
		}
		time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
	}
	return nil
}

func newAutoTestTTSProvider() (tts.TTSProvider, error) {
	cosyVoiceConfig := map[string]interface{}{
		"api_url":        "https://tts.linkerai.cn/tts",
		"spk_id":         "OUeAo1mhq6IBExi",
		"frame_duration": FrameDurationMs,
		"target_sr":      SampleRate,
		"audio_format":   "mp3",
		"instruct_text":  "你好",
	}
	edgeConfig := map[string]interface{}{
		"voice":           "zh-CN-XiaoxiaoNeural",
		"rate":            "+0%",
		"volume":          "+0%",
		"pitch":           "+0Hz",
		"connect_timeout": 10,
		"receive_timeout": 60,
	}
	edgeOfflineConfig := map[string]interface{}{
		"server_url":        "ws://192.168.208.214:8081/tts",
		"timeout":           30.0,
		"handshake_timeout": 10.0,
		"sample_rate":       SampleRate,
		"channels":          1,
		"frame_duration":    FrameDurationMs,
	}
	providerName := strings.TrimSpace(ttsProviderName)
	var providerConfig map[string]interface{}
	switch providerName {
	case "edge_offline":
		providerConfig = edgeOfflineConfig
	case "edge":
		providerConfig = edgeConfig
	case "cosyvoice":
		providerConfig = cosyVoiceConfig
	default:
		return nil, fmt.Errorf("不支持的tts provider: %s, 可选: edge_offline|edge|cosyvoice", providerName)
	}
	return tts.GetTTSProvider(providerName, providerConfig)
}

func waitForMCPResultCountEvents(snapshot func() []protocolEvent, expected int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		count := 0
		for _, event := range snapshot() {
			if event.Direction == "send" && event.Type == MessageTypeMcp && event.Note == "result" {
				count++
			}
		}
		if count >= expected {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("等待 MQTT MCP result 数量达到 %d 超时", expected)
}

func evaluateProtocolCaseFromEvents(events []protocolEvent, testCase *protocolTestCase) error {
	return evaluateProtocolCase(&sessionRuntime{events: events}, testCase)
}

func init() {
	_ = audio.GetAudioProcesser
}
