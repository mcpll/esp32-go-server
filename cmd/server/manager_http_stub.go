//go:build !manager

package main

import log "xiaozhi-esp32-server-golang/logger"

// StartManagerHTTP 未启用 manager 编译时的空实现。需使用 -tags manager 编译以启用内嵌 manager HTTP。
func StartManagerHTTP(configPath string) {
	log.Warn("manager 内嵌未编译进本二进制，请使用 -tags manager 重新编译以启用")
}

// StopManagerHTTP 未启用 manager 编译时的空实现。
func StopManagerHTTP() {}
