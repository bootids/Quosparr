---
id: 20261018-meter-panel-speed
title: Kimi/Z.ai 页签只留 5h+周池，并为 kimi/zai 增加 oh-my-pi Token 速度
status: done
kind: plan
created: 2026-10-18
updated: 2026-10-18
parent: 20261018-meter-providers
---

# Kimi/Z.ai 页签简化与 oh-my-pi Token 速度

Scope: internal/{kimi,zai,meter,app,runway,export}、AGENTS.md、README（七语言）
Revision/context: main @ 201838e 之上，含未提交的 20261018-meter-config-rates 全部改动（保留）

## 用户反馈（原始两项）

1. kimi、zai 的 tab 面板样式有问题，很丑；只关注 5 小时和一周使用量以及对应重置时间。
2. kimi、zai 的 token 速度从 ohmypi 的文件分析，cursor 从 agent cli 分析，agy 从 agy cli 的文件分析。

## 数据源裁定（本机核查 + 用户确认）

- ohmypi = oh-my-pi（omp），本机统计库 `~/.omp/stats.db`（SQLite，`messages` 表含 provider/model/timestamp/duration/ttft/token 计数，逐条记录）。provider 值：`kimi-code`、`zai`、`cursor`、`google-antigravity` 等。
- Cursor Agent CLI 本地无 token 记录：`~/.cursor/ai-tracking/ai-code-tracking.db` 只有代码统计；`~/.cursor/chats/*/store.db`（blobs/meta 两表）消息 JSON 无 usage 字段；`agent --help` 无 usage 命令。
- agy CLI 本地无 token 记录：`~/.gemini/antigravity-cli/conversations/*.db` 的 steps/gen_metadata 为无 schema protobuf，日志只含认证信息。
- 用户选择：仅 kimi、zai 显示 Token 速度（读 omp 统计）；cursor/agy 不显示、不以 omp 代替。

## Outcome

- **池过滤（业务层）**：`internal/kimi/kimi.go` normalize 只保留 5h（300min）与 7d（10080min）各至多一池，偏好顺序：有百分比数据者 > limits[] 条目 > usage 块 > usages 条目；月池、未知窗口与同窗重复表示全部丢弃。`internal/zai/quota.go` Fetch 只保留窗口为 5h/7d 的池（tokens/credits），`tools_monthly` 与未知窗口丢弃；usageDetails 聚合不变。
- **主行渲染**：`internal/app/panel_meter.go` panelMeterReady 增加 provider 参数，kimi/zai 的所有额度行走主行样式（剩余百分比 + 进度条 + 重置时间），cursor/agy 保持首行主行、其余细行。
- **Token 速度（kimi/zai）**：`internal/meter/omp_speed.go` ScanOMPSpeed 只读打开 `~/.omp/stats.db`，仅查 messages 的数值列（时间戳/耗时/首字/输出 token），按 session_file 分组转 speed.Sample，speed.Build 出报告；文件缺失或无数据返回零报告（不显示），错误只含路径与类别。cursor/agy 不接 speedScan。
- **管线与镜像**：`runway.MeterState` 增加 `Speed *speed.Report`/`SpeedError`；meterModule.Refresh 在配额刷新后附带扫描；panel `meters` 块增加可选 `speed` 字段（schema 兼容，旧端缺省为不显示），远程端同路径只读展示。状态栏/悬浮窗/Widget 不展示。
- **文案**：新增 `xMeterSpeedSource`（oh-my-pi 来源说明）补齐七语言；AGENTS.md 隐私边界与 v1 说明、README×7 同步。

## Non-goals

- 不做 cursor agent CLI / agy CLI 私有格式的逆向解析（protobuf 无 schema，不可靠）。
- 不改消耗速率分析（meter-history 采样）与四家配额端点行为。
- 不读取 `~/.omp/stats.db` 的 `user_messages` 等正文表；omp 库只读、不写。

## 验证记录

工作目录：仓库根。环境：darwin/arm64，Go 1.27.1（go.mod 固定）。

| 状态 | 项目 | 证据 |
| --- | --- | --- |
| passed | `go build ./...` | 全仓编译通过（2026-10-18） |
| passed | `go vet ./...` | 无诊断 |
| passed | `go test ./...` | 全部包 ok、0 失败（含新增 kimi/zai 过滤、omp_speed、panel 速度块、export/remote 往返用例） |
| passed | `go test -race -count=1 ./internal/...` | 全部通过，无数据竞争 |
| passed | 真库冒烟：`go run ./out/scratch/ompspeed-smoke`（只读打开真实 `~/.omp/stats.db`） | kimi-code → k3-256k 25 次 p50 TTFT 3.7s 32.5 tok/s；zai → glm-5.3 603 次 48.8 tok/s（输出在会话记录，脚本已于验收后删除） |
| not run | 面板真机视觉验收 | 需 `bash Scripts/run-dev-app.sh` 退出已安装实例后人工确认，按 AGENTS 既有清单另行执行 |

修复过程中返工三处并复验：StatePlumbing 漏加 `runway`/`store` 测试导入、meter_export_test 的 agy 变量作用域、kimi 过滤测试期望与「百分比优先、同有百分比时 limits[] 优先」裁定不一致——均含于上述绿灯结果。

## 后续（非本链）

- 远程端展示 Token 速度依赖对端为新版本（旧对端 `speed` 缺省渲染为空，不报错）。
