# asr_server 合入方案（与 manager/backend 同形式）

## 目标

- **asr_server 保持独立仓库形态**：拥有自己的 `go.mod`、`main.go`，可单独克隆、构建、运行。
- **主程序可初始化**：像 `manager/backend` 一样，主进程通过 `replace` 引用子目录，并在需要时在进程内启动 asr_server 的 HTTP 服务（独立端口），无需单独起进程。

## 引入方式：推荐使用 Git Submodule

主仓可以通过两种方式得到 `asr_server/` 目录：

| 方式 | 说明 |
|------|------|
| **Git Submodule（推荐）** | asr_server 保持独立 Git 仓库；主仓用 `git submodule add` 引用，得到的是“指向 asr_server 某次提交”的目录，主仓只记录子模块路径和提交号。 |
| 拷贝/移动代码 | 将 asr_server 的代码直接放进主仓目录，asr_server 与主仓共用一个 Git 历史（或作为主仓一部分）。 |

下面以 **Submodule** 方式为准说明步骤；主仓侧 `replace` 与内嵌启动逻辑与“拷贝方式”相同。

## 参考：manager/backend 的合入形式

| 项     | manager/backend 做法 |
|--------|----------------------|
| 目录   | 主仓库下 `manager/backend/` |
| 模块名 | `xiaozhi/manager/backend`（backend 内 go.mod） |
| 主仓引用 | `replace xiaozhi/manager/backend => ./manager/backend` |
| 独立运行 | `manager/backend/main.go`：LoadWithPath → database.Init → router.Setup → r.Run() |
| 主程序内嵌 | `cmd/server/manager_http.go`：同套 config/database/router，自己起 `http.Server` 在另一端口 |

## asr_server 合入设计（对齐上述形式）

### 1. 目录与模块（Submodule 方式）

- **asr_server 须先有独立 Git 仓库**（若当前在 monorepo 里，可先拆成独立 repo，或使用现有 asr_server 仓库的 URL）。
- **在主仓中添加 submodule**（在主仓根目录执行，且 `asr_server` 目录尚不存在）：
  ```bash
  cd xiaozhi-esp32-server-golang
  git submodule add <asr_server 仓库 URL> asr_server
  ```
  完成后主仓会多出：
  - 目录 `asr_server/`（内容为 asr_server 仓库当前检出的一次提交）
  - 文件 `.gitmodules`，以及 `git submodule status` 可看到的子模块记录
- **目录路径**：主仓内为 `xiaozhi-esp32-server-golang/asr_server/`，与“拷贝方式”一致，主仓 go 代码和 go.mod 的 `replace` 都指向 `./asr_server`。
- **模块名**：保持 asr_server 现有模块名 **`voice_server`**（便于其作为独立仓库时直接 `go build`，无需改 import）。
- **主仓 go.mod**：增加一行：
  - `replace voice_server => ./asr_server`
- **asr_server 的 go.mod**：保持 `module voice_server`，不引用主仓；独立仓库时无 replace，合入主仓后仅主仓侧 replace 即可。

**克隆主仓时拉取 submodule**（任选其一）：

```bash
# 克隆时一次性拉取子模块
git clone --recurse-submodules <主仓 URL>

# 或先克隆再初始化子模块
git clone <主仓 URL>
cd xiaozhi-esp32-server-golang
git submodule update --init --recursive
```

**CI / 自动化构建**：若主仓需要构建依赖 asr_server 的代码，需在构建前执行 `git submodule update --init --recursive`（或使用 `--recurse-submodules` 克隆）。

### 2. 独立运行（asr_server 仍是“独立仓库”）

- 单独克隆/打开 `asr_server` 目录时：
  - `go build -o asr_server .`
  - `./asr_server` 使用 `config.json`（或 `-config` 指定路径），行为与现在一致。
- 不依赖主仓；主仓的 `replace` 只影响主仓的构建。

### 3. 主程序初始化（内嵌 asr_server）

- **入口**：在主仓增加 `cmd/server/asr_server_http.go`（与 `manager_http.go` 同级）。
- **逻辑**（与 manager_http 一致）：
  1. 由主进程在启动时按配置决定是否调用（例如 `-asr-enable` + `-asr-config`）。
  2. 使用 asr_server 的包：
     - `voice_server/config`：`InitConfig(configPath)`，再 `GetConfig()` 得到 `*Config`。
     - `voice_server/internal/bootstrap`：`InitApp(cfg)` 得到 `*AppDependencies`。
     - `voice_server/internal/router`：`NewRouter(deps)` 得到 `*gin.Engine`。
  3. 用 `deps.RateLimiter.Middleware(r)` 作为 Handler，在**单独端口**（如 8080）起 `http.Server`，在 goroutine 中 `ListenAndServe`。
  4. 退出时提供 `StopAsrServerHTTP()`，对 `http.Server` 做 `Shutdown`，并做必要的资源释放（如 bootstrap 中需要 Close 的组件）。
- **配置**：asr_server 仍用自身 `config.json`；内嵌时配置文件路径由主进程参数或主仓配置指定（如 `asr_server/config.json` 或 `config/asr_server.json`）。

### 4. 主仓改动清单（Submodule 方式）

| 位置 | 改动 |
|------|------|
| 主仓根 | 执行 `git submodule add <asr_server 仓库 URL> asr_server`，得到 `asr_server/` 目录及 `.gitmodules`（asr_server 需先有独立 Git 仓库） |
| `xiaozhi-esp32-server-golang/go.mod` | 增加 `replace voice_server => ./asr_server`；若主仓代码要 import voice_server，再在 `require` 中加 `voice_server`（或由 `go mod tidy` 自动补） |
| `xiaozhi-esp32-server-golang/cmd/server/main.go` | 解析 `-asr-enable`、`-asr-config`；若 enable，在 `Run()` 前调用 `StartAsrServerHTTP(configPath)`；在 `<-quit` 后调用 `StopAsrServerHTTP()` |
| 新增 `xiaozhi-esp32-server-golang/cmd/server/asr_server_http.go` | 实现 `StartAsrServerHTTP(configPath string)`、`StopAsrServerHTTP()`，内部使用 `voice_server/config`、`voice_server/internal/bootstrap`、`voice_server/internal/router`，与 manager_http 模式一致 |

### 5. asr_server 侧需要配合的暴露

- **config**：已有 `InitConfig(path)`、`GetConfig()`，主进程可直接用。
- **bootstrap**：已有 `InitApp(cfg *config.Config)`，返回 `*AppDependencies`，主进程可直接用。
- **router**：已有 `NewRouter(deps) *gin.Engine`；主进程用 `deps.RateLimiter.Middleware(r)` 作为 Handler 即可。
- **优雅退出**：若 bootstrap 内有需要 `Close()` 的资源（如 VAD 池、全局 recognizer 等），需在 asr_server 内提供统一的 `Shutdown(deps *AppDependencies)` 或类似函数，供 `StopAsrServerHTTP()` 调用；若当前没有，可先只做 `Server.Shutdown`，后续再补。

### 6. 依赖与构建

- asr_server 的依赖（sherpa-onnx、qdrant、ten-vad 等）保留在 **asr_server/go.mod** 中；主仓**不**把 asr_server 的依赖提升到主 go.mod 的 require 中，仅通过 `require voice_server`（或等价）引用子模块，由 `go mod tidy` 在主仓拉齐依赖。
- 若主仓构建时出现缺少依赖，再在主仓 go.mod 的 `require` 中显式添加 asr_server 用到的直接依赖即可。
- CGO、本地 lib（如 ten_vad、sherpa-onnx 的 so/dll）仍按 asr_server 现有方式放在 asr_server 目录或主仓统一 `lib/`，构建脚本/文档中说明即可。

### 7. 与 manager/backend 的差异说明

- manager/backend 模块名是 `xiaozhi/manager/backend`，asr_server 保持 `voice_server`，这样 asr_server 作为独立仓库时无需改 import。
- 主仓用 `replace voice_server => ./asr_server` 即可，无需改 asr_server 内部包路径。
- 主程序“初始化”方式一致：不调 asr_server 的 `main()`，只复用 config + bootstrap + router，在主进程内起一个带独立端口的 HTTP 服务。

### 8. 小结（Submodule 方式）

- **独立仓库**：asr_server 是独立 Git 仓库，拥有自己的 `go.mod`（`module voice_server`）和 `main.go`，可单独克隆、构建、运行。
- **合入主仓**：主仓用 **Git submodule** 引用 asr_server，得到 `asr_server/` 目录；主仓 `replace voice_server => ./asr_server`；克隆主仓后需执行 `git submodule update --init`（或 `git clone --recurse-submodules`）。
- **主程序初始化**：主仓新增 `asr_server_http.go`，按配置在进程内启动 asr_server 的 HTTP 服务（独立端口），逻辑与 `manager_http.go` 对齐。

**构建说明**：asr_server 依赖 sherpa-onnx（CGO），主仓通过**构建标签**将内嵌设为可选：
- **默认构建**（不启用内嵌 asr_server）：`go build -o xiaozhi_server ./cmd/server`，此时 `-asr-enable` 会打出“未编译进本二进制”的提示。
- **启用内嵌 asr_server**：`go build -tags asr_server -o xiaozhi_server ./cmd/server`，需本机具备 CGO 及 sherpa-onnx 所需环境。

如确认按此方案实施，可再细化：asr_server 内 `Shutdown(deps)` 的职责列表、默认端口与配置路径、以及主仓 `main.go` 的参数命名与默认值。
