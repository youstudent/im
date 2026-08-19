// Command server 是 IM 服务端启动入口（HTTP + WS 双通道）。
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"im/service/internal/admin"
	"im/service/internal/auth"
	"im/service/internal/config"
	"im/service/internal/file"
	"im/service/internal/gateway"
	"im/service/internal/message"
	"im/service/internal/social"
	"im/service/internal/pkg/jwt"
	"im/service/internal/pkg/log"
	"im/service/internal/pkg/pwd"
	"im/service/internal/pkg/snowflake"
	"im/service/internal/server"
	"im/service/internal/store/mysql"
	"im/service/internal/store/oss"
	"im/service/internal/store/redis"
)

var (
	configPath = flag.String("config", "../../configs/config.yaml", "path to config file")
)

func main() {
	flag.Parse()

	// 1. 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Init("info", "stdout")
		log.L().Error("load config failed", "error", err)
		os.Exit(1)
	}
	log.Init(cfg.Log.Level, cfg.Log.Output)
	logger := log.L()

	// 2. 连接基础设施
	logger.Info("connecting to mysql")
	mysqlDB, err := mysql.New(cfg.MySQL)
	if err != nil {
		logger.Error("init mysql failed", "error", err)
		os.Exit(1)
	}
	defer mysqlDB.Close()
	logger.Info("mysql connected")

	logger.Info("connecting to redis")
	redisClient, err := redis.New(cfg.Redis)
	if err != nil {
		logger.Error("init redis failed", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()
	logger.Info("redis connected")

	// 3. 执行数据库迁移（迁移失败不阻塞启动，记录错误）
	if err := mysqlDB.Migrate(cfg.MySQL.MigrateDir); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}
	logger.Info("migrations applied")

	// 4. 初始化 OSS（配置缺失时降级为不可用，不阻塞启动）
	ossClient, err := oss.New(cfg.OSS)
	if err != nil {
		logger.Warn("oss not configured, file features disabled", "error", err)
	} else {
		logger.Info("oss connected", "bucket", cfg.OSS.Bucket)
		// 自动配置 Bucket 跨域规则，允许前端（localhost:5173 / Electron）直传/读取
		if cerr := ossClient.EnsureCORS([]string{"*"}); cerr != nil {
			logger.Warn("oss ensure cors failed (may lack permission), frontend upload may hit CORS", "error", cerr)
		} else {
			logger.Info("oss cors rule ensured")
		}
	}
	_ = ossClient

	// 5. 初始化认证服务与 JWT
	jwtMgr := jwt.New(cfg.JWT)
	sf, err := snowflake.New(1)
	if err != nil {
		logger.Error("init snowflake failed", "error", err)
		os.Exit(1)
	}
	// 初始化默认管理员（表为空时创建 admin/admin123，并标记首次登录必须改密；
	// 存量部署中仍使用默认密码的管理员同样置位强制改密标记）
	if err := seedDefaultAdmin(mysqlDB, sf.NextID); err != nil {
		logger.Warn("seed default admin failed", "error", err)
	}
	if err := flagDefaultPwdAdmins(mysqlDB); err != nil {
		logger.Warn("flag default password admins failed", "error", err)
	}
	authSvc := auth.New(mysqlDB, redisClient, jwtMgr, sf.NextID)
	authHdlr := auth.NewHandler(authSvc)

	// 6. 消息服务 + WS 网关（阶段三）
	// publish 回调用于向接收方投递 HTTP 链路发送的消息（WS 链路在 adapter 中推送）。
	var hub *gateway.Hub
	var socialSvc *social.Service // publish 需查群成员，social 在后面初始化，先声明后赋值
	var ossSigner message.OSSSigner
	if ossClient != nil {
		ossSigner = ossClient
	}
	msgSvc := message.New(mysqlDB, sf.NextID, func(convID int64, msg *message.MessageDTO) {
		if hub == nil {
			return
		}
		// 修复审计 P1：HTTP 发送的消息此前无推送，接收方收不到实时消息。
		// 按会话类型分发：单聊推对端，群聊推除发送者外的全体成员；hub.Push 自动处理离线队列与跨节点路由。
		conv, err := mysqlDB.GetConversationByID(convID)
		if err != nil || conv == nil {
			return
		}
		frame := &gateway.Frame{Ver: 1, Type: gateway.FrameMsgPush, Seq: 0, Body: msg}
		if conv.Type == 1 {
			// 对端 = 会话中非发送方的一方（单聊双方共享 conv_id，行可能是任一视角）
			peer := conv.OwnerUID
			if peer == msg.SenderUID {
				peer = conv.TargetID
			}
			if peer > 0 && peer != msg.SenderUID {
				hub.Push(peer, frame)
			}
			return
		}
		if socialSvc == nil {
			return
		}
		members, err := socialSvc.GetGroupMembers(conv.TargetID)
		if err != nil {
			return
		}
		for _, uid := range members {
			if uid != msg.SenderUID {
				hub.Push(uid, frame)
			}
		}
	}, ossSigner)
	// extra.url 域名白名单（审计 H4）：媒体消息资源必须来自本 OSS Bucket 域名，
	// 防伪造文件消息指向外部 URL 诱导接收方下载恶意文件；OSS 未配置时不校验（降级放行）。
	if cfg.OSS.Bucket != "" && cfg.OSS.Endpoint != "" {
		endpoint := strings.TrimPrefix(strings.TrimPrefix(cfg.OSS.Endpoint, "https://"), "http://")
		msgSvc.SetAllowedMediaHosts([]string{cfg.OSS.Bucket + "." + endpoint})
	}
	// 原子 seq 取号：Redis INCR 计数器（首次使用时以 DB 当前最大 seq 初始化）。
	// 修复审计 P0：NextSeq=MAX+1 两步非原子导致并发同会话发送 seq 冲突丢消息（实测 200 并发丢 86%）；
	// Redis 不可用时 service 层自动回退本地 MAX+1，唯一键冲突时重试。
	seqScript := goredis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  redis.call('SET', KEYS[1], ARGV[1])
end
return redis.call('INCR', KEYS[1])
`)
	msgSvc.SetSeqGen(func(convID int64) (int64, error) {
		base, err := mysqlDB.NextSeq(convID) // 当前最大 seq + 1
		if err != nil {
			return 0, err
		}
		// 计数器初始化为当前最大 seq，INCR 后取到 max+1；已初始化则直接递增
		res, err := seqScript.Run(context.Background(), redisClient.Client,
			[]string{fmt.Sprintf("msgseq:%d", convID)}, base-1).Int64()
		if err != nil {
			return 0, err
		}
		return res, nil
	})
	msgHdlr := message.NewHandler(msgSvc)

	adapter := gateway.NewAdapter(msgSvc, nil)
	// 节点 ID（审计 P1）：多实例部署时同名节点会导致跨节点路由错乱，优先读配置，缺省用主机名
	nodeID := cfg.Gateway.Node
	if nodeID == "" {
		if hn, herr := os.Hostname(); herr == nil && hn != "" {
			nodeID = hn
		} else {
			nodeID = "node-1"
		}
	}
	wsServer := gateway.NewServer(redisClient, jwtMgr, adapter, nodeID, cfg.Gateway)
	hub = wsServer.Hub()
	// 用户退出登录时断开其 WS 连接
	authSvc.SetDisconnectWS(func(uid int64) {
		hub.Disconnect(uid)
	})

	// 7. 社交模块（阶段四）—— notifier 通过 WS 网关实时推送好友/群事件
	socialSvc = social.New(mysqlDB, sf.NextID, func(uid int64, event string, data interface{}) {
		if hub != nil {
			hub.PublishLocal(uid, &gateway.Frame{Ver: 1, Type: "social", Seq: 0, Body: map[string]interface{}{"event": event, "data": data}})
		}
	})
	// 登录接口返回"是否有待处理好友申请"（导航栏红点状态，避免前端额外请求）
	authSvc.SetPendingReqCheck(socialSvc.HasPendingFriendRequest)
	// 群聊多路分发：把 social 的群成员查询注入网关适配器
	adapter.SetGroupMembers(socialSvc.GetGroupMembers)
	// 群聊同步会话：把群成员查询注入消息服务，供发送群消息时更新每个成员会话的最后消息
	msgSvc.SetGroupMembers(socialSvc.GetGroupMembers)
	// 注入系统消息推送能力：按 uid 推送 msg.push 帧（群创建系统消息等多接收方场景）
	msgSvc.SetPushFunc(func(uid int64, msg *message.MessageDTO) {
		if hub != nil {
			hub.PublishLocal(uid, &gateway.Frame{Ver: 1, Type: gateway.FrameMsgPush, Seq: 0, Body: msg})
		}
	})
	// 创建群/邀请入群后自动发送系统消息，并推送 conversation.created 事件让成员前端刷新会话列表
	socialSvc.SetGroupSysMsgSender(func(ownerUID, gUID, convID int64, content, extra string, memberUIDs []int64) {
		msgSvc.SendGroupSystemMessage(ownerUID, gUID, convID, content, extra, memberUIDs)
		if hub != nil {
			// 通知所有群成员（含群主）：新会话已建立；conv_id 传字符串（雪花 ID 防 JS 精度丢失），
			// 客户端据此增量插入会话项，无需全量重载会话列表
			all := append([]int64{ownerUID}, memberUIDs...)
			for _, uid := range all {
				hub.PublishLocal(uid, &gateway.Frame{Ver: 1, Type: "social", Seq: 0, Body: map[string]interface{}{
					"event": "conversation.created",
					"data":  map[string]interface{}{"conv_id": fmt.Sprintf("%d", convID), "g_uid": gUID, "type": 2},
				}})
			}
		}
	})
	// 定向可见的群系统消息（退群消息仅群主可见）：只落库 + 只推送接收者，不发 conversation 事件
	socialSvc.SetGroupSysMsgToSender(func(gUID, convID int64, content, extra string, recipientUIDs []int64) {
		msgSvc.SendGroupSystemMessageTo(gUID, convID, content, extra, recipientUIDs)
	})
	socialHdlr := social.NewHandler(socialSvc)

	// 8. 管理后台（阶段五）
	adminSvc := admin.New(mysqlDB, jwtMgr, sf.NextID)
	// 管理端登录限流（审计 H2）：同一用户名窗口内最多 5 次尝试，防爆破管理员密码
	adminSvc.SetLoginCache(redisClient)
	// 版本下载地址域名白名单（审计 L2）：发布接口强制 https + 可信域名（复用 OSS Bucket 域名），
	// 防发布指向恶意域的安装包（客户端自动更新会下载执行）；OSS 未配置时仅强制 https
	if cfg.OSS.Bucket != "" && cfg.OSS.Endpoint != "" {
		endpoint := strings.TrimPrefix(strings.TrimPrefix(cfg.OSS.Endpoint, "https://"), "http://")
		adminSvc.SetAllowedDownloadHosts([]string{cfg.OSS.Bucket + "." + endpoint})
	}
	// 禁用用户时：若其在线，立刻踢下线（推送 kick 帧后断开连接，客户端回到登录页）
	adminSvc.SetKickFunc(func(uid int64, reason string) {
		if hub != nil {
			hub.Kick(uid, reason)
		}
	})
	adminHdlr := admin.NewHandler(adminSvc)

	// 9. 文件预签名（阶段五）：OSS 未配置时返回不可用占位
	var fileHdlr *file.Handler
	if ossClient != nil {
		fileHdlr = file.NewHandler(ossClient, sf.NextID)
	} else {
		fileHdlr = file.NewHandler(&file.DisabledOSS{}, sf.NextID)
	}

	// 10. 装配路由并启动 HTTP + WS 服务
	deps := &server.Dependencies{
		MySQL:       mysqlDB,
		Redis:       redisClient,
		JWT:         jwtMgr,
		AuthSvc:     authSvc,
		AuthHdlr:    authHdlr,
		MessageSvc:  msgSvc,
		MessageHdlr: msgHdlr,
		WSGateway:   wsServer,
		SocialSvc:   socialSvc,
		SocialHdlr:  socialHdlr,
		AdminHdlr:   adminHdlr,
		FileHdlr:    fileHdlr,
	}
	router := server.NewRouter(deps, cfg.Server.Mode)

	srv := &http.Server{
		Addr:    cfg.Server.HTTPAddr,
		Handler: router,
	}

	go func() {
		logger.Info("http server starting", "addr", cfg.Server.HTTPAddr, "name", cfg.Server.Name)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	// 11. 优雅退出：先断全部 WS 长连接（触发 readLoop 退出并清理在线状态），再关 HTTP
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down server")

	if hub != nil {
		hub.DisconnectAll()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
	}
	logger.Info("server exited")
}

// seedDefaultAdmin 当 admin_users 表为空时创建默认管理员 admin/admin123。
// 弱口令防护（审计 P0）：种子账号置 must_change_pwd=1，首次登录强制改密。
func seedDefaultAdmin(db *mysql.DB, genID func() int64) error {
	count, err := db.CountAdmins()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := pwd.Hash("admin123")
	if err != nil {
		return err
	}
	return db.CreateAdmin(&mysql.AdminUser{
		ID:            genID(),
		Username:      "admin",
		PasswordHash:  hash,
		Nickname:      "超级管理员",
		Role:          1,
		Status:        1,
		MustChangePwd: 1,
	})
}

// flagDefaultPwdAdmins 启动时检测仍在使用默认密码 admin123 的管理员（老部署升级场景），
// 对其置位强制改密标记，避免弱口令长期暴露。
func flagDefaultPwdAdmins(db *mysql.DB) error {
	admins, err := db.ListAdmins()
	if err != nil {
		return err
	}
	for _, a := range admins {
		if a.MustChangePwd == 1 {
			continue // 已标记，无需重复置位
		}
		if pwd.Verify(a.PasswordHash, "admin123") {
			if err := db.SetAdminMustChangePwd(a.ID); err != nil {
				return err
			}
			log.L().Warn("admin still uses default password, force change on next login", "admin_id", a.ID, "username", a.Username)
		}
	}
	return nil
}
