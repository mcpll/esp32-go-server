package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
	"xiaozhi-esp32-server-golang/internal/app/server/auth"
	redisdb "xiaozhi-esp32-server-golang/internal/db/redis"
	user_config "xiaozhi-esp32-server-golang/internal/domain/config"

	log "xiaozhi-esp32-server-golang/logger"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"github.com/mitchellh/hashstructure/v2"
	logrus "github.com/sirupsen/logrus"

	"github.com/spf13/viper"
)

// Globals controlling periodic updates
var (
	configUpdateTicker *time.Ticker
	configUpdateStop   chan struct{}
	configUpdateWg     sync.WaitGroup
)

func Init(configFile string) error {
	//init config
	err := initConfig(configFile)
	if err != nil {
		fmt.Printf("initConfig err: %+v", err)
		os.Exit(1)
		return err
	}

	//init log
	initLog()

	// Init config system (including WebSocket connection)
	// Note: do not register ApplySystemConfigToViper here alone; it would run before main's callback so main would already see merged config. Merge in main's callback after reading and comparing current.
	ctx := context.Background()
	if err := user_config.InitConfigSystem(ctx); err != nil {
		fmt.Printf("config system init failed: %v\n", err)
	}

	// Fetch config from API and update
	if err := updateConfigFromAPI(); err != nil {
		fmt.Printf("fetching config from the API failed, using local config: %v\n", err)
	}

	// Start periodic config updates
	startPeriodicConfigUpdate()

	//init vad
	initVad()

	//init redis
	initRedis()

	// memory module is lazy-loaded; no explicit init needed

	//init auth
	err = initAuthManager()
	if err != nil {
		fmt.Printf("initAuthManager err: %+v", err)
		os.Exit(1)
		return err
	}

	return nil
}

// startPeriodicConfigUpdate starts periodic config updates
func startPeriodicConfigUpdate() {
	// Update interval from config; default 5 minutes
	updateInterval := viper.GetDuration("config_provider.update_interval")
	if updateInterval <= 0 {
		updateInterval = 30 * time.Second
	}

	// Check whether periodic updates are enabled
	if !viper.GetBool("config_provider.enable_periodic_update") {
		log.Info("Periodic config update disabled")
		return
	}

	configUpdateStop = make(chan struct{})
	configUpdateTicker = time.NewTicker(updateInterval)

	configUpdateWg.Add(1)
	go func() {
		defer configUpdateWg.Done()
		defer configUpdateTicker.Stop()

		for {
			select {
			case <-configUpdateTicker.C:
				if err := updateConfigFromAPI(); err != nil {
					log.Warnf("Periodic config update failed: %v", err)
				} else {
					//log.Debug("Periodic config update succeeded")
				}
			case <-configUpdateStop:
				log.Info("Periodic config update stopped")
				return
			}
		}
	}()

	log.Infof("Periodic config update started, interval: %v", updateInterval)
}

// StopPeriodicConfigUpdate stops periodic config updates
func StopPeriodicConfigUpdate() {
	if configUpdateStop != nil {
		close(configUpdateStop)
		configUpdateWg.Wait()
		logrus.Info("Periodic config update stopped")
	}
}

func initConfig(configFile string) error {
	viper.SetConfigFile(configFile)

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	return nil
}

// ApplySystemConfigToViper merges system config into viper for live WebSocket system_config updates (void callback)
func ApplySystemConfigToViper(data map[string]interface{}) {
	if err := viper.MergeConfigMap(data); err != nil {
		log.Warnf("Failed to merge pushed config into viper: %v", err)
		return
	}
	log.Info("Merged system config from WebSocket push into viper")
}

// SystemConfigEqual compares semantic equality via hashstructure fingerprint (map key order independent)
func SystemConfigEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		log.Debugf("[SystemConfigEqual] result: true (both nil)")
		return true
	}
	if a == nil || b == nil {
		log.Debugf("[SystemConfigEqual] result: false (one side is nil)")
		return false
	}
	ha, err1 := hashstructure.Hash(a, hashstructure.FormatV2, nil)
	hb, err2 := hashstructure.Hash(b, hashstructure.FormatV2, nil)
	if err1 != nil || err2 != nil {
		log.Debugf("[SystemConfigEqual] result: false (Hash failed err1=%v err2=%v)", err1, err2)
		return false
	}
	equal := ha == hb
	log.Debugf("[SystemConfigEqual] result: %t (ha=%d hb=%d), a: %+v, b: %+v", equal, ha, hb, a, b)
	return equal
}

// updateConfigFromAPI fetches config from API and updates viper
// Retries until success before returning
func updateConfigFromAPI() error {
	configProviderType := viper.GetString("config_provider.type")
	retryInterval := 10 * time.Second // Retry interval
	retryCount := 0

	for {
		// Backend management URL from config
		configProvider, err := user_config.GetProvider(configProviderType)
		if err != nil {
			retryCount++
			log.Warnf("Failed to get config provider (retry %d): %v, retry after %v", retryCount, err, retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		// Create context
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		// Get system config JSON string
		configJSON, err := configProvider.GetSystemConfig(ctx)
		cancel()

		if err != nil {
			retryCount++
			log.Warnf("Failed to get system config (retry %d): %v, retry after %v", retryCount, err, retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		if configJSON == "" {
			// Empty config counts as success (service may return empty)
			if retryCount > 0 {
				log.Infof("Config fetch succeeded (empty config, after %d retries)", retryCount)
			}
			return nil
		}

		// Parse JSON into map
		var configMap map[string]interface{}
		if err := json.Unmarshal([]byte(configJSON), &configMap); err != nil {
			retryCount++
			log.Warnf("Failed to parse config JSON (retry %d): %v, retry after %v", retryCount, err, retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		//log.Debugf("Load config from API: %+v", configMap)

		// Merge into viper via MergeConfigMap
		if err := viper.MergeConfigMap(configMap); err != nil {
			retryCount++
			log.Warnf("Failed to merge config into viper (retry %d): %v, retry after %v", retryCount, err, retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		// Success
		if retryCount > 0 {
			log.Infof("Config fetch succeeded (after %d retries)", retryCount)
		} else {
			log.Debug("Config fetch succeeded")
		}
		return nil
	}
}

func initLog() error {
	// Write to file
	binPath, _ := os.Executable()
	baseDir := filepath.Dir(binPath)
	logPath := fmt.Sprintf("%s/%s%s", baseDir, viper.GetString("log.path"), viper.GetString("log.file"))
	/* Log rotation helpers
	`WithLinkName` creates a symlink to the latest log
	`WithRotationTime` sets how often to rotate
	Only one of WithMaxAge and WithRotationCount may be set
		`WithMaxAge` max age before cleanup
		`WithRotationCount` max file count before cleanup
	*/
	// Config below: rotate every 1 minute, keep last 3 minutes, auto-clean extras.
	writer, err := rotatelogs.New(
		logPath+".%Y%m%d",
		rotatelogs.WithLinkName(logPath),
		rotatelogs.WithRotationCount(uint(viper.GetInt("log.max_age"))),
		rotatelogs.WithRotationTime(time.Duration(86400)*time.Second),
	)
	if err != nil {
		fmt.Printf("init log error: %v\n", err)
		os.Exit(1)
		return err
	}

	// Choose output target from config
	if viper.GetBool("log.stdout") {
		// Write to both file and stdout
		multiWriter := io.MultiWriter(writer, os.Stdout)
		logrus.SetOutput(multiWriter)
		logrus.SetFormatter(&logrus.TextFormatter{
			TimestampFormat: "2006-01-02 15:04:05.000", // Timestamp format with milliseconds
			ForceColors:     true,                      // Enable colors for stdout
		})
	} else {
		// File output only
		logrus.SetOutput(writer)
		logrus.SetFormatter(&logrus.TextFormatter{
			TimestampFormat: "2006-01-02 15:04:05.000", // Timestamp format with milliseconds
			ForceColors:     false,                     // No colors for file output
		})
	}

	// Disable default caller reporting; use custom caller field
	logrus.SetReportCaller(false)
	logLevel, _ := logrus.ParseLevel(viper.GetString("log.level"))
	logrus.SetLevel(logLevel)

	return nil
}

func initVad() error {
	log.Infof("Starting VAD module init...")
	vadProvider := viper.GetString("vad.provider")
	log.Infof("VAD provider: %s", vadProvider)

	// VAD is lazy-loaded via the global pool on first use
	log.Infof("VAD module will use lazy load and init on first use")
	return nil
}

func initRedis() error {
	// Init our unified Redis module
	redisConfig := &redisdb.Config{
		Host:     viper.GetString("redis.host"),
		Port:     viper.GetInt("redis.port"),
		Password: viper.GetString("redis.password"),
		DB:       viper.GetInt("redis.db"),
	}

	err := redisdb.Init(redisConfig)
	if err != nil {
		fmt.Printf("init redis error: %v\n", err)
		return err
	}

	return nil
}

func initAuthManager() error {
	return auth.Init()
}
