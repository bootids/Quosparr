# 本地保存系统

状态：已按仓库事实充实（2026-10-18，由 harness-init 创建）。本项目非游戏；「玩家设置 vs 进度」对应「应用偏好 vs 派生数据 / 账号库」。

## 保存归属与格式

- 所有 Quosparr 自有数据统一在 `~/.quosparr`；默认只保存非密钥派生数据，应用缓存不写入 `~/.codex` 等官方目录。
- 凭据文件（`accounts/<id>/auth.json`）原子写入，目录 `0700`、文件 `0600`；Windows 使用仅当前用户的 protected ACL 并关闭权限继承（不能以 chmod 代替实际权限检查）。
- 偏好：macOS `com.quosparr.app`（UserDefaults 兼容例外）；Windows / Linux 保存在自有目录。偏好键 `showsSkills`、`fontFamily`、对象 `floatingWindow` 仅 Go 版写入，默认省略。
- 带版本的持久格式：Widget 快照 `widget-snapshot.json`（schema 1、`0600`）；`quota-estimate-history.json`（推算历史，只含派生数据）。

## 写入耐久与损坏处理

- 官方 `auth.json` / `config.toml` / `settings.json` 写入：备份到 `~/.quosparr/backups`（各保留最近 10 份）→ 同一事务逐行 / 逐键编辑 → 失败整体回滚（中转账号切换）。
- macOS 钥匙串写入经 `security -i` 标准输入（单行上限 4095 字节，超限拒绝不退回 argv），写后读回逐字节比对，不一致回滚原条目。
- 会话修复只改 `session_index.jsonl`，写入前必须备份；会话目录读取失败时在备份、替换索引前终止修复。
- 迁移为暂存事务，支持中断恢复；完成标记防止重复导入。

## 迁移兼容

- 旧身份 → 新身份（`com.quosparr.app`、`~/.quosparr`）：复制旧应用数据并导入缺失偏好，保留原目录与已有 Quosparr 数据；新目录已存在时补缺文件与账号 ID，新账号元数据、当前选择和数据库整组优先。
- 旧名称账号包仍可导入；新导出格式 `quosparr-accounts`。
- 已安装 Swift 同身份版本经 appcast 升级到 Go 版本；不提供 Swift 回滚包。

## 测试要求

- 保存 / 加载、写入中断、版本不兼容、重置类测试必须用一次性 fixture（如 `internal/*/testdata/`、临时 HOME + `QUOSPARR_PREFS_DOMAIN=<测试域>`），绝不覆盖用户真实保存数据。
- `testdata/swift_*` 等 Swift 版本冻结 fixture 只读保留、不再重新生成。
- 相关检查入口：`go test ./internal/store/... ./internal/migration/...`（本次初始化未执行，`not run`）。

## 不可信文件边界

- 读入的官方配置、会话 JSONL、SKILL.md frontmatter 均视为外部输入；Skill 正文只在用户展开时读取前 256 KB，不跟随指向凭据文件的链接。
- 不存在云同步；系统各凭据库（Keychain / Credential Manager / Secret Service）是唯一的跨进程共享存储。
