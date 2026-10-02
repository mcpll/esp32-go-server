package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"xiaozhi-esp32-server-golang/internal/app/server"
	user_config "xiaozhi-esp32-server-golang/internal/domain/config"
	"xiaozhi-esp32-server-golang/internal/domain/config/store"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/spf13/viper"
)

func main() {
	// parse command-line flags
	configFile := flag.String("c", defaultConfigFilePath, "config file path")
	asrEnable := flag.Bool("asr-enable", defaultAsrEnable, "run the embedded asr_server")
	asrConfig := flag.String("asr-config", "", "asr_server config file path, optional when it is enabled, default asr_server/config.json")
	flag.Parse()

	if *configFile == "" {
		fmt.Println("config file path is required")
		return
	}

	if *asrEnable {
		StartAsrServerHTTP(*asrConfig)
	}
	err := Init(*configFile)
	if err != nil {
		return
	}

	// start pprof from config
	if viper.GetBool("server.pprof.enable") {
		pprofPort := viper.GetInt("server.pprof.port")
		go func() {
			log.Infof("Starting pprof service, port: %d", pprofPort)
			if err := http.ListenAndServe(fmt.Sprintf(":%d", pprofPort), nil); err != nil {
				log.Errorf("pprof service start failed: %v", err)
			}
		}()
		log.Infof("pprof address: http://localhost:%d/debug/pprof/", pprofPort)
	} else {
		log.Info("pprof service disabled")
	}

	// create server
	appInstance := server.NewApp()

	var lock sync.RWMutex
	// register system config hot-reload: compare viper config with the new config; merge and hot-reload only when content changed
	user_config.RegisterSystemConfigHandler(func(data map[string]interface{}) {
		lock.Lock()
		defer lock.Unlock()
		current := store.AllSettings()
		oldMqttServer := current["mqtt_server"]
		oldMqtt := current["mqtt"]
		oldUdp := current["udp"]
		oldMcp := current["mcp"]
		oldLocalMcp := current["local_mcp"]

		var doMqttServer, doMqttReload, doUdpReload, doMcpReload bool
		if data["mqtt_server"] != nil {
			if !SystemConfigEqual(data["mqtt_server"], oldMqttServer) {
				doMqttServer = true
			}
		}
		if data["mqtt"] != nil {
			if !SystemConfigEqual(data["mqtt"], oldMqtt) {
				doMqttReload = true
			}
		}
		if data["udp"] != nil {
			if udpListenChanged(data["udp"], oldUdp) {
				doUdpReload = true
			}
		}
		if data["mcp"] != nil {
			if !SystemConfigEqual(data["mcp"], oldMcp) {
				doMcpReload = true
			}
		}
		if data["local_mcp"] != nil {
			if !SystemConfigEqual(data["local_mcp"], oldLocalMcp) {
				doMcpReload = true
			}
		}

		ApplySystemConfigToViper(data)

		var wg sync.WaitGroup
		if doMqttServer {
			wg.Add(1)
			go func() {
				defer wg.Done()
				appInstance.ReloadMqttServer()
			}()
		}
		if doMqttReload || doUdpReload {
			wg.Add(1)
			go func() {
				defer wg.Done()
				appInstance.ReloadMqttUdpWithFlags(doMqttReload, doUdpReload)
			}()
		}
		if doMcpReload {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := appInstance.ReloadMCP(); err != nil {
					log.Errorf("ReloadMCP failed: %v", err)
				}
			}()
		}
		wg.Wait()
	})
	appInstance.Run()

	// block waiting for exit signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Info("Server started, press Ctrl+C to exit")
	<-quit

	log.Info("Shutting down server...")

	if *asrEnable {
		StopAsrServerHTTP()
	}

	log.Info("Server closed")
}

func udpListenChanged(newUdpCfg interface{}, oldUdpCfg interface{}) bool {
	newListenHost, newListenPort := udpListenHostPort(newUdpCfg)
	oldListenHost, oldListenPort := udpListenHostPort(oldUdpCfg)
	if newListenHost == "" && newListenPort == 0 {
		return false
	}
	return newListenHost != oldListenHost || newListenPort != oldListenPort
}

func udpListenHostPort(cfg interface{}) (string, int) {
	if cfg == nil {
		return "", 0
	}
	type udpListen struct {
		ListenHost string `json:"listen_host"`
		ListenPort int    `json:"listen_port"`
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", 0
	}
	var parsed udpListen
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", 0
	}
	return parsed.ListenHost, parsed.ListenPort
}
