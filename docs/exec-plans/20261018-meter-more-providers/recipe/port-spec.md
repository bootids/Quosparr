# 移植规格：minimax / deepseek / pixellab / elevenlabs

来源：../aimeter @ 0a796a0（src/{providers,credentials,config,model}.rs、research/fixtures/{minimax,deepseek,pixellab,elevenlabs}/、tests/integration.rs）。
本文是四个 provider 业务行为的唯一事实来源；与 aimeter 其他文档冲突时以 aimeter 已实现行为为准。

## 共同行为（与 20261018-meter-providers 的 port-spec 相同，重述要点）

- HTTP：GET、`Accept: application/json`、`User-Agent: Quosparr/<version>`（经 `meter.GetJSON`）；不跟随重定向（302 视为 http 错误）。非 2xx 先于 JSON/业务解析：401/403 → auth；429 → rate_limited；其他 → http。网络/超时 → network/timeout；JSON 解析或必需形状失败 → parse。
- 凭据缺失 = not_configured（不发起请求）；凭据存在但无效 = auth 失败（不是零额度）。
- 脱敏：错误消息与 State 中不得出现原始 key（`meter.Redact`）；测试断言 key 不出现在任何输出。
- 数值字段接受 JSON number 或数字字符串（aimeter `number()`：as_f64 或字符串 parse，且必须 finite）；数字字符串如 deepseek 的 `"110.00"`。
- timestamp 解析（aimeter `timestamp()`）：数值 ≥1e12 视作毫秒、否则秒；或 RFC3339 字符串 → UTC。

## 显式配置（Quosparr 约定，对齐 cursor/kimi 模式）

四家均为单账号显式配置，存于 `~/.quosparr/accounts/<provider>/auth.json`（目录 0700、文件 0600、原子写，Windows protected ACL）：

- deepseek：`{"apiKey": "..."}`
- pixellab：`{"apiKey": "..."}`
- elevenlabs：`{"apiKey": "..."}`
- minimax：`{"apiKey": "...", "region": "cn"|"intl"}`（region 可省略，省略 = 未设置）

API 形状对齐 internal/kimi/config.go：`ConfigPath(root)`、`LoadConfig(root) (cfg, ok, err)`（缺失/空 = 未配置，不报错）、`SaveConfig`、`RemoveConfig`。minimax 的 LoadConfig 返回 `(apiKey, region string, ok bool, err error)`；region 仅接受 `"cn"`/`"intl"`，其他值按未设置处理（写入库时由 SaveConfig 规范化）。

## 凭据优先级（对齐 aimeter config.rs 常规 env 回退 + 文件发现）

- **deepseek**：显式配置 > `DEEPSEEK_API_KEY`。无文件发现。
- **minimax**：显式配置 > `MINIMAX_CODING_API_KEY` > `MINIMAX_API_KEY` > 文件 `~/.config/minimax/config.json`（根字段 `api_key` 非空；`base_url` 含 `minimaxi.com` → region 提示 `cn`、含 `minimax.io` → 提示 `intl`）。region 解析：显式配置 region 已设置则用之，否则文件提示，否则默认 `cn`。
- **pixellab**：显式配置 > `PIXELLAB_API_KEY` > 文件 `~/.config/pixellab/token`（纯文本，首尾空白去除）。
- **elevenlabs**：显式配置 > `ELEVENLABS_API_KEY` > 文件 `~/.config/elevenlabs/api-key`（纯文本，首尾空白去除）。

文件只读，缺失 = 该来源无凭据；文件存在但不可读/不可解码 = parse 错误 State（消息不含路径外内容）。aimeter 不设 XDG_CONFIG_HOME，路径固定为 `$HOME/.config/...`，Quosparr 保持一致。

## deepseek

**请求**：base `https://api.deepseek.com`，`GET /user/balance`，`Authorization: Bearer <key>`。

**归一化**：`balance_infos[]` 每项 → `meter.Balance{Currency, Total, Granted, ToppedUp}`：currency 取 `currency` 文本；total/granted/topped_up 取 `total_balance`/`granted_balance`/`topped_up_balance`（数字字符串可接受，缺失 = NaN）。无 limit、无 plan。`balance_infos` 缺失或非数组 → parse 失败。

**余额展示**：`meter.State.Balances` → app 层映射为 Detail 行（见 PLAN M0 契约）。

fixtures：success-multicurrency.json（CNY 110.00 = 赠 10.00 + 充 100.00；USD 12.50）。

## minimax

**请求**：region `intl` → base `https://api.minimax.io`，否则（默认 `cn`）→ `https://api.minimaxi.com`；`GET /v1/token_plan/remains`，头 `Authorization: Bearer <key>`、`Content-Type: application/json`。

**业务失败**：2xx 且 `base_resp.status_code != 0` → `status_code == 1004` 为 auth，否则 api（消息取 `base_resp.status_msg`，HTTP 状态保留）。业务失败不当作零额度。

**归一化**：`model_remains[]` 每项（`model_name` 缺失 → parse 失败）：
- 窗口 `5h`：percent 取 `current_interval_remaining_percent`，起止 `start_time`/`end_time`；
- 窗口 `7d`：percent 取 `current_weekly_remaining_percent`，起止 `weekly_start_time`/`weekly_end_time`；
- percent 缺失则跳过该窗口（不报错）；
- 每条：`Limit{ID: "<model_name>:<5h|7d>", Unit: "percent", WindowMinutes: 300/10080, Percent: 100 − remaining_pct, HasPercent: true}`；`ResetsAt` = 窗口 end 时间戳（timestamp 规则）；WindowMinutes 由 end−start 秒数换算（18000s→300、604800s→10080，其他取整分钟）；used/limit/remaining 均缺失（NaN）。
- 计数字段（`current_interval_total_count` 等）与 status 字段忽略。

fixtures：success-token-plan.json（general:5h used_percent 10、general:7d 22、5h 时长 18000s、used 为空）、key-rejected-http-200.json（status_code 1004 → auth，HTTP 200）、credentials-structure.json（文件发现形状）。

## pixellab

**请求**：base `https://api.pixellab.ai`，`GET /v2/balance`，`Authorization: Bearer <key>`。

**归一化**（响应须为对象，否则 parse）：
- `credits.usd` 为数值 → `Balance{Currency: text(credits.type)（如 "usd"）, Total: usd}`；granted/topped_up 缺失。
- `subscription` 为对象 → 一条 `Limit{ID: "generations", Title: "Generations", Unit: "generations", WindowMinutes: 0（无窗口时长）}`：remaining=`subscription.generations`、limit=`subscription.total`、used=total−remaining、percent=used/limit×100；`ResetsAt` 缺失。
- plan = `subscription.plan` 文本。
- `balances` 与 `limits` 均为空 → parse 失败。
- 其他字段（type/status）忽略。

fixtures：balance-success.json（usd 0.0；generations remaining 1922、total 2000、used 78、percent 3.9；plan "Tier 1: Pixel Apprentice"）。

## elevenlabs

**请求**：base `https://api.elevenlabs.io`，`GET /v1/user/subscription`，头 `xi-api-key: <key>`（**不是** Bearer）。

**归一化**（响应须为对象，否则 parse）：
- characters 池：`Limit{ID: "characters", Title: "Characters", Unit: "characters", WindowMinutes: 0}`；window 标签取 `character_refresh_period`：`"monthly_period"` 或缺失 → monthly（无时长，展示层用标题），其他原样；used=`character_count`、limit=`character_limit`、remaining=limit−used、percent=used/limit×100；`ResetsAt`=`next_character_count_reset_unix`（timestamp 规则）。
- 静态池（used 与 limit 键**均**为 null 时跳过，否则照建）：
  - `voice_slots`（unit `voices`）：used=`voice_slots_used`、limit=`voice_limit`；
  - `professional_voice_slots`（unit `voices`）：used=`professional_voice_slots_used`、limit=`professional_voice_limit`；
  - `voice_add_edits`（unit `edits`）：used=`voice_add_edit_counter`、limit=`max_voice_add_edits`；
  - 每条 remaining=limit−used、percent=used/limit×100、无窗口无重置。
- plan = `tier` 文本。Title 分别为 "Voice slots" / "Professional voices" / "Voice edits"。

fixtures：subscription-success.json（characters 23243/131000 rem 107757、reset 2026-11-07T09:27:44Z、voice_slots 4/30、prof 0?→used=professional_voice_slots_used=0 limit=1、voice_add_edits 4/95、tier creator）。

---

# Quosparr 目标契约（M0 定义）

## internal/meter 增量

```go
// Balance is one prepaid balance row (deepseek, pixellab). Numeric fields
// use NaN for "absent" like Limit.
type Balance struct {
    Currency                string
    Total, Granted, ToppedUp float64
}

// State 增加字段：
type State struct {
    // ...现有字段不变...
    Balances []Balance // 余额行（deepseek/pixellab；其余 provider 为 nil）
}
```

- app 层 `meterStateFromRaw`：MeterReady 且 `Balances` 非空时，在 `Detail` 前插入余额行：`Title = lc.Text(KeyMeterBalance) + " (" + Currency + ")"`（Currency 大写原样，aimeter deepseek 返回 "CNY"/"USD"、pixellab 返回 "usd"——原样展示，不改大小写）；`Value = meterAmount(Total)`，Granted/ToppedUp 非 NaN 时以 ` · ` 追加 `lc.Text(KeyMeterBalanceGranted) + " " + meterAmount(g)` 与 `lc.Text(KeyMeterBalanceToppedUp) + " " + meterAmount(t)`。
- 余额不进 Meters（无比例条）、不进速率采样（meter/history 只采样 Limits）。

## store.Provider 增量

`ProviderDeepSeek = "deepseek"`、`ProviderMiniMax = "minimax"`、`ProviderPixelLab = "pixellab"`、`ProviderElevenLabs = "elevenlabs"`；`Providers()` 顺序：codex, claude, grok, cursor, kimi, zai, deepseek, agy, minimax, pixellab, elevenlabs（保持 aimeter 相对顺序：deepseek 在 zai 后、minimax/pixellab/elevenlabs 在 agy 后）。`MeterProviders()` 同序追加四家。选择规范化（preferences/floating 的 canonicalProviders）按 `Providers()` 迭代，自动生效。

## 业务包 API 形状

- `deepseek.Fetch(ctx, doer, key, now) (meter.State)`；`deepseek.EnvKey() string`；config 三函数。
- `minimax.Fetch(ctx, doer, key, region, now)`；`minimax.EnvKey()`；`minimax.LoadCredential`（文件发现含 region 提示）；config 三函数（带 region）。
- `pixellab.Fetch(ctx, doer, key, now)`；`pixellab.EnvKey()`；`pixellab.LoadCredential`（token 文件）；config 三函数。
- `elevenlabs.Fetch(ctx, doer, key, now)`；`elevenlabs.EnvKey()`；`elevenlabs.LoadCredential`（api-key 文件）；config 三函数。
- 所有 Fetch 走 `meter.GetJSON` + `meter.Redact`；不打印、不写缓存。

## l10n 契约 key（七语言：en、zh-Hans、zh-Hant、ko、ja、ru、fr）

tables.go：`providerDeepSeek`=DeepSeek、`providerMiniMax`=MiniMax、`providerPixelLab`=PixelLab、`providerElevenLabs`=ElevenLabs、`meterBalance`、`meterBalanceGranted`、`meterBalanceToppedUp`。

extra_l10n.go（设置页签文案，key 名由集成子任务定义并保持七语言）：四家各 intro/来源行/标签/占位/必填提示，minimax 另有 region 标签与两选项。

## 明确非目标

- 不做四家的多账号库、token 图表、API 等价成本、会话、Skill 关联、oh-my-pi Token 速度（本机无记录）。
- 不写任何官方文件；`~/.config/{minimax,pixellab,elevenlabs}` 只读。
- Swift Widget 快照类型不变（providers/meters 块 schema 不动，余额走既有 Detail 行）；Widget 选择 UI 不扩展。
- aimeter 的 mimo provider 不在本次范围。
