---
id: 20261018-remote-panel-parity
title: 远程只读模式的面板功能对齐（本地读取一致）
status: done
kind: plan
parent: 20261018-ssh-remote-view
created: 2026-10-18
updated: 2026-10-18
---

# 远程只读模式的面板功能对齐（本地读取一致）

Scope: `internal/export`、`internal/runway`、`internal/app`、`internal/alerts`（如需）、`AGENTS.md`
Planning policy: docs/PLANS.md
Handoff: out/20261018-remote-panel-parity/HANDOFF.md

## Outcome, acceptance, and non-goals

用户在 MacBook 安装 Quosparr 并开启远程读取后，主面板 / 状态栏 / 悬浮窗 / 设置的**只读展示**须与本地读取一致。前置审计（三个只读 scout）确认当时远程模式只镜像 widget 级摘要，缺口：

1. Grok / Claude：API 等价成本（Cost 与明细 QueryCost）、token 图表、最近会话、速度分析、reset credits、Claude ExtraUsage / API 中转视图、Grok 账单弹层（GrokState.Detail）均未镜像。
2. Codex：周额度推算 + 历史对比（Estimate/EstimateError）、速度分析、图表双序列（全部设备 + 本机，现仅单序列）、reset credits 明细列表（CreditDetails）、API Key 模式与中转余额（APIKeyMode/APIBaseURL/APIBalance）。
3. 三家账号列表只读展示（AccountsView / AccountQuotas，标签绝不回退到邮箱）。
4. 拉取失败时远程快照被错误视图整体替换（本地睡眠 / 网络抖动即清空面板）；应保留上一份好数据并叠加错误。
5. RemoteService 偏好（语言、刷新间隔、渲染选择）改动需重启才生效。
6. 远程模式无任何通知（本地有配额阈值与 reset credit 到期提醒）。

验收（工程）：

- `go build ./...`、`go vet ./...`、`go test -count=1 ./...` 全绿；race（runway/export/app/store/remote）全绿。
- 导出侧：`statusSnapshotLocked` 在开启导出时写出 `panel` 块（schema 1）；旧字段不变；不含 Email / token / 凭据。
- 导入侧：含 `panel` 块的伪造 status.json 经 `BuildRemoteSnapshot` 后，上述 1–3 全部字段还原；无 `panel` 块（旧远端）退化为现状行为，不报错。
- 失败保持：fake fetcher 先成功后失败，快照保留上一份好数据并在 provider LastError 中可见错误；无历史时维持现有错误快照。
- 远程 QueryCost：三家按日行过滤任意范围返回汇总（日粒度）；窗口与导出汇总一致时附带 breakdown 行。
- 远程通知：成功拉取后按本机偏好生成配额阈值 / 到期 credit 候选，本机 alerts.json 去重后经 OnEvent 发出（与本地 deliver 语义一致）。
- l10n：不新增 key（复用现有文案；远程只读横幅已存在）。

非目标：

- Skill 管理功能（用户明确排除；远程模式维持现状：读操作走本机、写操作禁用）。
- 一切写操作（切号、登录/导入、修复、启停、reaction 提交）保持禁用。
- 经 SSH 直读远端 `~/.codex` 等原始文件；远程写回；Widget 显示远端数据以外的扩展。
- reset-today 检测提醒（来自远端 provider 轮询，不在本期）。

## Relevant context and constraints

- 隐私上限（AGENTS.md）：`providers` 块字段上限不变；新 `panel` 块只放派生展示数据（数字、日期、模型名、会话标题/路径——与既有顶层 sessions 块同域、账号标签、缓存配额行），**不得**包含凭据、邮箱、token；账号标签解析链去掉 Email 回退（`codex.ManagedAccount.DisplayNameWithoutEmail`）。
- 架构缝：runway 不 import grok/claude。Grok 账单导出走 `GrokState.Billing *export.PanelGrokBilling` 镜像字段（internal/app 适配器填充；先例：`GrokState.Widget`）。远程 Detail 重建在 internal/app（OnSnapshot 内经 `RawStatus()` 取原始块）。
- 跨切片契约：`internal/export/panel.go` schema 1（冻结）；S3 提供 `RemoteService.SetPreferences` / `RawStatus` / `RemoteOptions.OnEvent` / `RemoteOptions.AlertsPath`；S4 消费并安装远程 `QueryCost func(context.Context, store.Provider, CostQuery) (usage.Summary, error)`。
- 双向兼容：新读旧（无 panel 块）→ 现状退化；旧读新（未知块忽略）。
- 时区：日行按开发机本地时区生成，远程按日期字符串过滤（UTC 日界半开区间），不做跨时区重算。

## Milestones

- M1 审计（3 scout，结论如上）。
- M2 契约：PLAN + HANDOFF + `internal/export/panel.go`（orchestrator 直接落地）。
- M3 并行三切片：S2 导出填充、S3 远程服务（panel 导入全量还原、SetPreferences、失败保持、RawStatus、告警候选提取）、S4 应用接线（OnEvent、远程 QueryCost、Grok Billing 镜像与 Detail 重建、偏好转发）。
- M4 门禁 + 数据通路端到端（`TestRemotePanelEndToEnd` 跨方向 round-trip）。
- M5 AGENTS.md 隐私段补记、验证证据、HANDOFF。

## Current state and next action


### 部署补记（2026-10-18 下午，实机 10.10.10.20 → 10.10.10.22）

- 实机部署发现：本地扫描（成本/图表/会话/速度/推算）原本只在面板打开时运行（`panelOpen` 门控），开发机重启后面板未开 → status.json 的 panel 块缺 estimate/speed/cost/图表/会话，MacBook 镜像退化为空模块。修复（沿用 `widgetLocal` 先例）：`exportsStatusJSON` 开启时完整刷新对所有供应商做本地扫描——`localScope.export` 驱动 grok/claude `RefreshLocal`、codex `loadRecentSessions`/`loadSpeed`、estimate 与 cost/heatmap 扫描门控（`TestClosedPanelExportStillScansLocalData` 回归）。
- 重新打包部署两端后（本机数据源 + MacBook 均为新构建）：本机开机即写出全量 panel 块（estimate/speed/credits/318 天全设备 + 9 天本机序列/账号/claude 成本与 6 天序列），MacBook 拉取后 widget 镜像 `generatedAt=2026-10-18T06:14:31Z` 同步携带 codex 318 天、claude 6 天序列与全部配额行；进程驻留。MacBook 远程配置复核：remoteSource → 10.10.10.20 默认目录。

### 用户实测反馈修复（2026-10-18 第三轮，MacBook 实机截图）

1. 会员信息缺失：`PanelIdentity` 增加 `tier`/`subscriptionExpiresAt`，`ProviderPanelBlock` 增加 `display`；导出侧 `panelIdentity` 对邮箱形态的 DisplayName 消毒（回退 Username，仍含 @ 则丢弃，测试 `TestPanelIdentityNeverCarriesEmail`），导入侧恢复 `AccountsView.Display`（Tier 徽标、到期角标、身份名）。
2. 供应商页签不可点击：远程模式安装 `SelectProvider`（本地偏好，经 applyPreferences→RemoteService.SetPreferences→wake 即时重渲），只同步数据、UI/设置各机自理。
3. Claude 数据不可见：随 2 解决（页签可切换；claude 块数据此前已镜像）。
4. Codex 窗口标签错误（周数据被标成 5 小时）：该账号 API Primary 实为周窗口（10080 分钟），`NewStatusQuota` 按位置硬命名 "5-hour"；`statusQuota` 改为按真实时长命名（≥6 天 → Weekly，≤12 小时 → 5-hour，未知时长回退位置名），`TestStatusQuotaNamesWindowsByLength` 回归。
5. 重置动态显示不可用：`remoteResetToday` 用 `resettoday.NewStateSnapshot` 从 widget 块重建 `Detail`（state yes/no/unknown + LastSuccessfulCheckAt），卡片渲染实际结论而非「不可用」。
6. 会话修复与 Skills 模块：远程模式 `panelModuleOn` 直接隐藏（不再渲染禁用态）。

验证：build/vet/test 全绿 + race 绿；两端重新部署后本机导出 `primary=Weekly 7%`、display 无邮箱（Bootis/pro200/到期日），MacBook 拉取后 codex 首行配额标题为「每周」。
- 2026-10-18：M1–M5 完成，链关闭。三切片落地后 orchestrator 集成修复：`codexPanelBlock` 导出/导入重名（导入侧改 `panelCodexBlock`）、S2 泛型 `line()` 方法约束不可编译（改为函数参数 `grokQuotaLine`/`claudeQuotaLine`）、S2 测试 `decimal.MustDecimal`→`MustParse` 与缺失 `apiquota` import、S3 测试 `CachedQuota`→冻结字段 `Quota`、远程成本过滤改 UTC 日字符串半开区间（原 `RangeForWindow+time.Local` 在非 UTC 机上边界漂移）、`SetPreferences` 增加 wake 通道让间隔/语言改动即时生效（原实现要等旧 sleep 到期）、E2E fixture 补 `providers` 块（生产两块同写）且 `asOf` 固定为 2026-10-17 UTC。

## Validation evidence

工作目录 /Users/yangxuhui/Projects/Tools/Quosparr，Go 1.27.1（GOPROXY=https://goproxy.cn,direct）：

- `go build ./...`：passed。
- `go vet ./...`：passed（含全部新增测试文件）。
- `go test -count=1 ./...`：passed（全仓无 FAIL）。
- `go test -race -count=1 ./internal/runway/ ./internal/export/ ./internal/app/ ./internal/store/ ./internal/remote/`：passed，无数据竞争。
- 数据通路端到端（passed）：`TestRemotePanelEndToEnd`（internal/runway/remote_test.go）用 S2 富 fixture 经 `statusExportPanel`→`StatusSnapshot.Encode()`（swiftjson）→解码→`BuildRemoteSnapshot`，断言 Codex 推算/历史/双序列 tokens/reset credits 明细/速度（NS↔Duration）/API 余额、账号视图（Email 恒空，全文 `@chatgpt.com`/`@example.com` 泄漏扫描）、Grok 成本/图表/会话/账单/账号行、Claude ExtraUsage/API/成本/账号行全部还原。
- 行为验证（passed，单测覆盖）：失败保持（先成功后失败→三家 LastError 非空且数据保留；首次失败→现有错误快照）；`SetPreferences`（切语言后文案变化；1s 间隔经 wake 通道自动再拉，Seq 递增）；远程 QueryCost（Custom 子窗口日行过滤/汇总/无 breakdown，Current 全窗口含 ModelRows，无块→unavailable）；告警去重（同一数据两次 fetch 只发一次，临时 AlertsPath）；Grok Billing 镜像 round-trip（reflect.DeepEqual）。
- GUI 像素级与 MacBook/开发机真机复验：not run（与前链相同限制，锁屏/权限；走 docs/v1-acceptance-checklist.md 流程另行推进）。

## Outcome and remaining gaps

- 已交付：远程只读模式与本地读取的只读展示对齐——Grok/Claude 成本+明细查询/图表/最近会话/速度/reset credits/ExtraUsage/API 视图/账单弹层、Codex 推算+历史/双序列图表/credits 明细/API Key 余额、三家只读账号列表（无邮箱）、失败保持、偏好即时生效、配额与到期 credit 通知；`panel` 块 schema 1 双向兼容（旧远端→现状退化；旧读取端→忽略未知块）；AGENTS.md 隐私边界补记。
- 已知轻微降级（设计内，记录不追）：远程账号卡附加模型行无 ResetsAt（`PanelQuotaMeterRow` 未含）；Grok/Claude 账号配额行 Title 由 Plan 回退（状态无 PeriodKind 可导）；非导出窗口的成本明细无模型/项目拆分行（日行过滤语义）；reset-today 检测提醒不远程化；Skill 管理不参与远程镜像（用户明确排除）。
- 真机：两端（开发机 10.10.10.20 与 MacBook 10.10.10.22）需同时升级到含本链的构建后，MacBook 端才能看到完整面板镜像。
