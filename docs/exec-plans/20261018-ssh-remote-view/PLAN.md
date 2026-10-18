---
id: 20261018-ssh-remote-view
title: MacBook 端经 SSH 只读查看开发机 Quosparr 数据
status: done
kind: plan
created: 2026-10-18
updated: 2026-10-18
---

# MacBook 端经 SSH 只读查看开发机 Quosparr 数据

Scope: `internal/remote`（新增）、`internal/export`、`internal/runway`、`internal/store`、`internal/app`、`internal/l10n`
Planning policy: docs/PLANS.md
Handoff: out/20261018-ssh-remote-view/HANDOFF.md

## Outcome, acceptance, and non-goals

用户的开发工作流是 MacBook SSH 到开发机；Quosparr 需要装在 MacBook 上查看开发机的配额/用量。目标：

1. 开发机正常运行 Quosparr 并开启「状态导出」（现有偏好 `exportsStatusJSON`）；`status.json` 扩展为携带 Codex + Grok + Claude 三家摘要（配额、余额、每日 token、reset credits、成本、最近会话），仍不含任何凭据。
2. MacBook 端 Quosparr 新增「远程数据源」设置（host/user/port/远端路径/开关 + 测试连接）。开启后进入远程只读模式：经系统 `ssh` 二进制（BatchMode，非交互，认证走 ssh config/key/agent）周期拉取远端 `status.json`，反向构建 `runway.Snapshot`，状态栏 + 主面板 + 悬浮窗（跟随主数据源）展示远端数据。
3. 远程模式下禁用本地采集（不启动本地 runway live sources / grok / claude 模块），账号管理、登录/导入、切号、token 刷新、会话修复、Skill 启停/删除等写操作全部隐藏或禁用并给出只读提示。

验收（工程）：

- `go vet ./...`、`go build ./...`、`go test ./...` 全绿；新增包有针对性行为测试。
- 远程模式冒烟：本地起 sshd 或用 `ssh localhost` 拉取伪造 status.json → 主面板/状态栏出现远端数据；断开（错误 host）→ 面板显示错误且不崩。
- 凭据边界不变：token 不离开开发机；`status.json` 扩展块不含 token/邮箱；日志/错误不含远端文件内容。
- l10n：新增 key 七语言（en/zh-Hans/zh-Hant/ko/ja/ru/fr）齐全，`tables_test`/`extra_l10n_test` 通过。

非目标（本期不做）：

- SSH/SFTP 直读远端 `~/.codex`、`~/.grok`、`~/.claude` 原始文件（全量功能远程化）。
- 远程写操作（切号、修复等经 SSH 写回）。
- WidgetKit 组件显示远端数据（Widget 读本地文件，天然不支持）。
- 引入第三方 SSH 库；代理 SSH 流量（走系统 ssh 自身配置，含 ProxyJump）。
- 密码交互式认证（无 sshpass；BatchMode 失败即报错）。

## Relevant context and constraints

- 现状：全仓无 FS 抽象，`internal/codex|grok|claude` 直读本机文件；UI 消费 `internal/runway.Snapshot`（`internal/runway/snapshot.go:38`），装配在 `internal/app/live.go:201-284` `newRuntime`。
- 已有导出：`~/.quosparr/status.json`（`export.StatusSnapshot`，`internal/export/status.go`，偏好 `ExportsStatusJSON` 开启才写，仅 Codex quota + cost + sessions）；`widget-snapshot.json` 仅 Widget 需要（无 Widget 时可能不写，不作为远程载体）。
- 网络层 `internal/netx` 仅 HTTP；系统二进制调用先例 `/usr/bin/security`（`internal/claude/credstore_keychain*.go`，CommandRunner 注入 + 超时 + 退出码区分）。
- 偏好：`store.Preferences` 单文档 JSON（macOS UserDefaults `com.quosparr.app` / 其他平台 `~/.quosparr/preferences.json`）；Go 专属键默认省略模式参照 `floatingWindow`（零值省略整个对象）。
- 设置 UI：`internal/app/settings_view.go` `settingsTabs`/`pageContent`/`sidebarGroupStarts`；新页面模式参照 `notificationsGroup`。
- l10n：`internal/l10n/tables.go` + 完整性测试；新增 key 必须七语言。
- 约束：不引入第三方依赖；SSH 走系统二进制（macOS/Linux `/usr/bin/ssh` 常见路径 + PATH 回退；Windows `System32\OpenSSH\ssh.exe`，缺失时给用户可读错误）；远程命令注入防护（远端路径需安全引用）；错误与日志不得包含远端文件内容或凭据。

## Milestones

- M1 契约与计划（本文件 + HANDOFF）。
- M2 并行切片：
  - A `internal/remote`：SSH 传输客户端（Config/Fetch/TestConnection，CommandRunner 注入，超时/退出码分类，安全引用）+ 单元测试（fake runner）。
  - B `internal/store`：`RemoteSource` 偏好对象（Enabled/Host/User/Port/Dir），codec、默认省略、Clone、Validate + 测试。
  - C `internal/export` + `internal/runway`：`StatusSnapshot` 增量扩展 `providers` 块（grok/claude 摘要，源自现有 GrokState/ClaudeState → widget 同源数据），`statusSnapshotLocked` 填充 + 测试；向后兼容（旧读新忽略）。
- M3 集成切片 D：
  - `internal/remote`（或 runway 内）：拉取循环 + `StatusSnapshot → runway.Snapshot` 反向构建器（含 StatusBar/Floating 视图、错误事件）。
  - `internal/app`：`newRuntime` 远程模式分支（不启动本地采集）、设置页「远程数据源」、写操作门控 + 只读提示。
  - l10n 七语言全量。
- M4 验证：全量门禁 + 冒烟（ssh localhost 拉伪造 status.json；错误 host 断链场景）。
- M5 收尾：PLAN 验证证据、HANDOFF 更新、README/docs 隐私边界补充。

## Current state and next action

- 2026-10-18：M1–M5 完成。M2 三切片（internal/remote、store.RemoteSource、export.StatusProviders+runway 填充）与 M3 两切片（runway 远程服务与反向构建、internal/app 接线/设置页/门控/l10n 18 key）全部落地；修复的子代理缺陷：readPanelProviders 被误替换、statusProviders 重名、accountKind 伪类型、测试游离片段/重复函数。

## Validation evidence

工作目录 /Users/yangxuhui/Projects/Tools/Quosparr，Go 1.27.1（本机需 `GOPROXY=https://goproxy.cn,direct`，proxy.golang.org 不通）：

- `go build ./...`：passed。
- `go vet ./...`：passed。
- `go test -count=1 ./...`：passed（全仓，无 FAIL）。
- `go test -race -count=1 ./internal/runway/ ./internal/remote/ ./internal/store/`：passed。
- 数据通路冒烟（passed）：`ssh -o BatchMode=yes localhost` 自连可用后，伪造含 providers 块的 status.json（/tmp 下），经 internal/remote 真实 ssh 拉取 + `BuildRemoteSnapshot`：成功路径输出 codex/grok/claude meters 与状态栏视图；错误路径（不存在目录）返回 `remote file not found: localhost: exit 1`，无 stderr/文件内容泄漏。
- 打包二进制 GUI 冒烟（数据面 passed / 像素面 not run）：`go build` 二进制 + `HOME=<临时> QUOSPARR_PREFS_DOMAIN=<测试域>`（偏好仅 remoteSource + 关自动更新）启动，进程驻留，临时 HOME 出现 `widget-snapshot.json` 且内容为伪造远端数据（claude max 55%/20%、codex pro resetCredits=2、grok supergrok）——证明 prefs→远程模式→ssh 拉取→解码→构建→发布→widget 写入全链路。像素级界面截图 not run：会话期间屏幕锁定且无屏幕录制/AX 权限；退出后已清理测试域与临时目录。
- 跨平台（not run on device）：Windows `System32\OpenSSH\ssh.exe` 解析与 Linux 路径仅经单测与编译检查，未做真机验收（与三端验收清单一致，另行推进）。

## Outcome and remaining gaps

- 已交付：SSH 远程只读查看（快照拉取通路、全局数据源切换、状态栏+主面板+悬浮窗远端数据、设置页「远程数据源」含测试连接、写操作门控、七语言文案、AGENTS.md 隐私边界）。
- 遗留（非本链验收项）：GUI 像素级与三端真机验收（锁屏/权限所限，走 docs/v1-acceptance-checklist.md 流程）；README/website 七语言功能与隐私说明随发布准备同步；RemoteService 构造后偏好变更（如语言）需重启生效；远端机器需手动开启「导出状态 JSON」。

### 部署补记（2026-10-18 下午）

- 实机部署发现 `quoteRemote` 把默认目录 `~/.quosparr` 整体单引号包裹，远端 shell 不再展开 `~`，`cat` exit 1（ErrFileNotFound）。修复：`remotePath` 对纯字符路径裸传、保留 `~`/`~user` 前缀不引用、其余引用；补 tilde+空格 用例。原冒烟用绝对路径目录，未覆盖默认目录场景——已记为教训。
- 两台实机验收：开发机 10.10.10.20（数据源，开启状态导出）→ MacBook 10.10.10.22（远程模式）：widget 快照实时镜像远端数据（claude Max 5x 2 行、codex pro、grok notLoggedIn 如实镜像），GUI launchd 上下文探针验证 BatchMode 认证与拉取 OK；剥离 quarantine 避免 App Translocation。
