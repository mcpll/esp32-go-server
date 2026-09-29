@echo off
setlocal enabledelayedexpansion
echo === 小智管理系统后端启动脚本 ===

REM 检查参数
if "%1"=="help" goto :help
if "%1"=="-h" goto :help
if "%1"=="--help" goto :help

REM 设置配置文件路径
set CONFIG_FILE=manager/backend/config/config.json
set RESET_DB=

if "%1"=="dev" (
    set CONFIG_FILE=manager/backend/config/config.dev.json
    echo 使用开发环境配置: %CONFIG_FILE%
) else if "%1"=="prod" (
    set CONFIG_FILE=manager/backend/config/config.prod.json
    echo 使用生产环境配置: %CONFIG_FILE%
) else if "%1"=="reset" (
    set RESET_DB=-reset-db
    echo 重置数据库并使用默认配置: %CONFIG_FILE%
) else if "%1"=="reset-dev" (
    set CONFIG_FILE=manager/backend/config/config.dev.json
    set RESET_DB=-reset-db
    echo 重置数据库并使用开发环境配置: %CONFIG_FILE%
) else if "%1"=="custom" (
    if "%2"=="" (
        echo 错误: 请指定配置文件路径
        echo 使用方法: start.bat custom config.json
        pause
        exit /b 1
    )
    set CONFIG_FILE=%2
    echo 使用自定义配置: %CONFIG_FILE%
) else if "%1"=="" (
    echo 使用默认配置: %CONFIG_FILE%
) else (
    echo 未知参数: %1
    echo 使用 'start.bat help' 查看帮助
    pause
    exit /b 1
)

REM 检查配置文件是否存在
if not exist "%CONFIG_FILE%" (
    echo 错误: 配置文件不存在: %CONFIG_FILE%
    pause
    exit /b 1
)

REM 进入后端目录
cd manager\backend

REM 安装依赖
echo 安装Go依赖...
go mod tidy

REM 启动服务
echo 启动服务...
if not "%RESET_DB%"=="" (
    echo 警告: 将重置数据库，所有数据将被删除！
    set /p confirm=确定要继续吗？(y/N): 
    if not "!confirm!"=="y" if not "!confirm!"=="Y" (
        echo 操作已取消
        pause
        exit /b 0
    )
    go run main.go -config="..\..\%CONFIG_FILE%" %RESET_DB%
) else (
    go run main.go -config="..\..\%CONFIG_FILE%"
)
goto :end

:help
echo 使用方法:
echo   start.bat                    # 使用默认配置文件
echo   start.bat dev                # 使用开发环境配置
echo   start.bat prod               # 使用生产环境配置
echo   start.bat custom config.json # 使用自定义配置文件
echo   start.bat reset              # 重置数据库并使用默认配置
echo   start.bat reset-dev          # 重置数据库并使用开发环境配置
echo   start.bat help               # 显示帮助信息

:end
pause