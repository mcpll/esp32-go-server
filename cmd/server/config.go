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

// 全局变量用于控制周期性更新
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

	// 初始化配置系统（包括WebSocket连接）
	// 注意：不要在此处单独注册 ApplySystemConfigToViper，否则会先于 main 的回调执行，导致 main 里读取的「当前配置」已是合并后的新配置；合并应在 main 的回调中、在读取 current 并比较之后再执行。
	ctx := context.Background()
	if err := user_config.InitConfigSystem(ctx); err != nil {
		fmt.Printf("初始化配置系统失败: %v\n", err)
	}

	// 从接口获取配置并更新
	if err := updateConfigFromAPI(); err != nil {
		fmt.Printf("从接口获取配置失败，使用本地配置: %v\n", err)
	}

	// 启动周期性配置更新
	startPeriodicConfigUpdate()

	//init vad
	initVad()

	//init redis
	initRedis()

	// memory 模块采用懒加载，使用时自动初始化，无需显式初始化

	//init auth
	err = initAuthManager()
	if err != nil {
		fmt.Printf("initAuthManager err: %+v", err)
		os.Exit(1)
		return err
	}

	return nil
}

// startPeriodicConfigUpdate 启动周期性配置更新
func startPeriodicConfigUpdate() {
	// 从配置中获取更新间隔，默认5分钟
	updateInterval := viper.GetDuration("config_provider.update_interval")
	if updateInterval <= 0 {
		updateInterval = 30 * time.Second
	}

	// 检查是否启用周期性更新
	if !viper.GetBool("config_provider.enable_periodic_update") {
		log.Info("周期性配置更新已禁用")
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
					log.Warnf("周期性配置更新失败: %v", err)
				} else {
					//log.Debug("周期性配置更新成功")
				}
			case <-configUpdateStop:
				log.Info("周期性配置更新已停止")
				return
			}
		}
	}()

	log.Infof("周期性配置更新已启动，更新间隔: %v", updateInterval)
}

// StopPeriodicConfigUpdate 停止周期性配置更新
func StopPeriodicConfigUpdate() {
	if configUpdateStop != nil {
		close(configUpdateStop)
		configUpdateWg.Wait()
		logrus.Info("周期性配置更新已停止")
	}
}

func initConfig(configFile string) error {
	viper.SetConfigFile(configFile)

	// 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	return nil
}

// ApplySystemConfigToViper 将系统配置合并到 viper，用于 WebSocket 推送的 system_config 实时更新（回调无返回值）
func ApplySystemConfigToViper(data map[string]interface{}) {
	if err := viper.MergeConfigMap(data); err != nil {
		log.Warnf("合并推送配置到 viper 失败: %v", err)
		return
	}
	log.Info("已从 WebSocket 推送合并系统配置到 viper")
}

// SystemConfigEqual 比较两段系统配置是否语义相同（使用 hashstructure 指纹，与 map key 顺序无关）
func SystemConfigEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		log.Debugf("[SystemConfigEqual] 结果: true (均为 nil)")
		return true
	}
	if a == nil || b == nil {
		log.Debugf("[SystemConfigEqual] 结果: false (一方为 nil)")
		return false
	}
	ha, err1 := hashstructure.Hash(a, hashstructure.FormatV2, nil)
	hb, err2 := hashstructure.Hash(b, hashstructure.FormatV2, nil)
	if err1 != nil || err2 != nil {
		log.Debugf("[SystemConfigEqual] 结果: false (Hash 失败 err1=%v err2=%v)", err1, err2)
		return false
	}
	equal := ha == hb
	log.Debugf("[SystemConfigEqual] 结果: %t (ha=%d hb=%d), a: %+v, b: %+v", equal, ha, hb, a, b)
	return equal
}

// updateConfigFromAPI 从接口获取配置并更新viper配置
// 内部会持续重试，直到成功后才返回
func updateConfigFromAPI() error {
	configProviderType := viper.GetString("config_provider.type")
	retryInterval := 10 * time.Second // 重试间隔
	retryCount := 0

	for {
		// 从配置文件获取后端管理系统地址
		configProvider, err := user_config.GetProvider(configProviderType)
		if err != nil {
			retryCount++
			log.Warnf("获取配置提供者失败 (第%d次重试): %v，%v后重试", retryCount, err, retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		// 创建上下文
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		// 获取系统配置JSON字符串
		configJSON, err := configProvider.GetSystemConfig(ctx)
		cancel()

		if err != nil {
			retryCount++
			log.Warnf("获取系统配置失败 (第%d次重试): %v，%v后重试", retryCount, err, retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		if configJSON == "" {
			// 配置为空，视为成功（可能服务返回空配置）
			if retryCount > 0 {
				log.Infof("配置获取成功（配置为空，经过%d次重试）", retryCount)
			}
			return nil
		}

		// 解析JSON为map
		var configMap map[string]interface{}
		if err := json.Unmarshal([]byte(configJSON), &configMap); err != nil {
			retryCount++
			log.Warnf("解析配置JSON失败 (第%d次重试): %v，%v后重试", retryCount, err, retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		//log.Debugf("Load config from API: %+v", configMap)

		// 使用viper.MergeConfigMap设置到viper
		if err := viper.MergeConfigMap(configMap); err != nil {
			retryCount++
			log.Warnf("合并配置到viper失败 (第%d次重试): %v，%v后重试", retryCount, err, retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		// 成功
		if retryCount > 0 {
			log.Infof("配置获取成功（经过%d次重试）", retryCount)
		} else {
			log.Debug("配置获取成功")
		}
		return nil
	}
}

func initLog() error {
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

	// 根据配置决定输出目标
	if viper.GetBool("log.stdout") {
		// 同时输出到文件和标准输出
		multiWriter := io.MultiWriter(writer, os.Stdout)
		logrus.SetOutput(multiWriter)
		logrus.SetFormatter(&logrus.TextFormatter{
			TimestampFormat: "2006-01-02 15:04:05.000", //时间格式化，添加毫秒
			ForceColors:     true,                      // 标准输出启用颜色
		})
	} else {
		// 只输出到文件
		logrus.SetOutput(writer)
		logrus.SetFormatter(&logrus.TextFormatter{
			TimestampFormat: "2006-01-02 15:04:05.000", //时间格式化，添加毫秒
			ForceColors:     false,                     // 文件输出不启用颜色
		})
	}

	// 禁用默认的调用者报告，使用自定义的caller字段
	logrus.SetReportCaller(false)
	logLevel, _ := logrus.ParseLevel(viper.GetString("log.level"))
	logrus.SetLevel(logLevel)

	return nil
}

func initVad() error {
	log.Infof("开始初始化 VAD 模块...")
	vadProvider := viper.GetString("vad.provider")
	log.Infof("VAD 提供商: %s", vadProvider)

	// VAD 使用懒加载模式，将在首次使用时通过全局资源池自动初始化
	log.Infof("VAD 模块将使用懒加载模式，在首次使用时自动初始化")
	return nil
}

func initRedis() error {
	// 初始化我们的统一Redis模块
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
