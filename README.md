# Lark OB — Personal Inbox Assistant

一个本地运行的飞书/Lark 个人消息工作台。它以用户身份读取当前账号有权访问的单聊、群聊和历史消息，在 Web 页面中统一展示，支持把用户明确指定的飞书 Docx/Wiki 页面导入本地知识库，并使用 Eino Agent 提炼跨会话人物记忆、群组记忆和回复建议。

> 本仓库只接受真实飞书数据，不内置假会话、Mock 消息或演示数据。当前开发机已经通过个人 `lark-cli` 授权完成真实消息链路验证；授权和本地数据库均留在仓库之外。

## 快速入口

- 第一次了解项目：阅读[背景与目标](#背景与目标)、[架构](#架构)和[模块边界](#模块边界)。
- 在 Linux 服务器部署：直接阅读[Docker 部署（推荐）](#docker-部署推荐)。
- 修改代码：阅读[开发与修复指南](#开发与修复指南)。
- 页面打不开、授权失败或 Agent 异常：阅读[常见故障排查](#常见故障排查)。
- 处理真实数据前：阅读[数据安全与备份](#数据安全与备份)和[安全原则](#安全原则)。

已配置好 `.env.docker` 和 `lark-cli` 授权的机器，可以用下面四条命令启动：

```bash
docker-compose build
docker-compose up -d
docker-compose ps
curl --fail http://127.0.0.1:8765/api/status
```

正常情况下容器状态应为 `healthy`，页面地址为 <http://127.0.0.1:8765>。

## 背景与目标

用户每天会同时收到多个单聊和群聊消息，需要逐个打开上下文、查找个人知识库并组织回复。期望最终形成一个命令行工具：

1. 启动后连接用户自己的飞书账号。
2. 近实时同步本人可见的全部单聊、群聊和聊天记录。
3. 启动一个只监听本机地址的 Web 工作台。
4. 在页面中查看会话列表、完整上下文和待回复消息。
5. 结合私人知识库和按人物/群组组织的记忆生成符合场景的回复建议：问题给答案，日常调侃给自然、幽默的回应。
6. 未来支持编辑后以本人身份发送；如果企业不开放发送权限，则保留一键复制。

当前已经实现第 1～5 项；Agent 只生成候选回复，没有发送工具。消息发送仍留待单独授权和人工确认阶段。

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
- 每个会话先同步最近 50 条消息以快速展示；首次初始化随后自动分页拉取最近 30 天的全部消息，消息列表仍可继续手动加载更早记录。
- 会话列表按最新活动排序并显示末条消息时间；聊天时间线按时间正序展示，打开会话默认定位到最底部的最新消息，手动滚动时不会强制跳回。
- 每个会话都可独立主动刷新，不需要等待全局轮询；左侧以本工具的“最后查看水位”区分新消息和 `@我`，不会冒充飞书客户端的官方未读状态。
- 文本、富文本、图片、文件、语音、视频、表情和撤回消息会转换为可展示内容。
- 独立 `knowledge` 领域模块负责文档读取、切分、索引和检索；飞书来源、SQLite 仓储及 HTTP 路由通过接口组合，不耦合到消息同步器。
- 独立 `memory` 领域模块存储带消息证据的记忆声明。群聊中用户 A 的发言归属用户 A 的稳定 `sender_id`，同时保留原始 `chat_id`，因此可跨会话聚合又不丢失群组上下文。
- 记忆仓库页面展示人物、群组及单聊上下文的总量、来源和有效评分，群组严格按真实 `group` 会话过滤，支持搜索、类型筛选及单条删除；列表首屏只加载 100 条，可按需继续加载。
- 记忆保留模型原始置信度，并依据最后证据时间动态衰减有效评分；默认半衰期 90 天，重复出现的新证据会刷新其时效。
- 独立 `initializer` 模块负责有界、可恢复的首次初始化：默认以最近 30 天为时间窗同步全部会话消息，再并发生成带证据的人物/群组记忆。
- 独立 `impression` 模块存储人物互动印象、我的基础表达风格和人物关系风格。画像是带证据、可版本化的派生快照，不与事实记忆混存。
- 独立 `maintenance` 模块负责每日画像任务：默认北京时间 03:00 同步新消息，只重新提炼有变化的会话与人物；失败时保留上一版可用画像且不推进检查点。
- 独立 `agent` 模块基于 CloudWeGo Eino：结构化流程负责记忆提取和知识摘要；回复 `ChatModelAgent` 使用服务端预加载的消息、记忆与知识，只需一次模型调用。
- 当前模型为 OpenAI-compatible 的 `grok-4.5`；模型 Provider 通过接口注入，可以不改业务模块地替换或在测试中使用假模型。
- OAuth token 自动刷新逻辑已经搭好；自建应用模式的 token 暂存 SQLite，正式版本应迁移到系统 Keychain。

### Web 工作台

- React + TypeScript + Vite。
- 会话列表、会话搜索、单聊/群聊筛选。
- 聊天时间线、发送者、本人消息区分、会话信息面板。
- SSE 推送同步状态，数据入库后页面自动刷新。
- 响应式布局，可在窄屏使用。
- 当前严格只读，没有发送入口。
- 未授权时只显示真实授权状态和操作提示，不生成任何假消息。
- 独立知识库页面可导入单篇飞书 Docx/Wiki 链接、选择全局或会话范围、重新同步、移除并验证搜索结果。
- 会话右侧的基础印象与当前记忆均默认折叠；展开基础印象可查看人物标签、沟通建议、关系风格、证据覆盖和更新时间。群聊中根据当前 `replyTarget` 选择人物。
- 底部可生成 2～3 条可复制的候选回复及依据。群聊中存在尚未回复的 `@我` 消息时，Agent 会把它锁定为 `replyTarget`，后续刷出的群消息只作为上下文；推荐回复会直接读取预生成的基础印象与风格，不在请求现场重新总结。
- Agent 面板显示首次消息/记忆初始化进度；完成后当前会话会自动显示生成的记忆。
- 知识库来源可显式生成 AI 摘要和关键事实。

### 本地数据

- SQLite WAL 模式。
- 表：`chats`、`messages`、`settings`、`knowledge_sources`、`knowledge_chunks`、`knowledge_summaries`、`memory_claims`、`person_impressions`、`style_profiles` 及其版本表，以及知识和记忆检索使用的 FTS5 索引。记忆索引覆盖人物名、来源会话名和正文，中文子串等 FTS 无法命中的查询会自动回退到模糊匹配。
- 消息以飞书 `message_id` 幂等写入。
- 会话按最后一条本地消息时间排序。
- 数据文件默认位于 `data/lark-ob.db`，已被 `.gitignore` 排除。

## 架构

```text
┌──────────────────────┐       ┌──────────────────────┐
│ Lark 会话/消息 API   │       │ 飞书 Docx/Wiki 页面 │
└──────────┬───────────┘       └──────────┬───────────┘
           ▼                              ▼
┌──────────────────────┐       ┌──────────────────────┐
│ conversation syncer  │       │ knowledge source    │
└──────────┬───────────┘       └──────────┬───────────┘
           │                              │
           ▼                              ▼
┌─────────────────────────────────────────────────────┐
│ SQLite：消息 / 人物与群组记忆 / 知识来源与分块     │
└──────────────────────────┬──────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────┐
│ Eino：记忆提取 / 知识总结 / 单次模型回复 Agent     │
└──────────────────────────┬──────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────┐
│ HTTP modules + React：消息工作台 / 独立知识库页面  │
└─────────────────────────────────────────────────────┘
```

关键策略：

- 冷启动：分页列出全部 `p2p,group` 会话。
- 首次消息：每个会话先读取最新 50 条以便快速展示，再以默认 30 天时间窗完成有界历史回溯。
- 首次记忆：30 天消息同步完成后，按会话自动提取人物和群组记忆；每个会话完成后记录检查点，重启只重试未完成项。
- 首次画像：首次记忆完成后立即跨会话生成人物互动印象、我的基础风格和样本充足的人物关系风格。
- 每日画像：默认每天北京时间 03:00 增量同步，只为检查点之后有新消息的会话和人物更新；写入新版本成功后才原子替换当前快照。
- 热轮询：按活跃时间只扫描最近 20 个会话，避免每轮遍历全部历史。
- 历史回溯：用户打开会话后按 `page_token` 向前加载，直到 `has_more=false`。
- 防漏：后续需要增加低频全量对账任务，目前尚未实现。

### 一条新消息的处理链路

```text
lark-cli 用户授权
        │
        ▼
syncer 增量轮询 ──► SQLite 幂等写入 ──► SSE 通知页面刷新
        │                    │
        │                    ├──► 更新会话排序、未读和 @我状态
        │                    └──► enqueue 独立 chat context
        ▼
每日维护任务 ──► 自动记忆 / 人物印象 / 自我与关系风格
                             │
用户点击推荐回复             ▼
        └──► 最近消息 + chat context + 有效记忆 + 知识库 + 风格
                                      │
                                      ▼
                              Eino / Grok 候选回复
```

推荐回复阶段不会重新扫描完整历史。较重的会话理解、记忆和人物印象在初始化、新消息处理或每日任务中提前生成，因此每个会话都有独立、持久化的 Agent 上下文。

### 模块边界

| 模块 | 职责 | 不负责 |
| --- | --- | --- |
| `internal/larkcli` | 调用官方 CLI、解析用户身份数据、资料和媒体 | 业务记忆、页面状态 |
| `internal/syncer` | 会话发现、增量轮询、历史分页、消息入库 | Agent 推理、知识库 |
| `internal/store` | SQLite schema、迁移、查询和事务 | 业务编排、模型调用 |
| `internal/initializer` | 最近 30 天首次历史和记忆初始化 | 每日增量维护 |
| `internal/maintenance` | 每日同步、记忆、印象和风格维护 | HTTP 请求处理 |
| `internal/memory` | 自动/手动记忆、有效期、衰减、检索优先级 | 人物表达风格 |
| `internal/impression` | 人物基础印象、个人及关系表达风格 | 确定性事实存储 |
| `internal/knowledge` | 用户指定文档导入、切分、索引和检索 | 自动扫描全部云文档 |
| `internal/agent` | Eino 编排、chat context、摘要和回复候选 | 数据所有权、自动发送 |
| `internal/server` | HTTP API、SSE、嵌入 React 静态资源 | 飞书协议细节 |
| `internal/server/ui` | 收件箱、聊天、知识库、记忆和推荐回复 UI | 直接访问飞书或数据库 |

增加功能时应把逻辑放进所属模块，通过接口组合；不要把同步、数据库 SQL、模型 Prompt 和 HTTP handler 堆在同一个文件中。

## Docker 部署（推荐）

当前 Linux 部署推荐使用 Docker Compose。镜像内包含编译后的 Go 服务、React 静态资源、Node.js 和固定版本的 `lark-cli`；聊天数据库、图片缓存和用户授权留在宿主机，不会打进镜像。

### 1. 准备运行环境

需要 Docker 和 Docker Compose v2。首次启动前创建 Docker 环境文件：

```bash
cp .env.docker.example .env.docker
chmod 600 .env.docker
```

在 `.env.docker` 中填写 `AGENT_API_KEY`。真实密钥不要写入镜像或提交到 Git。当前实现不附带任何额外计费归因 Header。

Compose 默认复用以下宿主机目录：

```text
~/.local/share/lark-ob     SQLite 数据库
~/.cache/lark-ob           消息图片缓存
~/.lark-cli                lark-cli 配置、日志和锁
~/.local/share/lark-cli    lark-cli 加密凭据和 master key
```

授权目录需要以读写方式挂载，因为 `lark-cli` 会自动刷新用户 token。镜像复用 Node 官方镜像的非 root 用户，以 UID/GID `1000:1000` 运行；部署前应确认宿主机挂载目录属于该用户。

### 2. 构建并启动

```bash
docker-compose build
docker-compose up -d
docker-compose ps
docker-compose logs -f lark-ob
```

也可以使用 Makefile 中的等价命令：

```bash
make docker-build
make docker-up
make docker-logs
```

服务使用 Linux host 网络并仍然只监听 `127.0.0.1:8765`。这样既不会暴露到公网，又可以访问同样只监听宿主机 localhost 的 Agent 代理 `127.0.0.1:18766`。

如果机器仍在运行旧的 systemd 服务，切换前先停止它，避免占用同一个端口：

```bash
systemctl --user stop lark-ob-local.service
docker-compose up -d
```

浏览器访问：<http://127.0.0.1:8765>。远程开发机继续使用 SSH 本地映射，不要把容器端口绑定到公网地址：

```bash
ssh -N -L 63544:127.0.0.1:8765 <server>
```

### 3. 新机器授权

已有授权目录会直接复用，不需要重新登录。全新机器可以通过同一个镜像执行授权，凭据会写入上面的挂载目录：

```bash
docker-compose run --rm --entrypoint lark-cli lark-ob auth login \
  --scope "im:chat:read im:message:readonly im:message.group_msg:get_as_user im:message.p2p_msg:get_as_user"
```

检查授权：

```bash
docker-compose run --rm --entrypoint lark-cli lark-ob auth status --verify
```

### 4. 升级、备份与回滚

SQLite 使用 WAL 模式，备份前应先停止容器，再复制整个数据目录：

```bash
docker-compose down
cp -a ~/.local/share/lark-ob ~/.local/share/lark-ob.backup
docker-compose build --pull
docker-compose up -d
```

镜像固定使用 `lark-cli` 1.0.93，避免每次构建引入未经验证的新版本。需要升级时显式指定并重新构建：

```bash
LARK_CLI_VERSION=<version> docker-compose build --pull
docker-compose up -d
```

Docker 版本出现问题时，可停止容器并恢复原来的 systemd 服务：

```bash
docker-compose down
systemctl --user start lark-ob-local.service
```

### 5. 容器设计

- `Dockerfile` 使用 Node 和 Go 两个构建阶段，最终镜像只保留运行时、编译后的 Go 二进制和 `lark-cli`。
- React 产物通过 Go `embed` 打进二进制，不需要单独部署 Nginx。
- 容器以 UID/GID `1000:1000` 的非 root 用户运行，根文件系统只读。
- `/tmp` 使用限制大小的临时文件系统；SQLite、授权和图片缓存使用宿主机持久化目录。
- `network_mode: host` 只用于当前 Linux 单机方案，让容器访问宿主机 localhost 上的模型代理。应用自身仍只监听 `127.0.0.1:8765`。
- 健康检查只验证本地 HTTP 服务可响应，不把短暂同步状态误判为容器崩溃。

## 原生环境启动

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
MEMORY_LOOKBACK_DAYS=30   # 首次消息与记忆初始化范围
MEMORY_INITIALIZATION_WORKERS=2
MEMORY_DECAY_HALF_LIFE_DAYS=90 # 记忆有效评分的默认半衰期
PROFILE_DAILY_TIME=03:00       # 每日画像任务的本地时间
PROFILE_TIMEZONE=Asia/Shanghai
```

Agent 环境变量（密钥只通过运行环境注入，不要写入仓库）：

```text
AGENT_API_KEY=<OpenAI-compatible API key>
AGENT_BASE_URL=http://127.0.0.1:18766/v1
AGENT_MODEL=grok-4.5
AGENT_MEMORY_REASONING_EFFORT=medium
AGENT_REPLY_REASONING_EFFORT=low
AGENT_MEMORY_TIMEOUT=5m
AGENT_REPLY_TIMEOUT=1m
```

`18766` 是宿主机上仅监听 localhost 的模型代理端口；不应为了使用 Agent 把工作台或代理端口暴露到公网。

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
internal/initializer/    首次时间窗消息同步与记忆初始化编排
internal/knowledge/      知识领域服务、分块与来源适配器
internal/memory/         人物/群组记忆领域服务
internal/agent/          Eino 模型 Provider、结构化流程与回复 Agent
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
POST /api/chats/{id}/refresh
POST /api/chats/{id}/viewed
POST /api/sync
GET  /api/knowledge/sources
POST /api/knowledge/sources
POST /api/knowledge/sources/{id}/sync
DELETE /api/knowledge/sources/{id}
GET  /api/knowledge/search?q=...
GET  /api/agent/status
GET  /api/initialization/status
GET  /api/chats/{id}/memories
POST /api/chats/{id}/memories/extract
GET  /api/memories?subjectType=...&sourceChatType=...&q=...&limit=...&offset=...
GET  /api/memories/subjects
POST /api/memories
PUT  /api/memories/{id}
DELETE /api/memories/{id}
POST /api/chats/{id}/recommendation
GET  /api/knowledge/sources/{id}/summary
POST /api/knowledge/sources/{id}/summary
GET  /api/events
GET  /auth/lark
GET  /auth/lark/callback
```

## 开发与修复指南

### 推荐开发流程

1. 先确认问题属于同步、存储、记忆、印象、知识库、Agent、HTTP 还是 UI，不要跨模块顺手重构。
2. 用本地 API 或容器日志复现问题，记录会话 ID、消息 ID、时间戳和错误阶段；不要把真实消息正文复制进公开 Issue。
3. 优先为对应模块补一个最小回归测试，再修改实现。
4. 后端修改运行 `go test ./...`；前端修改运行 `npm run build`；跨层修改两项都要执行。
5. Docker 相关修改必须重新构建镜像，并验证容器健康、授权、数据库和 Agent 状态。
6. 只提交源代码和空配置模板，不提交本地数据库、授权目录、图片缓存或真实密钥。

最小验证集合：

```bash
go test ./...
cd internal/server/ui && npm run build && cd ../../..
docker-compose build
docker-compose up -d
docker-compose ps
curl --fail http://127.0.0.1:8765/api/status
curl --fail http://127.0.0.1:8765/api/agent/status
```

### 后端开发

- HTTP handler 只负责参数校验、状态码和调用领域服务。
- 业务规则放在对应领域模块，SQL 只放在 `internal/store`。
- 所有 schema 变化都通过幂等迁移完成，必须兼容已有 SQLite，不能要求用户删除数据库。
- 消息排序必须同时使用 `created_at`、飞书 `message_position` 和稳定 ID 作为最终兜底。
- 自动提炼只能替换 `source_type=agent` 的记忆，不能覆盖用户维护的手动记忆。
- 消息盒子会话不参与自动记忆、印象和风格学习。
- Agent Prompt 中的消息、文档和记忆都视为不可信资料，不能赋予其工具调用或系统指令权限。

### 前端开发

```bash
cd internal/server/ui
npm ci
npm run dev     # Vite 开发服务器
npm run build   # TypeScript 检查 + 生产构建
```

- API 类型和页面状态按 `inbox`、`agent`、`knowledge`、`memory` 分目录维护。
- 聊天时间线是独立滚动容器；修改布局时必须验证默认定位底部和向上加载历史。
- SSE 更新不得无条件重置用户滚动位置、筛选条件或正在编辑的推荐回复。
- 新增可交互状态时必须覆盖 loading、empty、error、disabled 四种情况。
- 生产页面来自嵌入式 `ui/dist`；修改前端后若只重启旧容器，页面不会变化，必须重新 build 镜像。

### 数据库与测试数据

开发测试使用 `t.TempDir()` 创建独立数据库，不要直接清空真实运行库。需要检查真实库时优先使用只读 API；确需迁移或修复前，先停止服务并备份整个数据目录。

不要手工删除 SQLite 的 `-wal` 或 `-shm` 文件。它们必须与主数据库一起由 SQLite 正常关闭或一并备份。

## 常见故障排查

先执行下面的基础检查：

```bash
docker-compose ps
docker-compose logs --tail 200 lark-ob
curl --fail http://127.0.0.1:8765/api/status
curl --fail http://127.0.0.1:8765/api/agent/status
```

| 现象 | 常见原因 | 检查和处理 |
| --- | --- | --- |
| 页面打不开 | 容器未启动、8765 被占用或 SSH 隧道断开 | 检查 `docker-compose ps`；确认没有同时运行 systemd 版本；重新建立 `ssh -L 63544:127.0.0.1:8765` |
| 容器反复重启 | 环境文件缺失、挂载目录权限不对、数据库无法打开 | 查看容器日志；确认四个持久化目录属于 UID 1000；不要删除数据库重试 |
| 页面还是旧版本 | 只重启了旧镜像或浏览器缓存未更新 | 执行 `docker-compose build && docker-compose up -d --force-recreate`，再强制刷新浏览器 |
| `lark-cli: unknown flag: --as` | CLI 版本过旧或容器调用了错误路径 | 容器内执行 `lark-cli version`；当前验证版本为 1.0.93；重新构建镜像并确认 `LARK_CLI_PATH` |
| 显示未授权或 token 无效 | 授权目录未挂载、master key 缺失或 token 过期 | 执行容器版 `lark-cli auth status --verify`；必须同时挂载 `.lark-cli` 和 `.local/share/lark-cli`，必要时重新登录 |
| Agent 未启用 | `AGENT_API_KEY` 未注入或模型代理不可达 | 查看 `/api/agent/status`；确认 host 网络和 `127.0.0.1:18766` 代理正在运行；不要把代理开放到公网 |
| 推荐回复很慢 | 模型代理排队、上下文尚未预生成或推理参数过高 | 检查 Agent 日志和 chat context 更新时间；回复使用 low reasoning，记忆任务可保留 medium |
| 推荐内容违背现实条件 | 人物地点等事实没有进入目录资料或有效记忆 | 在记忆仓库补充带有效期的手动事实/硬约束；不要让模型从旧聊天猜测当前位置 |
| 新消息没有出现 | 增量轮询还未扫描该会话或授权调用失败 | 点击会话主动刷新，检查 `/api/status` 和同步日志；不要仅靠全局 SSE 判断飞书是否有新消息 |
| 消息顺序与飞书不同 | 时间戳相同但 position 缺失，或旧记录尚未回填 | 检查日志中的顺序回填错误；排序修复必须保留 position 和稳定 ID 兜底 |
| 图片只显示 `img_...` | 图片缓存不可写、授权失效或消息资源权限不足 | 检查缓存挂载权限和授权状态；通过图片 API 复现，避免把原始图片写进仓库 |
| SQLite locked | systemd 与 Docker 同时打开同一数据库，或异常工具持有写锁 | 保证只运行一个实例；优雅停止另一个进程后再重启容器；不要直接删除 WAL |
| 记忆没有自动更新 | 初始化未完成、每日任务未到时间或会话位于消息盒子 | 查看 `/api/initialization/status`、`/api/profile-maintenance/status`；消息盒子被设计为不提炼记忆 |
| “消息盒子”与飞书不一致 | 飞书内建消息盒子成员关系没有公开 OpenAPI | 当前仅支持本地手动状态，不要把“免打扰”误当作消息盒子 |

### 修复后的交付标准

每次修复至少说明：问题根因、改动模块、是否涉及数据库迁移、验证命令、运行态结果和回滚方法。涉及真实飞书接口时还要说明使用的用户/机器人身份和权限范围，但不能输出 token。

## 数据安全与备份

- 运行库默认是 `~/.local/share/lark-ob/lark-ob.db`，图片在 `~/.cache/lark-ob`。
- 飞书 CLI 授权分别位于 `~/.lark-cli` 和 `~/.local/share/lark-cli`；二者都属于敏感数据。
- 备份 SQLite 前先执行 `docker-compose down`，然后复制整个 `~/.local/share/lark-ob` 目录。
- 恢复时也要先停止服务，并保留一份当前目录作为可回滚副本。
- 不要把运行目录挂载到公网共享盘，不要把数据库或授权文件上传到 Issue、对象存储或代码仓库。
- `.env.docker` 被 Git 忽略；仓库只保留不含密钥的 `.env.docker.example`。

## 当前验证状态

已经验证：

- 前端生产构建通过。
- Go 全量测试和二进制构建通过。
- SQLite 初始化、消息幂等写入和时间排序通过测试。
- 消息正文解析覆盖文本、富文本和媒体占位符。
- 官方 `lark-cli` 用户授权、`+chat-list` 与 `+chat-messages-list` 已在当前开发机跑通。
- 真实会话与消息可持续同步，运行数据库位于用户数据目录；仓库中没有 token、App Secret、聊天数据库或真实消息。
- 知识库迁移、导入/检索接口契约、文档切分、FTS5 更新替换均通过自动化测试。
- 新二进制已在 `127.0.0.1:8765` 运行验证，知识源列表与空检索接口返回正常。
- Docker 多阶段镜像与 Compose 已完成真实构建和重启验证；容器以非 root、只读根文件系统运行，健康检查通过。
- Docker 容器已复用宿主机加密授权目录，`lark-cli` 用户身份和 token 校验通过。
- Eino + `grok-4.5` 已通过 dev 模型代理完成真实调用：记忆提取成功，回复 Agent 单次模型调用返回多条建议与证据。
- 人物记忆跨会话聚合、群组来源保留、无效模型主体/证据过滤以及知识摘要持久化均有自动化测试。

尚未验证：

- 大量真实单聊/群聊的完整分页边界。
- 用一篇用户指定的真实飞书 Docx/Wiki 页面完成导入、AI 摘要与块级引用验证。
- 大量会话首次同步时的限流和耗时。
- 外部群、话题群、合并转发和复杂卡片的实际显示效果。

下一步应先用用户明确指定的真实飞书文档验证知识导入和摘要；不要扫描或自动导入用户未指定的云文档。

## MVP 验收标准

- [x] 使用本人飞书账号完成用户 OAuth，且不依赖机器人入群。
- [x] 页面显示本人可见的真实单聊和群聊，不含任何假数据。
- [x] 打开会话能看到真实消息，并按最新消息优先展示。
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

### P1：知识库、记忆与回复建议

- 当前支持显式导入单篇飞书 Docx/Wiki 页面；继续增加 Wiki 子树递归同步及 Markdown、TXT、PDF、DOCX。
- 当前使用 SQLite FTS5；继续增加 Embedding 混合检索和重排。
- 建立独立记忆模块：同一人物在单聊、群聊中的陈述统一归属人物，同时保留原会话与消息证据。
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
