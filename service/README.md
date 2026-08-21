# IM Service（Go 服务端）

IM 即时通讯系统服务端：Go + Gin + MySQL + Redis + 阿里云 OSS。提供认证、单聊/群聊消息、好友与群组关系、WebSocket 长连接网关、文件预签名与管理后台 API。

## 技术栈

| 层 | 选型 |
|---|---|
| HTTP 框架 | Gin |
| 数据库 | MySQL 8.0（go-sql-driver；消息分 4 表，FNV 散列路由） |
| 缓存/在线/路由 | Redis（go-redis/v9：在线状态、离线队列、跨节点 Pub/Sub、原子 seq 取号、令牌黑名单） |
| 对象存储 | 阿里云 OSS（预签名直传，extra.url 域名白名单校验） |
| 鉴权 | golang-jwt/v5（用户 JWT 双令牌 + 独立 admin JWT） |
| 日志 | 标准库 slog（结构化 JSON） |

## 目录结构

```
service/
├── configs/config.yaml       # 配置（不入库，见 config.example.yaml）
├── cmd/
│   ├── server/main.go        # 服务启动入口（HTTP + WS，启动时自动执行迁移）
│   ├── migrate/main.go       # 独立迁移命令
│   └── diag_trend/main.go    # 数据看板趋势诊断工具
├── internal/
│   ├── config/               # 配置加载与校验
│   ├── server/               # 路由装配 + 中间件（日志/恢复/CORS/JWT/AdminAuth）
│   ├── gateway/              # WebSocket 网关：鉴权/心跳/ack/离线队列/跨节点路由/通话信令转发
│   ├── auth/                 # 认证：注册/登录/JWT 刷新（轮换+黑名单）/二维码登录
│   ├── message/              # 消息：发送/历史/撤回/已读/未读/搜索/会话视图管理
│   ├── social/               # 社交：好友/群组/群成员/通知/入群确认/禁言
│   ├── admin/                # 管理后台：登录/看板/用户/群组/版本发布
│   ├── file/                 # 文件预签名（OSS）
│   ├── store/                # 数据访问层
│   │   ├── mysql/            # 连接池 + DAO（用户/会话/消息/群/通知/管理员/版本）
│   │   ├── redis/            # 客户端封装
│   │   └── oss/              # 阿里云 OSS 预签名
│   └── pkg/                  # err / resp / log / jwt / snowflake / pwd
├── migrations/               # SQL 迁移文件（0001~0014）
├── docker-compose.yml        # 本地 MySQL + Redis
└── Makefile
```

## 快速开始

```bash
# 1. 启动本地依赖（MySQL + Redis）
make docker-up

# 2. 配置 config.yaml（MySQL DSN / Redis / JWT secret / OSS 密钥）
#    注意：migrate_dir 需为绝对路径

# 3. 执行数据库迁移（服务启动时也会自动补齐）
make migrate

# 4. 启动服务（默认 :8080）
make run

# 5. 健康检查
curl http://127.0.0.1:8080/healthz
# => {"code":0,"message":"ok","data":{"status":"ok"}}
```

## 常用命令

```bash
make run          # 运行
make build        # 编译到 bin/im-server
make migrate      # 执行迁移
make test         # 运行单测（内存 mock，无需数据库）
make lint         # go vet 静态检查
make docker-up    # 起 MySQL + Redis
make docker-down  # 停依赖
make tidy         # 整理依赖
```

## HTTP 接口总览

> 统一响应 `{ code, message, data }`，`code===0` 成功。用户接口鉴权头 `Authorization: Bearer <access_token>`；雪花 ID 一律以字符串下发（防 JS 精度丢失）。

### 认证 `/api/v1/auth`（无需鉴权）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/register` | 注册（bcrypt，注册即登录，返回双令牌） |
| POST | `/login` | 登录（返回双令牌 + 是否有待处理好友申请） |
| POST | `/refresh` | 刷新令牌（轮换 + 旧令牌黑名单撤销） |
| POST | `/logout` | 退出（撤销 refresh） |
| POST | `/qrcode/create` `/qrcode/poll` | 二维码生成 / 轮询 |
| POST | `/qrcode/confirm` | 扫码确认（需登录，确认者 uid 取自令牌） |

### 会话 / 消息 `/api/v1/conversations`（需鉴权）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `` | 会话列表；`?changed_since=<unix秒>` 差量刷新（仅返回此后变化的会话） |
| POST | `` | 发送消息（WS 的 HTTP 兜底；msg_id 幂等） |
| GET | `/search` | 消息搜索（keyword / type，跨分表） |
| GET | `/:id/messages` | 历史消息（`after_seq` 增量 / `before_seq` 翻页） |
| POST | `/:id/recall` | 撤回消息（2 分钟时限） |
| PUT | `/:id/settings` | 置顶 / 免打扰 |
| DELETE | `/:id` | 删除会话（仅删本人视图行保留消息，再收发自动重建） |

### 好友 / 用户 / 通知（需鉴权）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/friends` | 好友列表（含备注） |
| GET/POST | `/api/v1/friends/requests` | 待处理申请列表 / 发送申请（防自加/防重复） |
| POST | `/api/v1/friends/requests/:id/handle` | 处理申请（accept/reject） |
| PUT | `/api/v1/friends/:uid/remark` | 设置好友备注 |
| DELETE | `/api/v1/friends/:uid` | 删除好友 |
| GET | `/api/v1/users/search` | 用户搜索（账号/昵称，加好友用） |
| GET | `/api/v1/notifications` | 通知列表（好友/邀请/入群确认/@提及/系统） |
| POST | `/api/v1/notifications/read` | 标记已读（`?all=1` 全部 / `?id=` 单条） |
| GET | `/api/v1/notifications/unread` | 未读通知数 |
| DELETE | `/api/v1/notifications` | 清空通知 |

### 群组 `/api/v1/groups`（需鉴权）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `` | 建群（成员共享群统一 conv_id） |
| GET | `` | 我加入的群 |
| GET | `/:gid` | 群详情（成员/公告/我的角色/我的禁言截止） |
| PUT | `/:gid` | 修改群名 / 公告（群主/管理员） |
| POST | `/:gid/members` | 邀请入群（开启入群确认时转待确认通知） |
| POST | `/:gid/invites/decide` | 入群确认处理（群主/管理员） |
| DELETE | `/:gid/members/me` | 退群 |
| POST | `/:gid/members/:uid/kick` | 移除成员（群主/管理员） |
| PUT | `/:gid/members/:uid/role` | 设/撤管理员（群主） |
| PUT | `/:gid/members/:uid/mute` | 成员禁言/解除（群主/管理员） |
| PUT | `/:gid/owner` | 转让群主 |
| PUT | `/:gid/members/me/nickname` | 设置我的群昵称 |
| PUT | `/:gid/settings` | 入群确认 / 全员禁言开关（群主/管理员） |
| PUT | `/:gid/saved` | 保存到通讯录开关 |

### 文件 / 版本

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/files/presign` | OSS 预签名上传/下载（objectKey 按 uid/类型隔离） |
| GET | `/api/v1/version/latest` | 客户端检查更新（公开接口，仅非敏感版本信息） |

### 管理后台 `/api/admin`（除 login 外需 admin JWT）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/login` | 管理员登录（默认 admin/admin123，首次登录强制改密） |
| POST | `/password` | 修改自己的密码 |
| GET | `/dashboard` | 数据看板统计（用户/消息/群组/趋势） |
| GET | `/users` | 用户列表（offset/limit/keyword 搜索） |
| DELETE | `/users/:uid/disable` `/users/:uid/enable` | 禁用 / 启用用户（禁用后踢线） |
| GET | `/groups` | 群列表（offset/limit/keyword 搜索） |
| GET | `/groups/:gid/messages` | 查看群消息 |
| DELETE | `/groups/:gid` | 解散群（全量清理并实时通知成员） |
| POST | `/version` · GET `/versions` | 版本发布 / 版本列表 |
| POST | `/files/presign` | 管理端上传预签名（安装包直传 OSS） |

## WebSocket 协议（`/ws`）

首帧 `auth` 鉴权（body `{token}`），心跳约 30s，断线进入离线队列（内容帧/控制帧分级，7 天 TTL），重连后补推。

| 帧 | 方向 | 说明 |
|---|---|---|
| `auth` | C→S | 建连鉴权 |
| `heartbeat` | C↔S | 心跳 |
| `msg` | C→S | 发送消息（body 含 `msg_id/conv_id/target_id/conv_type/type/content/extra`） |
| `msg.push` | S→C | 推送消息（携带 `conv_type/target_id`，接收方删除会话后可据此重建） |
| `ack` | C→S | 送达回执（超时重发由网关跟踪） |
| `read` / `read.sync` | C→S / S→C | 已读回执与广播 |
| `typing` | C↔S | 正在输入（仅单聊） |
| `social` | S→C | 好友/群/会话事件（`friend.accepted`、`conversation.created`、`group.updated`、`group.left`、`group.kicked`、`group.muted`、`group.announcement` 等） |
| `notify` | S→C | 通知提醒 |
| `kick` | S→C | 强制下线（账号被禁用/他处登录） |
| `call` / `call.push` | C→S / S→C | 语音通话信令（纯转发，不落库） |

## 关键设计

- **会话视图模型**：每个用户一张会话视图行，主键 `(owner_uid, target_id)`；单聊双方 / 群全体成员共享同一业务 `conv_id`（`groups.conv_id` 为群统一会话 ID）。删除会话仅删视图行，再次收发消息自动以统一 conv_id 重建（防 conv_id 分叉）。
- **消息分表**：`messages_0~3`，按 conv_id FNV-1a 散列路由；会话内 seq 单调（Redis INCR 原子取号，本地 MAX+1 兜底）。
- **未读计数**：`unread_count` 列维护（发消息累加、已读清零、撤回递减），消除撤回场景虚高。
- **群聊批量写**：最后消息/未读/同步游标均为单条批量 SQL（按 `target_id`），消除逐成员写放大。
- **发送守卫**：单聊目标用户存在且未禁用；群聊必须为成员；全员禁言/个人禁言校验；敏感词过滤；每用户 20 条/秒频率风控；extra ≤2KB 且媒体 URL 域名白名单。
- **差量同步**：会话列表支持 `changed_since`；客户端据此做本地秒开 + 增量刷新。
- **离线投递**：用户离线时帧入 Redis 离线队列（内容帧上限 1000 / 控制帧 200），重连补推。

## 数据库迁移（migrations/）

| 文件 | 内容 |
|---|---|
| 0001~0004 | 用户 / 会话 / 消息分表 / 社交（好友/群/通知）/ 管理员基础表 |
| 0005 / 0006 | 单聊双方共享 conv_id；群统一 conv_id（groups.conv_id） |
| 0007 / 0011 | 用户禁用列；管理员首次登录强制改密 |
| 0008 / 0010 | 应用版本表（含安装包 sha256） |
| 0009 | 会话 unread_count 未读计数列 |
| 0012 | 群增强（群昵称等） |
| 0013 | 会话最后消息发送者（群列表"发送者: 内容"前缀） |
| 0014 | 群 P2：入群确认 / 全员禁言 / 成员禁言 / 保存到通讯录 |

## 测试

```bash
make test   # go test ./...（内存 mock Store，无需数据库/Redis）
```

覆盖认证闭环、消息发送/幂等/撤回/未读、群聊分发与 P2、会话删除重建防分叉、搜索、WS 可靠性（心跳/ack/多端投递）等。
