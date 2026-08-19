# IM Service（Go 服务端）

IM 即时通讯系统服务端，采用 Go + Gin + MySQL + Redis + 阿里云 OSS。

## 技术栈

| 层 | 选型 |
|---|---|
| HTTP 框架 | Gin |
| 数据库 | MySQL 8.0（go-sql-driver） |
| 缓存/在线/未读/路由 | Redis（go-redis/v9） |
| 对象存储 | 阿里云 OSS（预签名直传） |
| 鉴权 | golang-jwt/v5 |
| 日志 | 标准库 slog（结构化 JSON） |

## 目录结构

```
service/
├── configs/config.yaml       # 配置
├── cmd/
│   ├── server/main.go        # 服务启动入口（HTTP + WS）
│   └── migrate/main.go       # 独立迁移命令
├── internal/
│   ├── config/               # 配置加载与校验
│   ├── server/               # 路由装配 + 中间件
│   ├── gateway/              # WebSocket 网关（连接/心跳/鉴权/路由/推送）
│   ├── auth/                 # 认证（注册/登录/JWT刷新/二维码）
│   ├── message/              # 消息（发送/历史/已读/未读/搜索）
│   ├── social/               # 社交（好友/群组/通知）
│   ├── admin/                # 管理后台（登录/看板/用户/群组）
│   ├── file/                 # 文件预签名（OSS）
│   ├── store/                # 数据访问层
│   │   ├── mysql/            # 连接池 + 迁移 + 用户 DAO
│   │   ├── redis/            # 客户端封装
│   │   └── oss/              # 阿里云 OSS 预签名
│   └── pkg/                  # err / resp / log / jwt / snowflake / pwd
├── migrations/               # SQL 迁移文件
├── docker-compose.yml        # 本地 MySQL + Redis
└── Makefile
```

## 快速开始

```bash
# 1. 启动本地依赖（MySQL + Redis）
make docker-up

# 2. 配置 config.yaml（改 DSN / Redis / JWT secret / OSS 密钥）

# 3. 执行数据库迁移
make migrate

# 4. 启动服务
make run

# 5. 健康检查
curl http://127.0.0.1:8080/healthz
# => {"code":0,"message":"ok","data":{"status":"ok"}}
```

## 常用命令

```bash
make run          # 运行
make build        # 编译到 bin/
make migrate      # 执行迁移
make docker-up    # 起 MySQL + Redis
make docker-down  # 停依赖
make tidy         # 整理依赖
```

## 阶段一完成项

- [x] Go 模块与分层目录骨架
- [x] 配置加载（config.yaml）
- [x] MySQL 连接池 + 迁移机制
- [x] Redis 客户端
- [x] 阿里云 OSS 预签名封装
- [x] 统一响应 / 错误码 / 请求日志 / panic 恢复中间件
- [x] 雪花 ID 生成器
- [x] docker-compose（mysql + redis）
- [x] /healthz 健康检查 + Makefile

## 阶段二完成项（认证与登录）

- [x] users 表迁移（含 email，用于找回密码）
- [x] 注册接口（bcrypt 加密，账号/昵称/密码校验，注册即登录）
- [x] 登录接口（签发 access + refresh token）
- [x] JWT 鉴权中间件 + `/auth/refresh` 刷新（令牌轮换 + 黑名单撤销）
- [x] 退出登录（refresh token 撤销）
- [x] 二维码登录（qrcodeId 生成 / 轮询 / 确认，Redis 存状态）
- [x] 认证单元测试（注册/登录/刷新/退出/二维码闭环）

### 认证接口

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/auth/register` | 注册（返回 access + refresh + 用户信息） |
| POST | `/api/v1/auth/login` | 登录 |
| POST | `/api/v1/auth/refresh` | 刷新令牌 |
| POST | `/api/v1/auth/logout` | 退出（撤销 refresh） |
| POST | `/api/v1/auth/qrcode/create` | 生成二维码 |
| POST | `/api/v1/auth/qrcode/poll` | 轮询二维码状态 |
| POST | `/api/v1/auth/qrcode/confirm` | 手机扫码确认 |

> 统一响应：`{ code, message, data }`，code===0 表示成功。鉴权头：`Authorization: Bearer <access_token>`。

## 阶段三完成项（消息与长连接）

- [x] 会话/消息/已读表迁移（消息分 4 表，`conv_id % 4` 路由）
- [x] WS 网关：建连 auth 首帧鉴权、心跳、断线清理、Redis 在线状态
- [x] 消息发送链路：msgId 幂等去重 → 落库 → 会话内 seq → 推送接收方
- [x] 单聊历史拉取 + 会话最后消息
- [x] 已读回执（read → 落库 + read.sync 广播）
- [x] Redis Pub/Sub 跨节点消息路由（uid→节点 映射 + 频道订阅）
- [x] HTTP 会话/消息接口 + 消息服务单元测试

### 会话 / 消息接口（需鉴权）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/conversations` | 会话列表 |
| POST | `/api/v1/conversations` | 发送消息（低频兜底） |
| GET | `/api/v1/conversations/:id/messages` | 拉取历史消息 |
| WS | `/ws` | 长连接网关 |

### WS 帧（阶段三已启用）

- `auth`（C→S 首帧，body `{token}`）
- `heartbeat`（C↔S 心跳）
- `msg`（C→S 发送，body `{msg_id, conv_id, target_id, type, content}`）
- `msg.push`（S→C 推送）
- `read`（C→S 已读，body `{conv_id, seq}`）
- `read.sync`（S→C 已读广播）
- `ack`（C→S 送达回执）
- `social`（S→C 好友/群事件推送，body `{event, data}`）

## 阶段四完成项（社交与群组）

- [x] 好友/群/群成员/申请/通知表迁移
- [x] 好友：申请 / 接受 / 拒绝 / 删除 / 列表 / 待处理申请列表
- [x] 群组：建群 / 邀请 / 退群 / 成员 / 群信息
- [x] 群聊多路分发（`conv_type=2` → 群成员列表推送）
- [x] 通知：reply/mention/system/friend/invite 类型、已读、聚合、未读计数
- [x] 好友/建群业务实时推送（WS `social` 帧）
- [x] 社交服务单元测试

### 社交接口（需鉴权）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/friends` | 好友列表 |
| GET | `/api/v1/friends/requests` | 我收到的待处理申请 |
| POST | `/api/v1/friends/requests` | 发送好友申请 |
| POST | `/api/v1/friends/requests/:id/handle` | 处理申请（accept） |
| DELETE | `/api/v1/friends/:uid` | 删除好友 |
| POST | `/api/v1/groups` | 建群 |
| GET | `/api/v1/groups` | 我加入的群 |
| GET | `/api/v1/groups/:gid` | 群详情 |
| POST | `/api/v1/groups/:gid/members` | 邀请入群 |
| DELETE | `/api/v1/groups/:gid/members/me` | 退群 |
| GET | `/api/v1/notifications` | 通知列表 |
| POST | `/api/v1/notifications/read` | 标记已读（?all=1 全部） |
| GET | `/api/v1/notifications/unread` | 未读通知数 |
| DELETE | `/api/v1/notifications` | 清空通知 |

> 群聊消息：WS `msg` 帧 body 增加 `conv_type`（1 单聊 / 2 群聊），群聊时 `target_id` 为群 `g_uid`。

## 阶段五完成项（富媒体与后台）

- [x] OSS 预签名上传接口（图片/文件/语音，objectKey 按 uid/类型隔离）
- [x] 消息搜索接口（关键字 LIKE + 类型过滤，跨分表）
- [x] admin 管理员表 + 默认管理员 seed（admin/admin123）
- [x] admin 登录鉴权（独立 admin JWT，IsAdmin 声明 + AdminAuth 中间件）
- [x] admin 数据看板 / 用户管理 / 群组管理接口
- [x] admin 前端骨架（Vue3 + Vite，登录/看板/用户/群组）
- [x] admin 单元测试

### 富媒体 / 搜索接口（需鉴权）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/files/presign` | 生成 OSS 预签名上传/下载 URL |
| GET | `/api/v1/conversations/search` | 消息搜索（keyword / type） |

### 管理后台接口

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/admin/login` | 管理员登录（返回 admin JWT） |
| GET | `/api/admin/dashboard` | 数据看板统计 |
| GET | `/api/admin/users` | 用户列表（offset/limit） |
| DELETE | `/api/admin/users/:uid/disable` | 禁用用户 |
| GET | `/api/admin/groups` | 群列表（offset/limit） |
| DELETE | `/api/admin/groups/:gid` | 解散群 |

> admin 接口前缀 `/api/admin`，除 login 外均需 `Authorization: Bearer <admin_token>`。
> 默认管理员：admin / admin123（首次启动自动创建）。
> admin 前端：`cd admin && npm run dev`，默认端口 5174。
