# 检车家 IM（WorkChat）

[![桌面端打包](https://github.com/youstudent/im/actions/workflows/build-desktop.yml/badge.svg)](https://github.com/youstudent/im/actions/workflows/build-desktop.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev/)
[![Electron](https://img.shields.io/badge/Electron-33-47848F?logo=electron&logoColor=white)](https://www.electronjs.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

一套完整的即时通讯系统，包含 **桌面客户端**（Electron）、**移动端**（uni-app x，UI 已完成、后端对接中）、**Web 管理后台**（Vue3）和 **服务端**（Go），覆盖单聊 / 群聊 / 好友关系 / 语音通话 / 通知中心 / 本地加密存储 / 自动更新等完整 IM 能力。

## 目录

- [效果预览](#效果预览)
- [功能概览](#功能概览)
- [整体架构](#整体架构)
- [项目结构](#项目结构)
- [技术栈](#技术栈)
- [环境要求](#环境要求)
- [快速开始](#快速开始)
- [测试](#测试)
- [打包与分发](#打包与分发)
- [常见问题](#常见问题)
- [文档](#文档)

## 效果预览

> 待补充：建议放 2~4 张界面截图（消息页 / 会话列表 / 语音通话 / 管理后台）到 `docs/images/` 后在此引用，例如：
>
> ```markdown
> | 消息页 | 会话与群聊 | 管理后台 |
> |---|---|---|
> | ![消息页](docs/images/chat.png) | ![会话](docs/images/conv.png) | ![后台](docs/images/admin.png) |
> ```

## 功能概览

| 模块 | 功能 |
|---|---|
| 消息 | 单聊 / 群聊、文本 / 图片 / 文件 / 语音 / 视频消息、已读回执、未读计数、离线消息补推、消息撤回（可重新编辑）、@提及与 @所有人、聊天记录本地搜索、引用回复、草稿 |
| 语音通话 | 1 对 1 语音通话（WebRTC P2P + WebSocket 信令）、忙线/离线自动应答、通话记录本地落库 |
| 会话 | 会话列表（置顶优先 + 时间倒序）、未读聚合、置顶/免打扰/标记未读、删除会话（再收发自动重建）、差量刷新（`changed_since`）、本地秒开 + 水位条件同步（追平免请求）、媒体摘要展示 |
| 通讯录 | 好友申请（防自加/防重复）、好友备注（展示名备注优先）、用户搜索、好友/群缓存本地持久化（账户隔离） |
| 群聊 | 建群 / 邀请入群、入群确认（G7）、群主与管理员权限、群昵称（G6）、群公告强提醒（G11）、全员禁言与成员禁言（G8）、移除成员、转让群主、保存到通讯录（G10）、退群/被踢清理 |
| 通知提醒 | 通知中心（好友/群邀请/@提及/系统）、桌面通知与提示音（可配置）、任务栏未读角标（数字/灰点区分免打扰）、窗口未聚焦闪烁 |
| 本地存储 | SQLite 全库加密（SQLCipher，每账户随机密钥）、历史消息本地优先加载、离线发送队列、媒体文件内容哈希缓存、语音已播放状态、备份/导出/占用清理、敏感会话不落盘 |
| 可靠性 | 消息乐观更新 + ack 超时重发、发送失败按真实时间位展示、WebSocket 心跳 / 断线重连 / 离线队列补推、令牌静默刷新 |
| 更新 | 应用内版本检查（24h TTL 缓存）、下载安装包 sha256 校验、静默安装（Windows） |
| 安全 | 登录滑动验证码（失败触发）、JWT 双令牌（轮换 + 黑名单撤销）、敏感词过滤、发送频率风控、文件 extra 域名白名单、敏感会话不落盘、管理端角色鉴权 |
| 管理后台 | 数据看板、用户管理（禁用/启用/搜索）、群组管理（成员/消息查看/解散并实时通知）、版本发布管理、默认管理员首次登录强制改密 |
| 移动端（uni-app x） | 微信风格 UI（58 页）：会话/聊天/通讯录/群聊/朋友圈/钱包（红包/转账/银行卡）/音视频通话/扫码/设置，后端对接规划中 |

## 整体架构

```
客户端层   desktop_client (Electron + Vue3)   uni-appx (uni-app x 移动端)   admin (Vue3 Web)
    │  ▲                                            │                          │  ▲
    │  │  HTTP REST + WebSocket 长连接             │  （后端对接中）         │  │  HTTP REST
    ▼  │                                            ▼                          ▼  │
接入层   HTTP API（Gin）        WebSocket 网关（gorilla/websocket）
    └────────────┬──────────────────────┬─────────────┘
                 ▼                      ▼
业务层   auth 认证 / social 好友与群关系 / message 消息收发与存储
         admin 管理端 / file 文件服务（OSS）/ gateway 长连接网关
    └────────────────────┬────────────────────┘
                         ▼
基础设施  MySQL 8.0（业务数据 + 消息分表）  Redis 7（在线状态 / 离线队列 / Pub-Sub / seq 取号）  阿里云 OSS（文件存储）
```

## 项目结构

```
.
├── desktop_client/        # 桌面客户端（Electron 33 + Vue 3 + Vite + better-sqlite3）
│   ├── electron/          #   主进程：窗口、自动更新、本地加密存储、媒体缓存、任务栏角标
│   ├── src/               #   渲染层：Vue 组件与 API 层
│   ├── scripts/           #   端到端测试脚本（Edge headless + CDP）
│   └── 打包指南.md        #   多平台打包说明
├── service/               # 服务端（Go 1.25 + Gin，详见 service/README.md）
│   ├── cmd/server         #   服务入口（启动时自动执行迁移）
│   ├── cmd/migrate        #   独立数据库迁移工具
│   ├── internal/          #   业务模块：auth / message / social / gateway / admin / file / store
│   ├── configs/           #   运行配置（config.yaml 不入库，见 config.example.yaml）
│   └── migrations/        #   SQL 迁移脚本（0001~0014）
├── admin/                 # 管理后台（Vue 3 + Vite + vue-router，dev 端口 5174）
├── uni-appx/              # 移动端（uni-app x / UVue + UTS，HBuilderX 工程；UI 已完成，后端对接中）
│   ├── pages/             #   58 个页面：会话/聊天/通讯录/群聊/朋友圈/钱包/音视频通话/设置等
│   ├── components/        #   TabBar、聊天输入栏、表情面板等公共组件
│   ├── pages.json         #   页面路由与窗口配置
│   └── manifest.json      #   应用配置（AppID / 端能力）
└── docs/                  # 架构设计、功能方案、优化排查等文档
```

## 技术栈

| 端 | 技术 |
|---|---|
| 服务端 | Go 1.25 · Gin · gorilla/websocket · go-redis v9 · go-sql-driver/mysql · golang-jwt v5 · 阿里云 OSS SDK |
| 桌面端 | Electron 33 · Vue 3 · Vite 5 · better-sqlite3-multiple-ciphers（SQLCipher 加密本地库）· WebRTC（语音通话）· electron-builder |
| 移动端 | uni-app x（UVue + UTS）· HBuilderX · Android / iOS（UI 阶段，后端对接中） |
| 管理后台 | Vue 3 · Vite 5 · vue-router |
| 基础设施 | MySQL 8.0 · Redis 7 · 阿里云 OSS |

## 环境要求

| 依赖 | 版本要求 | 说明 |
|---|---|---|
| Go | ≥ 1.25 | 服务端编译（见 `service/go.mod`） |
| Node.js | ≥ 18（建议 20+） | 前端构建与 E2E 脚本（脚本依赖原生 fetch） |
| Docker | 任意新版 | 本地 MySQL 8.0 + Redis 7（`service/docker-compose.yml`） |
| HBuilderX | 最新正式版 | 移动端 uni-app x 工程的运行/打包（含 uni-app x 编译环境） |
| 阿里云 OSS | — | 图片/文件/语音存储；本地调试也需配置（无降级路径） |
| 构建工具链 | Python 3 + VS Build Tools | 仅当 better-sqlite3 无预编译包需源码编译时（Windows 常见） |

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

go run ./cmd/migrate -config configs/config.yaml -dir migrations   # 手动初始化数据库（可选）
go run ./cmd/server -config configs/config.yaml                    # 启动服务（默认 :8080，启动时自动补齐迁移）
```

> ⚠️ `config.yaml` 含真实凭证，已被 gitignore，**不要提交到代码库**。
> ⚠️ `migrate_dir` 需配置为**绝对路径**（相对路径在部分启动目录下会失效）。

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
npm run dev        # 端口 5174，/api 代理到 :8080
```

默认管理员账号：`admin` / `admin123`（服务端首次启动自动 seed，首次登录会强制修改密码）。

### 5. 运行移动端（uni-app x）

1. 安装 [HBuilderX](https://www.dcloud.io/hbuilderx.html)（需含 uni-app x 编译环境，Android 端需 Android Studio / iOS 需 Xcode）；
2. HBuilderX 打开 `uni-appx/` 目录，「运行 → 运行到手机或模拟器」选择目标设备；
3. 打包：「发行 → 原生 App 云打包 / 本地打包」。

> ⚠️ 移动端当前为 **UI 阶段**：页面与交互已完成（会话/聊天/通讯录/群聊/朋友圈/钱包/音视频通话/设置等 58 页），尚未对接服务端接口与 WebSocket，数据均为本地静态演示数据。

## 测试

**服务端单测**（内存 mock，无需数据库）：

```powershell
cd service
go test ./...
```

**桌面端 E2E**：`desktop_client/scripts/` 提供 40+ 端到端脚本（需服务端 `:8080` 与前端 dev server `:5173` 均已启动），覆盖核心场景，例如：

```powershell
node scripts/test_read_receipt.mjs          # 协议层已读闭环
node scripts/test_unread_cycle.mjs          # 未读计数完整周期
node scripts/test_voice_call.mjs            # 语音通话信令端到端
node scripts/test_delete_conv_recreate.mjs  # 删除会话后对方来消息自动重建
node scripts/test_delete_group_conv_dup.mjs # 群会话删除后重建不分叉、成员列表不重复
node scripts/test_p2_group.mjs              # 群聊 P2（入群确认/禁言/@所有人）API 冒烟
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

## 常见问题

| 问题 | 处理 |
|---|---|
| 启动服务端报 `load config failed` / 迁移目录找不到 | `config.yaml` 中 `migrate_dir` 必须填**绝对路径**（相对路径仅在 `cmd/server` 目录下启动才可能生效），模板里已注明 |
| `npm install` 时 better-sqlite3 编译失败 | 需本机构建工具链（Windows：`npm i -g windows-build-tools` 或 VS Build Tools + Python 3）；postinstall 会自动按 Electron ABI 重编 |
| 客户端连不上后端 | 默认连 `127.0.0.1:8080`，改连其他环境需同时改 `desktop_client/src/api/http.js`（BASE）与 `src/api/ws.js`（WS_URL） |
| 端口被占用 | 服务端改 `config.yaml` 的 `http_addr`；前端 Vite 改 `vite.config.js`（桌面端 5173 / 管理后台 5174） |
| OSS 上传失败 | 核对 endpoint/bucket/密钥；媒体消息 URL 受服务端域名白名单校验，自定义域名需同步配置 |
| 管理后台登录被锁 | 默认账号 admin/admin123 首次登录强制改密；忘记密码需数据库重置 must_change_pwd 标记 |

## 文档

| 文档 | 内容 |
|---|---|
| [IM 系统架构设计](docs/IM系统架构设计.md) | 整体架构、数据模型、协议设计 |
| [执行计划](docs/执行计划.md) | 项目实施计划 |
| [群聊单聊功能完善方案](docs/群聊单聊功能完善方案.md) | 会话/群聊功能清单与验收（含 P2 三期） |
| [桌面端本地存储方案](docs/桌面端本地存储方案.md) | SQLite 加密本地库设计 |
| [桌面端本地存储实施计划](docs/桌面端本地存储实施计划.md) | 本地存储落地步骤 |
| [桌面端消息同步减压优化方案](docs/桌面端消息同步减压优化方案.md) | 水位条件同步 + 增量列表 |
| [桌面端本地数据复用与请求减压排查方案](docs/桌面端本地数据复用与请求减压排查方案.md) | 本地复用排查与分优先级减压方案 |
| [项目优化分析报告](docs/项目优化分析报告.md) | 性能与体验优化分析 |

## License

[MIT](LICENSE) © WorkChat Contributors
