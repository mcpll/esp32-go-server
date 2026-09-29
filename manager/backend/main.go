package main

import (
	"flag"
	"log"
	"xiaozhi/manager/backend/config"
	"xiaozhi/manager/backend/database"
	"xiaozhi/manager/backend/router"

	"github.com/gin-gonic/gin"
)

func main() {
	// 定义命令行参数
	var configFile string
	flag.StringVar(&configFile, "config", "config/config.json", "配置文件路径")
	flag.StringVar(&configFile, "c", "config/config.json", "配置文件路径 (简写)")
	flag.Parse()

	// 加载配置
	cfg := config.LoadWithPath(configFile)

	// 初始化数据库
	db := database.Init(cfg.Database)
	if db == nil {
		log.Fatal("数据库初始化失败，服务退出")
	}
	defer database.Close(db)

	// 设置Gin模式
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 初始化路由
	r := router.Setup(db, cfg)

	// 启动服务器
	log.Printf("使用配置文件: %s", configFile)
	log.Printf("服务器启动在端口: %s", cfg.Server.Port)
	if err := r.Run(":" + cfg.Server.Port); err != nil {
		log.Fatal("服务器启动失败:", err)
	}
}
