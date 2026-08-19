# IM 即时通讯系统架构设计

> 版本：v1.1　日期：2026-08-13
> 范围：`desktop_client`（桌面端）、`admin`（管理后台）、`service`（服务端）
> 前提：桌面端前端 UI 已完成（Electron + Vue3），本文档基于其已实现功能反向推导后端架构。
>
> **v1.1 变更**：服务端确定使用 **Go**；移除短信验证码、Elasticsearch、消息队列（Kafka/RabbitMQ）。搜索改用 MySQL、跨节点消息路由改用 Redis Pub/Sub。

---

## 1. 背景与目标

### 1.1 项目现状

| 子项目 | 技术栈 | 现状 |
|---|---|---|
| `desktop_client` | Electron + Vue3 + Vite | UI 已完成，功能为**纯前端 mock**，无网络层/后端 |
| `admin` | Vue3 + Vite + Element Plus | 空目录 |
| `service` | Go | 空目录 |

### 1.2 前端已实现功能盘点（后端需承接）

前端 UI 已经把产品功能"画全"了，后端设计以它为准绳：

| 模块 | 前端已实现 | 后端需补能力 |
|---|---|---|
| 登录/注册 | 账号(手机/邮箱)+密码、明文切换、记住我、表单校验、二维码登录弹窗、注册(昵称/账号/密码/确认/协议) | 真实鉴权、Token 签发、真实扫码登录、邮箱找回密码 |
| 会话/消息 | 单聊/群聊、会话列表(未读数/时间/最后消息)、文本消息、已读回执(写死)、会话搜索、免打扰 | 实时收发、多类型消息、已读回执协议、未读计数、离线消息 |
| 消息输入栏 | 表情/图片/文件/截图/语音/提及 6 个图标按钮(无逻辑) | 对应消息类型、@提及、文件上传 |
| 通讯录 | 好友列表、群列表、发消息跳转 | 好友关系、群关系、分组 |
| 加好友 | 账号搜索、二维码邀请、复制链接、好友请求接受/拒绝 | 好友申请全流程、真实二维码 |
| 资料面板 | 单聊/群聊资料、群公告、群文件、群成员、免打扰 | 群管理、公告、群文件存储 |
| 通知中心 | 回复/@提及/系统/好友请求/群邀请 分类、全部已读 | 通知下发、未读聚合 |
| 搜索聊天记录 | 关键字、类型(图片/文件/链接)、日期分组、跳转高亮 | 关键字/类型/日期检索（MySQL） |
| 设置 | 主题/字体(已真实持久化)、通知/隐私/存储各开关、退出登录 | 配置项持久化、开机自启、导出 |

> 关键结论：前端**数据模型已隐含**（用户 `id/name/avatar/color/signature/workchatId`、会话 `type/unread/lastMessage/time`、消息 `type/text/readAt`、群 `groupId/memberCount/announcement/files/members`、通知 `type/unread/action`），后端 schema 直接据此设计即可无缝对接。

---

## 2. 整体架构

采用「**网关独立 + 业务按领域拆分**」的四层架构：

```
客户端层   desktop_client (Electron+Vue3)   admin (Web 管理后台)
    │  ▲                                        │  ▲
    │  │  HTTP REST / WebSocket 长连接          │  │  HTTP REST
    ▼  │                                        ▼  │
接入层    HTTP API 网关          WebSocket 长连接网关
    │           │                    │
    └───────────┴────────────────────┘
                       │ 服务注册 / 路由 / 鉴权
    ┌──────────────────┼──────────────────────┐
    ▼                  ▼                      ▼
业务服务层  认证服务  用户服务  消息服务  群组服务
            通知服务  文件服务  搜索服务
    └──────────────────┬──────────────────────┘
                       │
基础设施层  MySQL   Redis   阿里云 OSS
```

**设计要点**

1. **长连接网关必须独立**：IM 的并发瓶颈在长连接，网关负责连接建立/心跳/鉴权/路由/推送，可无状态横向扩容；业务逻辑下沉到服务层。
2. **业务服务按领域拆分**：初期可用「模块化单体」承载全部业务，接口边界清晰后按需拆微服务，避免过早拆分的运维复杂度。
3. **双通道分工**：高频实时走 WebSocket，低频重逻辑（登录、拉历史、好友、群管理、上传）走 HTTP REST。

---

## 3. 技术选型

| 层 | 选型 | 说明 |
|---|---|---|
| 桌面端 | Electron + Vue3 + Vite（沿用） | 已 1:1 还原设计稿 |
| 管理后台 | Vue3 + Vite + Element Plus | 与桌面端同栈，统一 TS |
| 服务端 | **Go** | goroutine 天然契合海量长连接与高并发分发、单二进制易部署 |
| HTTP 框架 | Gin / Kratos | 路由 + 中间件（鉴权/限流/日志） |
| 长连接 | WebSocket（JSON 帧，可演进 Protobuf） | Electron 原生支持；JSON 直接映射前端字段 |
| 关系库 | MySQL 8.0（预留分库分表） | 用户/好友/群/会话/消息/检索 |
| 缓存 | Redis | 在线状态、未读计数、跨节点消息路由、热消息、会话列表缓存 |
| 文件 | 阿里云 OSS | 图片/文件/语音/群文件，走预签名 URL 直传 |

> 说明：跨节点消息路由用 **Redis Pub/Sub**（已属 Redis 选型内，不引入额外 MQ 中间件）；聊天记录搜索用 **MySQL**（LIKE + FULLTEXT ngram），规模增长后可平滑升级 ES。

---

## 4. 通信协议设计

### 4.1 双通道职责

| 通道 | 场景 | 示例 |
|---|---|---|
| WebSocket 长连接 | 实时消息、在线状态、正在输入、已读回执、通知推送 | 发送/接收消息、ACK、typing |
| HTTP REST | 低频重逻辑 | 登录、注册、拉历史消息、好友申请、群管理、文件上传 |

### 4.2 长连接消息帧（JSON）

```json
{
  "ver": 1,
  "type": "msg",
  "seq": 1001,
  "body": {}
}
```

帧类型 `type` 枚举：

| type | 方向 | 说明 |
|---|---|---|
| `auth` | C→S | 携带 Token 建连鉴权 |
| `heartbeat` | C↔S | 心跳保活（间隔约 30s） |
| `msg` | C→S | 发送消息 |
| `msg.push` | S→C | 服务端推送消息 |
| `ack` | C→S | 客户端收到消息回执（送达） |
| `read` | C→S | 已读回执（携带会话最后已读 seq） |
| `read.sync` | S→C | 服务端广播已读状态给发送方 |
| `typing` | C↔S | 正在输入（可选） |
| `presence` | S→C | 好友/群成员在线状态变更 |
| `notify` | S→C | 通知（系统、@提及、好友请求、群邀请） |

### 4.3 消息可靠性（不丢、不重、有序）

- **客户端幂等**：每条消息携带客户端生成的 `msgId`（UUID），服务端以 `msgId` 去重，网络重试不产生重复。
- **有序性**：消息服务为每个会话维护单调递增 `seq`，接收端按 `seq` 排序/去重，乱序不展示。
- **ACK 机制**：接收方对每条消息回 `ack(msgId)`；未收到 ACK 的消息在下次拉取/重连时由服务端补发（客户端携带本会话已确认的最大 seq）。
- **离线消息**：接收方不在线时消息落入离线队列（Redis），上线后由网关批量推送或由客户端拉取补齐。

### 4.4 HTTP API 概览（按服务）

| 服务 | 主要接口 |
|---|---|
| 认证 | `POST /auth/register`、`POST /auth/login`、`POST /auth/logout`、`POST /auth/refresh`、二维码登录 `POST /auth/qrcode/create`、`POST /auth/qrcode/poll`、`POST /auth/qrcode/confirm` |
| 用户 | `GET /users/me`、`PATCH /users/me`、`GET /users/search` |
| 好友 | `POST /friends/request`、`POST /friends/accept`、`POST /friends/delete`、`GET /friends` |
| 会话 | `GET /conversations`、`GET /conversations/{id}/messages`、`DELETE /conversations/{id}` |
| 群组 | `POST /groups`、`POST /groups/{id}/members`、`PATCH /groups/{id}`、`POST /groups/{id}/leave`、群公告、群文件 |
| 文件 | `POST /files/presign`（预签名直传）、`GET /files/{id}` |
| 搜索 | `GET /search/messages`（关键字/类型/日期/会话） |
| 通知 | `GET /notifications`、`POST /notifications/read-all` |

---

## 5. 核心流程设计

### 5.1 登录与鉴权

1. 账号+密码 → 认证服务校验 → 签发 **JWT（access，短期）+ refresh token（长期）**。（注册即账号+密码，无短信验证码）
2. 桌面端保存 refresh token（Electron `safeStorage` 加密存本地），access 过期自动刷新。
3. 建立 WebSocket 时首帧 `auth` 携带 access token，网关校验后绑定 `uid → 连接` 映射并写入 Redis 在线状态。
4. 二维码登录：服务端生成一次性 `qrcodeId`（带过期），桌面端轮询状态；手机 App 扫码后 `confirm` 接口回写状态，桌面端轮询到「已确认」后签发 token。
5. 忘记密码：通过账号绑定的邮箱发送重置链接完成重置（不使用短信）。

### 5.2 消息发送链路

```
发送方 → WS网关(鉴权/限流) → 消息服务(去重/落库/生成seq)
  → Redis(会话列表+未读计数)  → 推送在线接收方(WS网关，跨节点走 Redis Pub/Sub)
  → 落离线队列(接收方不在线)  → 触发通知(@提及/回复)
```

关键点：

- 消息先**落库成功再推送**，保证不丢。
- 未读数用 Redis 原子自增 `INCR conv:{uid}:{convId}:unread`，免打扰/静默会话不计入红点。
- 会话列表排序字段（`last_msg_id`、`last_msg_time`）实时更新，桌面端会话列表据此重排。
- 跨节点路由：Redis 记录 `uid → 网关节点` 映射，通过节点订阅的 Pub/Sub 频道把消息投递给目标网关节点。

### 5.3 已读回执

- **单聊**：接收方读到消息后发 `read(convId, lastReadSeq)`，消息服务把「已读」状态广播给发送方，前端气泡显示「已读」。
- **群聊**：记录每个成员的 `last_read_seq`，可精确到「N 人已读」；发送方可见已读人数（前端当前仅展示单聊已读）。
- 已读回执受「设置-隐私-已读回执」开关控制，关闭时不回传。

### 5.4 好友申请

```
A 搜索/扫码 → 填验证信息 → POST /friends/request
  → B 收到 notify(friend) 通知 → 接受/拒绝
  → 接受后双向写入 friends，并生成单聊会话 conversation
```

### 5.5 在线状态与通知

- 在线状态：网关连接建立/断开时写 Redis `presence:{uid}`，并广播 `presence` 给相关会话成员/好友。
- 通知下发：通知服务产生事件 → 落库 → 直接经网关推送给在线用户（跨节点走 Redis Pub/Sub）；前端通知中心按 `type` 分类展示、红点聚合。

---

## 6. 数据模型

> 字段命名与前端 mock 对齐。**用户以 `uid`、群以 `g_uid`（均为 10 位随机数字）作为业务主键**，消息/好友/群成员/会话/通知等业务关联统一走 `uid` / `g_uid`；各表内部主键 `id` 采用雪花 ID，仅用于存储与索引。

### 6.1 users（用户）

| 字段 | 类型 | 说明 |
|---|---|---|
| id | bigint PK | 内部主键（雪花 ID，仅存储/索引用） |
| uid | bigint UK | 业务 UID，10 位随机数字，全局唯一，业务关联与对外展示用（对应前端 `workchatId`） |
| account | varchar | 登录账号（手机/邮箱） |
| password_hash | varchar | 密码哈希（bcrypt/argon2） |
| email | varchar | 绑定邮箱（用于找回密码，可空） |
| nickname | varchar | 昵称 |
| avatar | varchar | 头像 URL（缺省用首字+color） |
| signature | varchar | 个性签名 |
| status | tinyint | 在线状态（离线/在线/忙碌/隐身） |
| last_seen_at | datetime | 最后上线时间 |
| created_at | datetime | 注册时间 |

> **uid 生成规则**：10 位随机数字（首位 1~9，即 1000000000 ~ 9999999999），注册时生成；数据库唯一索引兜底，冲突时重试。uid 即对外展示的 WorkChat ID，用户搜索/添加好友也用它。

### 6.2 friends（好友关系）

| 字段 | 类型 | 说明 |
|---|---|---|
| uid | bigint | 用户 uid |
| friend_uid | bigint | 好友 uid |
| remark | varchar | 备注（前端「同事·产品组」） |
| tags | json | 标签（前端「产品/同事」） |
| status | tinyint | 关系状态 |
| created_at | datetime | 建立时间 |

> 主键 `(uid, friend_uid)`，双向各存一条。好友申请另用 `friend_requests` 表。

### 6.3 groups（群）

| 字段 | 类型 | 说明 |
|---|---|---|
| id | bigint PK | 内部主键（雪花 ID，仅存储/索引用） |
| g_uid | bigint UK | 业务群 ID，10 位随机数字，全局唯一，业务关联与对外展示用（对应前端 `groupId` 群号） |
| name | varchar | 群名 |
| owner_uid | bigint | 群主 uid |
| announcement | text | 群公告 |
| member_count | int | 成员数 |
| avatar | varchar | 群头像 |
| created_at | datetime | 创建时间 |

> **g_uid 生成规则**：10 位随机数字（首位 1~9，即 1000000000 ~ 9999999999），建群时生成；数据库唯一索引兜底，冲突时重试。g_uid 即对外展示的群号。

### 6.4 group_members（群成员）

| 字段 | 类型 | 说明 |
|---|---|---|
| g_uid | bigint | 群 g_uid |
| uid | bigint | 成员 uid |
| role | tinyint | 角色（群主/管理员/成员） |
| join_time | datetime | 入群时间 |

### 6.5 conversations（会话）

| 字段 | 类型 | 说明 |
|---|---|---|
| id | bigint PK | 会话 ID（雪花，内部主键） |
| type | tinyint | 1 单聊 / 2 群聊 |
| owner_uid | bigint | 归属用户 uid（每个用户一个会话视图） |
| target_id | bigint | 对方 uid（单聊）或群 g_uid（群聊） |
| last_msg_id | bigint | 最后一条消息 |
| last_msg_time | datetime | 最后消息时间（会话排序） |
| muted | tinyint | 免打扰（前端 toggle） |
| pinned | tinyint | 置顶（预留） |

### 6.6 messages（消息）

| 字段 | 类型 | 说明 |
|---|---|---|
| id | bigint PK | 消息 ID（雪花，全局唯一） |
| conv_id | bigint | 所属会话（分表键） |
| seq | bigint | 会话内单调递增序号 |
| sender_uid | bigint | 发送者 uid |
| type | tinyint | text/image/file/voice/video/system/... |
| content | text | 文本或结构化 JSON（@提及、引用等） |
| extra | json | 扩展（文件信息、链接 URL、撤回标记等） |
| status | tinyint | 正常/已撤回 |
| created_at | datetime | 发送时间 |

> 搜索支撑：`type` 建索引用于「图片/文件」筛选；链接类消息在 `extra.url` 存 URL 用于「链接」筛选；关键字检索走 `content` 的 LIKE 或 MySQL FULLTEXT（ngram 中文分词），建议索引 `(conv_id, created_at)`。

#### 分表规则（4 张表）

- **分表键**：`conv_id`（会话 ID）
- **表命名**：`messages_0` ~ `messages_3`
- **路由算法**：`table_index = conv_id % 4`（等价于 `conv_id & 0x03`）
- **建表**：4 张表结构完全一致，字段与 `messages` 相同

**为什么按 conv_id 分表**：消息读写几乎都围绕「会话」维度（发送落库、拉历史、按 `seq` 增量同步），同一会话的所有消息恒落在同一张表，保证 `seq` 单表内连续有序、查询只走一张表。

**读写路由**：
- 落库：`INSERT INTO messages_{conv_id % 4} (...)`
- 拉历史 / 增量：`SELECT ... FROM messages_{conv_id % 4} WHERE conv_id = ? AND seq > ? ORDER BY seq DESC`

**注意**：
- 全局消息 ID（雪花）不受分表影响，仍全局唯一。
- `seq` 为会话内序号，同会话同表，单表维护即可。
- 跨会话搜索（查找聊天记录）需遍历 4 张表，用 `UNION ALL` 后按时间排序；仅 4 张表开销可接受。
- 后续扩容保持 2 的幂（4 → 8 → 16），迁移时按 `conv_id` 重新路由。

### 6.7 message_reads（已读状态）

| 字段 | 类型 | 说明 |
|---|---|---|
| uid | bigint | 成员 uid |
| conv_id | bigint | 会话 |
| last_read_seq | bigint | 最后已读 seq |

### 6.8 notifications（通知）

| 字段 | 类型 | 说明 |
|---|---|---|
| id | bigint PK | 通知 ID |
| uid | bigint | 接收者 uid |
| type | tinyint | reply/mention/system/friend/invite |
| title | varchar | 标题 |
| summary | varchar | 摘要 |
| action | json | 操作（如好友请求 accept） |
| read | tinyint | 已读状态 |
| created_at | datetime | 时间 |

### 6.9 files（文件）

| 字段 | 类型 | 说明 |
|---|---|---|
| id | bigint PK | 文件 ID |
| uploader_uid | bigint | 上传者 uid |
| object_key | varchar | 阿里云 OSS object key |
| name | varchar | 文件名 |
| size | bigint | 大小（前端 `size` 文案） |
| mime | varchar | MIME 类型 |
| created_at | datetime | 上传时间 |

---

## 7. 存储与缓存方案

| 数据 | 存储 | 策略 |
|---|---|---|
| 关系数据（用户/好友/群/会话） | MySQL | 索引优化，读写分离 |
| 消息 | MySQL（冷）+ Redis（热） | 近期消息缓存 Redis，历史查 MySQL；按 conv 分片 |
| 在线状态 | Redis | `SET presence:{uid} EX 心跳`，断开清除 |
| 未读计数 | Redis | 原子自增；点开会话清零 |
| 消息检索 | MySQL | `type` 索引 + 关键字 LIKE/FULLTEXT(ngram)；规模增长后可平滑升级 ES |
| 跨节点路由 | Redis Pub/Sub | 网关节点订阅各自频道，消息服务按 `uid→节点` 映射投递 |
| 文件 | 阿里云 OSS | 客户端预签名 URL 直传，减轻网关压力 |
| 会话列表 | Redis 缓存 + MySQL 落盘 | 高频读走缓存 |

---

## 8. 部署架构（演进）

**阶段一（MVP，推荐起步）**

- 单实例：网关（HTTP+WS 合一）+ 模块化单体业务服务 + MySQL + Redis + 阿里云 OSS。
- 目标：先跑通「登录 → 单聊收发 → 已读回执 → 未读数 → 通讯录/好友」。

**阶段二（规模增长）**

- WS 网关独立并多实例，Redis Pub/Sub 做跨节点消息路由。
- 消息服务、群组服务拆出独立进程。
- 消息量增大后按需升级：分库分表、引入 ES 承载聊天记录搜索。

**阶段三（成熟）**

- 消息分库分表、冷热分离、阿里云 OSS + CDN 加速、多机房容灾、压测调优。

---

## 9. 安全设计

- 密码 bcrypt/argon2 加盐存储；JWT 短期 access + 可撤销 refresh。
- Electron 侧用 `safeStorage` 加密 token；`contextIsolation` 保持开启（前端已按此规范）。
- 长连接鉴权 + 心跳超时踢下线 + 单用户多端连接管理。
- 接口限流、消息风控（频率、敏感词）、文件类型/大小校验、预签名 URL 时效。
- 传输层 TLS；敏感日志脱敏。
- 「端到端加密」为前端文案占位，若需真 E2EE，采用 Signal 协议（X3DH+双棘轮），本架构第一版先做传输加密，E2EE 作为可选演进项。

---

## 10. 管理后台（admin）功能规划

| 模块 | 功能 |
|---|---|
| 用户管理 | 用户列表/检索/封禁/详情 |
| 群组管理 | 群列表/成员/解散/违规处置 |
| 消息审计 | 消息检索、敏感词、举报处理 |
| 通知运营 | 系统通知下发、推送任务 |
| 数据看板 | 在线数、消息量、日活、留存 |
| 系统配置 | 敏感词库、风控阈值、文件策略 |

---

## 11. 落地建议（分阶段）

1. **P0**：搭建 `service`（Go）骨架 + MySQL/Redis 基础设施 → 认证服务（登录/注册/Token，无短信验证码）。
2. **P1**：长连接网关 + 消息服务（单聊收发、历史、未读、已读回执）→ 桌面端接入真实接口替换 mock。
3. **P2**：用户/好友/通讯录 → 群组服务 → 通知中心。
4. **P3**：文件/图片/语音消息、搜索（MySQL）、管理后台 MVP。
5. **P4**：语音/视频通话（WebRTC，可接入信令）、E2EE、多端同步、压测与优化。

> 前端当前唯一现成的 Electron 桥接是 `window.electronAPI.selectDirectory()`，后续可沿用 `preload.js` 模式扩展系统通知、文件读写、开机自启动、托盘等 IPC。
>
> 桌面端本地持久化（SQLite + IPC、离线可用/秒开/导出备份）见 `docs/桌面端本地存储方案.md`。
