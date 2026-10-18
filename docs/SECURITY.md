# 安全

状态：已按仓库事实充实（2026-10-18，由 harness-init 创建）。完整权威清单见 [AGENTS.md](../AGENTS.md)「隐私与安全」；本文是面向本仓库的落地视图。

## 信任边界

本应用以本机特权读取三家官方 CLI 的配置与凭据，并代用户写入其中有限字段——这是最大的信任边界：

- 不可信 / 外部输入：官方配置与凭据文件、会话 JSONL、中转站点响应、更新 appcast 与归档、SKILL.md frontmatter、`~/.agents/.skill-lock.json` 与 git 仓库配置。
- 官方文件写入白名单（全部要求备份 + 原子 / 事务写入）：
  - `~/.codex/auth.json`：仅 OAuth refresh 回写、用户主动切号、编辑当前 API 账号选「保存并应用」。
  - `~/.codex/config.toml`：仅切中转账号时的 `model_provider` 指针与 `[model_providers.quosparr]`，逐行编辑保留其他内容。
  - Claude：`<config>/settings.json` 的 `env.ANTHROPIC_*` 键、钥匙串 `claudeAiOauth`（切号时）、`.claude.json` 只替换 `oauthAccount` 一个键。
  - `~/.grok/auth.json`：切号只替换 OAuth / 兼容 legacy scope，保留 API Key 与未知 scope。
- 不删除 `~/.codex/sessions/**`、`~/.codex/archived_sessions/**`、`~/.claude/projects/**` 或全局状态文件；会话修复只处理 `session_index.jsonl` 且写入前备份。

## 密钥与凭据

- 多账号凭据唯一存放位置：`~/.quosparr/accounts/<id>/auth.json`（目录 `0700`、文件 `0600`、原子写入；Windows protected ACL 并关闭继承）。索引文件不得含 token。
- 代理密码：macOS Keychain / Windows Credential Manager / Linux Secret Service，不写普通设置。
- `/usr/bin/security` 使用绝对路径；密钥只经 stdin 传递，超上限（4095 字节）报错不退回 argv；stderr 进日志前只保留退出码和错误类别。
- 钥匙串写入后读回逐字节比对，不一致回滚。

## 输出与日志红线

- 日志、测试输出、README、issue 模板中不得出现 access token、refresh token、API key；错误信息面向用户但不包含本地密钥或完整认证 JSON。
- 仓库 `.gitignore` 已忽略 `*.key`、`certs/`、`*.pfx`、`*.p12` 等签名材料；签名私钥必须匹配配置公钥，只由发布环境提供。

## 权限与分发

- macOS 包当前 ad-hoc signed、未公证；更新归档 Ed25519 验签，`SUPublicEDKey` 保留；隔离验收须用真实 Sparkle 归档验签，不得改生产源或关 TLS 校验。
- macOS 保持 `LSUIElement=true`；新身份不自动继承系统权限（登录项、桌面组件需用户重新确认）。
- Skill 管理删除路径：移入自有回收站前重新校验路径、类型与所属根目录；彻底删除经系统回收站；符号链接只移除链接本身。

## 依赖与输入检查

- 依赖以 `go.mod` 锁定为准（purego、go-keyring 等用途受限）；默认不引入新第三方包。
- SQLite 运行库随包分发并经 `internal/sqliteruntime/verify.go` 校验，首次运行不联网下载。

## 验证

- Windows ACL、系统权限与真机安装验收不能以交叉编译或自动化测试替代（见 [v1-acceptance-checklist.md](v1-acceptance-checklist.md)）。
- 相关检查入口：`go test ./...`、`go vet ./...`、`Scripts/verify-v1-*.sh|py|ps1`（本次初始化未执行，`not run`）。
