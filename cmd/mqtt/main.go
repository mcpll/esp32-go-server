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

// 初始化函数
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
	// 不再检查stdout配置，统一输出到文件
	// 输出到文件
	binPath, _ := os.Executable()
	baseDir := filepath.Dir(binPath)
	logPath := fmt.Sprintf("%s/%s%s", baseDir, viper.GetString("log.path"), viper.GetString("log.file"))
	/* 日志轮转相关函数
	`WithLinkName` 为最新的日志建立软连接
	`WithRotationTime` 设置日志分割的时间，隔多久分割一次
	WithMaxAge 和 WithRotationCount二者只能设置一个
		`WithMaxAge` 设置文件清理前的最长保存时间
		`WithRotationCount` 设置文件清理前最多保存的个数
	*/
	// 下面配置日志每隔 1 分钟轮转一个新文件，保留最近 3 分钟的日志文件，多余的自动清理掉。
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
		TimestampFormat: "2006-01-02 15:04:05.000", //时间格式化，添加毫秒
		ForceColors:     false,                     // 文件输出不启用颜色
	})

	// 禁用默认的调用者报告，使用自定义的caller字段
	logrus.SetReportCaller(false)
	logLevel, _ := logrus.ParseLevel(viper.GetString("log.level"))
	logrus.SetLevel(logLevel)

	return nil

}

func initConfig(configFile string) error {
	basePath, file := filepath.Split(configFile)

	// 获取文件名和扩展名
	fileName, fileExt := func(file string) (string, string) {
		if pos := strings.LastIndex(file, "."); pos != -1 {
			return file[:pos], strings.ToLower(file[pos+1:])
		}
		return file, ""
	}(file)

	// 设置配置文件名(不带扩展名)
	viper.SetConfigName(fileName)
	viper.AddConfigPath(basePath)

	// 根据文件扩展名设置配置类型
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
	// 解析命令行参数
	configFile := flag.String("c", "config/mqtt_config.json", "配置文件路径")
	flag.Parse()

	if *configFile == "" {
		fmt.Println("配置文件路径不能为空")
		return
	}

	// 初始化配置和日志
	err := Init(*configFile)
	if err != nil {
		fmt.Printf("初始化失败: %v\n", err)
		return
	}

	// 启动MQTT服务器
	err = mqtt_server.StartMqttServer()
	if err != nil {
		log.Errorf("启动MQTT服务器失败: %v", err)
		return
	}

	fmt.Println("MQTT服务器已启动")

	// 阻塞监听退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Info("MQTT服务器已启动，按 Ctrl+C 退出")
	<-quit

	log.Info("正在关闭MQTT服务器...")
	log.Info("MQTT服务器已关闭")
}
