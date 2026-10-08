package pocketbase

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCommandMovesFromPendingThroughRunningToDone(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	started := make(chan struct{})
	release := make(chan struct{})
	p.RegisterCommand("inject_msg", func(ctx context.Context, payload map[string]any) (any, error) {
		if payload["device"] != testDevice || payload["message"] != "ciao dalla console" || payload["skip_llm"] != true {
			t.Errorf("handler payload = %#v", payload)
		}
		close(started)
		<-release
		return map[string]any{"ok": true}, nil
	})
	rec := fake.seed("commands", map[string]any{
		"type":    "inject_msg",
		"status":  "pending",
		"created": time.Now().UTC().Add(-time.Second).Format(pocketBaseDateLayout),
		"payload": map[string]any{
			"device":      testDevice,
			"message":     "ciao dalla console",
			"skip_llm":    true,
			"auto_listen": false,
		},
	})
	id := rec["id"].(string)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p.Start(ctx, func(map[string]interface{}) {})
	p.ArmCommands()

	waitFor(t, func() bool {
		cur := fake.find("commands", "id", id)
		return cur != nil && cur["status"] == "running"
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("inject handler was not called")
	}
	close(release)
	waitFor(t, func() bool {
		cur := fake.find("commands", "id", id)
		if cur == nil || cur["status"] != "done" {
			return false
		}
		result, _ := cur["result"].(map[string]any)
		return result["ok"] == true
	})
}

func TestCommandErrorIsTheHandlerMessage(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	p.RegisterCommand("inject_msg", func(ctx context.Context, payload map[string]any) (any, error) {
		return nil, fmt.Errorf("device %s not found or offline", testDevice)
	})
	id := seedPending(fake, "inject_msg", map[string]any{
		"device": testDevice, "message": "ciao", "skip_llm": true,
	}, time.Now().UTC())

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p.Start(ctx, func(map[string]interface{}) {})
	p.ArmCommands()

	waitFor(t, func() bool {
		cur := fake.find("commands", "id", id)
		return cur != nil && cur["status"] == "error" && cur["error"] == "device "+testDevice+" not found or offline"
	})
}

func TestPendingCommandOlderThanSixtySecondsExpires(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }
	p.RegisterCommand("inject_msg", func(ctx context.Context, payload map[string]any) (any, error) {
		if payload["message"] != "in tempo" {
			t.Errorf("ran %v", payload["message"])
		}
		return map[string]any{"ok": true}, nil
	})
	stale := seedPending(fake, "inject_msg", map[string]any{"device": testDevice, "message": "tardi"}, now.Add(-61*time.Second))
	fresh := seedPending(fake, "inject_msg", map[string]any{"device": testDevice, "message": "in tempo"}, now.Add(-60*time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p.Start(ctx, func(map[string]interface{}) {})
	p.ArmCommands()

	waitFor(t, func() bool {
		old := fake.find("commands", "id", stale)
		young := fake.find("commands", "id", fresh)
		return old != nil && old["status"] == "error" && old["error"] == "expired" &&
			young != nil && young["status"] == "done"
	})
}

func TestCommandCreatedWhileDisconnectedRunsAfterReconnect(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	p.RegisterCommand("inject_msg", func(ctx context.Context, payload map[string]any) (any, error) {
		if payload["message"] != "dopo" {
			t.Errorf("message = %#v", payload["message"])
		}
		return map[string]any{"ok": true}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p.Start(ctx, func(map[string]interface{}) {})
	p.ArmCommands()
	waitFor(t, func() bool { return fake.subscriptionCount() >= 1 })
	if !contains(fake.lastSubscriptions(), "commands/*") {
		t.Fatalf("subscriptions = %v", fake.lastSubscriptions())
	}

	fake.holdSSE(true)
	fake.dropStreams()
	waitFor(t, func() bool { return fake.streamCount() == 0 })
	id := seedPending(fake, "inject_msg", map[string]any{
		"device": testDevice, "message": "dopo", "skip_llm": false,
	}, time.Now().UTC())
	fake.holdSSE(false)

	waitFor(t, func() bool {
		cur := fake.find("commands", "id", id)
		return cur != nil && cur["status"] == "done"
	})
}

func TestStaleCommandExpiresWhenTheServerReconnects(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }
	p.RegisterCommand("inject_msg", func(ctx context.Context, payload map[string]any) (any, error) {
		t.Error("expired command was run")
		return map[string]any{"ok": true}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p.Start(ctx, func(map[string]interface{}) {})
	p.ArmCommands()
	waitFor(t, func() bool { return fake.subscriptionCount() >= 1 })

	fake.holdSSE(true)
	fake.dropStreams()
	waitFor(t, func() bool { return fake.streamCount() == 0 })
	id := seedPending(fake, "inject_msg", map[string]any{"message": "scaduto"}, now.Add(-2*time.Minute))
	fake.holdSSE(false)

	waitFor(t, func() bool {
		cur := fake.find("commands", "id", id)
		return cur != nil && cur["status"] == "error" && cur["error"] == "expired"
	})
}

func TestSettingsReloadRereadsSettings(t *testing.T) {
	fake := newFakePB(t)
	p := newTestProvider(t, fake)
	setting := fake.seed("settings", map[string]any{
		"key": "ota",
		"value": map[string]any{
			"test": map[string]any{"websocket": map[string]any{"url": "ws://old/xiaozhi/v1/"}},
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	urls := make(chan string, 8)
	p.Start(ctx, func(cfg map[string]interface{}) { urls <- otaTestURL(cfg) })
	p.ArmCommands()
	waitForURL(t, urls, "ws://old/xiaozhi/v1/")

	settingID := setting["id"].(string)
	fake.patch("settings", settingID, map[string]any{
		"value": map[string]any{
			"test": map[string]any{"websocket": map[string]any{"url": "ws://reloaded/xiaozhi/v1/"}},
		},
	})
	cmdID := seedPending(fake, "settings_reload", map[string]any{}, time.Now().UTC())
	fake.publish("commands/"+cmdID, `{"action":"create","record":{}}`)

	waitForURL(t, urls, "ws://reloaded/xiaozhi/v1/")
	waitFor(t, func() bool {
		cur := fake.find("commands", "id", cmdID)
		if cur == nil || cur["status"] != "done" {
			return false
		}
		result, _ := cur["result"].(map[string]any)
		return result["ok"] == true
	})
}

func seedPending(fake *fakePB, commandType string, payload map[string]any, created time.Time) string {
	rec := fake.seed("commands", map[string]any{
		"type":    commandType,
		"status":  "pending",
		"created": created.UTC().Format(pocketBaseDateLayout),
		"payload": payload,
	})
	return rec["id"].(string)
}
