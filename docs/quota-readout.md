# Quota 只读接口

在设置中开启“导出状态 JSON”，保持应用运行，并将要查询的供应商保留在面板供应商选择中。
导出仍是显式选择，默认关闭；查询本身不会启用导出或额外轮询隐藏的供应商。

```sh
quosparr --quota
quosparr --quota --max-age 300 codex kimi zai
/Applications/Quosparr.app/Contents/MacOS/Quosparr --quota codex
quosparr --quota --status-file /path/to/status.json codex
```

参数放在供应商名称之前。命令不启动 UI、不拿单实例锁、不读取偏好、账号库或官方认证文件，
不联网、不刷新 token、不写文件。需要最新值时在应用中刷新后再查询，不支持 `--refresh`。
开发构建可使用 `go build -o /tmp/quosparr .`。

## 数据结构

CLI 输出独立版本化 JSON；同一份数据保存在 `~/.quosparr/status.json` 的 `quotaReport`。
直接读文件的调用方也必须在读取时检查新鲜度，而不是把 `generated_at` 当成每家的抓取时间。

```json
{
  "schema_version": 1,
  "generated_at": "2026-10-20T12:00:00Z",
  "providers": {
    "kimi": {
      "status": "ok",
      "fetched_at": "2026-10-20T11:59:30Z",
      "age_seconds": 30,
      "limits": [{
        "id": "5h",
        "scope": "standard",
        "unit": "percent",
        "used_percent": 20.25,
        "duration_seconds": 18000,
        "resets_at": "2026-10-20T14:30:00Z"
      }]
    }
  }
}
```

- `providers` 按 ID 索引：`codex`、`grok`、`claude`、`cursor`、`kimi`、`zai`、`agy`、`deepseek`、`minimax`、`pixellab`、`elevenlabs`。不指定名称时包含全部，含未配置状态；不支持 MiMo。
- 只导出当前账号，不包含其他托管账号。Codex/Claude API 账号为 `unsupported`，不把 API 余额伪装成订阅额度。
- `status` 为 `ok`、`stale`、`error`、`not_configured`、`loading` 或 `unsupported`。只有 `ok` 可作额度决策依据。
- `fetched_at` 是最后成功抓取时间；CLI 在查询时计算 `age_seconds`。`generated_at` 是源报告生成时间，不是查询时间。时间带时区，文件通常为 UTC 秒精度。
- `--max-age` 为正整数秒，默认 900。抓取时间或导出时间超过上限、时间戳缺失、时钟超前超过 1 分钟、任一窗口重置时间已到，均标记 `stale`。旧数值保留供诊断，不推断已补满额度。
- `error` 仅为固定代码：`fetch_failed`、`missing_timestamp`、`clock_skew`、`max_age_exceeded`、`reset_elapsed`；不含上游正文或认证错误原文。最后抓取失败但仍有成功值时为 `stale`，无成功值时为 `error`。
- `limits` 的 `used_percent` 为已用百分比；`used`、`limit`、`remaining` 是可选原始数值，按 `unit` 解释。未知值省略，不补成零。八家配额 provider 保留业务数值精度；Grok/Claude 为现有面板的整数百分比。
- `duration_seconds` 未知时为零或省略，不从标题猜周期。`resets_at` 未知时省略。窗口没有重置时间不代表无限额度。
- `scope` 为 `standard` 或 `model_specific`；后者不是所有模型共用的额度。`group` 标识模型组，agy 的 Gemini 和 Claude/GPT 分开处理。
- `id` 为供应商池 ID；Codex 为 `primary`、`secondary`、`additional_N`，Grok/Claude 为源窗口顺序 `window_N`。这些位置 ID 不保证跨账号不变；`title` 是展示文本，可能本地化，不能用于推断周期。
- `balances` 为独立余额数组：`currency`、`total`、可选 `granted`、`topped_up`，不换算币种或订阅百分比。DeepSeek 和 PixelLab 支持。

状态文件保留已有 UI 镜像、成本与最近会话结构；CLI 只输出 `quotaReport`，不输出这些其他块。
报告不包含 token、API Key、邮箱、账号标识或 API 站点。导出文件为应用自有目录内原子写入的非密钥派生数据，
沿用现有 `0644` 权限边界。关闭导出不删除以前的快照，旧文件会随数据年龄变为 stale。

## 退出码与边界

- `0`：成功输出报告，即使某个供应商为 stale/error；调用方逐项检查状态。
- `1`：文件缺失、不可读、损坏或报告版本不支持；stdout 为空，stderr 为固定提示。
- `2`：参数错误或未知供应商；stdout 为空。

旧版本 status 文件没有 `quotaReport` 时明确报错，不回退解析旧 UI 字段。
远程文件可通过 `--status-file` 显式读取；远程查看模式不另外生成本地 quota 镜像，
也不会因为查询创建本地采集器。CLI 只读取指定文件，不判断其来源机器。

主应用目前使用 ad-hoc 签名的 macOS 分发；本接口的自动化测试不证明真实账号服务可用性、
签名包安装升级或 Windows/Linux 真机行为。
