package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	mqtt_server "xiaozhi-esp32-server-golang/internal/app/mqtt_server"
	log "xiaozhi-esp32-server-golang/logger"
)

// Init function
func Init(configFile string) error {
	err := initConfig(configFile)
	if err != nil {
		return err
	}

	err = initLog()
	if err != nil {
		return err
	}

	return nil
}

func initLog() error {
	// No longer check stdout config; always write to file
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
	logrus.SetOutput(writer)
	logrus.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: "2006-01-02 15:04:05.000", // Timestamp format with milliseconds
		ForceColors:     false,                     // No colors for file output
	})

	// Disable default caller reporting; use custom caller field
	logrus.SetReportCaller(false)
	logLevel, _ := logrus.ParseLevel(viper.GetString("log.level"))
	logrus.SetLevel(logLevel)

	return nil

}

func initConfig(configFile string) error {
	basePath, file := filepath.Split(configFile)

	// Get file name and extension
	fileName, fileExt := func(file string) (string, string) {
		if pos := strings.LastIndex(file, "."); pos != -1 {
			return file[:pos], strings.ToLower(file[pos+1:])
		}
		return file, ""
	}(file)

	// Set config name (without extension)
	viper.SetConfigName(fileName)
	viper.AddConfigPath(basePath)

	// Set config type from file extension
	switch fileExt {
	case "json":
		viper.SetConfigType("json")
	case "yaml", "yml":
		viper.SetConfigType("yaml")
	default:
		return fmt.Errorf("unsupported config file type: %s", fileExt)
	}

	return viper.ReadInConfig()
}

func main() {
	// Parse command-line flags
	configFile := flag.String("c", "config/mqtt_config.json", "配置文件路径")
	flag.Parse()

	if *configFile == "" {
		fmt.Println("config file path is required")
		return
	}

	// Init config and logging
	err := Init(*configFile)
	if err != nil {
		fmt.Printf("init failed: %v\n", err)
		return
	}

	// Start MQTT server
	err = mqtt_server.StartMqttServer()
	if err != nil {
		log.Errorf("Failed to start MQTT server: %v", err)
		return
	}

	fmt.Println("MQTT server started")

	// Block waiting for exit signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Info("MQTT server started, press Ctrl+C to exit")
	<-quit

	log.Info("Shutting down MQTT server...")
	log.Info("MQTT server closed")
}
