# 检车家 IM（WorkChat）

一套完整的即时通讯系统，包含 **桌面客户端**（Electron）、**Web 管理后台**（Vue3）和 **服务端**（Go），覆盖单聊 / 群聊 / 好友关系 / 通知中心 / 本地消息存储 / 自动更新等完整 IM 能力。

## 功能概览

| 模块 | 功能 |
|---|---|
| 消息 | 单聊 / 群聊、文本 / 图片 / 文件消息、已读回执、未读计数、离线消息补推、消息撤回、@提及 |
| 会话 | 会话列表、未读聚合、会话置顶/免打扰、会话增量同步（水位机制） |
| 通讯录 | 好友申请（防自加/防重复）、好友备注（展示名备注优先）、分组、二维码邀请 |
| 群聊 | 建群 / 邀请入群、群主与管理员权限、群公告、群成员管理、退群清理 |
| 本地存储 | 桌面端 SQLite 本地消息库、历史消息本地优先加载、聊天记录搜索、退出登录本地清理 |
| 可靠性 | 消息乐观更新 + 30 秒超时去重、发送失败按序展示、WebSocket 心跳与令牌刷新重连 |
| 更新 | 应用内版本检查、静默下载安装、NSIS 覆盖安装 |
| 安全 | 登录滑动验证码（失败触发）、JWT 双令牌、文件上传校验、管理端角色鉴权 |
| 管理后台 | 数据看板、用户管理（禁用/启用）、群组管理、版本发布管理 |

## 整体架构

```
客户端层   desktop_client (Electron + Vue3)      admin (Vue3 Web)
    │  ▲                                            │  ▲
    │  │  HTTP REST + WebSocket 长连接             │  │  HTTP REST
    ▼  │                                            ▼  │
接入层   HTTP API（Gin）        WebSocket 网关（gorilla/websocket）
    └────────────┬──────────────────────┬─────────────┘
                 ▼                      ▼
业务层   auth 认证 / social 好友与群关系 / message 消息收发与存储
         admin 管理端 / file 文件服务（OSS）/ gateway 长连接网关
    └────────────────────┬────────────────────┘
                         ▼
基础设施  MySQL 8.0（业务数据 + 搜索）  Redis 7（在线状态 / 路由 / Pub-Sub）  阿里云 OSS（文件存储）
```

## 项目结构

```
.
├── desktop_client/        # 桌面客户端（Electron 33 + Vue 3 + Vite + better-sqlite3）
│   ├── electron/          #   主进程：窗口、自动更新、本地存储
│   ├── src/               #   渲染层：Vue 组件与 API 层
│   └── 打包指南.md        #   多平台打包说明
├── service/               # 服务端（Go 1.25 + Gin）
│   ├── cmd/server         #   服务入口
│   ├── cmd/migrate        #   数据库迁移工具
│   ├── internal/          #   业务模块：auth / message / social / gateway / admin / file
│   ├── configs/           #   运行配置（config.yaml 不入库，见 config.example.yaml）
│   └── migrations/        #   SQL 迁移脚本（0001~0010）
├── admin/                 # 管理后台（Vue 3 + Vite + vue-router）
└── docs/                  # 架构设计、实施计划等文档
```

## 技术栈

| 端 | 技术 |
|---|---|
| 服务端 | Go 1.25 · Gin · gorilla/websocket · go-redis v9 · go-sql-driver/mysql · golang-jwt v5 · 阿里云 OSS SDK |
| 桌面端 | Electron 33 · Vue 3 · Vite 5 · better-sqlite3（本地消息库）· electron-builder |
| 管理后台 | Vue 3 · Vite 5 · vue-router |
| 基础设施 | MySQL 8.0 · Redis 7 · 阿里云 OSS |

## 快速开始

### 1. 启动依赖（MySQL + Redis）

```powershell
cd service
docker compose up -d
```

### 2. 配置并启动服务端

```powershell
cd service
copy configs\config.example.yaml configs\config.yaml
# 编辑 configs/config.yaml：填入 MySQL DSN、阿里云 OSS 凭证、JWT 密钥

go run ./cmd/migrate -config configs/config.yaml -dir migrations   # 初始化数据库
go run ./cmd/server -config configs/config.yaml                    # 启动服务（默认 :8080）
```

> ⚠️ `config.yaml` 含真实凭证，已被 gitignore，**不要提交到代码库**。

### 3. 启动桌面客户端（开发模式）

```powershell
cd desktop_client
npm install        # postinstall 会自动编译 better-sqlite3 到 Electron ABI
npm run dev        # Vite (5173) + Electron 联调
```

> 注意：客户端默认连接 `http://127.0.0.1:8080`（`src/api/http.js`）与 `ws://127.0.0.1:8080/ws`（`src/api/ws.js`），连其他环境需先修改地址。

### 4. 启动管理后台（开发模式）

```powershell
cd admin
npm install
npm run dev
```

## 打包与分发

桌面端支持 Windows / macOS / Linux 三平台打包（命令见 `desktop_client/package.json`）：

```powershell
npm run build:win          # Windows NSIS 安装包
npm run build:mac          # macOS DMG（必须在 Mac 上执行）
npm run build:linux        # Linux AppImage（必须在 Linux 上执行）
```

mac / Linux 包通过 GitHub Actions 云构建（`.github/workflows/build-desktop.yml`）：推送 `v*` 标签自动触发三平台并行打包，产物在 Actions artifacts 下载。

详细说明见 [desktop_client/打包指南.md](desktop_client/打包指南.md)。

## 文档

| 文档 | 内容 |
|---|---|
| [IM 系统架构设计](docs/IM系统架构设计.md) | 整体架构、数据模型、协议设计 |
| [执行计划](docs/执行计划.md) | 项目实施计划 |
| [桌面端本地存储方案](docs/桌面端本地存储方案.md) | SQLite 本地库设计 |
| [桌面端本地存储实施计划](docs/桌面端本地存储实施计划.md) | 本地存储落地步骤 |
| [桌面端消息同步减压优化方案](docs/桌面端消息同步减压优化方案.md) | 条件同步 + 增量列表 |
| [项目优化分析报告](docs/项目优化分析报告.md) | 性能与体验优化分析 |

## License

MIT
