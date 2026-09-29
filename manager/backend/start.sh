#!/bin/bash

echo "=== 小智管理系统后端启动脚本 ==="

# 检查参数
if [ "$1" = "help" ] || [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    echo "使用方法:"
    echo "  ./start.sh                    # 使用默认配置文件"
    echo "  ./start.sh dev                # 使用开发环境配置"
    echo "  ./start.sh prod               # 使用生产环境配置"
    echo "  ./start.sh custom config.json # 使用自定义配置文件"
    echo "  ./start.sh reset              # 重置数据库并使用默认配置"
    echo "  ./start.sh reset-dev          # 重置数据库并使用开发环境配置"
    echo "  ./start.sh help               # 显示帮助信息"
    exit 0
fi

# 设置配置文件路径
CONFIG_FILE="manager/backend/config/config.json"

RESET_DB=""

case "$1" in
    "dev")
        CONFIG_FILE="manager/backend/config/config.dev.json"
        echo "使用开发环境配置: $CONFIG_FILE"
        ;;
    "prod")
        CONFIG_FILE="manager/backend/config/config.prod.json"
        echo "使用生产环境配置: $CONFIG_FILE"
        ;;
    "reset")
        RESET_DB="-reset-db"
        echo "重置数据库并使用默认配置: $CONFIG_FILE"
        ;;
    "reset-dev")
        CONFIG_FILE="manager/backend/config/config.dev.json"
        RESET_DB="-reset-db"
        echo "重置数据库并使用开发环境配置: $CONFIG_FILE"
        ;;
    "custom")
        if [ -z "$2" ]; then
            echo "错误: 请指定配置文件路径"
            echo "使用方法: ./start.sh custom config.json"
            exit 1
        fi
        CONFIG_FILE="$2"
        echo "使用自定义配置: $CONFIG_FILE"
        ;;
    "")
        echo "使用默认配置: $CONFIG_FILE"
        ;;
    *)
        echo "未知参数: $1"
        echo "使用 './start.sh help' 查看帮助"
        exit 1
        ;;
esac

# 检查配置文件是否存在
if [ ! -f "$CONFIG_FILE" ]; then
    echo "错误: 配置文件不存在: $CONFIG_FILE"
    exit 1
fi

# 进入后端目录
cd manager/backend

# 安装依赖
echo "安装Go依赖..."
go mod tidy

# 启动服务
echo "启动服务..."
if [ -n "$RESET_DB" ]; then
    echo "警告: 将重置数据库，所有数据将被删除！"
    read -p "确定要继续吗？(y/N): " confirm
    if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
        echo "操作已取消"
        exit 0
    fi
    go run main.go -config="../../$CONFIG_FILE" $RESET_DB
else
    go run main.go -config="../../$CONFIG_FILE"
fi