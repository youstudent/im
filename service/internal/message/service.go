// Package message 实现消息领域：发送链路、历史、未读、已读回执。
// 与 gateway 配合：gateway 负责长连接收发与推送，service 负责持久化与业务规则。
package message

import (
	"encoding/json"
	"errors"
	"math/rand"
	"net/url"
	"strings"
	"time"

	apperr "im/service/internal/pkg/err"
	"im/service/internal/pkg/log"
	"im/service/internal/store/mysql"
)

// Store 消息模块依赖的存储接口。
type Store interface {
	GetOrCreateConversation(ownerUID, targetID int64, typ int8, newID int64) (*mysql.Conversation, error)
	EnsureConversationID(ownerUID, targetID int64, typ int8, convID int64) (*mysql.Conversation, error)
	GetConversation(ownerUID, targetID int64) (*mysql.Conversation, error)
	GetConversationByID(convID int64) (*mysql.Conversation, error)
	// 审计 P0：历史拉取前的会话归属校验（防越权读取任意会话）
	IsConversationMember(uid, convID int64) (bool, error)
	ListConversations(ownerUID int64) ([]*mysql.Conversation, error)
	ListConversationsChangedSince(ownerUID int64, since time.Time) ([]*mysql.Conversation, error)
	UpdateConversationLastMsg(ownerUID, targetID int64, lastMsgID int64, text string, t time.Time) error
	UpdateConversationSyncedSeq(ownerUID, targetID, seq int64) error
	CreateMessage(m *mysql.Message) (int64, error)
	NextSeq(convID int64) (int64, error)
	GetMessage(convID, msgID int64) (*mysql.Message, error)
	UpdateMessageStatus(convID, msgID int64, status int8) error
	GetLastActiveMessage(convID int64) (*mysql.Message, error)
	ListMessages(convID, afterSeq int64, limit int) ([]*mysql.Message, error)
	ListMessagesBefore(convID, beforeSeq int64, limit int) ([]*mysql.Message, error)
	MessageExists(convID, msgID int64) (bool, error)
	GetLastReadSeq(uid, convID int64) (int64, error)
	// P1 性能优化：会话列表对端已读游标批量查（替代逐会话 N+1）
	GetPeerReadSeqs(pairs [][2]int64) (map[int64]int64, error)
	UpsertReadSeq(uid, convID, seq int64) error
	SearchMessages(keyword string, msgType int8, convIDs []int64, limit int) ([]*mysql.SearchResult, error)
	GetUserName(uid int64) string
	GetUserByUID(uid int64) (*mysql.User, error)
	// P1 性能优化：群聊批量写 + 昵称批量查
	GetUserNames(uids []int64) map[int64]string
	EnsureGroupConversationViews(convID, gUID int64, memberUIDs []int64) error
	UpdateGroupConversationsLastMsg(gUID, lastMsgID int64, text string, t time.Time) error
	UpdateGroupConversationsSyncedSeq(gUID, seq, excludeUID int64) error
	UpdateGroupConversationsLastMsgExcept(gUID, excludeUID, lastMsgID int64, text string, t time.Time) error
	// P2 未读计数：unread_count 列维护（发消息累加、已读清零、撤回递减）
	BumpConversationUnread(ownerUID, targetID, delta int64) error
	BumpGroupConversationsUnread(gUID, excludeUID, delta int64) error
}

// Publish 消息推送接口：由 gateway 实现，负责把消息投递给接收方（含跨节点）。
type Publish func(convID int64, msg *MessageDTO)

// PushFunc 按指定 uid 推送消息帧（由网关实现，用于系统消息等多接收方投递）。
type PushFunc func(uid int64, msg *MessageDTO)

// OSSSigner 用于为图片/文件消息重新生成有效的下载 URL（历史消息中的预签名 URL 会过期）。
type OSSSigner interface {
	PublicURL(objectKey string) string
}

// Service 消息服务。
type Service struct {
	store    Store
	genID    func() int64
	publish  Publish
	pushFunc PushFunc // 可选；按 uid 推送（系统消息等多接收方场景）
	oss      OSSSigner // 可选；nil 时不刷新下载 URL
	limiter  *rateLimiter // 消息频率风控
	groupMembers func(gUID int64) ([]int64, error) // 可选；查询群成员 uid 列表（由 social 注入），用于群聊同步各成员会话最后消息
	seqGen func(convID int64) (int64, error) // 可选；原子 seq 取号源（Redis INCR），nil 时回退本地 MAX+1
	mediaHosts map[string]bool // 可选；extra.url 允许的媒体资源域名白名单（OSS），空时不校验
}

// 输入长度约束（审计 H3）：防通过 HTTP 通道提交超大消息体造成存储膨胀/慢查询滥用。
// WS 通道另有 64KB 帧上限，HTTP 通道此前无任何限制，故在服务层统一拦截。
const (
	maxContentRunes = 4000 // 文本内容上限 4000 字符
	maxExtraBytes   = 2048 // extra JSON 上限 2KB（图片/文件元数据远小于此）
)

// SetSeqGen 注入原子 seq 取号源（由装配层用 Redis 实现）。
// 修复审计 P0：NextSeq=SELECT MAX+INSERT 两步非原子，并发同会话发送时 seq 冲突丢消息。
func (s *Service) SetSeqGen(fn func(convID int64) (int64, error)) {
	s.seqGen = fn
}

// GroupMembersFunc 群成员查询函数签名。
type GroupMembersFunc func(gUID int64) ([]int64, error)

// SetGroupMembers 运行时注入群成员查询能力（由 social 服务实现，供群聊同步成员会话）。
func (s *Service) SetGroupMembers(fn GroupMembersFunc) {
	s.groupMembers = fn
}

// New 创建消息服务。publish 由网关注入，实现消息投递。
// oss 可选：为图片/文件历史消息重新签名下载 URL，避免预签名过期导致展示失败。
func New(store Store, genID func() int64, publish Publish, oss OSSSigner) *Service {
	return &Service{
		store:   store,
		genID:   genID,
		publish: publish,
		oss:     oss,
		// 频率风控：每用户 1 秒窗口内最多 20 条
		limiter: newRateLimiter(time.Second, 20),
	}
}

// SetOSSSigner 运行时注入 OSS 签名能力（可在服务启动后调用）。
func (s *Service) SetOSSSigner(oss OSSSigner) {
	s.oss = oss
}

// SetAllowedMediaHosts 注入 extra.url 域名白名单（审计 H4）：媒体消息的资源地址必须来自
// 可信 OSS 域名，防止伪造文件消息指向任意外部 URL 诱导接收方下载/打开恶意文件。
// 白名单为空时不校验（OSS 未配置降级，不阻断主流程）。
func (s *Service) SetAllowedMediaHosts(hosts []string) {
	m := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			m[h] = true
		}
	}
	s.mediaHosts = m
}

// SetPushFunc 运行时注入按 uid 推送能力（系统消息等多接收方投递）。
func (s *Service) SetPushFunc(fn PushFunc) {
	s.pushFunc = fn
}

// SendGroupSystemMessage 群系统消息（建群/邀请入群）：为全体接收成员落库并实时推送。
// 所有成员共享统一的 convID 会话视图；extra 携带结构化信息（如邀请详情），
// 客户端按查看者身份渲染个性化文案；content 为共享存储文案。成员去重（ownerUID 可能已在 memberUIDs 中）。
func (s *Service) SendGroupSystemMessage(ownerUID, gUID, convID int64, content, extra string, memberUIDs []int64) {
	seen := make(map[int64]struct{}, len(memberUIDs)+1)
	all := make([]int64, 0, len(memberUIDs)+1)
	for _, u := range append([]int64{ownerUID}, memberUIDs...) {
		if u <= 0 {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		all = append(all, u)
	}
	// 1. 单条 INSERT IGNORE 补齐全部成员的统一会话视图（审计 P1：替代逐成员 GetOrCreateConversation）
	_ = s.store.EnsureGroupConversationViews(convID, gUID, all)
	// 2. 落库系统消息
	msg := &mysql.Message{ID: s.genID(), ConvID: convID, SenderUID: 0, Type: 6, Content: content, Extra: extra, CreatedAt: time.Now()}
	seq, err := s.store.CreateMessage(msg)
	if err != nil {
		log.L().Warn("group system message save failed", "conv", convID, "err", err)
		return
	}
	msg.Seq = seq
	// 3. 单条 SQL 更新全部成员会话的最后消息（审计 P1：替代逐成员 updateConvLastMsg）。
	// 系统消息不累加未读（与此前 seq 差值行为一致：不推进 last_synced_seq 即不计未读）。
	_ = s.store.UpdateGroupConversationsLastMsg(gUID, msg.ID, convPreview(msg.Type, msg.Content), msg.CreatedAt)
	// 4. 直接用行数据构造 DTO，避免 toDTO 回查 GetMessage（审计 P1）；系统消息无发送者昵称
	dto := s.dtoFromRow(convID, msg, "")
	// 5. 推送给所有成员（含群主）
	if s.pushFunc != nil {
		for _, uid := range all {
			s.pushFunc(uid, dto)
		}
	}
}

// SendGroupSystemMessageTo 仅对指定成员可见的群系统消息（如退群消息仅群主可见）：
// 消息落共享历史（extra 携带可见身份，客户端按查看者渲染），但只推送给 recipientUIDs、
// 只更新他们的会话视图最后消息，其他成员不感知。
func (s *Service) SendGroupSystemMessageTo(gUID, convID int64, content, extra string, recipientUIDs []int64) {
	seen := make(map[int64]struct{}, len(recipientUIDs))
	all := make([]int64, 0, len(recipientUIDs))
	for _, u := range recipientUIDs {
		if u <= 0 {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		all = append(all, u)
	}
	if len(all) == 0 {
		return
	}
	_ = s.store.EnsureGroupConversationViews(convID, gUID, all)
	msg := &mysql.Message{ID: s.genID(), ConvID: convID, SenderUID: 0, Type: 6, Content: content, Extra: extra, CreatedAt: time.Now()}
	seq, err := s.store.CreateMessage(msg)
	if err != nil {
		log.L().Warn("targeted group system message save failed", "conv", convID, "err", err)
		return
	}
	msg.Seq = seq
	// 只更新接收者的会话视图最后消息（不能用全成员批量 SQL，避免不感知者预览变化）
	for _, uid := range all {
		_ = s.store.UpdateConversationLastMsg(uid, gUID, msg.ID, convPreview(msg.Type, msg.Content), msg.CreatedAt)
	}
	dto := s.dtoFromRow(convID, msg, "")
	if s.pushFunc != nil {
		for _, uid := range all {
			s.pushFunc(uid, dto)
		}
	}
}

// MessageDTO 对外返回的消息结构（对应网关推送帧 body 与 HTTP 接口返回）。
// 雪花 ID（id/conv_id）以字符串输出，避免前端 JS Number 精度丢失。
type MessageDTO struct {
	ID         int64  `json:"id,string"`
	ConvID     int64  `json:"conv_id,string"`
	Seq        int64  `json:"seq"`
	SenderUID  int64  `json:"sender_uid"`
	SenderName string `json:"sender_name"` // 发送者昵称（群聊展示用）
	Type       int8   `json:"type"`
	Content    string `json:"content"`
	Extra      string `json:"extra"`       // 扩展 JSON（图片/文件 URL、文件名等元数据）
	Status     int8   `json:"status"`      // 0 正常 / 1 已撤回
	CreatedAt  int64  `json:"created_at"`  // Unix 秒
}

// SendReq 发送消息请求。
type SendReq struct {
	ConvID   int64  `json:"conv_id,string"` // 雪花会话 ID，前端传字符串
	TargetID int64  `json:"target_id"`      // 单聊对方 uid 或群 g_uid
	ConvType int8   `json:"conv_type"`      // 1 单聊 / 2 群聊（缺省单聊）
	Type     int8   `json:"type"`
	MsgID    int64  `json:"msg_id,string"` // 客户端生成的消息 ID（幂等），传字符串
	Content  string `json:"content"`
	Extra    string `json:"extra"`         // 扩展 JSON（图片/文件 URL、文件名等元数据）
}

// GetMessage 按会话与消息 ID 查询原始消息（含发送方 uid），供网关 ack 转发等使用。
func (s *Service) GetMessage(convID, msgID int64) (*mysql.Message, error) {
	return s.store.GetMessage(convID, msgID)
}

// Send 发送单聊消息：幂等去重 → 落库 → 更新会话 → 推送。
// 返回 (dto, isNew, err)：isNew=false 表示 msgId 已存在（幂等重发，未落新库、不重复推送）。
func (s *Service) Send(senderUID int64, req *SendReq) (*MessageDTO, bool, error) {
	if req.Type == 0 {
		req.Type = 1 // 默认文本
	}
	if req.Content == "" {
		return nil, false, apperr.BadRequest("消息内容不能为空")
	}
	// 长度约束（审计 H3）：content/extra 超限直接拒绝
	if len([]rune(req.Content)) > maxContentRunes {
		return nil, false, apperr.BadRequest("消息内容过长（上限 4000 字）")
	}
	if err := validateExtra(req.Extra, s.mediaHosts); err != nil {
		return nil, false, err
	}
	// 6.2 频率风控：同一用户单位时间窗口内发送超限则拒绝（limiter 为 nil 时不启用，便于测试）
	if s.limiter != nil {
		if ok, _ := s.limiter.Allow(senderUID); !ok {
			return nil, false, apperr.BadRequest("发送过于频繁，请稍后重试")
		}
	}
	// 6.2 敏感词过滤：命中敏感词则替换为 ***
	if req.Type == 1 { // 仅对文本消息过滤
		filtered, _ := FilterSensitive(req.Content)
		req.Content = filtered
	}

	// 会话类型：单聊 1 / 群聊 2。target 为对方 uid（单聊）或群 g_uid（群聊）。
	convType := req.ConvType
	if convType != 2 {
		convType = 1
	}
	// 发送守卫（审计 P1）：
	//  - 单聊：目标用户必须存在且未被禁用（防对任意 uid 强建会话/骚扰）；
	//  - 群聊：发送者必须是群成员（防向任意群灌消息）。
	var groupMembers []int64
	if convType == 1 {
		tu, err := s.store.GetUserByUID(req.TargetID)
		if err != nil {
			return nil, false, apperr.BadRequest("目标用户不存在")
		}
		if tu.Disabled == 1 {
			return nil, false, apperr.Forbidden("对方账号已被禁用，无法发送消息")
		}
	} else {
		if s.groupMembers == nil {
			return nil, false, apperr.Forbidden("群聊服务不可用")
		}
		ms, err := s.groupMembers(req.TargetID)
		if err != nil {
			return nil, false, apperr.Forbidden("群不存在或不可用")
		}
		for _, m := range ms {
			if m == senderUID {
				groupMembers = ms
				break
			}
		}
		if groupMembers == nil {
			return nil, false, apperr.Forbidden("你不是群成员，无法发送消息")
		}
	}
	conv, err := s.store.GetOrCreateConversation(senderUID, req.TargetID, convType, s.genID())
	if err != nil {
		return nil, false, apperr.WrapInternal("获取会话失败", err)
	}
	// 单聊关键：确保接收方会话视图与发送方共享同一 conv_id，
	// 否则双方历史消息/已读游标各自独立（不同 conv_id），对方拉取历史为空、已读回执无法闭环。
	if convType == 1 && req.TargetID != senderUID {
		if _, err := s.store.EnsureConversationID(req.TargetID, senderUID, convType, conv.ID); err != nil {
			log.L().Warn("ensure peer conversation failed", "err", err, "conv", conv.ID)
		}
	}

	// 幂等去重：同一 msgID（全局雪花）不重复入库
	if req.MsgID > 0 {
		exists, err := s.store.MessageExists(conv.ID, req.MsgID)
		if err != nil {
			return nil, false, apperr.WrapInternal("校验消息失败", err)
		}
		if exists {
			return s.toDTO(conv.ID, req.MsgID), false, nil
		}
	}

	msg := &mysql.Message{
		ID:        req.MsgID,
		ConvID:    conv.ID,
		SenderUID: senderUID,
		Type:      req.Type,
		Content:   req.Content,
		Extra:     req.Extra,
		CreatedAt: time.Now(),
	}
	if msg.ID == 0 {
		msg.ID = s.genID()
	}
	seq, err := s.createMessageWithRetry(msg)
	if err != nil {
		if errors.Is(err, errIdempotentHit) {
			// 并发幂等竞态：消息已被并发请求落库，按重发语义返回（不重复推送）
			return s.toDTO(conv.ID, msg.ID), false, nil
		}
		return nil, false, apperr.WrapInternal("保存消息失败", err)
	}
	msg.Seq = seq

	// 更新会话的最后消息：
	//  - 单聊：同时更新发送方与接收方两侧会话视图。
	//  - 群聊：P1 优化——三条批量 SQL 替代逐成员 3N 次查询/更新（写放大消除）。
	if convType == 1 {
		s.updateConvLastMsg(senderUID, req.TargetID, msg, convType)
		s.updateConvLastMsg(req.TargetID, senderUID, msg, convType)
		// 更新接收方已同步游标（LastSyncedSeq），供增量拉取使用。
		// 注意：只更新接收方，不更新发送方——发送方已读自己的消息，更新会使其误显未读。
		_ = s.store.UpdateConversationSyncedSeq(req.TargetID, senderUID, msg.Seq)
		// 未读计数（P2）：接收方视图 +1；已读时由 UpsertReadSeq 清零
		_ = s.store.BumpConversationUnread(req.TargetID, senderUID, 1)
	} else if len(groupMembers) > 0 {
		// 成员列表已在入口守卫处查得，直接复用，避免二次查询
		all := make([]int64, 0, len(groupMembers)+1)
		all = append(all, senderUID)
		for _, m := range groupMembers {
			if m != senderUID {
				all = append(all, m)
			}
		}
		// 1) 单条 INSERT IGNORE 补齐缺失的成员会话视图
		_ = s.store.EnsureGroupConversationViews(conv.ID, req.TargetID, all)
		// 2) 单条 SQL 更新全部成员视图的最后消息（含发送者）
		_ = s.store.UpdateGroupConversationsLastMsg(req.TargetID, msg.ID, convPreview(msg.Type, msg.Content), msg.CreatedAt)
		// 3) 单条 SQL 更新成员已同步游标（排除发送者，避免其误显未读）
		_ = s.store.UpdateGroupConversationsSyncedSeq(req.TargetID, msg.Seq, senderUID)
		// 4) 单条 SQL 未读计数 +1（排除发送者）
		_ = s.store.BumpGroupConversationsUnread(req.TargetID, senderUID, 1)
	}

	// 消息体已在内存，直接用行数据构造 DTO，避免 toDTO 回查 GetMessage（审计 P1）
	dto := s.dtoFromRow(conv.ID, msg, s.store.GetUserName(senderUID))
	// 推送接收方
	if s.publish != nil {
		s.publish(conv.ID, dto)
	}
	log.L().Info("message sent", "conv_id", conv.ID, "seq", seq, "sender", senderUID)
	return dto, true, nil
}

// createMessageWithRetry 落库并在 seq 冲突（并发取号）时重新取号重试。
// 优先用原子取号源（Redis INCR）预分配 seq；未注入/取号失败时回退本地 MAX+1。
// 唯一键 uk_conv_seq 作为最后防线：冲突不再丢消息，而是重试。
func (s *Service) createMessageWithRetry(msg *mysql.Message) (int64, error) {
	const maxRetries = 8
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// 抖动退避：打散并发节奏，避免同一批请求反复互锁/冲突
			time.Sleep(time.Duration(5+rand.Intn(15*attempt)) * time.Millisecond)
		}
		msg.Seq = 0
		if s.seqGen != nil {
			if seq, gerr := s.seqGen(msg.ConvID); gerr == nil && seq > 0 {
				msg.Seq = seq
			}
		}
		_, err = s.store.CreateMessage(msg)
		if err == nil {
			return msg.Seq, nil
		}
		errMsg := err.Error()
		// msg_id 幂等竞态：并发两个相同 msg_id 请求都通过 MessageExists 检查，
		// 后插入者主键冲突——消息其实已落库，按幂等语义返回已有消息（不报错、不重试）
		if strings.Contains(errMsg, "Duplicate") && strings.Contains(errMsg, "PRIMARY") && msg.ID > 0 {
			if exists, existsErr := s.store.MessageExists(msg.ConvID, msg.ID); existsErr == nil && exists {
				return 0, errIdempotentHit
			}
		}
		// 可重试：seq 唯一键冲突（并发取号）、死锁（INSERT...SELECT 互锁被回滚）
		if !strings.Contains(errMsg, "uk_conv_seq") && !strings.Contains(errMsg, "Deadlock") {
			return 0, err
		}
		log.L().Warn("create message conflict, retry", "conv", msg.ConvID, "attempt", attempt+1, "err", errMsg)
	}
	return 0, err
}

// errIdempotentHit 哨兵错误：msg_id 幂等命中（消息已存在），Send 层据此返回 isNew=false
var errIdempotentHit = errors.New("idempotent hit")

func (s *Service) updateConvLastMsg(ownerUID, targetID int64, msg *mysql.Message, convType int8) {
	if convType == 0 {
		convType = 1
	}
	conv, err := s.store.GetOrCreateConversation(ownerUID, targetID, convType, s.genID())
	if err != nil {
		return
	}
	// 会话列表最后一条消息预览：图片/文件/语音/视频等非文本类型直接展示类型占位，而非资源 URL。
	_ = s.store.UpdateConversationLastMsg(conv.OwnerUID, conv.TargetID, msg.ID, convPreview(msg.Type, msg.Content), msg.CreatedAt)
}

// audioExts 可作为 FILE 类型发送的音频扩展名集合（桌面端录音等以文件形式发出）。
var audioExts = map[string]bool{
	".webm": true, ".m4a": true, ".aac": true, ".mp3": true,
	".wav": true, ".ogg": true, ".flac": true,
}

// isAudioContent 判断 FILE 消息的 content（资源 URL）是否为音频文件：
// 录音产物以 FILE(type=3) 发出，按音频后缀识别后会话摘要才能展示 [语音] 而非 [文件]。
func isAudioContent(content string) bool {
	p := content
	if u, err := url.Parse(content); err == nil && u.Path != "" {
		p = u.Path
	} else {
		p, _, _ = strings.Cut(p, "?")
	}
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	if i := strings.LastIndex(p, "."); i >= 0 {
		return audioExts[strings.ToLower(p[i:])]
	}
	return false
}

// convPreview 根据消息类型生成会话列表最后一条消息的展示文本。
// 消息类型：1 文本 / 2 图片 / 3 文件 / 4 语音 / 5 视频 / 6 系统。
// 非文本类型返回类型占位（如 [图片]），避免会话列表展示原始资源 URL。
func convPreview(msgType int8, content string) string {
	switch msgType {
	case 2:
		return "[图片]"
	case 3:
		if isAudioContent(content) {
			return "[语音]"
		}
		return "[文件]"
	case 4:
		return "[语音]"
	case 5:
		return "[视频]"
	case 6:
		return "[系统消息]"
	default:
		return preview(content)
	}
}

// GetHistory 拉取某用户参与的某会话历史消息。
// beforeSeq<=0：拉取该会话最新 limit 条；beforeSeq>0：向前翻页，拉取 seq<beforeSeq 的 limit 条。
func (s *Service) GetHistory(uid, convID, beforeSeq int64, limit int) ([]*MessageDTO, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	// 归属校验（审计 P0）：仅会话参与者可拉取历史，防越权读取任意会话
	if ok, err := s.store.IsConversationMember(uid, convID); err != nil || !ok {
		return nil, apperr.Forbidden("无权查看该会话的历史消息")
	}
	msgs, err := s.store.ListMessagesBefore(convID, beforeSeq, limit)
	if err != nil {
		return nil, apperr.WrapInternal("拉取历史失败", err)
	}
	return s.msgsToDTOs(convID, msgs), nil
}

// GetHistoryAfterSeq 增量拉取：返回 seq > afterSeq 的消息（升序），供客户端本地缓存补齐新消息。
// 相比 GetHistory 的倒序翻页，增量同步只需本地已有数据的“新增部分”，减少传输量。
func (s *Service) GetHistoryAfterSeq(uid, convID, afterSeq int64, limit int) ([]*MessageDTO, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	// 归属校验（审计 P0）：与 GetHistory 一致，防越权拉取
	if ok, err := s.store.IsConversationMember(uid, convID); err != nil || !ok {
		return nil, apperr.Forbidden("无权查看该会话的历史消息")
	}
	msgs, err := s.store.ListMessages(convID, afterSeq, limit)
	if err != nil {
		return nil, apperr.WrapInternal("增量拉取历史失败", err)
	}
	return s.msgsToDTOs(convID, msgs), nil
}

// ListConversations 列出某用户会话列表（含最后消息）。
func (s *Service) ListConversations(uid int64) ([]*ConversationDTO, error) {
	convs, err := s.store.ListConversations(uid)
	if err != nil {
		return nil, apperr.WrapInternal("获取会话列表失败", err)
	}
	return s.toConversationDTOs(uid, convs), nil
}

// ListConversationsChangedSince 差量刷新：仅返回 since 之后有变化的会话（含空会话）。
// 客户端本地已有全量时传本地最新 last_msg_time，避免每次全量查表（阶段二减压）。
func (s *Service) ListConversationsChangedSince(uid int64, since time.Time) ([]*ConversationDTO, error) {
	convs, err := s.store.ListConversationsChangedSince(uid, since)
	if err != nil {
		return nil, apperr.WrapInternal("获取会话列表失败", err)
	}
	return s.toConversationDTOs(uid, convs), nil
}

// toConversationDTOs 会话行 → DTO（未读数直读 unread_count 列；单聊附带对端已读游标）。
func (s *Service) toConversationDTOs(uid int64, convs []*mysql.Conversation) []*ConversationDTO {
	// 对端已读游标（仅单聊）：用于前端恢复"我发出的消息是否已被对方读"。
	// P1 优化：先收集全部 (对端 uid, conv_id) 对一次批量查，替代逐会话 GetLastReadSeq 的 N+1。
	pairs := make([][2]int64, 0, len(convs))
	for _, c := range convs {
		if c.Type != 1 {
			continue
		}
		peer := c.TargetID
		if c.OwnerUID != uid {
			peer = c.OwnerUID
		}
		pairs = append(pairs, [2]int64{peer, c.ID})
	}
	readSeqs, err := s.store.GetPeerReadSeqs(pairs)
	if err != nil {
		readSeqs = map[int64]int64{} // 批量查失败降级为全 0，不阻塞会话列表
	}
	list := make([]*ConversationDTO, 0, len(convs))
	for _, c := range convs {
		// 未读数直接读 unread_count 列（P2：发消息累加/已读清零/撤回递减，
		// 替代旧 seq 差值计算：消除撤回场景虚高）
		unread := int(c.UnreadCount)
		var lastMsgTime int64
		if c.LastMsgTime != nil {
			lastMsgTime = c.LastMsgTime.Unix()
		}
		list = append(list, &ConversationDTO{
			ID:          c.ID,
			Type:        c.Type,
			TargetID:    c.TargetID,
			LastMsg:     c.LastMsgText,
			LastMsgTime: lastMsgTime,
			Unread:      unread,
			Muted:       c.Muted == 1,
			PeerReadSeq: readSeqs[c.ID], // 群聊/无记录为 0
			// 已同步水位：客户端据此条件同步（本地已追平则跳过历史拉取，减压服务端）
			LastSyncedSeq: c.LastSyncedSeq,
		})
	}
	return list
}

// ConversationDTO 会话列表项。ID 为雪花 ID，以字符串输出避免精度丢失。
type ConversationDTO struct {
	ID          int64  `json:"id,string"`
	Type        int8   `json:"type"`
	TargetID    int64  `json:"target_id"`
	LastMsg     string `json:"last_msg"`
	LastMsgTime int64  `json:"last_msg_time"` // 最后消息 unix 秒时间戳，用于前端排序；无消息为 0
	Unread      int    `json:"unread"`        // 未读消息数
	Muted       bool   `json:"muted"`
	PeerReadSeq int64  `json:"peer_read_seq"`   // 对端已读游标（单聊），前端据此恢复已读状态；群聊为 0
	LastSyncedSeq int64 `json:"last_synced_seq"` // 本会话已同步最大 seq，客户端条件同步水位（本地已追平则免拉历史）
}

// MarkRead 已读回执：记录已读 seq 并返回是否需要广播。
func (s *Service) MarkRead(uid, convID, seq int64) error {
	return s.store.UpsertReadSeq(uid, convID, seq)
}

// GetConversationPeer 返回会话中对端用户 uid，用于已读回执广播给消息发送方。
// 仅支持单聊（conv_type=1）：对端 = 会话中非当前用户的一方；群聊或未找到返回 0。
func (s *Service) GetConversationPeer(convID, uid int64) int64 {
	conv, err := s.store.GetConversationByID(convID)
	if err != nil || conv == nil {
		return 0
	}
	if conv.Type == 2 { // 群聊：不做已读广播
		return 0
	}
	if conv.OwnerUID == uid {
		return conv.TargetID
	}
	return conv.OwnerUID
}

// SearchResultDTO 搜索结果。ConvID 为雪花 ID，以字符串输出避免精度丢失。
type SearchResultDTO struct {
	ConvID  int64       `json:"conv_id,string"`
	Message *MessageDTO `json:"message"`
}

// Search 搜索消息：仅限当前用户自己的会话范围（审计 P1，同时修复越权搜索）。
func (s *Service) Search(uid int64, keyword string, msgType int8, limit int) ([]*SearchResultDTO, error) {
	if keyword == "" {
		return nil, apperr.BadRequest("搜索关键字不能为空")
	}
	// 取用户会话的 conv_id 列表，限定搜索范围（防止跨用户消息泄露）
	convs, err := s.store.ListConversations(uid)
	if err != nil {
		return nil, apperr.WrapInternal("搜索消息失败", err)
	}
	if len(convs) == 0 {
		return []*SearchResultDTO{}, nil
	}
	convIDs := make([]int64, 0, len(convs))
	for _, c := range convs {
		convIDs = append(convIDs, c.ID)
	}
	results, err := s.store.SearchMessages(keyword, msgType, convIDs, limit)
	if err != nil {
		return nil, apperr.WrapInternal("搜索消息失败", err)
	}
	// 发送者昵称一次批量查询后组装（审计 P1：消除逐条 GetUserName 的 N+1）
	uidSet := make(map[int64]struct{})
	for _, r := range results {
		if r.Message.SenderUID > 0 {
			uidSet[r.Message.SenderUID] = struct{}{}
		}
	}
	uids := make([]int64, 0, len(uidSet))
	for u := range uidSet {
		uids = append(uids, u)
	}
	names := s.store.GetUserNames(uids)
	out := make([]*SearchResultDTO, 0, len(results))
	for _, r := range results {
		// 直接用行数据构造 DTO（不再逐条回查）
		out = append(out, &SearchResultDTO{
			ConvID:  r.ConvID,
			Message: s.dtoFromRow(r.ConvID, r.Message, names[r.Message.SenderUID]),
		})
	}
	return out, nil
}

func (s *Service) toDTO(convID, msgID int64) *MessageDTO {
	m, err := s.store.GetMessage(convID, msgID)
	if err != nil {
		return &MessageDTO{ID: msgID, ConvID: convID}
	}
	return s.dtoFromRow(convID, m, s.store.GetUserName(m.SenderUID))
}

// dtoFromRow 直接用行数据构造 DTO（避免逐条 GetMessage 回查，审计 P1）。
func (s *Service) dtoFromRow(convID int64, m *mysql.Message, senderName string) *MessageDTO {
	return &MessageDTO{
		ID:         m.ID,
		ConvID:     convID,
		Seq:        m.Seq,
		SenderUID:  m.SenderUID,
		SenderName: senderName,
		Type:       m.Type,
		Content:    m.Content,
		Extra:      s.refreshMediaURL(m.Type, m.Extra),
		Status:     m.Status,
		CreatedAt:  m.CreatedAt.Unix(),
	}
}

// msgsToDTOs 批量构造 DTO：发送者昵称一次批量查询，消除逐条 GetUserName 的 N+1（审计 P1）。
func (s *Service) msgsToDTOs(convID int64, msgs []*mysql.Message) []*MessageDTO {
	uidSet := make(map[int64]struct{})
	for _, m := range msgs {
		if m.SenderUID > 0 {
			uidSet[m.SenderUID] = struct{}{}
		}
	}
	uids := make([]int64, 0, len(uidSet))
	for uid := range uidSet {
		uids = append(uids, uid)
	}
	names := s.store.GetUserNames(uids)
	list := make([]*MessageDTO, 0, len(msgs))
	for _, m := range msgs {
		list = append(list, s.dtoFromRow(convID, m, names[m.SenderUID]))
	}
	return list
}

// RecallReq 撤回消息请求。
type RecallReq struct {
	MsgID int64 `json:"msg_id,string"` // 待撤回消息 ID（雪花，前端传字符串）
}

// ErrRecallTimeout 撤回超时错误（超过 2 分钟）。
var ErrRecallTimeout = apperr.BadRequest("消息已超过 2 分钟，无法撤回")

// RecallMessage 撤回消息：
//  1. 仅允许发送者撤回自己的消息；
//  2. 超过 2 分钟不可撤回；
//  3. 已撤回的消息不可重复撤回；
//  4. 撤回后若该消息是会话最后一条，回退会话 last_msg 到最后一条未撤回消息；
//  5. 返回撤回后的 DTO（status=1），由调用方决定是否实时推送对端/群成员。
func (s *Service) RecallMessage(uid, convID, msgID int64) (*MessageDTO, error) {
	m, err := s.store.GetMessage(convID, msgID)
	if err != nil {
		return nil, apperr.BadRequest("消息不存在或已删除")
	}
	if m.SenderUID != uid {
		return nil, apperr.Forbidden("只能撤回自己发送的消息")
	}
	if m.Status == 1 {
		return nil, apperr.BadRequest("消息已撤回，请勿重复操作")
	}
	if time.Since(m.CreatedAt) > 2*time.Minute {
		return nil, ErrRecallTimeout
	}
	// 更新为已撤回
	if err := s.store.UpdateMessageStatus(convID, msgID, 1); err != nil {
		return nil, apperr.WrapInternal("撤回消息失败", err)
	}
	// 更新会话最后消息预览：若撤回的是会话最后一条，发送方显示"你撤回了一条消息"、
	// 接收方显示"对方撤回了一条消息"；否则保持最新消息预览不变。
	s.revertConvLastMsg(convID, uid, msgID)
	dto := s.toDTO(convID, msgID)
	// 实时通知接收方：单聊推给对端，群聊推给除发送者外的成员（发送者本端由 UI 即时更新）
	s.notifyRecall(uid, convID, dto)
	return dto, nil
}

// notifyRecall 把撤回通知推送给会话接收方（单聊对端 / 群聊其他成员）。
func (s *Service) notifyRecall(senderUID, convID int64, dto *MessageDTO) {
	if s.pushFunc == nil {
		return
	}
	conv, err := s.store.GetConversationByID(convID)
	if err != nil || conv == nil {
		return
	}
	if conv.Type == 1 {
		// 单聊：对端 = 非发送者一方
		peer := conv.TargetID
		if conv.OwnerUID != senderUID {
			peer = conv.OwnerUID
		}
		if peer != senderUID {
			s.pushFunc(peer, dto)
		}
		return
	}
	// 群聊：推送除发送者外的所有成员
	if s.groupMembers == nil {
		return
	}
	members, err := s.groupMembers(conv.TargetID)
	if err != nil {
		return
	}
	for _, m := range members {
		if m != senderUID {
			s.pushFunc(m, dto)
		}
	}
}

// revertConvLastMsg 撤回后更新会话最后消息预览：
//   - 若被撤回消息是会话当前最后一条（conv.last_msg_id == msgID），
//     则发送方会话视图显示"你撤回了一条消息"、接收方视图显示"对方撤回了一条消息"，
//     且 last_msg_id 仍指向这条被撤回消息（标记最新事件为该撤回操作）。
//   - 若被撤回的不是最后一条，则不更新（会话列表保持显示更晚的那条消息预览）。
//
// 注意：单聊双方各有独立会话记录 (owner_uid=己方, target_id=对方, 共享 conv_id)。
// GetConversationByID 返回的记录可能是任一方视角，故对端必须从 owner/target 中
// 取"非发送方"的那一个，绝不能直接用 conv.TargetID 当作发送方的 target（否则会把
// (owner=发送方, target=发送方) 当成会话 target 而误新建脏会话）。
func (s *Service) revertConvLastMsg(convID, senderUID, msgID int64) {
	conv, err := s.store.GetConversationByID(convID)
	if err != nil || conv == nil {
		return
	}
	// 未读递减（P2）：被撤回消息的接收方视图未读 -1（下限 0）——无论它是否是最后一条，
	// 避免旧 seq 差值方案把已撤回消息仍计入未读的虚高。单聊减对端，群聊批量减除发送者外成员。
	if conv.Type == 1 {
		peer := conv.OwnerUID
		if peer == senderUID {
			peer = conv.TargetID
		}
		_ = s.store.BumpConversationUnread(peer, senderUID, -1)
	} else {
		_ = s.store.BumpGroupConversationsUnread(conv.TargetID, senderUID, -1)
	}
	// 仅当被撤回的是会话当前最后一条消息时才更新预览
	if conv.LastMsgID != msgID {
		return
	}
	now := time.Now()

	if conv.Type == 1 {
		senderView := &mysql.Message{ID: msgID, Type: 1, Content: "你撤回了一条消息", CreatedAt: now}
		peerView := &mysql.Message{ID: msgID, Type: 1, Content: "对方撤回了一条消息", CreatedAt: now}
		// 单聊：owner 与 target 互为对端，发送方是 senderUID
		peer := conv.OwnerUID
		if peer == senderUID {
			peer = conv.TargetID
		}
		// 发送方视图 (owner=senderUID, target=peer)
		s.updateConvLastMsg(senderUID, peer, senderView, 1)
		// 对端视图 (owner=peer, target=senderUID)
		s.updateConvLastMsg(peer, senderUID, peerView, 1)
		return
	}
	// 群聊（审计 P1：两条批量 SQL 替代逐成员 updateConvLastMsg 循环）：
	// 发送方视图显示"你撤回了一条消息"，其余成员视图显示"对方撤回了一条消息"
	_ = s.store.UpdateConversationLastMsg(senderUID, conv.TargetID, msgID, "你撤回了一条消息", now)
	_ = s.store.UpdateGroupConversationsLastMsgExcept(conv.TargetID, senderUID, msgID, "对方撤回了一条消息", now)
}

// refreshMediaURL 对图片/文件消息的 extra 重新生成有效的下载 URL。
// 历史消息里存的预签名 URL 会过期，这里用保存的 object_key 重新签名；extra 非 JSON 或已过期时兜底返回原值。
func (s *Service) refreshMediaURL(msgType int8, extra string) string {
	// 仅对图片(2)/文件(3)刷新
	if msgType != 2 && msgType != 3 {
		return extra
	}
	if s.oss == nil {
		return extra
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(extra), &m); err != nil {
		return extra
	}
	key, _ := m["key"].(string)
	if key == "" {
		return extra
	}
	newURL := s.oss.PublicURL(key)
	if newURL == "" {
		return extra
	}
	m["url"] = newURL
	b, err := json.Marshal(m)
	if err != nil {
		return extra
	}
	return string(b)
}

func preview(content string) string {
	r := []rune(content)
	if len(r) > 50 {
		return string(r[:50]) + "…"
	}
	return content
}

// validateExtra 校验消息扩展字段（审计 H3/H4）：
//   - 长度不超 maxExtraBytes，且必须为合法 JSON 对象（客户端按结构化元数据消费）；
//   - 携带 url 时必须是 https 且域名在 mediaHosts 白名单内（白名单为空时放行，
//     兼容 OSS 未配置部署），拦截伪造的外部资源地址。
func validateExtra(extra string, mediaHosts map[string]bool) error {
	if extra == "" {
		return nil
	}
	if len(extra) > maxExtraBytes {
		return apperr.BadRequest("消息扩展信息过大（上限 2KB）")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(extra), &m); err != nil {
		return apperr.BadRequest("消息扩展信息格式错误")
	}
	rawURL, _ := m["url"].(string)
	if rawURL == "" || len(mediaHosts) == 0 {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || !mediaHosts[strings.ToLower(u.Hostname())] {
		return apperr.BadRequest("媒体资源地址不可信，发送失败")
	}
	return nil
}
