# 网络边界

状态：已按仓库事实充实（2026-10-18，由 harness-init 创建）。本项目是本机桌面应用，无自建服务端；所有出站请求面向第三方。

## 对端与服务

| 类别 | 对端 | 说明 |
|---|---|---|
| Codex 订阅 / OAuth | OpenAI / Codex 官方端点 | OAuth 刷新、配额窗口、重置动态 |
| Grok 额度 | 官方 CLI chat-proxy `/v1/billing?format=credits` | 只用该端点，不读浏览器 Cookie、不从本地日志伪造额度 |
| Claude 订阅用量 | 非公开 OAuth usage 端点 | 界面写作「Claude Code 订阅用量」；`cedar_ember` 块附带 `cedar_ember=1` 返回 |
| 中转站点 | 用户填写的站点 | 余额（new-api 令牌额度 / OpenAI 兼容账单接口）与模型列表；站点不提供时明确说明，不估算 |
| 公开源 | 公开状态 / 价格源 | API 价格表（`openai-docs-` 版本） |
| 软件更新 | mygo 更新 feed / GitHub Releases | Ed25519 验签；appcast 由 `Scripts/release-assets sparkle-appcast` 生成 |

## 代理策略

- 支持跟随系统、HTTP、SOCKS5 及用户名 / 密码，覆盖应用发起的全部请求（Codex / Grok / Claude、CLI 操作、公开源、更新）。
- 代理密码分别存 macOS Keychain / Windows Credential Manager / Linux Secret Service；不修改系统代理。
- 修改仅在「保存代理设置」后生效；进行中的更新保持启动时配置。
- 「测试连接」只用当前草稿访问测试站点（默认 `https://github.com`，可自定义），任何非 407 / 5xx 响应视为连通；不发送账号凭据、不保存草稿。
- 错误配置、凭据不可用或自定义代理失败时不得自动直连；浏览器登录页与外部 CLI 沿用各自设置。

## 认证与状态权威

- Key 只放请求头；错误、日志与缓存结果中不得出现 Key 或响应正文。
- 官方配额 / 用量以服务端为权威（「官方统计（多端）」可能延迟或修订）；「本机日志（全部本机会话）」可能跨账号，二者不能视为包含关系或相减。
- Token 刷新权威规则：非当前托管账号只写账号库副本；当前账号同步官方文件与副本；不覆盖外部切号、登出或并发凭据变更。

## 失败处理与重试

- 更新检查：开启自动检查时启动后一次、运行期间每小时（失败后 1 小时重试）；默认不自动下载安装；开发包禁用更新器。
- 中转余额 / 模型列表只请求用户填写的站点，经应用代理策略、不直连回退。
- 详细隐私边界见 [SECURITY.md](SECURITY.md) 与 [AGENTS.md](../AGENTS.md)。

## 验证场景

- 真实网络行为需真机与隔离环境验收（`docs/v1-isolated-update-proxy.md`、`docs/testing/v1-update-proxy.example.yaml`）；不得改生产源或关闭 TLS 校验。
- 相关检查入口：`go test ./internal/netx/... ./internal/codex/... ./internal/grok/... ./internal/claude/...`（本次初始化未执行，`not run`）。
