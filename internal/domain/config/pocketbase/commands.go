package pocketbase

import (
	"context"
	"fmt"
	"time"

	log "xiaozhi-esp32-server-golang/logger"
)

const (
	commandPending = "pending"
	commandRunning = "running"
	commandDone    = "done"
	commandError   = "error"

	// commandExpiry is how long a pending command may wait before a scan marks it expired.
	commandExpiry = 60 * time.Second
)

// CommandHandler runs one command. A nil result is stored as {"ok": true}.
type CommandHandler func(ctx context.Context, payload map[string]any) (result any, err error)

// RegisterCommand keeps the handler for a commands.type value.
// settings_reload is built in and does not use this map.
func (p *Provider) RegisterCommand(commandType string, handler CommandHandler) {
	p.commandMu.Lock()
	defer p.commandMu.Unlock()
	p.commandHandlers[commandType] = handler
}

// ArmCommands starts running pending commands. Call it after the handlers are registered,
// so a command that was waiting while the server was down is not failed as unknown.
func (p *Provider) ArmCommands() {
	p.commandMu.Lock()
	p.commandsArmed = true
	ctx := p.rootCtx
	p.commandMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	p.scanCommands(ctx)
}

func (p *Provider) commandsReady() bool {
	p.commandMu.Lock()
	defer p.commandMu.Unlock()
	return p.commandsArmed
}

func (p *Provider) handlerFor(commandType string) CommandHandler {
	p.commandMu.Lock()
	defer p.commandMu.Unlock()
	return p.commandHandlers[commandType]
}

// scanCommands claims every pending record: expired ones become error, the rest become
// running and their handler runs. Events lost during a disconnect are covered because
// every reconnect calls this again.
func (p *Provider) scanCommands(ctx context.Context) {
	if !p.commandsReady() {
		return
	}
	p.scanMu.Lock()
	defer p.scanMu.Unlock()

	records, err := p.client.List(ctx, commandsCollection, `status = "pending"`, "")
	if err != nil {
		log.Warnf("pocketbase: list pending commands: %v", err)
		return
	}
	for _, rec := range records {
		p.claim(ctx, rec)
	}
}

func (p *Provider) claim(ctx context.Context, rec Record) {
	id := rec.String("id")
	if id == "" {
		return
	}
	if p.expired(rec) {
		p.finishError(ctx, id, "expired")
		return
	}
	if err := p.client.Update(ctx, commandsCollection, id, map[string]any{"status": commandRunning}); err != nil {
		log.Warnf("pocketbase: command %s running: %v", id, err)
		return
	}
	payload := rec.Object("payload")
	if payload == nil {
		payload = map[string]any{}
	}
	commandType := rec.String("type")
	handler := p.handlerFor(commandType)
	if commandType == "settings_reload" {
		handler = p.settingsReloadHandler()
	}
	go p.finish(ctx, id, commandType, payload, handler)
}

func (p *Provider) finish(ctx context.Context, id, commandType string, payload map[string]any, handler CommandHandler) {
	if handler == nil {
		p.finishError(ctx, id, fmt.Sprintf("unknown command type %s", commandType))
		return
	}
	result, err := handler(ctx, payload)
	if err != nil {
		p.finishError(ctx, id, err.Error())
		return
	}
	if result == nil {
		result = map[string]any{"ok": true}
	}
	if err := p.client.Update(ctx, commandsCollection, id, map[string]any{
		"status": commandDone,
		"result": result,
	}); err != nil {
		log.Warnf("pocketbase: command %s done: %v", id, err)
	}
}

func (p *Provider) finishError(ctx context.Context, id, message string) {
	if err := p.client.Update(ctx, commandsCollection, id, map[string]any{
		"status": commandError,
		"error":  message,
	}); err != nil {
		log.Warnf("pocketbase: command %s error: %v", id, err)
	}
}

func (p *Provider) expired(rec Record) bool {
	created, ok := parsePocketBaseTime(rec.String("created"))
	if !ok {
		return false
	}
	now := time.Now()
	if p.now != nil {
		now = p.now()
	}
	return now.Sub(created) > commandExpiry
}

func parsePocketBaseTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{pocketBaseDateLayout, time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func (p *Provider) settingsReloadHandler() CommandHandler {
	return func(ctx context.Context, _ map[string]any) (any, error) {
		p.commandMu.Lock()
		onSystemConfig := p.onSystemConfig
		p.commandMu.Unlock()
		cfg, err := p.LoadSystemConfig(ctx)
		if err != nil {
			return nil, err
		}
		if onSystemConfig != nil {
			onSystemConfig(cfg)
		}
		return map[string]any{"ok": true}, nil
	}
}
