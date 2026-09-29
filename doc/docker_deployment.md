# Docker 本地编译支持

新增了 `docker-compose.local.yml` 文件，支持本地编译和多架构部署。

## 新增文件

- `docker/docker-composer/docker-compose.local.yml` - 本地编译配置文件

## 编译方法

### 默认编译（AMD64）

```bash
cd docker/docker-composer
docker-compose -f docker-compose.local.yml up --build
```

### ARM64 编译（Apple Silicon）

```bash
cd docker/docker-composer
TARGETARCH=arm64 docker-compose -f docker-compose.local.yml up --build
```

## 运行方法

编译完成后，服务会自动启动，包括：
- 主服务器（端口 8989）
- 后端管理（端口 8081）
- 前端界面（端口 8080）
- MySQL 数据库（端口 23306）

访问 http://<服务器IP或域名>:8080 查看前端界面。

## 🏗️ 多架构支持

### 自动架构检测（推荐）

`docker-compose.local.yml` 支持自动检测当前系统架构：

```bash
# 自动检测架构并构建（默认行为）
docker-compose -f docker-compose.local.yml up --build
```

### 手动指定架构

如果需要为特定架构构建：

```bash
# 为 ARM64 架构构建
TARGETARCH=arm64 docker-compose -f docker-compose.local.yml up --build

# 为 AMD64 架构构建
TARGETARCH=amd64 docker-compose -f docker-compose.local.yml up --build
```

### 支持的架构

- **AMD64/x86_64**: Intel/AMD 处理器（默认）
- **ARM64**: Apple Silicon (M1/M2)、ARM 服务器

## 📁 配置文件说明

### docker-compose.yml

使用预构建的官方镜像，适合生产环境：

```yaml
services:
  mysql:
    image: docker.jsdelivr.fyi/mysql:8.0
  main-server:
    image: docker.jsdelivr.fyi/hackers365/xiaozhi_golang:0.1
  backend:
    image: docker.jsdelivr.fyi/hackers365/xiaozhi_backend:0.1
  frontend:
    image: docker.jsdelivr.fyi/hackers365/xiaozhi_frontend:0.1
```

### docker-compose.local.yml

本地构建版本，支持代码修改和多架构：

```yaml
services:
  main-server:
    build:
      context: ../..
      dockerfile: docker/Dockerfile.main
      args:
        TARGETARCH: ${TARGETARCH:-amd64}
```

## 🔧 环境变量配置

### 架构相关

| 变量名 | 默认值 | 说明 |
|-------|-------|------|
| `TARGETARCH` | `amd64` | 目标架构（amd64/arm64） |


## 🛠️ 常见操作

### 查看服务状态

```bash
# 查看所有服务状态
docker-compose ps

# 查看服务日志
docker-compose logs -f main-server
docker-compose logs -f backend
docker-compose logs -f frontend
```

### 停止和重启服务

```bash
# 停止所有服务
docker-compose down

# 重启特定服务
docker-compose restart main-server

# 重新构建并启动
docker-compose up --build
```
