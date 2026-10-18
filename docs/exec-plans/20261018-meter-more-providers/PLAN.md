---
id: 20261018-meter-more-providers
title: 新增 minimax / deepseek / pixellab / elevenlabs 四个配额 provider（以 ../aimeter 为参照）
status: done
kind: plan
created: 2026-10-18
updated: 2026-10-18
parent: 20261018-meter-providers
---

# 新增 minimax / deepseek / pixellab / elevenlabs 四个配额 provider

Scope: internal/{meter,deepseek,minimax,pixellab,elevenlabs,runway,store,l10n,app}、AGENTS.md、README×7
Revision/context: main @ d6eb243（工作区干净；两条旧链已补提交）
Planning policy: docs/PLANS.md
Handoff: out/20261018-meter-more-providers/HANDOFF.md
移植规格: recipe/port-spec.md（业务行为的唯一事实来源，源自 ../aimeter 源码与 fixtures）

## Outcome, acceptance, and non-goals

**Outcome.** Quosparr 以只读方式再接入四个配额 provider，行为与 ../aimeter 一致（凭据发现、端点、响应归一化、错误分类、脱敏）：主面板配额区块、状态栏（≤2）与悬浮窗（≤3）可选、status.json providers/panel meters 块、设置账号管理四家单账号配置页签（minimax 含 region 选择）。

**Acceptance.**

1. `go build ./...`、`go vet ./...`、`go test ./...` 全绿；`go test -race -count=1 ./internal/...` 全绿。
2. 四个新包各有确定性测试：httptest + fixtures（复制自 ../aimeter/research/fixtures/<p>/）覆盖 success、401/auth、业务失败（minimax 1004）、not_configured、文件发现（minimax region 提示、pixellab/elevenlabs 纯文本修剪）、脱敏（key 不出现在任何输出）。
3. 面板数据面冒烟：deepseek Balances → Detail 行；pixellab/elevenlabs meters 非空；minimax 5h/7d 池（单测级）。
4. l10n 新 key 七语言齐全，语言表完整性测试通过。
5. AGENTS.md 隐私边界更新；README 七语言功能段同步。

**Non-goals.** 见 recipe/port-spec.md「明确非目标」（无多账号库/图表/速度/Swift UI；mimo 不在范围）。

## Relevant context and constraints

- 参照：../aimeter @ 0a796a0 src/{providers,credentials,config,model}.rs；fixtures 在 research/fixtures/{minimax,deepseek,pixellab,elevenlabs}/；tests/integration.rs 的断言已并入 recipe。
- 模板：internal/kimi（包结构/flexFloat/GetJSON 用法/测试风格）、internal/kimi/config.go（显式配置三函数）、internal/zai/quota.go（region 分支与 EnvKey）。
- seam 已存在：runway MeterProvider/meterModule/MeterState、MeterProviders() 迭代处（widget/export/statusbar/remote）自动覆盖新 provider。
- 约束：与上一链相同（netx 代理、不跟随重定向、密钥只存账号库 0600/0700、错误脱敏、业务包不依赖 UI）。

## Milestones

- **M0 契约**（本会话直接做）：本计划 + recipe；meter.Balance + State.Balances；store 四个新枚举与 Providers() 顺序；meterStateFromRaw 余额→Detail；tables.go provider 名称与余额 key 七语言。
- **M1 业务包**（4 个并行派发）：internal/deepseek、internal/minimax、internal/pixellab、internal/elevenlabs，各自含 config/credential/Fetch 与 fixture 测试。验收：包级 go test 绿（由编排者统一跑）。
- **M2 集成**（2 路并行）：A runway/app 接线（MeterProviders、newMeterModules、quotaOnlyProvider、providerKey、受影响 runway/export/app 测试）；B 设置页签 + extra_l10n 七语言 + 账号页测试。
- **M3 文档**：AGENTS.md（inline）、README×7（派发）、INDEX 登记。
- **M4 终验**：全量门禁 + 隔离 self-check 冒烟 + 证据 + 提交。

## Current state and next action

- 2026-10-18：M0–M4 全部完成（见 Validation evidence），链收尾。

## Decisions and open questions

- 显示名：MiniMax / DeepSeek / PixelLab / ElevenLabs；Providers() 顺序 codex, claude, grok, cursor, kimi, zai, deepseek, agy, minimax, pixellab, elevenlabs（aimeter 相对顺序，grok 为 Quosparr 独有插在 claude 后）。
- 余额（deepseek/pixellab）走 meter.State.Balances → app Detail 行；不进 Meters/速率采样/Widget 快照新字段（Detail 已在 panel meters 块 schema 内，远程镜像自动携带）。
- minimax region：显式配置（cn/intl，省略未设置）> 文件 base_url 提示 > 默认 cn（api.minimaxi.com）。
- 四家无 oh-my-pi Token 速度（本机无 token 记录，对齐 cursor/agy 决策）。
- env 回退对齐 aimeter 常规键：DEEPSEEK_API_KEY、MINIMAX_CODING_API_KEY>MINIMAX_API_KEY、PIXELLAB_API_KEY、ELEVENLABS_API_KEY。

## Validation evidence

工作目录 `/Users/yangxuhui/Projects/Tools/Quosparr`（main @ d6eb243 起步：先把两条已完成旧链的未提交工作补提交为 d6eb243，再开始本链）。

1. `go build ./...`、`go vet ./...` — passed（2026-10-18，含新增 internal/{deepseek,minimax,pixellab,elevenlabs} 与 meter/store/runway/app/export/l10n 改动）。
2. `go test ./...` — passed（38 个包 ok，退出 0，无 FAIL；含四包 fixture/凭据/配置测试、runway statusbar/export 新分支、app 接线与设置页签测试、l10n 七语言完整性与 key 计数 683）。
3. `go test -race -count=1 ./internal/...` — passed（无数据竞争）。
4. 隔离冒烟：`go build -o <scratch>/Quosparr . && HOME=<临时目录> QUOSPARR_PREFS_DOMAIN=com.quosparr.app.v1-more-meters ./Quosparr --self-check` — passed（版本/目录/锁检查输出正常，退出 0；不触网、不读真实凭据）。
5. 数据面冒烟（一次性测试 internal/app/more_meters_smoke_scratch_test.go，跑完已删除）：四家 Fetch 输出形状（各包 fixture 测试锁定）经 meterStateFromRaw 映射 — passed：deepseek 双币种余额 → 2 条 Detail（"Balance (CNY)" = "110 · granted 10 · topped up 100"）、minimax 5h/7d 池（300/10080 分钟、10%/22%、带重置）、pixellab Generations 池（"78 / 2000"）+ usd 余额行、elevenlabs 4 池（characters 带重置、"23243 / 131000"）。
6. UI 布局回归：settings 最小宽度（TestSettingsSidebarMinimumSizeTranslatedPages）与面板头部（TestPanelSpeedCardRowLayoutInEveryLanguage）在 11 家 provider 下通过 — providerTabsOf 轨道 Wrap+Shrink(1)、providerStrip >7 家时紧凑图标模式。
7. registry：`registry.mjs --write`/`--lint` — 0 errors（W13 为 AGENTS.md 文档性 out/ 提及，先前已存在）；--write 顺带漂移的 docs/PLANS.md 模板差异已还原，未入库。

未执行（如实记录）：真实上游账号联调（仓库测试纪律禁止触真实账户）；Windows/Linux 真机运行与 GUI 走查（按 AGENTS.md 走 v1-acceptance-checklist，另行推进）；Swift 侧无改动（快照 schema 未变，`xcrun swiftc -parse` 未重跑）。

## Outcome and remaining gaps

Outcome：四家 provider 全量落地并通过验收 1–5。internal/{deepseek,minimax,pixellab,elevenlabs}（kimi 式显式配置，minimax 含 region）+ meter.Balance/State.Balances；runway seam 与 refresh/statusbar/export/widget/remote 按 MeterProviders 自动覆盖；status.json providers 块新增四个可选字段；app 接线、设置账号管理四页签（minimax region 选择）、面板余额 Detail 行与紧凑 provider 条；l10n provider 名称与余额 key 七语言 + extra_l10n 31 key 七语言；AGENTS.md 与 README×7 同步。

Remaining gaps（显式非目标，非未完成）：四家无多账号库/图表/API 成本/会话/Skill/oh-my-pi 速度；Widget 选择 UI 不扩展（类型副本可解码）；aimeter 的 mimo provider 不在范围；真机验收另行推进。
