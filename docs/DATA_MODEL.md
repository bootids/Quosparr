# 数据模型与持久化

状态：已按仓库事实充实（2026-10-18，由 harness-init 创建）；细节以 [AGENTS.md](../AGENTS.md) 与源码为准。

## 存储系统与归属

| 数据 | 位置 | 说明 |
|---|---|---|
| 账号托管凭据 | `~/.quosparr/accounts/<id>/auth.json` | 目录 `0700`、文件 `0600`、原子写入；唯一允许存多账号凭据的位置（codex- / grok- / claude- / claude-api- 前缀） |
| 账号索引 | `~/.quosparr/accounts/index.json`、`grok-index.json`、`claude-index.json` | 只含非密钥元数据（Base URL、Key 哈希提示、模型映射、余额缓存），不得包含 token |
| 偏好 | macOS UserDefaults `com.quosparr.app`；Windows / Linux 自有目录 | 旧域 `com.github.codex-runway`、`dev.licoy.codex-runway` 仅一次性导入；代理密码在系统凭据库，不进设置 |
| 派生数据 | `~/.quosparr/`（如 `quota-estimate-history.json`、`widget-snapshot.json`） | 快照 schema 1、`0600`、主程序唯一写入者；推算历史只存派生 Credits 与占用率 |
| 备份 | `~/.quosparr/backups/` | 官方 `config.toml` / `settings.json` / 会话索引写入前备份，各保留最近 10 份 |
| Skill 回收站 | `~/.quosparr/skills-trash/<id>/` | 目录私有，`meta.json` `0600` 只含非密钥元数据与 `item/` 原件 |

SQLite 运行库由 `internal/sqliteruntime` 加载并校验（`verify.go`；Windows 按架构选择 `library_windows_*.go`，安装包随包携带相应架构运行库，首次运行不联网下载）。`internal/usage/testdata/swift_compat/swift_index.sqlite3` 为冻结的 Swift 版本兼容 fixture，只读保留。具体运行时 schema 未在本文档登记；需要时以 `internal/sqliteruntime` / `internal/usage` 源码为准，不凭本文推测。

## 只读外部数据（不在 Quosparr 归属内）

- 官方 CLI 目录：`~/.codex`（`CODEX_HOME`）、`~/.grok`（`GROK_HOME`）、`~/.claude`（`CLAUDE_CONFIG_DIR`）。
- 官方文件的写入白名单与最小字段见 [AGENTS.md](../AGENTS.md)「隐私与安全」；`config.toml` / `settings.json` 只做逐行 / 逐键编辑并保留其他内容。
- `internal/migration` 负责旧身份数据迁移：暂存事务、中断恢复、完成标记防止已删除旧账号重新导入；新目录已有数据时整组优先补缺。

## 不变量与兼容

- 索引文件（`index.json`、`grok-index.json`、`claude-index.json`）永不含 token / Key / 完整认证 JSON。
- 会话修复只处理 `session_index.jsonl`，写入前必须备份；不删 `sessions/**`、`archived_sessions/**` 或全局状态文件。
- 迁移不修改官方 Codex / Grok 登录文件；引用的代理凭据在系统凭据库内复制。
- Widget 快照 schema 变更需与 `Sources/QuosparrWidget` 的快照类型副本同步。

## 验证与恢复

- 恢复手段：`~/.quosparr/backups/` 的分项备份；迁移可中断后重跑恢复。
- 相关检查入口：`go test ./internal/store/... ./internal/migration/...`（本次初始化未执行，`not run`）；Windows ACL、真机安装验收见 [v1-acceptance-checklist.md](v1-acceptance-checklist.md)。
