//go:build !asr_server

package main

import log "xiaozhi-esp32-server-golang/logger"

// StartAsrServerHTTP no-op when built without asr_server. Use -tags asr_server to embed asr_server.
func StartAsrServerHTTP(configPath string) {
	log.Warn("Embedded asr_server not compiled into this binary; rebuild with -tags asr_server to enable")
}

// StopAsrServerHTTP no-op when built without asr_server.
func StopAsrServerHTTP() {}
