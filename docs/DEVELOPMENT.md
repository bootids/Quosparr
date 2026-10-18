# 开发与验证

状态：已按仓库事实充实（2026-10-18，由 harness-init 创建）。本次初始化未执行任何项目命令，所有命令均标 `not run`。

## 发现证据

- profile：`generic`；engine：`none`（依据：`go.mod`：`module github.com/licoy/quosparr`、`go 1.27.1`）。
- 启用 capabilities：`database`、`local-save`、`networking`、`security`。
- CI：`.github/workflows/ci.yml`、`release.yml`、`website.yml`（仅改 `website/` 或 `website.yml` 的推送不触发 CI）。

## 入口

- Go 入口：`main.go`；业务模块在 `internal/*`（结构见 [ARCHITECTURE.md](../ARCHITECTURE.md)）。
- 工具链：Go 1.27.1、mygo v0.5.5（`go.mod` 锁定）；Widget 工程由 `WidgetExtension/project.yml` 经 xcodegen 生成。
- 常用命令（均 `not run`，来源 AGENTS.md；脚本文件已确认存在于 `Scripts/`）：

```bash
go test ./...
go vet ./...
go build ./...
go test -race -count=1 ./internal/...
go run . --self-check
bash Scripts/run-dev-app.sh            # 桌面开发入口，可选 --widget / --tier-badges
V1_DEVELOPMENT_PACKAGE=1 bash Scripts/package-v1-app.sh   # 开发打包，可选 ARCH=arm64|x86_64
```

- 隔离运行（不触碰开发机真实数据）：

```bash
QUOSPARR_TEST_HOME="$(mktemp -d)"
HOME="$QUOSPARR_TEST_HOME" QUOSPARR_PREFS_DOMAIN=com.quosparr.app.v1-test go run . --self-check
```

- 发布 / 验收脚本：`Scripts/verify-v1-*.sh|py|ps1`、`Scripts/release-assets`（appcast 生成）、`Scripts/prepare-windows-sqlite.py`；流程见 [docs/v1-release.md](v1-release.md)。

## 环境与约束

- `go run . --self-check` 不联网、不解析凭据；脱敏认证摘要、Grok / Claude 认证状态等仍为 `not ported yet`。
- `run-dev-app.sh` 以正式身份 `com.quosparr.app` 在前台使用真实 HOME 运行，需先退出已安装实例。
- 开发打包必须显式 `V1_DEVELOPMENT_PACKAGE=1`；生产默认 `0` 并要求发布环境提供更新签名私钥。
- 手动开发运行保留 `QUOSPARR_DEV_*` 环境钩子；测试偏好域用 `QUOSPARR_PREFS_DOMAIN=<测试域>`。
- 交叉编译不能替代 Windows ACL、系统权限或真机安装验收。

## 验证记录

| 检查 | 状态 | 说明 |
|---|---|---|
| `go build ./...` | not run | 本次初始化未执行 |
| `go test ./...` | not run | 同上 |
| `go vet ./...` | not run | 同上 |
| `--self-check` / 真机验收 | not run | 见 [docs/v1-acceptance-checklist.md](v1-acceptance-checklist.md) |

记录新检查时写明：工作目录、命令、输入与环境、观察结果、证据位置；状态只用 `passed` / `failed` / `blocked` / `not run` / `not applicable`（附原因）。区分编译、启动、功能行为、打包验签与真机交互验收。

## 恢复与贡献

- 迁移与升级测试使用一次性 VM；日常开发用隔离 HOME，不碰真实账号、Keychain、登录项或生产 feed，结束后退出自行启动的进程。
- 官方文件写入前备份到 `~/.quosparr/backups`（各保留最近 10 份）；会话修复前备份 `session_index.jsonl`。
- 需要恢复点的变更遵循[规划策略](PLANS.md)。
