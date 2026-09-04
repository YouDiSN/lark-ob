# Lark OB — Personal Inbox Assistant

一个本地运行的飞书/Lark 个人消息工作台。它以用户身份读取当前账号有权访问的单聊、群聊和历史消息，在 Web 页面中统一展示。后续将在同一框架上增加私人知识库检索、回复建议和人工确认发送。

> 本仓库只接受真实飞书数据，不内置假会话、Mock 消息或演示数据。当前环境无法稳定持有目标账号的用户授权，因此真实数据的端到端验证需要在有飞书权限的设备/环境中继续完成。

## 背景与目标

用户每天会同时收到多个单聊和群聊消息，需要逐个打开上下文、查找个人知识库并组织回复。期望最终形成一个命令行工具：

1. 启动后连接用户自己的飞书账号。
2. 近实时同步本人可见的全部单聊、群聊和聊天记录。
3. 启动一个只监听本机地址的 Web 工作台。
4. 在页面中查看会话列表、完整上下文和待回复消息。
5. 未来结合私人知识库生成符合场景的回复建议：问题给答案，日常调侃给自然、幽默的回应。
6. 未来支持编辑后以本人身份发送；如果企业不开放发送权限，则保留一键复制。

本阶段只实现第 1～4 项，不做知识库、AI 推荐和消息发送。

## 已确认的平台边界

- 不使用机器人会话，也不要求机器人加入群聊。
- 数据身份必须是 `user_access_token`，目标是用户本人所在的单聊和群聊。
- 官方开放平台当前没有“个人全部消息”的统一实时推送；`im.message.receive_v1` 是机器人事件，不能替代个人收件箱。
- 因此采用用户身份增量轮询：首次发现全部会话，之后周期性检查最近活跃会话，体验目标为 3～10 秒延迟。
- 以本人身份发送需要额外的 `im:message.send_as_user` 权限，且部分企业不允许；当前版本不申请、不发送。
- 用户 OAuth 可能受企业安全策略、异地登录或设备策略影响。优先在实际有权限、长期运行本工具的环境中完成授权。

## 当前实现

### 后端与命令行

- Go 单二进制命令行程序。
- 自动启动本地 HTTP 服务并打开浏览器。
- 默认监听 `127.0.0.1:8765`，不暴露到局域网。
- 优先复用官方 `lark-cli` 的用户登录，项目本身无需保存 App Secret。
- 也保留自建应用 OAuth 适配器，供没有 `lark-cli` 的环境使用。
- 首次同步全部可见单聊和群聊；后续每 8 秒检查最近活跃的 20 个会话。
- 每个会话先同步最近 50 条消息，页面顶部可继续分页加载更早记录。
- 文本、富文本、图片、文件、语音、视频、表情和撤回消息会转换为可展示内容。
- OAuth token 自动刷新逻辑已经搭好；自建应用模式的 token 暂存 SQLite，正式版本应迁移到系统 Keychain。

### Web 工作台

- React + TypeScript + Vite。
- 会话列表、会话搜索、单聊/群聊筛选。
- 聊天时间线、发送者、本人消息区分、会话信息面板。
- SSE 推送同步状态，数据入库后页面自动刷新。
- 响应式布局，可在窄屏使用。
- 当前严格只读，没有发送入口。
- 未授权时只显示真实授权状态和操作提示，不生成任何假消息。

### 本地数据

- SQLite WAL 模式。
- 表：`chats`、`messages`、`settings`。
- 消息以飞书 `message_id` 幂等写入。
- 会话按最后一条本地消息时间排序。
- 数据文件默认位于 `data/lark-ob.db`，已被 `.gitignore` 排除。

## 架构

```text
                     ┌──────────────────────┐
                     │ lark-cli 用户 OAuth │
                     │ 或自建应用 OAuth     │
                     └──────────┬───────────┘
                                │ user identity
                                ▼
┌──────────────┐      ┌──────────────────────┐
│ lark-ob CLI  │─────▶│ 会话/消息增量同步器  │
└──────┬───────┘      └──────────┬───────────┘
       │                          │ upsert
       │                    ┌─────▼─────┐
       │                    │  SQLite   │
       │                    └─────┬─────┘
       │ HTTP + SSE               │
       ▼                          ▼
┌──────────────────────────────────────────┐
│ React Web：会话列表 / 搜索 / 消息时间线 │
└──────────────────────────────────────────┘
```

关键策略：

- 冷启动：分页列出全部 `p2p,group` 会话。
- 首次消息：每个会话读取最新 50 条，优先让工作台尽快可用。
- 热轮询：按活跃时间只扫描最近 20 个会话，避免每轮遍历全部历史。
- 历史回溯：用户打开会话后按 `page_token` 向前加载，直到 `has_more=false`。
- 防漏：后续需要增加低频全量对账任务，目前尚未实现。

## 真实环境启动

### 1. 安装依赖

需要：

- Go 1.24+
- Node.js 20+
- 官方 `lark-cli` 最新版

```bash
npm install -g @larksuite/cli@latest
make build
```

### 2. 在有权限的环境完成用户授权

```bash
lark-cli auth login --scope "im:chat:read im:message:readonly im:message.group_msg:get_as_user im:message.p2p_msg:get_as_user"
```

授权页应只包含以下只读权限：

```text
im:chat:read
im:message:readonly
im:message.group_msg:get_as_user
im:message.p2p_msg:get_as_user
```

检查登录：

```bash
lark-cli auth status --verify
```

返回的用户身份可用后，再验证两条最小链路：

```bash
lark-cli im +chat-list --as user --types p2p,group --sort active_time --page-size 5 --format json
lark-cli im +chat-messages-list --as user --chat-id <真实 chat_id> --page-size 5 --no-reactions --format json
```

### 3. 启动工作台

```bash
./bin/lark-ob start
```

浏览器将打开：<http://127.0.0.1:8765>

其他参数：

```bash
./bin/lark-ob start --no-open
./bin/lark-ob start --listen 127.0.0.1:9000
./bin/lark-ob start --data /absolute/path/messages.db
```

环境变量：

```text
LARK_SYNC_CHAT_LIMIT=0    # 0 = 首次发现全部会话
LARK_POLL_INTERVAL=8s
```

## 自建应用模式（备用）

如果运行环境没有 `lark-cli`，可以复制 `.env.example`，填写企业自建应用凭证：

```bash
cp .env.example .env
```

应用后台需要配置回调地址：

```text
http://127.0.0.1:8765/auth/lark/callback
```

飞书中国区使用：

```text
LARK_API_BASE=https://open.feishu.cn
LARK_AUTH_BASE=https://accounts.feishu.cn
```

Lark 国际版使用：

```text
LARK_API_BASE=https://open.larksuite.com
LARK_AUTH_BASE=https://accounts.larksuite.com
```

不要提交 `.env`、token、SQLite 数据库或聊天附件。

## 开发命令

```bash
make ui       # 安装并构建前端
make build    # 构建前端和 Go 单二进制
make dev      # 启动真实数据开发服务
make test     # Go 测试 + 前端构建
```

项目结构：

```text
cmd/lark-ob/             CLI 入口
internal/lark/           自建应用 OAuth/OpenAPI 适配器
internal/larkcli/        官方 lark-cli 真实数据适配器
internal/model/          领域模型
internal/store/          SQLite 数据层
internal/syncer/         会话、消息和历史分页同步
internal/server/         HTTP API、SSE 和嵌入式前端
internal/server/ui/      React 工作台
```

主要本地 API：

```text
GET  /api/status
GET  /api/chats
GET  /api/chats/{id}/messages
GET  /api/chats/{id}/history
POST /api/chats/{id}/history
POST /api/sync
GET  /api/events
GET  /auth/lark
GET  /auth/lark/callback
```

## 当前验证状态

已经验证：

- 前端生产构建通过。
- Go 全量测试和二进制构建通过。
- SQLite 初始化、消息幂等写入和时间排序通过测试。
- 消息正文解析覆盖文本、富文本和媒体占位符。
- 官方 `lark-cli` 已确认包含 `+chat-list` 与 `+chat-messages-list` 所需命令。
- 本地用户授权已退出，仓库中没有 token、App Secret、聊天数据库或真实消息。

尚未验证：

- 目标企业账号真实 OAuth 成功后的 API 返回。
- 真实单聊/群聊数量和分页边界。
- 当前 `lark-cli` JSON 包装格式与 `internal/larkcli` 解析器的完整兼容性。
- 大量会话首次同步时的限流和耗时。
- 外部群、话题群、合并转发和复杂卡片的实际显示效果。

下一位 AI/开发者应首先在有权限的环境完成“真实环境启动”中的两条最小 CLI 验证。不要先扩展 AI 功能；必须先让真实会话和真实消息稳定显示在页面里。

## MVP 验收标准

- [ ] 使用本人飞书账号完成用户 OAuth，且不依赖机器人入群。
- [ ] 页面显示本人可见的真实单聊和群聊，不含任何假数据。
- [ ] 打开任意会话能看到最近 50 条真实消息。
- [ ] 可持续加载更早历史，直至没有更多消息。
- [ ] 新消息在目标轮询窗口内出现在页面，延迟不超过 10 秒。
- [ ] 本人消息与他人消息方向正确，群聊发送者姓名正确。
- [ ] 重启不会重复写入消息，授权失效时页面明确提示。
- [ ] API 限流时自动退避，不导致高频失败循环。
- [ ] 数据仅绑定本机地址，敏感文件不进入 Git。

## 后续路线

### P0：真实消息闭环

- 在有权限环境完成端到端验证并修正 JSON 解析。
- 增加同步并发上限、429 指数退避和低频全量对账。
- 补充话题回复、合并转发、图片和文件展示。
- 明确“未读”语义：使用官方未读状态，或改成工具自己的待处理状态，不能用假数字。

### P1：知识库与回复建议

- 支持 Markdown、TXT、PDF、DOCX 和飞书 Wiki。
- SQLite FTS5 + Embedding 混合检索。
- 每次生成使用最近 20～50 条聊天上下文和命中的知识片段。
- 分类问题、请求、通知、闲聊和调侃，生成推荐版、简洁版、轻松版。
- 在页面展示引用来源、置信度和可编辑草稿。
- 聊天内容与知识库均按不可信输入处理，防止提示词注入。

### P2：人工确认发送

- 默认一键复制。
- 企业允许时单独申请 `im:message.send_as_user`。
- 每次发送前展示收件会话、发送身份和最终文本。
- 添加幂等键、审计记录和明确的失败状态；永不自动发送。

## 安全原则

- 最小权限，当前只申请读取会话和消息所需权限。
- 仅监听回环地址，不默认开放远程访问。
- 不把真实聊天内容写入日志。
- 不提交 token、App Secret、数据库、附件或模型密钥。
- 正式版本将用户 token 放入系统 Keychain，而不是 SQLite。
- AI 阶段默认人工确认，任何消息内容都不能触发自动外部操作。
