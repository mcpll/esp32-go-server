package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	"xiaozhi-esp32-server-golang/internal/app/server/auth"
	"xiaozhi-esp32-server-golang/internal/app/server/secrets"
	redisdb "xiaozhi-esp32-server-golang/internal/db/redis"
	user_config "xiaozhi-esp32-server-golang/internal/domain/config"
	"xiaozhi-esp32-server-golang/internal/domain/config/store"

	log "xiaozhi-esp32-server-golang/logger"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"github.com/mitchellh/hashstructure/v2"
	logrus "github.com/sirupsen/logrus"

	"github.com/spf13/viper"
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

	// Start the config provider. PocketBase settings are merged into viper before the servers
	// start; if PocketBase is down, the server still starts and the merge happens when it answers.
	if err := user_config.InitConfigSystem(context.Background(), applySystemConfig); err != nil {
		fmt.Printf("config system init failed: %v\n", err)
		os.Exit(1)
		return err
	}

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

func initConfig(configFile string) error {
	viper.SetConfigFile(configFile)

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		return err
	}
	secrets.ApplyEnv()
	if err := secrets.Check(); err != nil {
		return err
	}

	return nil
}

// applySystemConfig takes system config from the provider. A handler registered by main merges it
// and reloads the services whose settings changed; before main registers one, it is only merged.
func applySystemConfig(data map[string]interface{}) {
	if user_config.DispatchSystemConfig(data) {
		return
	}
	ApplySystemConfigToViper(data)
}

// ApplySystemConfigToViper applies the settings snapshot to viper, then puts env secrets
// back so a settings record cannot override them.
func ApplySystemConfigToViper(data map[string]interface{}) {
	if err := store.MergeThen(data, secrets.ApplyEnv); err != nil {
		log.Warnf("Failed to merge system config into viper: %v", err)
		return
	}
	log.Info("Merged system config into viper")
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
	if !store.GetBool("redis.enable") {
		log.Infof("Redis is disabled; short memory is off")
		return nil
	}

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
