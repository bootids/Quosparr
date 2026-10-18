# 架构

状态：已按仓库事实充实（2026-10-18，由 harness-init 创建）。权威规则仍以 [AGENTS.md](AGENTS.md) 为准。

## 初始化观测上下文

- profile：`generic`；engine：`none`（依据：`go.mod` 工具链清单）。
- 启用 capabilities：`database`、`local-save`、`networking`、`security`；未选不代表项目不具备。
- 范围：仅本仓库目录；初始化本身未执行任何项目命令。

## 目的与边界

Quosparr 是 macOS / Windows / Linux 原生菜单栏 / 系统托盘应用（Go + mygo/ui），读取本机 Codex / Grok / Claude Code 配置与会话数据，展示订阅配额、reset credits、API 等价成本，并提供多账号管理、会话索引修复与 Skill 管理。macOS 另有 Swift WidgetKit 扩展。

非目标：不引入 WebView 前端；默认不引入第三方依赖；不修改系统代理与 shell rc；不做跨 bundle ID 的 Sparkle 自动安装。

## 模块地图（仓库相对路径）

| 路径 | 职责 |
|---|---|
| `main.go` | Go 入口 |
| `internal/app` | 原生 UI 与系统协调 |
| `internal/runway` | 后台协调 |
| `internal/store` | 偏好与本地存储（`preferences.go`、`userdefaults_darwin.go`、代理设置等） |
| `internal/platform` | 平台边界（钥匙串、ACL 等） |
| `internal/netx` | 网络与代理边界 |
| `internal/l10n` | 手工维护的多语言表（七语言） |
| `internal/codex` `internal/codexapi` `internal/grok` `internal/claude` | 各供应商业务模块（订阅、API 账号、切号） |
| `internal/usage` `internal/estimate` `internal/export` `internal/resettoday` `internal/alerts` `internal/speed` | 用量统计、成本推算、导出、重置动态、提醒、速率 |
| `internal/sessions` | 会话扫描与 `session_index.jsonl` 修复 |
| `internal/skills` | 全局 skill 扫描、来源识别、自有回收站（启停写入在各供应商包） |
| `internal/migration` | 旧身份（codex-runway）数据迁移，暂存事务、可中断恢复 |
| `internal/sqliteruntime` | SQLite 运行库加载 / 校验 / 缓存（Windows 按架构加载，其他平台 `load_other.go`） |
| `internal/tomlstmt` | `config.toml` 逐行编辑辅助 |
| `Sources/QuosparrWidget`、`Sources/QuosparrWidgetBridge`、`WidgetExtension/` | macOS Swift WidgetKit 扩展与桥接（xcodegen 生成工程） |
| `Scripts/` | 打包、验签、发布资产脚本 |
| `website/` | 官网源码（`go run ./website/gen` 预渲染） |

允许依赖：标准库 + `go.mod` 锁定的 `mygo`、`purego`、`go-keyring`、`BurntSushi/toml`、`golang.org/x/{image,sys,text}` 等。关键禁边：业务核心不得依赖 UI；purego 仅用于平台 API、SQLite 与 Swift 桥接边界；UI 经业务 API 读结果，不自行解析 JSON 或扫描文件。

## 运行时与状态归属

- 单实例菜单栏 / 托盘应用，`LSUIElement=true`，无 Dock 图标；后台协调在 `internal/runway`。
- 自有数据统一在 `~/.quosparr`（详见 [docs/DATA_MODEL.md](docs/DATA_MODEL.md) 与 [docs/SAVE_SYSTEM.md](docs/SAVE_SYSTEM.md)）。
- macOS 偏好写 `com.quosparr.app`（UserDefaults 兼容例外）；Windows / Linux 偏好在自有目录；代理密码分别存 Keychain / Credential Manager / Secret Service。
- 更新：三端 mygo 原生更新器（检查、下载、验签、安装、重启）；macOS 旧 Swift 同身份版本经 Ed25519 签名 appcast 升级到 Go 版本。
- Widget 快照：主程序是 `~/.quosparr/widget-snapshot.json`（schema 1）唯一写入者，组件只读。

## 契约与不变量

- 版本契约：Go 1.27.1、mygo v0.5.5（以 `go.mod` 为准）；macOS 最低 12.0，Widget 要求 macOS 14+。
- `LSUIElement=true` 必须保持。
- 隐私不变量与官方文件写入白名单见 [AGENTS.md](AGENTS.md)「隐私与安全」与 [docs/SECURITY.md](docs/SECURITY.md)；`~/.codex`、`~/.grok`、`~/.claude` 的官方文件只按列明的最小字段与事务写入。
- 用户可见文案必须七语言齐全（en / zh-Hans / zh-Hant / ko / ja / ru / fr），Widget 侧自带副本需同步。
- 价格表与推算必须带版本日期；未知模型不估算精确费用。

## 验证

常用命令（来自 AGENTS.md，本次初始化未执行，均 `not run`）：

```bash
go test ./...
go vet ./...
go build ./...
go test -race -count=1 ./internal/...
go run . --self-check
bash Scripts/run-dev-app.sh
V1_DEVELOPMENT_PACKAGE=1 bash Scripts/package-v1-app.sh
```

安装、升级与真机验收按 [docs/v1-acceptance-checklist.md](docs/v1-acceptance-checklist.md)、[docs/v1-release.md](docs/v1-release.md) 执行；交叉编译不能替代三端真机验收。

另见[知识索引](docs/index.md)。
