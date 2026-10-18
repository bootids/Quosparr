# 移植规格：cursor / kimi / zai / agy

来源：../aimeter @ 0a796a0（src/{providers,credentials,config}.rs、research/ENDPOINTS.md、research/fixtures/、tests/integration.rs）。
本文是四个 provider 业务行为的唯一事实来源；与 ENDPOINTS.md 中互相冲突的外部约定一律以 aimeter 已实现行为为准。

## 共同行为

- HTTP：GET、`Accept: application/json`、`User-Agent: Quosparr/<version>`；不跟随重定向（302 视为 http 错误）。非 2xx 先于 JSON/业务解析失败：401/403 → auth；429 → rate_limited；其他 → http（保留状态码）。网络/超时 → network/timeout。JSON 解析或必需形状失败 → parse。
- 凭据缺失 = not_configured（不发起请求）；凭据存在但过期/无效 = auth 失败（不是零额度）。
- 脱敏：错误消息与输出中的密钥（及其百分号编码形式；cursor 另含 `user::token` 编码形式）替换为「前 4 字符 + 省略号」；zai 业务错误若回显 key 也必须被掩盖。测试断言原始密钥不出现在任何输出。
- 数值不裁剪、不修复上游不一致（如 zai usage−remaining ≠ limit 差一）；未知单位输出 unknown。

## cursor

**凭据**（按序）：
1. 显式 access_token（本仓库 M1 仅实现文件发现；显式 token 用于测试注入）。
2. 只读打开 SQLite（`SQLITE_OPEN_READONLY`）`state_db`，默认 macOS `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb`、其他 `~/.config/Cursor/User/globalStorage/state.vscdb`；查 `ItemTable` 键 `cursorAuth/accessToken`，value 为原始 JWT 字符串。不写回、不读 Keychain。
3. JWT 第二段 base64url 解码为 JSON：`sub` 取最后一个 `|` 之后一段作为 user（缺 sub → 凭据无效）；`exp` epoch 秒 → 毫秒过期时间。过期/无效在请求前失败。库/键/空 token 缺失 → not_configured。

**请求**：base `https://cursor.com`，`GET /api/usage-summary`；Cookie `WorkosCursorSessionToken=<percent-encode("user::token")>`，百分号编码为逐字节大写 HEX，ASCII 字母数字与 `-_.~` 不转义。

**归一化**：`billingCycleEnd` → reset（UTC）；`membershipType` → plan。每个存在池产出一条 limit：`unit=usd`、window=monthly、used/limit 由美分 ÷100；remaining=limit−used；percent 取 `totalPercentUsed`（数值时），否则 used/limit×100（limit≠0）。
- `individualUsage.plan` → id `plan`；若有任一 percent 字段再产出 `plan_auto`（autoPercentUsed）、`plan_api`（apiPercentUsed）（unit=percent）。
- `individualUsage.overall` → `overall`；`individualUsage.onDemand` → `on_demand`（`enabled == false` 时跳过）；`teamUsage.pooled` → `team_pooled`。
- 缺失/空池省略；无 balance。

fixtures：../aimeter/research/fixtures/cursor/success-personal.json、success-team.json（断言：plan $5 used/$15 remaining、on-demand 10%、overall 30%、team pooled $1000 limit）。

## kimi

**凭据**（按序）：
1. `KIMI_CODE_API_KEY`（显式配置优先于它）。
2. 凭据文件：`$KIMI_SHARE_DIR/credentials/kimi-code.json`，无环境变量时 `~/.kimi/credentials/kimi-code.json`；根字段 `access_token` 非空即可用；`expires_at`（epoch 秒）为过期。不用 refresh_token/expires_in/scope/token_type，不刷新。
不使用 KIMI_API_KEY、MOONSHOT_API_KEY。

**请求**：base `https://api.kimi.com`，`GET /coding/v1/usages`；`Authorization: Bearer <key-or-token>`。不切换 api.kimi.ai、不调网页 Billing RPC、不调 Moonshot 余额。

**归一化**：响应至少含 `usage`、`limits`、`usages` 之一。
- `usage` 对象 → id `7d`、unit unknown、时长 604800s；复制数值 `limit`/`remaining`/`used`/`resetTime`（数字字符串可接受）；缺 used 时 used=limit−remaining；可行则算 percent。
- `limits[]` 每项 → window=duration×timeUnit（仅 MINUTE/HOUR/DAY）；18000s → `5h`、604800s → `7d`、否则 `<秒>s`；id=window；`detail` 同上复制。
- `usages` 对象每个键 → id=键、unit percent；键含 `5h` → 5h/18000、`7d` → 7d/604800、`month` → monthly/无时长；否则 window=键无时长；used_percent=used_ratio×100。
- counts 与 ratio 同现时都输出，不去重不合并；id 冲突时后者加 `#2` 后缀。

fixtures：success-counts.json（counts：7d used 28；5h used 0=limit−remaining）、success-ratio-pools.json（limit_5h 62.5%、monthly）、unauthorized.json（HTTP401，body 不解析）。

## zai

**凭据/配置**：key 仅来自配置（本仓库：账号库当前账号 > `ZAI_API_KEY` > `Z_AI_API_KEY`）；无凭据文件/Keychain/过期字段。`region`：`cn` → `https://open.bigmodel.cn`，其余（默认 `intl`）→ `https://api.z.ai`。`auth_scheme`：`bearer` → `Authorization: Bearer <key>`，默认 raw → `Authorization: <key>`。

**请求**：`GET /api/monitor/usage/quota/limit`，头含 Accept、UA、`Content-Type: application/json`、上述 Authorization；无 body。
业务失败：2xx 且 `success != true` → code==401 为 auth，否则 api（带 msg 与 HTTP 状态）；缺 `data.limits` → parse 失败。业务失败不当作零额度。

**归一化**（`data.limits[]`）：
- 前缀/单位：`TOKENS_LIMIT`→`tokens`/tokens；`CREDIT_LIMIT`→`credits`/credits；`TIME_LIMIT`→`tools`/requests；未知→小写前缀/unknown。
- 窗口：整数截断 unit×number：unit3 → `<n>h`（n×3600s）；unit4 → `<n>d`（n×86400）；unit5&n1 → monthly（无时长）、n≠1 → `<n>mo`；unit6 → `7d`（604800，忽略 number）；其他 → `u<unit>n<number>` 无时长。id=`<prefix>_<window>`。
- `nextResetTime`：≥1e12 视作毫秒、否则秒的 epoch，或 RFC3339 字符串 → UTC。`currentValue`→used、`usage`→limit、`remaining`→remaining；percent 取 `percentage` 否则 used/limit。plan=`data.level`。字段独立复制，不修复不一致。

**model-usage（尽力而为）**：quota 成功后同 base 同头 GET `/api/monitor/usage/model-usage?startTime=…&endTime=…`；end=UTC now、start=end−7d；时间戳 naive `YYYY-MM-DD+HH:MM:SS`，`+`→`%2B`、`:`→`%3A`。失败忽略，不影响 quota。
成功时（`usage.data` 为对象）：ISO 起止、granularity、total_calls=totalUsage.totalModelCallCount、total_tokens=totalUsage.totalTokensUsage、models[]=modelSummaryList[]（modelName/totalTokens）、features[]=各 quota limit 的 usageDetails[]（modelCode→id、usage→used）。仅当 total 调用/token 存在或 models 非空才输出 usage；总量与分模型之和不相等时原样保留。

fixtures：success-tokens-and-tools.json、success-credits.json（12000−1438≠10561 原样）、no-coding-plan.json（success:false code:500 → api）、success-model-usage.json（总量 688,408,686 ≠ 分模型和 884,896,079，原样）。

## agy

**发现**：`agy` 于 PATH，之后 `~/.local/bin/agy`（显式 bin 优先，可为绝对/相对路径，须存在且可执行）。profile 目录默认 `~/.gemini/antigravity-cli`，必须是目录（只检测，不读取、不传子进程）。缺任一 → not_configured。不取 token、无 HTTP。

**命令**：`agy -p /usage --output-format json`（stdin 关闭/Discard）。成功判据：stdout 可解析 JSON 且 `command.data.groups` 为数组（此形状检查先于退出码；退出码非 0 但形状有效仍算成功）。否则 subprocess 错误（含状态码与 stdout/stderr 裁剪摘录，摘录须脱敏换行与控制字符）。

**归一化**（`command.data.groups[]`，每组有 `buckets[]` 时每桶一条）：
- window 缺省 `"unknown"`；id=桶 `id`，缺省 `<slug(group.name)>:<raw-window>`（slug=小写、ASCII 字母数字段以 `-` 连接）。
- window `"5h"` → 5h/18000；`"weekly"` 或 `"7d"` → 7d/604800；其他原样无时长。
- unit=percent；used_percent=(1−remaining_fraction)×100；reset_time 时间戳→UTC。不算 used/remaining/limit 数值。`response`、顶层 `usage`、描述忽略。

fixture：usage-success.json（两组四桶 → 四条 limit，weekly→7d）。注意勿用 HTTP 形状 fixtures（http-antigravity-summary-success.json 等是另一 schema）。

---

# Quosparr 目标契约（M0 定义，业务包遵守）

## internal/meter（共享类型，无业务）

```go
type Doer interface { Do(*http.Request) (*http.Response, error) }

// Status：一次拉取的结果分类（对齐 aimeter Failure 类别 + not_configured）。
type Status string
const (
    StatusOK Status = "ok"           // 有数据
    StatusNotConfigured Status = "notConfigured"
    StatusAuth Status = "auth"       // 401/403/过期/业务 401
    StatusRateLimited Status = "rateLimited"
    StatusHTTP Status = "http"       // 含 302 等非 2xx
    StatusNetwork Status = "network"
    StatusTimeout Status = "timeout"
    StatusAPI Status = "api"         // 2xx 业务失败（zai success!=true 且 code!=401）
    StatusParse Status = "parse"
    StatusSubprocess Status = "subprocess" // agy
)

// Limit：一个配额池的归一化行（金额池 USD 数值，比例池仅 Percent）。
type Limit struct {
    ID string; Title string; Unit string // usd|tokens|credits|requests|percent|unknown
    WindowMinutes int // 5h=300、7d=10080、monthly=43200（30d）、无=0
    Used, LimitValue, Remaining float64 // NaN 表示缺失
    Percent float64; HasPercent bool
    ResetsAt time.Time; HasReset bool
}

// Usage：zai 7 天模型用量汇总（其余 provider 为 nil）。
type Usage struct { Start, End time.Time; Granularity string; TotalCalls, TotalTokens float64; Models []UsageModel; Features []UsageFeature }

// State：一个 meter provider 的业务状态（可 JSON 序列化做快照，无凭据）。
type State struct {
    Status Status; Message string // 已脱敏错误摘要
    Plan string; Limits []Limit; Usage *Usage
    FetchedAt time.Time
}
```

- `meter.GetJSON(doer, url, headers, timeout)`：共享 GET + 状态分类 + body 上限读取；不跟随重定向由调用方 http.Client 保证（netx client 默认策略）。
- `meter.Redact(msg, secret)` 及编码形式掩盖；业务包在返回前对 State/错误统一脱敏。
- 金额显示（cursor usd）：Limit.Used/LimitValue 为美元数值，UI 层格式化 `$x.xx`。

## 业务包 API 形状

- `cursor.Detect(home string) (Credential, bool, error)` + `cursor.Fetch(ctx, doer, cred, now) (meter.State, error)`；内部 `cursor.Token` 解析 JWT（user、过期）。
- `kimi.Detect(env, home)`（KIMI_CODE_API_KEY → credentials 文件）+ `kimi.Fetch(ctx, doer, secret, now)`。
- `zai`：`store.go`（账号库：`~/.quosparr/accounts/zai-api-<id>/auth.json` `{"apiKey"}` 0600 原子写、`zai-index.json` 行含 id/label/region/authScheme/keyHash/updatedAt，不含 key；CurrentID）+ `zai.Fetch(ctx, doer, key, region, scheme, now)`（quota + 尽力 model-usage）。
- `agy.Detect(env path)`（bin+profileDir）+ `agy.Fetch(ctx, bin, now)`（exec + 形状判据 + 归一化）。
- 所有 Fetch 接受 `context.Context` 与超时；不打印、不写缓存（缓存/调度归 runway/app）。

## runway seam（M2 骨架，先于并行派发定稿）

```go
type MeterAvailability int // MeterNotConfigured / MeterLoading / MeterReady / MeterError
type MeterState struct {
    Availability MeterAvailability
    Plan string
    Meters []Meter        // 由业务 Limits 映射：有 percent 的池 → 标准条；金额池附 ValueText
    Detail []DetailLine   // zai usage 汇总、无比例池的明细行
    FetchedAt time.Time
}
type MeterProvider interface {
    State() MeterState
    Refresh() error      // 网络拉取（各自内部串行锁）
    RefreshLocal() error // 凭据重检测（无网络）
    SetOnChange(func())
}
// Options.Meters map[store.Provider]MeterProvider；Snapshot.Meters、StatusInputs.Meters 同键。
```
