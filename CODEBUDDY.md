# CODEBUDDY.md 本文件为在此仓库中使用 CodeBuddy 工作提供指引。

## 项目概览

WorkChat IM 即时通讯系统，由三个子项目组成：

- `service/` — Go 服务端（Gin + MySQL + Redis + 阿里云 OSS + WebSocket 网关）
- `desktop_client/` — 桌面端（Electron + Vue 3 + Vite），UI 已完成，网络层已接入真实后端接口
- `admin/` — 管理后台（Vue 3 + Vite），用于用户/群组管理、数据看板

后端已按文档驱动的分阶段开发推进到「阶段五」（认证、消息长连接、社交群组、富媒体/管理后台均已落地）。详细架构见 `docs/IM系统架构设计.md`，落地计划见 `docs/执行计划.md`。

## 常用命令

### service (Go)

```bash
cd service
make docker-up        # 启动本地 MySQL + Redis（docker compose）
make migrate          # 执行 SQL 迁移（cmd/migrate）
make run              # 启动服务（cmd/server，HTTP + WS 共用 :8080）
make build            # 编译二进制到 bin/
make lint             # 静态检查（go vet ./...）
make test             # 运行全部测试（go test ./...）
```

单测运行（包内测试用内存 mock，无需数据库）：

```bash
cd service
go test ./internal/message/...
go test ./internal/auth/...
go test -run TestSendAndHistory ./internal/message/
```

服务启动后健康检查：`curl http://127.0.0.1:8080/healthz`（需先 `make docker-up` + `make migrate`，并按需修改 `configs/config.yaml` 中的 MySQL/Redis/OSS/JWT 密钥）。

### desktop_client (Electron + Vue)

```bash
cd desktop_client
npm run dev           # 同时启动 Vite (5173) + Electron
npm run build:renderer # 仅构建渲染进程
npm run build         # 构建 + electron-builder 打包
```

### admin (Vue)

```bash
cd admin
npm run dev           # Vite dev，默认端口 5174
npm run build         # 构建
```

默认管理员账号：`admin` / `admin123`（服务端首次启动自动 seed）。

## 架构

### 服务端分层（service/internal）

Go 后端是「模块化单体」，核心分层：**router（server）→ 领域服务（auth/message/social/admin/file）→ 数据访问层（store/mysql、redis、oss）→ pkg 公共工具**。

- `internal/server/router.go`：集中装配路由。`Dependencies` 结构体持有所有服务、`WSGateway`、MySQL、Redis、JWT。所有业务路由挂 `/api/v1`（`/auth/*` 无鉴权，其余挂 `middleware.JWT`）；WS 直接注册 `GET /ws`（鉴权在首帧内完成）；管理后台挂 `/api/admin`（`/login` 外均需 `middleware.AdminAuth`）。全局中间件：`Logger` / `Recovery` / `CORS`。HTTP 与 WS 共用同一 `http.Server`。
- 中间件（`internal/server/middleware/`）：`JWT` 解析 `Authorization: Bearer`，要求 `claims.Type == "access"` 并把 `uid` 注入 `CtxUIDKey`；`AdminAuth` 要求 `claims.IsAdmin == true`。
- 每个领域包按 **Service（业务） + Handler（HTTP 层）** 组织；Service 通过定义最小 `Store` 接口消费 `store/mysql` 的 DAO，便于用内存 mock 做单测（见 `internal/message/service_test.go` 的 `mockStore` 模式，不依赖真实 DB）。测试文件用 `TestMain` 调 `log.Init` 初始化日志。
- `store/mysql`：连接池 + 迁移（`splitStatements` 解析 migrations/*.sql）+ 各 DAO。**消息分表**：4 张表 `messages_0~3`，路由 `conv_id % 4`（`msgTable`），同会话恒落同表保证 `seq` 单表有序；`NextSeq = MAX(seq)+1`。`SearchMessages` 遍历全部分表 `content LIKE`。
- `store/redis`：封装 go-redis。Redis 承载在线状态（`presence:{uid}` → 节点）、跨节点路由、二维码状态。当前是单机部署，路由仍走 Redis。
- `store/oss`：阿里云 OSS 预签名。

### WebSocket 网关（internal/gateway）

连接管理核心，四个文件各司其职：

- `gateway.go`：帧协议（`Frame` = `{ver, type, seq, body}`），类型枚举 `auth / heartbeat / msg / msg.push / ack / read / read.sync / typing / presence / notify`。
- `server.go`：升级连接 → 首帧必须 `auth`（解析 access token 得 uid）→ `hub.Add` → 读循环。分发 `msg`/`read`/`ack`/`heartbeat`。
- `hub.go`：连接注册中心。`Add`/`Remove` 维护 `uid → 多连接` 并写 Redis `presence:{uid}`。**消息投递** `Push`：本节点在线则 `deliverLocal` 直发，否则读 Redis 的 `uid→节点` 映射并向目标节点频道 `im:route:{node}` 发布（Redis Pub/Sub），本节点 `subscribe` 订阅自身频道接收跨节点消息。`Broadcast` 用于群聊多路分发。
- `adapter.go`：把 `message.Service` 适配为网关的 `MessageHandler`。`HandleMsg` 调 `svc.Send`（返回 `isNew`）后返回 `(pushFrame, echoFrame, recipients)`：新消息才向接收方推送（`pushFrame`），幂等重发（同一 `msg_id` 已存在）时两端均不回推，避免重复消息；`echoFrame` 回显给发送方替换乐观渲染。`HandleRead` 返回 `(read.sync, peerUIDs)`，对单聊把已读广播给对端；群聊不广播。`HandleAck` 把送达回执转发给消息发送方。

### 消息发送链路（核心）

发送方 WS `msg` 帧 → `adapter.HandleMsg` → `message.Service.Send`：
1. `GetOrCreateConversation`（单聊按 `ownerUID,targetID` 建会话，**单聊双方必须共享同一 conv_id**；`Send` 内通过 `EnsureConversationID` 确保接收方会话复用发送方 conv_id，否则双方历史/已读各自独立）
2. `MessageExists` 按全局消息 ID 幂等去重
3. `CreateMessage`（`NextSeq` 生成会话内 seq）落库成功后才推送（保证不丢）
4. 更新会话最后消息（单聊更新双方）；**对接收方更新 `LastSyncedSeq`**（未读数 = LastSyncedSeq - readSeq 的依赖，只更新接收方不更新发送方，避免发送方误显未读）
5. `publish(convID, dto)` → 网关 `Push` 接收方（含跨节点路由）

**防重复**：`Send` 返回 `(dto, isNew, err)`。幂等重发（同一 `msg_id` 已存在）时 `isNew=false`，`adapter.HandleMsg` 返回 `pushFrame=nil, echoFrame=nil` 完全静默，不重复推送接收方、不重复回显发送方。桌面端 `onWsMessage` 再按消息 id 去重兜底（覆盖弱网重发/离线补发）。

**帧约束**：`msg` 帧 body 含 `msg_id / conv_id / target_id / conv_type(1单聊|2群聊) / type / content`。心跳为应用层帧，但服务端 `readLoop` 当前未真正实现超时踢线（仅回 pong）。

### 单聊已读未读（核心）

**模型**：会话级「已读游标」而非逐消息标记。服务端 `message_reads` 表按 `(uid, conv_id)` 存每个用户各自的 `last_read_seq`，已读/未读通过游标与消息 `seq` 比较判断。**未读数 = 会话最新 seq - 自己已读 seq**（`ListConversations`）。

**已读闭环（单聊，群聊不实现）**：
1. 接收方打开会话/滚到底部看到新消息 → 桌面端 `wsClient.sendRead(convId, maxSeq)` 发 `read` 帧。
2. 服务端 `adapter.HandleRead`：`MarkRead` 落库 `last_read_seq`（只前向推进），并经 `GetConversationPeer` 找到对端，把 `read.sync` 帧 `hub.Push` 给**消息发送方**（`server.handleRead` 不回显读方自己）。
3. 发送方收到 `read.sync` → `MainWindow.onWsRead` 把该会话「自己发出且 `seq <= 对方已读游标`」的消息标记为「已读」。
4. **会话切换/刷新恢复**：`ListConversations` 返回 `peer_read_seq`（单聊对端已读游标，`ConversationDTO` 新增字段）；桌面端 `readCursorMap` 缓存游标，加载历史后 `restoreReadState` 按游标恢复已读标记。

**「滚到底部才算已读」（实时收消息场景）**：桌面端 `onWsMessage` 收到对方消息时仅当 `isNearBottom()` 为 true 才发已读回执；否则置 `pendingReadAck=true` 并显示「回到底部」按钮，待用户滚动到底部后补发（`attachScrollWatcher`）。

**切换会话即已读**：`switchConversation` 打开会话后**直接 `sendReadReceipt(target)`**（切换会话=查看最新消息），不依赖 `isNearBottom()`——`scrollToBottom` 用 `nextTick` 异步设滚动位置，同步调 `isNearBottom` 会读到切换前的残留 scrollTop，消息较多时误判「不在底部」而漏发已读（对端收不到已读提示）。

**乐观消息替换（含敏感词）**：`onWsMessage` 对发送方自己发出的消息按**消息 ID** 匹配乐观消息（乐观 id=`tmp-<msgId>` ↔ 服务端回显的 `msg.id`，因 `Send` 用 `req.MsgID` 作消息 ID）。不能用内容匹配——敏感词被服务端过滤替换后回显内容与原始输入不同，内容匹配会失败导致「一条消息显示两条」。

**已读状态文案**（`MainWindow` 消息 `readAt` 字段）：`发送中…` / `''`（发送成功未读）/ `已读` / `发送失败`。

### 桌面端网络层（desktop_client/src/api）

纯前端 UI 已完成，网络层为真实实现：

- `http.js`：fetch 封装，baseURL `http://127.0.0.1:8080/api/v1`，统一响应 `{code, message, data}`（code===0 成功）；401 自动用 refresh token 刷新后重试一次（并发去重 `refreshPromise`）。API 路径与后端路由一一对应。
- `ws.js`：WebSocket 客户端，`ws://127.0.0.1:8080/ws`。建连 `onopen` 首帧 `auth`、15s 心跳（连续 3 次未响应断线重连、3s 重连）。`sendMessage`/`sendRead`/`sendAck` 对应帧。**发送可靠性**：`sendMessage` 后进 `pendingAcks` 待确认队列，5s 未收 ack 最多重发 3 次（配合服务端按 `msg_id` 幂等去重，保证不重复落库/推送）。收到 `msg.push` 自动回 `ack`，收到 `read.sync` 触发 `read` 事件。
- `token.js`：token 存取（经 Electron `safeStorage` 加密）。
- `auth.js` / `message.js` / `social.js` / `qrcode.js`：领域 API，对应用户端后端接口。`social.js` 的 friends/groups 带共享缓存并**持久化到 localStorage**：`list(false)` 仅读缓存（空则返回 `[]`），`list(true)` 请求后端刷新缓存并写回 localStorage；`isFriendCacheLoaded()`/`isGroupCacheLoaded()` 判断缓存是否已初始化。消息页 `buildContactMap` 在缓存**未初始化**时才 `list(true)`，避免刷新浏览器后内存缓存丢失导致消息页重复请求 `/friends`、`/groups`；增删好友/群会 `clearCache` 清空缓存（含 localStorage）。

桌面端 Electron IPC：`electron/main.js` + `preload.js` 经 `contextBridge` 暴露 `window.electronAPI`（版本、目录选择、`secureStorage` 加密存储）。`docs/桌面端本地存储方案.md` 规划了后续用 SQLite + better-sqlite3 做本地缓存（当前未落地，本地库是缓存而非事实源，服务端为准）。

**群详情按需加载**：`MainWindow.loadLiveGroupMembers` 打开群聊会话时才请求 `/groups/:gid`（展示群资料面板），并按 gUid 缓存成员（`groupMembersCache`），同一群反复打开复用缓存；邀请成员后失效对应缓存。

### 端到端测试脚本（desktop_client/scripts）

后端需已启动（`:8080`）且前端 dev server 运行（`:5173`）。`cdp_util.mjs` 是 Edge headless + CDP 驱动库（Node 24 内置 `WebSocket`），其余为场景测试：

```bash
cd desktop_client
node scripts/test_read_receipt.mjs   # 协议层已读闭环（Node WS 模拟 A/B）
node scripts/test_read_ui2.mjs       # 双浏览器已读闭环（Edge headless）
node scripts/test_read_switch.mjs    # 切换会话已读（回归）
node scripts/test_sensitive.mjs      # 敏感词不产生重复消息
node scripts/test_unread_refresh.mjs # 刷新后未读气泡保留
node scripts/test_group_detail_req.mjs # 群详情按需请求
```

这些脚本用真实服务端+前端，测试会注册临时用户、启动 Edge headless，结束后自动退出。

### 管理后台（admin）

Vue3 + Vite，`src/api/http.js` + `admin.js` 对接 `/api/admin`（login 外均需 admin JWT）。功能：登录、数据看板、用户列表/禁用、群列表/解散。

## 关键约定

- 统一响应：`{code, message, data}`，`code===0` 成功；业务错误用 `internal/pkg/err`（`BadRequest`/`WrapInternal`/`Unavailable` 等），经 `resp.OK`/`resp.Fail` 输出。HTTP 状态码：`resp.Fail` 输出统一 500 JSON（panic 走 `Recovery` → `{code:50000}`）。
- 业务主键：用户 `uid`、群 `g_uid`（10 位随机数字，前端展示）；各表内部主键 `id` 用雪花 ID（`internal/pkg/snowflake`）。
- 消息类型：`1 text / 2 image / 3 file / 4 voice / 5 video / 6 system`；会话类型 `1 单聊 / 2 群聊`。
- 已读未读采用「会话级已读游标」：`message_reads(uid, conv_id, last_read_seq)` 存每个用户各自已读游标（`UpsertReadSeq` 只前向推进）；会话列表 `peer_read_seq` 返回单聊对端已读游标，供前端恢复已读状态。**已读回执与消息发送、幂等去重均只按单聊实现，群聊不实现已读**。
- **单聊双方必须共享同一 conv_id**：消息/已读/未读都以 conv_id 为纽带，若双方 conv_id 不同会导致历史拉空、已读断裂。`Send` 内通过 `EnsureConversationID` 保证；**未读数依赖会话表 `last_synced_seq`**（`Send` 对接收方更新），缺一不可。
- 会话/消息接口、前端组件变更后建议跑 `desktop_client/scripts/` 下的端到端测试回归（需后端 `:8080` + 前端 `:5173` 运行）。
- 数据库迁移：新 schema 在 `service/migrations/` 加 `NNNN_*.sql`，通过 `make migrate` 应用（`splitStatements` 按 `;` 拆分执行）。
- 敏感配置（OSS AccessKey、JWT secret）只放本地 `configs/config.yaml`，勿提交到代码库。
- 文档即契约：服务端 schema 与前端 mock 字段在 `docs/IM系统架构设计.md` 第 6 节对齐，改动数据模型前先读该文档。
