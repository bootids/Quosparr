---
id: 20261018-meter-config-rates
title: 四家配额 provider 的账号配置与消耗速率分析（含 zai 抓不到修复）
status: done
kind: plan
created: 2026-10-18
updated: 2026-10-18
parent: 20261018-meter-providers
---

# 四家配额 provider 的账号配置与消耗速率分析

Scope: internal/{meter,cursor,kimi,zai,agy,runway,export,app}、AGENTS.md、README（七语言）
Revision/context: main @ 201838e + 该提交之上未提交的 quota-only 页签面板改动（保留）
上游规格: ../20261018-meter-providers/recipe/port-spec.md（行为基线不变，本链只加显式配置与派生分析）

## 用户反馈（原始三项）

1. zai 配置了账号数据也抓不到。
2. kimi、cursor、agy 没有账号配置的地方。
3. 额外加的 4 个账号数据也要加上速率分析功能。

## Outcome

- **zai 修复**：根因是 `zai.Store.Add` 从不设置 `Current`，唯一账号未被选中时 `zaiCurrent` 落到环境变量回退并报 notConfigured。现在 `Add` 在无当前账号时自动设为当前；`Current()` 对「空/失效指针 + 仅剩一行」自愈返回该行（不落盘），已添加但未设当前的存量安装直接恢复抓取。
- **kimi / cursor / agy 配置**：设置「账号管理」新增 Cursor、Kimi、Antigravity 三个页签（单配置，无多账号行）。存储对齐 zai 惯例：`~/.quosparr/accounts/kimi/auth.json` `{"apiKey"}`、`accounts/cursor/auth.json` `{"accessToken"}`（0600 原子写、Windows protected ACL）、`accounts/agy/config.json` `{"bin","profileDir"}`（非密钥）。优先级：显式配置 > 本机发现（kimi 为 显式 > `KIMI_CODE_API_KEY` > 凭据文件）。配置页显示当前凭据来源与状态，保存/清除后即时刷新对应 provider；不写任何官方 CLI 文件。
- **消耗速率分析**：`internal/meter/history.go` 以 `~/.quosparr/meter-history.json` 保存各池采样（仅时间戳与用量数值，5 分钟最小间隔、30 天/池 1500 条上限），成功刷新后记录；速率 = 重置感知的尾段真实耗时斜率（≥3 样本最小二乘、2 样本端点差，段距 ≥30 分钟才可用），派生每池消耗速率（$/h、%/h 或单位/h）、线性耗尽时间与重置时投影（按 limit/100 截断）。四家 provider 的配额区块渲染「消耗速率」段；`runway.MeterState.Rates` 经 panel `meters` 块 `rates` 字段（schema 1 可选字段）随远程镜像同步，远程端同一渲染路径只读展示。状态栏、悬浮窗、Widget 不展示速率。

## Non-goals

- 不做 kimi/cursor/agy 的多账号库、token 刷新或官方文件写入。
- 不改四家的配额端点、归一化与脱敏行为（port-spec 不变）。
- Widget 选择 UI 不扩展；`meter-history.json` 不进入 status.json `providers` 块。

## 验证记录

工作目录：仓库根。环境：darwin/arm64，Go 1.27.1（go.mod 固定）。

| 状态 | 项目 | 证据 |
| --- | --- | --- |
| passed | `go build ./...` | 全仓编译通过（2026-10-18） |
| passed | `go vet ./...` | 无诊断 |
| passed | `go test ./...` | 34 个包 ok、0 失败（含新增 zai/meter/kimi/cursor/agy/app/runway 用例） |
| passed | `go test -race -count=1 ./internal/...` | 全部通过，无数据竞争 |
| passed | 隔离自检冒烟：`go build -o /tmp/quosparr-orch-selfcheck .` 后以临时 HOME + `QUOSPARR_PREFS_DOMAIN=com.quosparr.app.v1-orchestrated` 运行 `--self-check` | 输出版本/目录/锁检查正常，未触碰真实数据 |
| not applicable | 三端真机安装验收 | 未修改安装/更新链路；按 AGENTS 既有清单另行执行 |

修复过程中返工两处并复验：meter history 的 AtReset 基准时间与斜率口径（改为真实耗时斜率，修正误推出的测试期望）、`meterPanelBlock` 被误删的 Detail 映射、accounts 页签丢失的 `accountKindCodex` 分支、`extra_l10n.go` 误删的 zh-Hant 行——均含于上述绿灯结果。

## 后续（非本链）

- 远程端展示速率依赖对端为新版本（旧对端 `rates` 缺省渲染为空，不报错）。
