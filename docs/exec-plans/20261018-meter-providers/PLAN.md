---
id: 20261018-meter-providers
title: 新增 cursor / kimi / zai / agy 四个配额 provider（以 ../aimeter 为参照）
status: done
kind: plan
created: 2026-10-18
updated: 2026-10-18
---

# 新增 cursor / kimi / zai / agy 四个配额 provider

Scope: internal/{meter,cursor,kimi,zai,agy,runway,store,export,l10n,app}、Sources/QuosparrWidget、AGENTS.md、README
Revision/context: main @ 62e93c6（工作区干净）
Planning policy: docs/PLANS.md
Handoff: out/20261018-meter-providers/HANDOFF.md
移植规格: recipe/port-spec.md（业务行为的唯一事实来源，源自 ../aimeter 源码与 fixtures）

## Outcome, acceptance, and non-goals

**Outcome.** Quosparr 以只读方式支持四个新配额 provider，行为与 ../aimeter 一致（凭据发现、端点、响应归一化、错误分类、脱敏）：

- 主面板：四个 provider 可被选为当前 provider / 出现在面板 provider 列表；各自渲染一个配额区块（套餐名、meter 条、明细行）。
- 状态栏：可在自定义 provider 选择（≤2）里选中它们并显示状态文本与 meter。
- 悬浮窗：可在 provider 名单（≤3）里选中它们。
- zai：支持在设置账号管理里添加/编辑/删除 API Key 账号（仅 Quosparr 账号库，不写任何官方文件），多账号可选当前；也读取 `ZAI_API_KEY`、`Z_AI_API_KEY` 作为未配置时的回退。
- widget 快照与 status.json：providers 块携带四者的 ProviderSnapshot；panel 块新增 `meters` 子块供远程镜像只读展示。
- 凭据与隐私：cursor 只读打开 state.vscdb；kimi 只读凭据文件/环境变量；agy 只运行 `agy -p /usage --output-format json`；密钥不进日志、错误、缓存与 status.json。

**Acceptance.**

1. `go build ./...`、`go vet ./...`、`go test ./...` 全绿；`go test -race -count=1 ./internal/...` 全绿。
2. 四个新包各有确定性测试：httptest / 假二进制 / 临时 state.vscdb 覆盖 success、401、业务失败（zai code=500）、not_configured、脱敏（密钥不出现在任何输出）。
3. 面板数据面冒烟：隔离 HOME 下构建 Snapshot，四个 provider 的 MeterState 经 statusbar/floating/export 路径产出非空结果（单测级）。
4. l10n 新 key 七语言齐全（en、zh-Hans、zh-Hant、ko、ja、ru、fr），语言表完整性测试通过。
5. AGENTS.md 隐私边界更新；README（七语言结构）功能段更新。

**Non-goals.**

- 不做账号切换/写官方文件：cursor/kimi/agy 完全只读；zai 只写 `~/.quosparr/accounts/zai-api-<id>/` 与 `zai-index.json`。
- 不做四者的本地 token 图表、API 等价成本、会话列表、Skill 关联（无本机会话数据源）。
- Swift Widget 的 provider 选择 UI 不扩展（现有 scope 组合不变）；仅同步快照类型副本使其可解码新块。
- 不动 codex/grok/claude 现有行为与偏好兼容（`both` 语义不变）。

## Relevant context and constraints

- 参照实现：../aimeter（Rust）。规格全文见 [recipe/port-spec.md](recipe/port-spec.md)；fixtures 在 ../aimeter/research/fixtures/{cursor,kimi,zai,agy}/。
- 集成面清单（SurfaceScout，2026-10-18）：store 枚举 `internal/store/enums.go:9-22`；选择规范化 `internal/store/preferences.go:190-205,247-253`、`internal/store/floating.go:105-115,149-159`；runway 分发 `internal/runway/{build,refresh,statusbar,floating,widget,export,export_panel}.go`；app 面板 `internal/app/panel_section.go:60-74`、账号页 `internal/app/accounts_page.go:21-50`；l10n `internal/l10n/tables.go`；快照 schema `internal/export/{widget,providers,panel,contract}.go`。
- 约束：
  - 遵守 AGENTS.md 隐私边界：密钥只存账号库（目录 0700、文件 0600、原子写）；错误/日志/缓存不出现密钥或响应正文；`~/.kimi`、Cursor 目录、`~/.gemini` 只读。
  - HTTP 一律经 internal/netx Context（代理策略归属调用方），禁止自动直连回退、不跟随重定向。
  - SQLite 只读打开用 internal/sqliteruntime（随包运行库）。
  - 业务包不得依赖 UI；UI 经业务 API 读取。
  - `runway.Meter`（internal/runway/meters.go:47）承载配额条；有金额的行用 `ValueText`，纯比例用 percent。
  - Widget 快照 schema 变更须同步 Sources/QuosparrWidget 类型副本（Swift Codable 忽略未知键，新增字段向后兼容）。

## Milestones

- **M0 合同**（本会话直接做）：本计划 + port-spec；internal/meter 共享类型与 HTTP 帮助；store.Provider 扩为 7 项。
- **M1 业务包**（4 个并行派发）：internal/cursor、internal/kimi、internal/zai（含账号库）、internal/agy，各自含测试。验收：包级 go test 绿。
- **M2 集成**（先写 runway seam 骨架，再 5 路并行）：
  - runway seam：MeterAvailability/MeterState/MeterProvider，Options.Meters、Snapshot.Meters、StatusInputs.Meters。
  - A statusbar+floating 分发；B refresh/build/widget/export/status.json/远程消费；C app 接线+面板区块+设置命名+zai 账号页；D l10n 七语言；E Swift 副本同步。
- **M3 文档收尾**：AGENTS.md、README、验证证据、提交。

## Current state and next action

- 2026-10-18：M0–M2 全部完成（见 Validation evidence）；M3 文档进行中（AGENTS.md 已更新，README 七语言同步派发中）。下一步：README 合入后统一验证并提交。

## Decisions and open questions

- 显示名：Cursor / Kimi / Z.ai / Antigravity（agy 的 CLI 属 Antigravity，profile 在 ~/.gemini/antigravity-cli）。
- provider 顺序（store.Providers()）：codex, claude, grok, cursor, kimi, zai, agy（与 aimeter 的 codex claude cursor kimi zai … agy 顺序保持相对一致，grok 为 Quosparr 独有）。
- 状态栏首选窗口：四者均按"首个带比例的标准 meter"（业务包按重要度排序输出）；不引入新的 preferred-windows 常量除非测试需要。
- zai 凭据优先级：账号库当前账号 > `ZAI_API_KEY` > `Z_AI_API_KEY`；均无 → not_configured。region/auth_scheme 存于 index 行（不含 key）。
- status.json panel 块保持 schema 1，新增可选 `meters` 字段（旧导出端无此字段时远程端按缺失处理）。
- 待验证：Swift 快照副本的具体字段名（M2-E 执行时对照 export/widget.go）。

## Validation evidence

工作目录 `/Users/yangxuhui/Projects/Tools/Quosparr`（main @ 62e93c6 起步，改动在收尾统一提交）。

1. `go build ./...` — passed（2026-10-18，含新增 internal/{meter,cursor,kimi,zai,agy} 与 app/runway/export/store/l10n 改动）。
2. `go vet ./...` — passed（无输出）。
3. `go test ./...` — passed（退出 0，无 FAIL；含 cursor/kimi/zai/agy/meter 各自 fixture 测试、runway statusbar/floating/export/remote 新测试、app meter/zai/panel 测试、l10n 七语言完整性）。
4. `go test -race -count=1 ./internal/...` — passed（退出 0，无数据竞争）。
5. 隔离冒烟：`go build -o <scratch>/Quosparr . && HOME=<临时目录> QUOSPARR_PREFS_DOMAIN=com.quosparr.app.v1-meter-smoke ./Quosparr --self-check` — passed（版本/目录/锁检查输出正常，退出 0；不触网、不读真实凭据）。
6. 数据面冒烟（一次性测试 internal/app/meter_smoke_scratch_test.go，跑完已删除）：真实 `newMeterModules(home, prefs, langs)` + 假 cursor state.vscdb（ItemTable 写入 JWT）+ 假 kimi 凭据文件 + PATH 上的假 agy — passed：agy 完整链路 MeterReady、1 条 meter、weekly→10080 分钟、50%；cursor/kimi 凭据被发现并在无网环境下以 error 状态干净失败；zai 无 key 时 NotConfigured。
7. Swift：`xcrun swiftc -parse -swift-version 6`（RunwayWidgetTypes.swift / RunwayWidgetComponents.swift）— passed（SwiftSync 子任务执行，退出 0）。

未执行（如实记录）：真实上游账号联调（仓库测试纪律禁止触真实账户）；Windows/Linux 真机运行与 GUI 像素级走查（按 AGENTS.md 走 v1-acceptance-checklist，另行推进）。

## Outcome and remaining gaps

Outcome：四家 provider 全量落地并通过验收 1–5（见 Validation evidence）。internal/meter + internal/{cursor,kimi,zai,agy}（zai 含 0600/0700 账号库）；runway seam（Options.Meters/Snapshot.Meters/StatusInputs.Meters）与 refresh 按需调度；statusbar/floating/panel 通用分发与渲染；status.json providers 四块 + panel `meters` 子块（schema 1 保持）与远程重建；Widget 快照类型副本可解码；l10n 15 个新 key 七语言；AGENTS.md 隐私边界与 README 七语言同步。

Remaining gaps（显式非目标，非未完成）：Widget 选择 UI 不含四家（仅类型副本可解码，见 Decisions）；真实上游账号联调与三端真机/GUI 走查未执行（仓库纪律与验收流程另行推进）。
